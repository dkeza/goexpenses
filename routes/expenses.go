package routes

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"goexpenses/database"
	"goexpenses/util"

	"github.com/labstack/echo/v4"
)

func DefineExpenses() {
	e := E
	auth := Auth

	e.GET("/expenses", func(c echo.Context) error {
		data := c.Get("data").(*util.Data)
		data.Active = "expenses"

		expenses := []util.Expense{}
		sql := fmt.Sprintf(`
		SELECT e1.id, e1.description, e1.amount, 
			CAST(case when e1.amount > 0 AND e1.exchange > 0 then e1.amount/e1.exchange else 0 end  AS Numeric(12,2)) AS amounte, 
			e1.expenses_id, coalesce(e2.description,'') AS expensedescription, e1.p_id 
			FROM expenses e1 
			LEFT JOIN expenses e2 ON e1.expenses_id = e2.id 
			WHERE e1.accounts_id = %v AND e1.deleted = 0 
			ORDER BY 2 ASC
		`, util.SqlParam(1))
		err := database.Db.Select(&expenses, sql, data.User.Default_accounts_id)

		//database.Db.Select(&expenses, "SELECT id, description FROM expenses WHERE accounts_id = ? AND deleted = 0 ORDER BY description ASC", data.User.Default_accounts_id)
		data.Expenses = expenses

		expensesadd := []util.Expense{}
		sql = fmt.Sprintf(`
		SELECT e1.id, e1.description, e1.amount, 
			CAST(case when e1.amount > 0 AND e1.exchange > 0 then e1.amount/e1.exchange else 0.00 end  AS Numeric(12,2)) AS amounte, 
			e1.expenses_id, coalesce(e2.description,'') AS expensedescription , e1.p_id 
			FROM expenses e1
			LEFT JOIN expenses e2 ON e1.expenses_id = e2.id 
			WHERE e1.accounts_id = %v AND e1.deleted = 0 
			UNION SELECT 0, ' ', 0.00, 0.00, 0 , '', ''
			FROM expenses
			ORDER BY 2 ASC
		`, util.SqlParam(1))
		err = database.Db.Select(&expensesadd, sql, data.User.Default_accounts_id)
		//database.Db.Select(&expenses, "SELECT id, description FROM expenses WHERE accounts_id = ? AND deleted = 0 ORDER BY description ASC", data.User.Default_accounts_id)

		data.ExpensesAdd = expensesadd

		err = c.Render(http.StatusOK, "expenses", data)
		fmt.Println(err)
		return err
	}, auth)

	e.GET("/expenses/show", func(c echo.Context) error {
		data := c.Get("data").(*util.Data)
		data.Active = "expenses"

		id := c.QueryParam("id")
		expenses := []util.Expense{}

		sql := fmt.Sprintf(`
		SELECT id, description, amount, 
			CAST(case when amount > 0 AND exchange > 0 then amount/exchange else 0 end  AS Numeric(12,2)) AS amounte, 
			expenses_id, p_id, '                                                        ' AS expenses_pid 
			FROM expenses 
			WHERE p_id = %v AND accounts_id = %v AND deleted = 0
		`, util.SqlParam(1), util.SqlParam(2))

		errsql := database.Db.Select(&expenses, sql, id, data.User.Default_accounts_id)
		if errsql != nil {
			return databaseReadError(c, "load expense", errsql)
		}
		if len(expenses) == 0 {
			return echo.NewHTTPError(http.StatusNotFound, "record not found")
		}

		if expenses[0].ExpensesId != 0 {
			expensesadd := []util.Expense{}
			sql := fmt.Sprintf(`
			SELECT p_id 
				FROM expenses 
				WHERE id = %v AND accounts_id = %v AND deleted = 0
			`, util.SqlParam(1), util.SqlParam(2))
			errsql := database.Db.Select(&expensesadd, sql, expenses[0].ExpensesId, data.User.Default_accounts_id)
			if !(errsql != nil || len(expensesadd) == 0) {
				expenses[0].ExpensesPid = expensesadd[0].Pid
			}

		}

		data.Expenses = expenses
		expensesadd := []util.Expense{}
		sql = fmt.Sprintf(`
		SELECT e1.id, e1.description, e1.amount, 
			CAST(case when e1.amount > 0 AND e1.exchange > 0 then e1.amount/e1.exchange else 0 end  AS Numeric(12,2)) AS amounte, 
			e1.expenses_id, 
			coalesce(e2.description,'') AS expensedescription,
			e1.p_id 
			FROM expenses e1
			LEFT JOIN expenses e2 ON e1.expenses_id = e2.id 
			WHERE e1.accounts_id = %v AND e1.deleted = 0 
			UNION SELECT 0, ' ', 0.00, 0.00, 0 , '', '' 
			FROM expenses
			ORDER BY 2 ASC
		`, util.SqlParam(1))
		errsql = database.Db.Select(&expensesadd, sql, data.User.Default_accounts_id)
		if errsql != nil {
			return databaseReadError(c, "load related expenses", errsql)
		}
		data.ExpensesAdd = expensesadd

		err := c.Render(http.StatusOK, "expensesshow", data)
		fmt.Println(err)
		return err
	}, auth)

	e.POST("/expenses/save", func(c echo.Context) error {
		data := c.Get("data").(*util.Data)

		description := c.FormValue("description")
		expenses_id := c.FormValue("expense_id")
		amount := c.FormValue("amount")
		amounte := c.FormValue("amounte")

		if strings.TrimSpace(description) == "" {
			util.Flash(`Invalid description!`, data, 0, ``, 0)
			return c.Redirect(http.StatusSeeOther, "/expenses")
		}

		expenses_idnum := 0
		if expenses_id != "" {
			expenses := []util.Expense{}
			sql := fmt.Sprintf(`
			SELECT id 
				FROM expenses 
				WHERE p_id = %v AND accounts_id = %v AND deleted = 0
			`, util.SqlParam(1), util.SqlParam(2))
			errsql := database.Db.Select(&expenses, sql, expenses_id, data.User.Default_accounts_id)
			if errsql != nil {
				return databaseReadError(c, "validate related expense", errsql)
			}
			if len(expenses) == 0 {
				util.Flash(`Changes not saved, because of invalid input data!`, data, 0, "", 0)
				return c.Redirect(http.StatusSeeOther, "/expenses")
			}
			expenses_idnum = expenses[0].Id
		}

		fmt.Println("expenses_idnum", expenses_idnum)

		amountnum, _ := strconv.ParseFloat(amount, 64)
		if amountnum == 0.00 {
			amounte, _ := strconv.ParseFloat(amounte, 64)
			if amounte != 0.00 {
				amountnum = util.ToFixed(amounte*data.Eur, 2)
			}
		}

		sql := fmt.Sprintf(`INSERT INTO expenses (description, accounts_id, amount, exchange, expenses_id, p_id) VALUES (%v,%v,%v,%v,%v,%v)`, util.SqlParam(1), util.SqlParam(2), util.SqlParam(3), util.SqlParam(4), util.SqlParam(5), util.SqlParam(6))
		if err := executeExactlyOne(database.Db, sql, strings.TrimSpace(description), data.User.Default_accounts_id, amountnum, data.Eur, expenses_idnum, util.Encrypt(util.CreateUUID())); err != nil {
			return databaseWriteError(c, "create expense", err)
		}
		return c.Redirect(http.StatusSeeOther, "/expenses")
	}, auth)

	e.POST("/expenses/update", func(c echo.Context) error {
		data := c.Get("data").(*util.Data)
		id := c.FormValue("id")
		description := c.FormValue("description")
		expenses_id := c.FormValue("expense_id")
		amount := c.FormValue("amount")
		amounte := c.FormValue("amounte")
		if id == "" || strings.TrimSpace(description) == "" {
			util.Flash(`Invalid description!`, data, 0, ``, 0)
			return c.Redirect(http.StatusSeeOther, "/expenses")
		}

		amountnum, _ := strconv.ParseFloat(amount, 64)
		if amountnum == 0.00 {
			amounte, _ := strconv.ParseFloat(amounte, 64)
			if amounte != 0.00 {
				amountnum = util.ToFixed(amounte*data.Eur, 2)
			}
		}

		fmt.Println("expenses_id:", expenses_id)

		expenses_idnum := 0
		if expenses_id != "" {
			expenses := []util.Expense{}
			sql := fmt.Sprintf(`
			SELECT id 
				FROM expenses 
				WHERE p_id = %v AND accounts_id = %v AND deleted = 0
			`, util.SqlParam(1), util.SqlParam(2))
			errsql := database.Db.Select(&expenses, sql, expenses_id, data.User.Default_accounts_id)
			if errsql != nil {
				return databaseReadError(c, "validate related expense", errsql)
			}
			if len(expenses) == 0 {
				util.Flash(`Changes not saved, because of invalid input data!`, data, 0, "", 0)
				return c.Redirect(http.StatusSeeOther, "/expenses")
			}
			expenses_idnum = expenses[0].Id
		}
		sql := fmt.Sprintf(`UPDATE expenses SET description = %v, amount = %v, exchange = %v, expenses_id = %v WHERE p_id = %v AND accounts_id = %v AND deleted = 0`, util.SqlParam(1), util.SqlParam(2), util.SqlParam(3), util.SqlParam(4), util.SqlParam(5), util.SqlParam(6))
		if err := executeExactlyOne(database.Db, sql, strings.TrimSpace(description), amountnum, data.Eur, expenses_idnum, id, data.User.Default_accounts_id); err != nil {
			return databaseWriteError(c, "update expense", err)
		}

		return c.Redirect(http.StatusSeeOther, "/expenses")
	}, auth)

	e.POST("/expenses/delete", func(c echo.Context) error {
		data := c.Get("data").(*util.Data)
		id := c.FormValue("id")

		fmt.Println("expenses/delete", id)

		sql := fmt.Sprintf(`UPDATE expenses SET deleted = 1 WHERE p_id = %v AND accounts_id = %v AND deleted = 0`, util.SqlParam(1), util.SqlParam(2))
		if err := executeExactlyOne(database.Db, sql, id, data.User.Default_accounts_id); err != nil {
			return databaseWriteError(c, "delete expense", err)
		}

		return c.Redirect(http.StatusSeeOther, "/expenses")
	}, auth)

}
