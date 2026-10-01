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

func rotateSession(c echo.Context, userID int, recordLogin ...bool) error {
	currentHash, ok := c.Get("_id").(string)
	if !ok || currentHash == "" {
		return errors.New("current session is missing")
	}

	token, tokenHash, err := util.NewSessionToken()
	if err != nil {
		return err
	}
	query := `UPDATE sessions SET uuid = $1, user_id = $2, created_at = $3 WHERE uuid = $4`
	updateSession := func(execer sqlExecer) error {
		result, err := execer.Exec(query, tokenHash, userID, time.Now(), currentHash)
		if err != nil {
			return err
		}
		rowsAffected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if rowsAffected != 1 {
			return errors.New("current session no longer exists")
		}
		return nil
	}
	if len(recordLogin) > 0 && recordLogin[0] {
		err = runTransaction(database.Db, func(tx *sqlx.Tx) error {
			if err := updateSession(tx); err != nil {
				return err
			}
			_, err := tx.Exec(`INSERT INTO admin_events (kind, status, user_id) VALUES ('auth_login', 'success', $1)`, userID)
			return err
		})
	} else {
		err = updateSession(database.Db)
	}
	if err != nil {
		return err
	}

	c.Set("_id", tokenHash)
	if data, ok := c.Get("data").(*util.Data); ok {
		data.CookieId = tokenHash
	}
	c.SetCookie(util.NewSessionCookie(token))
	return nil
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

		user, err := authenticateUser(username, password)
		if err == nil {
			publicRequestLimiter.reset(rateLimitIdentifierKey("login", "username", username))
			if err := rotateSession(c, user.Id, true); err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "could not rotate session").SetInternal(err)
			}
			c.Set("id", user.Id)
			c.Set("name", user.Name)
			c.Set("username", user.Username)
			c.Set("email", user.Email)

		} else if errors.Is(err, errInvalidCredentials) {
			util.Flash(`Unknown user or invalid password!`, data, 0, "", 0)
			if c.FormValue("next") == "/admin" {
				return c.Redirect(http.StatusSeeOther, "/login?next=/admin")
			}
			return c.Redirect(http.StatusSeeOther, "/login")
		} else {
			return echo.NewHTTPError(http.StatusInternalServerError, "could not authenticate user").SetInternal(err)
		}

		if c.FormValue("next") == "/admin" {
			return c.Redirect(http.StatusSeeOther, "/admin")
		}
		return c.Redirect(http.StatusSeeOther, "/posts")
	}, rateLimitMiddleware(publicRequestLimiter, loginRateLimitRules))
}
