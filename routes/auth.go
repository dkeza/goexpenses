package routes

import (
	stdsql "database/sql"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"goexpenses/database"
	"goexpenses/util"

	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo/v4"
)

var errInvalidCredentials = errors.New("invalid credentials")

// unknownUserPasswordHash is checked when no matching user exists, so a failed
// sign-in takes as long for unknown user names as for wrong passwords.
var unknownUserPasswordHash = sync.OnceValue(func() string {
	hash, err := util.HashPassword("unknown-user-placeholder")
	if err != nil {
		return ""
	}
	return hash
})

func authenticateUser(username, password string) (util.User, error) {
	user := util.User{}
	query := `SELECT id, name, username, email, password FROM users WHERE lower(btrim(username)) = $1 AND email_verified = true AND blocked_at IS NULL`
	if err := database.Db.Get(&user, query, strings.ToLower(strings.TrimSpace(username))); err != nil {
		if errors.Is(err, stdsql.ErrNoRows) {
			util.VerifyPassword(unknownUserPasswordHash(), password)
			return util.User{}, errInvalidCredentials
		}
		return util.User{}, err
	}

	valid, needsRehash := util.VerifyPassword(user.Password, password)
	if !valid {
		return util.User{}, errInvalidCredentials
	}
	if !needsRehash {
		return user, nil
	}

	passwordHash, err := util.HashPassword(password)
	if err != nil {
		return util.User{}, err
	}
	query = `UPDATE users SET password = $1 WHERE id = $2 AND password = $3`
	result, err := database.Db.Exec(query, passwordHash, user.Id, user.Password)
	if err != nil {
		return util.User{}, err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return util.User{}, err
	}
	if rowsAffected != 1 {
		return util.User{}, errInvalidCredentials
	}

	user.Password = passwordHash
	return user, nil
}

// Session rotation queries. Every rotation replaces the token; a rotation
// that signs a user in or out also drops any pending second step and setup
// secret, so nothing of a previous sign-in attempt survives in the session.
const (
	clearSessionPendingColumns = `pending_2fa_user_id = NULL, pending_2fa_at = NULL, pending_2fa_failures = 0, pending_totp_secret = NULL, pending_totp_created_at = NULL`
	rotateSessionQuery         = `UPDATE sessions SET uuid = $1, user_id = $2, created_at = $3, ` + clearSessionPendingColumns + ` WHERE uuid = $4`
	// A session waiting for the second factor is signed out (user_id = 0):
	// the password alone never yields a signed-in session.
	rotatePendingSessionQuery = `UPDATE sessions SET uuid = $1, user_id = 0, created_at = $2, pending_2fa_user_id = $3, pending_2fa_at = NOW(), pending_2fa_failures = 0, pending_totp_secret = NULL, pending_totp_created_at = NULL WHERE uuid = $4`
	lockLoginUserQuery        = `SELECT password, totp_enabled_at FROM users WHERE id = $1 AND blocked_at IS NULL AND email_verified = true FOR UPDATE`
	recordLoginQuery          = `INSERT INTO admin_events (kind, status, user_id) VALUES ('auth_login', 'success', $1)`

	// Sessions waiting for the second factor have user_id = 0, so revoking a
	// user's sessions has to match the pending user as well. The parentheses
	// keep an added "AND uuid <> ..." from applying to the pending part only.
	deleteUserSessionsQuery      = `DELETE FROM sessions WHERE (user_id = $1 OR pending_2fa_user_id = $1)`
	deleteOtherUserSessionsQuery = deleteUserSessionsQuery + ` AND uuid <> $2`
)

// rotateSessionTx gives the session a new token and signs userID in (0 signs
// out). The caller sets the cookie with setSessionCookie after its
// transaction commits.
func rotateSessionTx(execer sqlExecer, currentHash string, userID int) (token string, tokenHash string, err error) {
	if currentHash == "" {
		return "", "", errors.New("current session is missing")
	}
	token, tokenHash, err = util.NewSessionToken()
	if err != nil {
		return "", "", err
	}
	if err := executeExactlyOne(execer, rotateSessionQuery, tokenHash, userID, time.Now(), currentHash); err != nil {
		return "", "", err
	}
	return token, tokenHash, nil
}

// setSessionCookie hands a rotated session token to the browser.
func setSessionCookie(c echo.Context, token, tokenHash string) {
	c.Set("_id", tokenHash)
	if data, ok := c.Get("data").(*util.Data); ok {
		data.CookieId = tokenHash
	}
	c.SetCookie(util.NewSessionCookie(token))
}

func rotateSession(c echo.Context, userID int) error {
	currentHash, _ := c.Get("_id").(string)
	token, tokenHash, err := rotateSessionTx(database.Db, currentHash, userID)
	if errors.Is(err, errRecordNotFound) && userID == 0 {
		// A password reset also revokes a session that was waiting for this
		// user's second factor, which can be the current one.
		if token, tokenHash, err = util.NewSessionToken(); err == nil {
			_, err = database.Db.Exec(`INSERT INTO sessions (uuid) VALUES ($1)`, tokenHash)
		}
	}
	if err != nil {
		return err
	}
	setSessionCookie(c, token, tokenHash)
	return nil
}

// loginStart is the rotated session after a correct password.
type loginStart struct {
	Token     string
	TokenHash string
	// Pending reports that the user still has to pass the second factor.
	Pending bool
}

// beginLogin turns a verified password into a session. authenticateUser
// checks the password without a lock, so the hash is compared again under a
// row lock: a sign-in with a password that was reset in between is refused.
// With two-factor authentication the session only becomes pending.
func beginLogin(sessionHash string, user util.User) (loginStart, error) {
	if sessionHash == "" {
		return loginStart{}, errors.New("current session is missing")
	}
	token, tokenHash, err := util.NewSessionToken()
	if err != nil {
		return loginStart{}, err
	}
	start := loginStart{Token: token, TokenHash: tokenHash}
	err = runTransaction(database.Db, func(tx *sqlx.Tx) error {
		var locked struct {
			Password      string     `db:"password"`
			TOTPEnabledAt *time.Time `db:"totp_enabled_at"`
		}
		if err := tx.Get(&locked, lockLoginUserQuery, user.Id); err != nil {
			if errors.Is(err, stdsql.ErrNoRows) {
				return errInvalidCredentials
			}
			return err
		}
		if locked.Password != user.Password {
			return errInvalidCredentials
		}
		if locked.TOTPEnabledAt != nil {
			start.Pending = true
			return executeExactlyOne(tx, rotatePendingSessionQuery, tokenHash, time.Now(), user.Id, sessionHash)
		}
		if err := executeExactlyOne(tx, rotateSessionQuery, tokenHash, user.Id, time.Now(), sessionHash); err != nil {
			return err
		}
		_, err := tx.Exec(recordLoginQuery, user.Id)
		return err
	})
	if err != nil {
		return loginStart{}, err
	}
	return start, nil
}

func logout(c echo.Context) error {
	sessionHash, ok := c.Get("_id").(string)
	if !ok || sessionHash == "" {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}

	userID, _ := c.Get("id").(int)
	query := `DELETE FROM sessions WHERE uuid = $1`
	err := runTransaction(database.Db, func(tx *sqlx.Tx) error {
		if _, err := tx.Exec(query, sessionHash); err != nil {
			return err
		}
		if userID > 0 {
			_, err := tx.Exec(`INSERT INTO admin_events (kind, status, user_id) VALUES ('auth_logout', 'success', $1)`, userID)
			return err
		}
		return nil
	})
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "could not end session").SetInternal(err)
	}
	c.SetCookie(util.ExpiredSessionCookie())
	return c.Redirect(http.StatusSeeOther, "/")
}

func DefineAuthRoutes() {
	e := E
	auth := Auth

	e.GET("/login", func(c echo.Context) error {
		data := c.Get("data").(*util.Data)
		data.Active = "login"
		if c.QueryParam("next") == "/admin" {
			data.LoginNext = "/admin"
		}
		return c.Render(http.StatusOK, "login", data)
	})

	e.POST("/logout", logout, auth)

	e.GET("/register", func(c echo.Context) error {
		data := c.Get("data").(*util.Data)
		data.Active = "login"

		return c.Render(http.StatusOK, "register", data)
	})

	e.POST("/register", func(c echo.Context) error {
		data := c.Get("data").(*util.Data)
		input, validationMessage := validateRegistration(
			c.FormValue("name"),
			c.FormValue("email"),
			c.FormValue("username"),
			c.FormValue("password"),
		)
		if validationMessage != "" {
			util.Flash(validationMessage, data, 0, ``, 0)
			return c.Redirect(http.StatusSeeOther, "/register")
		}

		passwordHash, err := util.HashPassword(input.Password)
		if err != nil {
			util.Flash(`Invalid password!`, data, 0, ``, 0)
			return c.Redirect(http.StatusSeeOther, "/register")
		}
		token, tokenHash, err := newVerificationToken()
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "could not prepare email verification").SetInternal(err)
		}
		if err := createUnverifiedUserWithAccount(input.Name, input.Email, input.Username, passwordHash, data.Lang, tokenHash, time.Now()); err != nil {
			if message := registrationConflictMessage(err); message != "" {
				util.Flash(message, data, 1, ``, 0)
				return c.Redirect(http.StatusSeeOther, "/login")
			}
			return databaseWriteError(c, "register user", err)
		}
		sendVerificationEmail(input.Email, token, data.Lang)
		util.Flash(registrationResponseMessage, data, 1, ``, 0)
		return c.Redirect(http.StatusSeeOther, "/login")
	}, rateLimitMiddleware(publicRequestLimiter, registrationRateLimitRules))

	e.GET("/verify-email", func(c echo.Context) error {
		c.Response().Header().Set("Cache-Control", "no-store")
		c.Response().Header().Set("Referrer-Policy", "no-referrer")
		data := c.Get("data").(*util.Data)
		token := c.QueryParam("t")
		valid, err := verificationTokenValid(token, time.Now())
		if err != nil {
			return databaseReadError(c, "check verification token", err)
		}
		if !valid {
			util.Flash("Invalid or expired confirmation link. Request a new one.", data, 0, "", 0)
			return c.Redirect(http.StatusSeeOther, "/resend-verification")
		}
		data.Token = token
		return c.Render(http.StatusOK, "verify-email", data)
	})

	e.POST("/verify-email", func(c echo.Context) error {
		data := c.Get("data").(*util.Data)
		confirmed, err := confirmEmail(c.FormValue("t"), time.Now())
		if err != nil {
			return databaseWriteError(c, "confirm email", err)
		}
		if !confirmed {
			util.Flash("Invalid or expired confirmation link. Request a new one.", data, 0, "", 0)
			return c.Redirect(http.StatusSeeOther, "/resend-verification")
		}
		util.Flash("E-Mail confirmed. You can now sign in.", data, 1, "", 0)
		return c.Redirect(http.StatusSeeOther, "/login")
	})

	e.GET("/resend-verification", func(c echo.Context) error {
		return c.Render(http.StatusOK, "resend-verification", c.Get("data").(*util.Data))
	})

	e.POST("/resend-verification", func(c echo.Context) error {
		data := c.Get("data").(*util.Data)
		recipient, token, created, err := requestVerification(c.FormValue("email"), time.Now())
		if err != nil {
			return databaseWriteError(c, "request verification", err)
		}
		if created {
			sendVerificationEmail(recipient, token, data.Lang)
		}
		util.Flash(verificationResponseMessage, data, 1, "", 0)
		return c.Redirect(http.StatusSeeOther, "/login")
	}, rateLimitMiddleware(publicRequestLimiter, verificationRateLimitRules))

	e.POST("/auth", func(c echo.Context) error {
		data := c.Get("data").(*util.Data)

		username := c.FormValue("username")
		password := c.FormValue("password")
		toAdmin := c.FormValue("next") == "/admin"

		user, err := authenticateUser(username, password)
		start := loginStart{}
		if err == nil {
			sessionHash, _ := c.Get("_id").(string)
			start, err = beginLogin(sessionHash, user)
		}
		if errors.Is(err, errInvalidCredentials) {
			util.Flash(`Unknown user or invalid password!`, data, 0, "", 0)
			if toAdmin {
				return c.Redirect(http.StatusSeeOther, "/login?next=/admin")
			}
			return c.Redirect(http.StatusSeeOther, "/login")
		}
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "could not sign in").SetInternal(err)
		}

		publicRequestLimiter.reset(loginAccountKey(c, username))
		setSessionCookie(c, start.Token, start.TokenHash)
		if start.Pending {
			return c.Redirect(http.StatusSeeOther, twoFactorLoginURL(toAdmin, false))
		}
		c.Set("id", user.Id)
		c.Set("name", user.Name)
		c.Set("username", user.Username)
		c.Set("email", user.Email)

		if toAdmin {
			return c.Redirect(http.StatusSeeOther, "/admin")
		}
		return c.Redirect(http.StatusSeeOther, "/posts")
	}, rateLimitMiddleware(publicRequestLimiter, loginRateLimitRules))
}
