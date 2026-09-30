package routes

import (
	"context"
	stdsql "database/sql"
	"errors"
	"net/http"
	"time"

	"goexpenses/database"
	"goexpenses/util"

	"github.com/labstack/echo/v4"
)

var E *echo.Echo
var Auth echo.MiddlewareFunc

const (
	HealthPath         = "/healthz"
	healthCheckTimeout = 2 * time.Second
)

func init() {
	E = echo.New()
	E.IPExtractor = clientIPExtractor()
}

func MainRoute() {
	E.GET("/", func(c echo.Context) error {
		var data *util.Data
		data = c.Get("data").(*util.Data)
		data.Active = "home"
		l := c.QueryParam("lang")
		if l != "" {

			if data.Lang != l {
				data.Lang = l
				sql := `UPDATE sessions SET lang = $1 WHERE uuid = $2`
				if _, err := database.Db.Exec(sql, l, data.CookieId); err != nil {
					return databaseWriteError(c, "update session language", err)
				}

				if data.Username != "" {
					sql := `UPDATE users SET lang = $1 WHERE id = $2`
					if _, err := database.Db.Exec(sql, l, data.User.Id); err != nil {
						return databaseWriteError(c, "update user language", err)
					}
					data.User.Lang = l
				}
			}
		}
		return c.Render(http.StatusOK, "index", data)
	})
}

func healthCheck(c echo.Context) error {
	if database.Db == nil {
		c.Logger().Error("health check failed: database is not initialized")
		return c.String(http.StatusServiceUnavailable, "service unavailable\n")
	}

	ctx, cancel := context.WithTimeout(c.Request().Context(), healthCheckTimeout)
	defer cancel()
	if err := database.Db.PingContext(ctx); err != nil {
		c.Logger().Errorf("health check database ping: %v", err)
		return c.String(http.StatusServiceUnavailable, "service unavailable\n")
	}

	return c.String(http.StatusOK, "ok\n")
}

func DefineRoutes() {

	// Middleware

	Auth = func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {

			uuid := c.Get("_id").(string)

			session := util.Session{}
			sql := `SELECT id, uuid, user_id FROM sessions WHERE uuid = $1`
			if err := database.Db.Get(&session, sql, uuid); err != nil {
				if errors.Is(err, stdsql.ErrNoRows) {
					return c.Redirect(http.StatusSeeOther, "/login")
				}
				return databaseReadError(c, "authorize session", err)
			}

			if session.User_id == 0 {
				return c.Redirect(http.StatusSeeOther, "/login")
			}

			return next(c)
		}
	}

	E.GET(HealthPath, healthCheck)

	MainRoute()
	DefineAuthRoutes()
	DefinePasswordRoutes()
	DefineAccountRoutes()
	DefinePosts()
	DefineExpenses()
	DefineIncomes()
	DefineAdminRoutes()
}
