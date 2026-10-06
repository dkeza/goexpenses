package routes

import (
	stdsql "database/sql"
	"errors"
	"html"
	"net/http"
	"net/url"
	"strings"
	"time"

	"goexpenses/database"
	"goexpenses/util"

	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo/v4"
)

const (
	passwordResetDuration        = 2 * time.Hour
	passwordResetRequestInterval = 15 * time.Minute
	passwordResetResponseMessage = "If the E-Mail exists, a reset link has been sent."
)

func createPasswordReset(email string, now time.Time) (recipient string, token string, created bool, err error) {
	tx, err := database.Db.Beginx()
	if err != nil {
		return "", "", false, err
	}
	defer tx.Rollback()

	user := util.User{}
	query := `SELECT id, email FROM users WHERE lower(btrim(email)) = $1 AND email_verified = true AND blocked_at IS NULL FOR UPDATE`
	if err = tx.Get(&user, query, strings.ToLower(strings.TrimSpace(email))); err != nil {
		if errors.Is(err, stdsql.ErrNoRows) {
			return "", "", false, nil
		}
		return "", "", false, err
	}

	requestCount := 0
	query = `SELECT COUNT(*) FROM passwordresets WHERE email = $1 AND created_at >= $2`
	if err = tx.Get(&requestCount, query, user.Email, now.Add(-passwordResetRequestInterval)); err != nil {
		return "", "", false, err
	}
	if requestCount > 0 {
		return "", "", false, nil
	}

	token, tokenHash, err := util.NewPasswordResetToken()
	if err != nil {
		return "", "", false, err
	}

	query = `UPDATE passwordresets SET done = 1 WHERE email = $1 AND done = 0`
	if _, err = tx.Exec(query, user.Email); err != nil {
		return "", "", false, err
	}
	query = `INSERT INTO passwordresets (email, token) VALUES ($1, $2)`
	if _, err = tx.Exec(query, user.Email, tokenHash); err != nil {
		return "", "", false, err
	}
	if err = tx.Commit(); err != nil {
		return "", "", false, err
	}

	return user.Email, token, true, nil
}

func passwordResetTokenValid(token string, now time.Time) (bool, error) {
	tokenHash, err := util.HashPasswordResetToken(token)
	if err != nil {
		return false, nil
	}

	reset := util.PasswordReset{}
	query := `SELECT id FROM passwordresets WHERE token = $1 AND created_at >= $2 AND done = 0`
	if err := database.Db.Get(&reset, query, tokenHash, now.Add(-passwordResetDuration)); err != nil {
		if errors.Is(err, stdsql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return reset.Id != 0, nil
}

func resetPasswordWithToken(token, passwordHash string, now time.Time) (bool, error) {
	tokenHash, err := util.HashPasswordResetToken(token)
	if err != nil {
		return false, nil
	}

	tx, err := database.Db.Beginx()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	reset := util.PasswordReset{}
	query := `SELECT id, email FROM passwordresets WHERE token = $1 AND created_at >= $2 AND done = 0 FOR UPDATE`
	if err = tx.Get(&reset, query, tokenHash, now.Add(-passwordResetDuration)); err != nil {
		if errors.Is(err, stdsql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}

	user := util.User{}
	query = `SELECT id FROM users WHERE email = $1 AND blocked_at IS NULL`
	if err = tx.Get(&user, query, reset.Email); err != nil {
		if errors.Is(err, stdsql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}

	query = `UPDATE passwordresets SET done = 1 WHERE token = $1 AND done = 0`
	result, err := tx.Exec(query, tokenHash)
	if err != nil {
		return false, err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if rowsAffected != 1 {
		return false, nil
	}

	query = `UPDATE passwordresets SET done = 1 WHERE email = $1 AND done = 0`
	if _, err = tx.Exec(query, reset.Email); err != nil {
		return false, err
	}
	query = `UPDATE users SET password = $1 WHERE id = $2`
	result, err = tx.Exec(query, passwordHash, user.Id)
	if err != nil {
		return false, err
	}
	rowsAffected, err = result.RowsAffected()
	if err != nil {
		return false, err
	}
	if rowsAffected != 1 {
		return false, errors.New("password reset user no longer exists")
	}

	query = `DELETE FROM sessions WHERE user_id = $1`
	if _, err = tx.Exec(query, user.Id); err != nil {
		return false, err
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}

	return true, nil
}

// changeOwnPassword replaces a signed-in user's password after verifying the
// current one and revokes every other session of that user.
func changeOwnPassword(userID int, currentPassword, newPasswordHash, currentSessionHash string) error {
	storedHash := ""
	if err := database.Db.Get(&storedHash, `SELECT password FROM users WHERE id = $1 AND blocked_at IS NULL`, userID); err != nil {
		if errors.Is(err, stdsql.ErrNoRows) {
			return errInvalidCredentials
		}
		return err
	}
	if valid, _ := util.VerifyPassword(storedHash, currentPassword); !valid {
		return errInvalidCredentials
	}

	return runTransaction(database.Db, func(tx *sqlx.Tx) error {
		// Matching the old hash keeps a concurrent password change from being
		// overwritten after the current password was verified.
		err := executeExactlyOne(tx, `UPDATE users SET password = $1 WHERE id = $2 AND password = $3`, newPasswordHash, userID, storedHash)
		if errors.Is(err, errRecordNotFound) {
			return errInvalidCredentials
		}
		if err != nil {
			return err
		}
		_, err = tx.Exec(`DELETE FROM sessions WHERE user_id = $1 AND uuid <> $2`, userID, currentSessionHash)
		return err
	})
}

func sendPasswordResetEmail(recipient, token, lang string) {
	resetURL := strings.TrimRight(util.Settings.Host, "/") + "/resetpassword?t=" + url.QueryEscape(token)

	queueTrackedEmail("password_reset", emailMessage{
		Recipient: recipient,
		Subject:   "Goexpenses " + util.GetLangText("reset password", lang),
		HTMLBody:  util.GetLangText(`Click to this link to reset password:`, lang) + ` <a href="` + html.EscapeString(resetURL) + `">Reset</a>`,
	})
}

func changePassword(c echo.Context) error {
	data := c.Get("data").(*util.Data)
	password := c.FormValue("password")
	repeatPassword := c.FormValue("repeatpassword")
	token := c.FormValue("_token")

	if password == "" || repeatPassword == "" || password != repeatPassword {
		util.Flash(`Invalid password!`, data, 0, ``, 0)
		return c.Redirect(http.StatusSeeOther, "/changepassword")
	}
	if !validNewPassword(password) {
		util.Flash(`Password must contain between 10 and 72 characters.`, data, 0, ``, 0)
		return c.Redirect(http.StatusSeeOther, "/changepassword")
	}

	passwordHash, err := util.HashPassword(password)
	if err != nil {
		util.Flash(`Invalid password!`, data, 0, ``, 0)
		return c.Redirect(http.StatusSeeOther, "/changepassword")
	}

	if token != "" {
		if data.User.Id != 0 {
			util.Flash(`Invalid token!`, data, 0, ``, 0)
			return c.Redirect(http.StatusSeeOther, "/changepassword")
		}
		changed, err := resetPasswordWithToken(token, passwordHash, time.Now())
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "could not reset password").SetInternal(err)
		}
		if !changed {
			util.Flash(`Invalid token!`, data, 0, ``, 0)
			return c.Redirect(http.StatusSeeOther, "/reset")
		}
		if err := rotateSession(c, 0); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "could not rotate session").SetInternal(err)
		}
		util.Flash(`Saved`, data, 1, ``, 0)
		return c.Redirect(http.StatusSeeOther, "/login")
	}

	if data.User.Id == 0 {
		util.Flash(`Invalid password!`, data, 0, ``, 0)
		return c.Redirect(http.StatusSeeOther, "/changepassword")
	}

	sessionHash, _ := c.Get("_id").(string)
	if err := changeOwnPassword(data.User.Id, c.FormValue("currentpassword"), passwordHash, sessionHash); err != nil {
		if errors.Is(err, errInvalidCredentials) {
			util.Flash(`Current password is incorrect!`, data, 0, ``, 0)
			return c.Redirect(http.StatusSeeOther, "/changepassword")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "could not change password").SetInternal(err)
	}
	if err := rotateSession(c, data.User.Id); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "could not rotate session").SetInternal(err)
	}
	util.Flash(`Saved`, data, 1, ``, 0)
	return c.Redirect(http.StatusSeeOther, "/posts")
}

func DefinePasswordRoutes() {
	e := E

	e.GET("/changepassword", func(c echo.Context) error {
		data := c.Get("data").(*util.Data)
		data.Active = "login"

		return c.Render(http.StatusOK, "changepassword", data)
	})

	e.POST("/changepassword", changePassword)

	e.GET("/reset", func(c echo.Context) error {
		data := c.Get("data").(*util.Data)
		data.Active = "login"

		return c.Render(http.StatusOK, "reset", data)
	})

	e.POST("/reset", func(c echo.Context) error {
		data := c.Get("data").(*util.Data)
		recipient, token, created, err := createPasswordReset(c.FormValue("email"), time.Now())
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "could not request password reset").SetInternal(err)
		}
		if created {
			sendPasswordResetEmail(recipient, token, data.Lang)
		}
		util.Flash(passwordResetResponseMessage, data, 1, "", 0)

		return c.Redirect(http.StatusSeeOther, "/login")
	}, rateLimitMiddleware(publicRequestLimiter, passwordResetRateLimitRules))

	e.GET("/resetpassword", func(c echo.Context) error {
		// The URL carries the reset token: keep it out of caches, Referer
		// headers and third-party scripts.
		c.Response().Header().Set("Cache-Control", "no-store")
		c.Response().Header().Set("Referrer-Policy", "no-referrer")
		data := c.Get("data").(*util.Data)
		data.Active = "login"
		data.HideThirdPartyScripts = true
		token := c.FormValue("t")
		if token == "" {
			return c.Redirect(http.StatusSeeOther, "/")
		}

		valid, err := passwordResetTokenValid(token, time.Now())
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "could not verify password reset token").SetInternal(err)
		}
		if !valid {
			util.Flash(`Invalid token!`, data, 0, "", 0)
			return c.Redirect(http.StatusSeeOther, "/reset")
		}

		data.Token = token

		return c.Render(http.StatusOK, "changepassword", data)
	})
}
