package midware

import (
	stdsql "database/sql"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"goexpenses/database"
	"goexpenses/routes"
	"goexpenses/util"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

// RequestBodyLimit caps request bodies; every form in the app is far smaller.
const RequestBodyLimit = "64K"

func SetMiddleware() {

	e := routes.E

	e.Use(SecurityHeaders)
	e.Use(RequestLogger(slog.Default()))
	e.Use(middleware.BodyLimit(RequestBodyLimit))
	e.Use(middleware.CSRFWithConfig(middleware.CSRFConfig{
		Skipper: func(c echo.Context) bool {
			return skipsSession(c.Request().URL.Path)
		},
		TokenLookup:    "form:_CSRF",
		CookiePath:     "/",
		CookieMaxAge:   int(util.SessionDuration.Seconds()),
		CookieSecure:   util.Settings.CookieSecure,
		CookieHTTPOnly: true,
		CookieSameSite: http.SameSiteLaxMode,
	}))
	e.Use(middleware.Recover())
	e.Use(ServerHeader)
	e.Use(CheckCookie)
}

// ServerHeader middleware adds a `Server` header to the response.
func ServerHeader(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		c.Response().Header().Set(echo.HeaderServer, "Keza Server 1.0")
		return next(c)
	}
}

func databaseReadError(c echo.Context, operation, message string, err error) error {
	c.Logger().Errorf("%s: %v", operation, err)
	return echo.NewHTTPError(http.StatusInternalServerError, message).SetInternal(err)
}

// skipsSession reports whether a request path needs neither a session nor a
// CSRF token. Static assets and health checks must not touch the database.
func skipsSession(path string) bool {
	switch path {
	case routes.HealthPath, "/favicon.ico", "/ads.txt", "/sw.js", "/manifest.webmanifest":
		return true
	}
	return strings.HasPrefix(path, "/static/")
}

func CheckCookie(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if skipsSession(c.Request().URL.Path) {
			return next(c)
		}

		data := new(util.Data)
		data.CSPNonce, _ = c.Get(cspNonceContextKey).(string)

		session, sessionHash, err := getOrCreateSession(c)
		if err != nil {
			return databaseReadError(c, "load session", "could not load session", err)
		}

		if session.Id == 0 {
			data.Lang = "EN"
		} else {
			// Flash values are shown once; clear them all with a single update.
			hasFlash := session.Message != "" || session.Expenses_id != 0 ||
				session.Last_post_description != "" || session.Message_success != 0
			data.Flash = session.Message
			data.Last_post_description = session.Last_post_description
			data.Message_success = session.Message_success
			if session.Expenses_id != 0 {
				expense := util.Expense{}
				sql := `SELECT p_id FROM expenses WHERE id = $1`
				if err := database.Db.Get(&expense, sql, session.Expenses_id); err != nil && !errors.Is(err, stdsql.ErrNoRows) {
					return databaseReadError(c, "load session expense", "could not load session data", err)
				}
				data.Expenses_id = expense.Pid
			}
			if hasFlash {
				sql := `UPDATE sessions SET message = '', expenses_id = 0, last_post_description = '', message_success = 0 WHERE uuid = $1`
				if _, err := database.Db.Exec(sql, sessionHash); err != nil {
					return echo.NewHTTPError(http.StatusInternalServerError, "could not update session").SetInternal(err)
				}
			}
		}

		c.Set("_id", sessionHash)
		data.CookieId = sessionHash
		if session.User_id > 0 {
			user := util.User{}
			sql := `SELECT id, name, username, email, default_accounts_id, lang, is_admin FROM users WHERE id = $1 AND email_verified = true AND blocked_at IS NULL`
			if err := database.Db.Get(&user, sql, session.User_id); err != nil {
				if !errors.Is(err, stdsql.ErrNoRows) {
					return databaseReadError(c, "load session user", "could not load user", err)
				}
				sql = `UPDATE sessions SET user_id = $1 WHERE uuid = $2`
				if _, err := database.Db.Exec(sql, 0, sessionHash); err != nil {
					return echo.NewHTTPError(http.StatusInternalServerError, "could not clear stale session").SetInternal(err)
				}
				session.User_id = 0
			} else {
				c.Set("id", user.Id)
				c.Set("name", user.Name)
				c.Set("username", user.Username)
				c.Set("email", user.Email)

				data.Username = user.Name
				data.User.Id = user.Id
				data.User.Name = user.Name
				data.User.Email = user.Email
				data.User.Username = user.Username
				data.User.Default_accounts_id = user.Default_accounts_id
				data.User.Lang = user.Lang
				data.User.IsAdmin = user.IsAdmin
				data.Lang = user.Lang
				accounts := []util.Account{}
				sql = `SELECT a.id, a.description FROM accountsusers au INNER JOIN accounts a ON au.accounts_id = a.id WHERE au.users_id = $1 ORDER BY description ASC`
				if err := database.Db.Select(&accounts, sql, data.User.Id); err != nil {
					return databaseReadError(c, "load user accounts", "could not load accounts", err)
				}
				data.Accounts = accounts
			}
		}
		if session.User_id == 0 {

			c.Set("id", 0)
			c.Set("name", "")
			c.Set("username", "")
			c.Set("email", "")
			if session.Lang != "" {
				data.Lang = session.Lang
			}
		}

		data.Csrf = c.Get("csrf").(string)

		currency, err := util.CurrentEURRate(c.Request().Context())
		if err != nil {
			return databaseReadError(c, "load exchange rate", "could not load exchange rate", err)
		}
		data.Eur = util.ToFixed(currency.Rate, 4)
		data.Eurdate = currency.Date

		if data.Eur == 0.00 {
			data.Eurdate = time.Now().Format("2006-01-02 15:04:05")
			util.RefreshExchangeRatesAsync()
		}
		c.Set("data", data)

		return next(c)
	}
}

func getOrCreateSession(c echo.Context) (util.Session, string, error) {
	session := util.Session{}
	sessionHash := ""

	if cookie, err := c.Cookie(util.SessionCookieName); err == nil {
		if tokenHash, err := util.HashSessionToken(cookie.Value); err == nil {
			sessionHash = tokenHash
			query := `SELECT id, uuid, user_id, lang, message, expenses_id, last_post_description, message_success FROM sessions WHERE uuid = $1 AND created_at >= $2`
			err = database.Db.Get(&session, query, sessionHash, time.Now().Add(-util.SessionDuration))
			if err != nil && !errors.Is(err, stdsql.ErrNoRows) {
				return util.Session{}, "", err
			}
		}
	}

	if session.Id != 0 {
		return session, sessionHash, nil
	}

	token, tokenHash, err := util.NewSessionToken()
	if err != nil {
		return util.Session{}, "", err
	}
	query := `INSERT INTO sessions (uuid) VALUES ($1)`
	if _, err := database.Db.Exec(query, tokenHash); err != nil {
		return util.Session{}, "", err
	}
	c.SetCookie(util.NewSessionCookie(token))

	return util.Session{}, tokenHash, nil
}
