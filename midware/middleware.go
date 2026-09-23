package midware

import (
	stdsql "database/sql"
	"errors"
	"fmt"
	"net/http"
	"time"

	"goexpenses/database"
	"goexpenses/routes"
	"goexpenses/util"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

func SetMiddleware() {

	e := routes.E

	e.Use(SecurityHeaders)
	e.Use(middleware.CSRFWithConfig(middleware.CSRFConfig{
		Skipper: func(c echo.Context) bool {
			return c.Request().URL.Path == routes.HealthPath
		},
		TokenLookup:    "form:_CSRF",
		CookiePath:     "/",
		CookieMaxAge:   int(util.SessionDuration.Seconds()),
		CookieSecure:   util.Settings.CookieSecure,
		CookieHTTPOnly: true,
		CookieSameSite: http.SameSiteLaxMode,
	}))
	//e.Use(middleware.Logger())
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

func CheckCookie(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if c.Request().URL.Path == routes.HealthPath {
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
			if session.Message != "" {
				data.Flash = session.Message
				sql := fmt.Sprintf(`UPDATE sessions SET message = %v WHERE uuid = %v`, util.SqlParam(1), util.SqlParam(2))
				if _, err := database.Db.Exec(sql, "", sessionHash); err != nil {
					return echo.NewHTTPError(http.StatusInternalServerError, "could not update session").SetInternal(err)
				}
			}
			if session.Expenses_id != 0 {

				expenses := []util.Expense{}
				sql := fmt.Sprintf(`
				SELECT id, p_id, description 
					FROM expenses 
					WHERE id = %v 
					ORDER BY description ASC
				`, util.SqlParam(1))
				if err := database.Db.Select(&expenses, sql, session.Expenses_id); err != nil {
					return databaseReadError(c, "load session expense", "could not load session data", err)
				}
				if len(expenses) > 0 && expenses[0].Pid != "" {
					data.Expenses_id = expenses[0].Pid
				}
				sql = fmt.Sprintf(`UPDATE sessions SET expenses_id = %v WHERE uuid = %v`, util.SqlParam(1), util.SqlParam(2))
				if _, err := database.Db.Exec(sql, 0, sessionHash); err != nil {
					return echo.NewHTTPError(http.StatusInternalServerError, "could not update session").SetInternal(err)
				}
			}
			if session.Last_post_description != "" {
				data.Last_post_description = session.Last_post_description
				sql := fmt.Sprintf(`UPDATE sessions SET last_post_description = %v WHERE uuid = %v`, util.SqlParam(1), util.SqlParam(2))
				if _, err := database.Db.Exec(sql, "", sessionHash); err != nil {
					return echo.NewHTTPError(http.StatusInternalServerError, "could not update session").SetInternal(err)
				}
			}
			if session.Message_success != 0 {
				data.Message_success = session.Message_success
				sql := fmt.Sprintf(`UPDATE sessions SET message_success = %v WHERE uuid = %v`, util.SqlParam(1), util.SqlParam(2))
				if _, err := database.Db.Exec(sql, 0, sessionHash); err != nil {
					return echo.NewHTTPError(http.StatusInternalServerError, "could not update session").SetInternal(err)
				}
			}
		}

		c.Set("_id", sessionHash)
		data.CookieId = sessionHash
		if session.User_id > 0 {
			user := util.User{}
			sql := fmt.Sprintf(`SELECT id, name, username, email, default_accounts_id, lang, is_admin FROM users WHERE id = %v AND email_verified = true AND blocked_at IS NULL`, util.SqlParam(1))
			if err := database.Db.Get(&user, sql, session.User_id); err != nil {
				if !errors.Is(err, stdsql.ErrNoRows) {
					return databaseReadError(c, "load session user", "could not load user", err)
				}
				sql = fmt.Sprintf(`UPDATE sessions SET user_id = %v WHERE uuid = %v`, util.SqlParam(1), util.SqlParam(2))
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
				sql = fmt.Sprintf(`SELECT a.id, a.description FROM accountsusers au INNER JOIN accounts a ON au.accounts_id = a.id WHERE au.users_id = %v ORDER BY description ASC`, util.SqlParam(1))
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

		currency := util.Currency{}
		sql := fmt.Sprintf(`SELECT id, code, rate, date FROM currencies WHERE code = %v`, util.SqlParam(1))
		if err := database.Db.Get(&currency, sql, `EUR`); err != nil && !errors.Is(err, stdsql.ErrNoRows) {
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
			query := fmt.Sprintf(`SELECT id, uuid, user_id, lang, message, expenses_id, last_post_description, message_success FROM sessions WHERE uuid = %v AND created_at >= %v`, util.SqlParam(1), util.SqlParam(2))
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
	query := fmt.Sprintf(`INSERT INTO sessions (uuid) VALUES (%v)`, util.SqlParam(1))
	if _, err := database.Db.Exec(query, tokenHash); err != nil {
		return util.Session{}, "", err
	}
	c.SetCookie(util.NewSessionCookie(token))

	return util.Session{}, tokenHash, nil
}
