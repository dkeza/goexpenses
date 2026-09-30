package routes

import (
	"net/http"
	"strings"

	"goexpenses/database"
	"goexpenses/util"

	"github.com/labstack/echo/v4"
)

func DefineIncomes() {
	e := E
	auth := Auth

	e.GET("/incomes", func(c echo.Context) error {
		data := c.Get("data").(*util.Data)
		data.Active = "incomes"

		incomes := []util.Income{}
		sql := `SELECT id, description, p_id FROM incomes WHERE accounts_id = $1 AND deleted = 0 ORDER BY description ASC`
		if err := database.Db.Select(&incomes, sql, data.User.Default_accounts_id); err != nil {
			return databaseReadError(c, "load incomes", err)
		}
		data.Incomes = incomes
		return c.Render(http.StatusOK, "incomes", data)
	}, auth)

	e.POST("/incomes/save", func(c echo.Context) error {
		data := c.Get("data").(*util.Data)

		description := c.FormValue("description")
		if strings.TrimSpace(description) == "" {
			util.Flash(`Invalid description!`, data, 0, ``, 0)
			return c.Redirect(http.StatusSeeOther, "/incomes")
		}
		if descriptionTooLongFor(description) {
			util.Flash(descriptionTooLong, data, 0, ``, 0)
			return c.Redirect(http.StatusSeeOther, "/incomes")
		}
		publicID, err := util.NewPublicID()
		if err != nil {
			return databaseWriteError(c, "generate income public ID", err)
		}
		sql := `INSERT INTO incomes (description, accounts_id, p_id) VALUES ($1, $2, $3)`
		if err := executeExactlyOne(database.Db, sql, strings.TrimSpace(description), data.User.Default_accounts_id, publicID); err != nil {
			return databaseWriteError(c, "create income", err)
		}

		return c.Redirect(http.StatusSeeOther, "/incomes")
	}, auth)

	e.POST("/incomes/update", func(c echo.Context) error {
		data := c.Get("data").(*util.Data)
		id := c.FormValue("id")
		description := c.FormValue("description")

		if id == "" || strings.TrimSpace(description) == "" {
			util.Flash(`Invalid description!`, data, 0, ``, 0)
			return c.Redirect(http.StatusSeeOther, "/incomes")
		}
		if descriptionTooLongFor(description) {
			util.Flash(descriptionTooLong, data, 0, ``, 0)
			return c.Redirect(http.StatusSeeOther, "/incomes")
		}
		sql := `UPDATE incomes SET description = $1 WHERE p_id = $2 AND accounts_id = $3 AND deleted = 0`
		if err := executeExactlyOne(database.Db, sql, strings.TrimSpace(description), id, data.User.Default_accounts_id); err != nil {
			return databaseWriteError(c, "update income", err)
		}

		return c.Redirect(http.StatusSeeOther, "/incomes")
	}, auth)

	e.POST("/incomes/delete", func(c echo.Context) error {
		data := c.Get("data").(*util.Data)
		id := c.FormValue("id")

		sql := `UPDATE incomes SET deleted = 1 WHERE p_id = $1 AND accounts_id = $2 AND deleted = 0`
		if err := executeExactlyOne(database.Db, sql, id, data.User.Default_accounts_id); err != nil {
			return databaseWriteError(c, "delete income", err)
		}

		return c.Redirect(http.StatusSeeOther, "/incomes")
	}, auth)

	e.GET("/incomes/show", func(c echo.Context) error {
		data := c.Get("data").(*util.Data)
		data.Active = "incomes"

		id := c.QueryParam("id")
		incomes := []util.Income{}
		sql := `SELECT id, description, p_id FROM incomes WHERE p_id = $1 AND accounts_id = $2 AND deleted = 0`
		if err := database.Db.Select(&incomes, sql, id, data.User.Default_accounts_id); err != nil {
			return databaseReadError(c, "load income", err)
		}
		if len(incomes) == 0 {
			return echo.NewHTTPError(http.StatusNotFound, "record not found")
		}
		data.Incomes = incomes
		return c.Render(http.StatusOK, "incomesshow", data)
	}, auth)
}
