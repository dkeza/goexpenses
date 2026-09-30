package routes

import (
	"net/http"
	"strconv"
	"strings"

	"goexpenses/database"
	"goexpenses/util"

	"github.com/labstack/echo/v4"
)

func selectAccount(c echo.Context) error {
	data, ok := c.Get("data").(*util.Data)
	if !ok || data.User.Id == 0 {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}

	accountID, err := strconv.Atoi(c.FormValue("accounts_id"))
	if err != nil || accountID <= 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid account")
	}

	query := `
		UPDATE users
		SET default_accounts_id = $1
		WHERE id = $2
		  AND EXISTS (
			SELECT 1
			FROM accountsusers au
			JOIN accounts a ON a.id = au.accounts_id
			WHERE au.accounts_id = $3
			  AND au.users_id = $4
			  AND a.deleted = 0
		)`

	result, err := database.Db.Exec(query, accountID, data.User.Id, accountID, data.User.Id)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "could not select account").SetInternal(err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "could not verify account selection").SetInternal(err)
	}
	if rowsAffected != 1 {
		return echo.NewHTTPError(http.StatusForbidden, "account is not available to this user")
	}

	return c.Redirect(http.StatusSeeOther, "/posts")
}

func DefineAccountRoutes() {
	e := E
	auth := Auth

	e.POST("/accounts/select", selectAccount, auth)

	e.POST("/accounts/show", func(c echo.Context) error {
		data := c.Get("data").(*util.Data)
		data.Active = ""
		return c.Render(http.StatusOK, "accountsshow", data)
	}, auth)

	e.GET("/accounts/show", func(c echo.Context) error {
		data := c.Get("data").(*util.Data)
		data.Active = ""
		return c.Render(http.StatusOK, "accountsshow", data)
	}, auth)

	e.POST("/accounts/save", func(c echo.Context) error {
		data := c.Get("data").(*util.Data)

		description := c.FormValue("description")

		if strings.TrimSpace(description) == "" {
			util.Flash(`Invalid description!`, data, 0, ``, 0)
			return c.Redirect(http.StatusSeeOther, "/accounts/show")
		}
		if descriptionTooLongFor(description) {
			util.Flash(descriptionTooLong, data, 0, ``, 0)
			return c.Redirect(http.StatusSeeOther, "/accounts/show")
		}

		if err := createAccount(data.User.Id, description); err != nil {
			return databaseWriteError(c, "create account", err)
		}

		return c.Redirect(http.StatusSeeOther, "/posts")
	}, auth)
}
