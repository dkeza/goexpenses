package routes

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/dkeza/goexpenses/database"
	"github.com/dkeza/goexpenses/util"

	"github.com/labstack/echo"
)

func DefineExpenses() {
	e := E
	auth := Auth

	e.GET("/expenses", func(c echo.Context) error {
		data := c.Get("data").(*util.Data)
		data.Active = "expenses"

		expenses := []util.Expense{}
		err := database.Db.Select(&expenses, `
			SELECT e1.id, e1.description, e1.amount, 
			CAST(case when e1.amount > 0 AND e1.exchange > 0 then e1.amount/e1.exchange else 0 end  AS Numeric(12,2)) AS amounte, 
			e1.expenses_id, coalesce(e2.description,'') AS expensedescription, e1.p_id 
			FROM expenses e1 
			LEFT JOIN expenses e2 ON e1.expenses_id = e2.id 
			WHERE e1.accounts_id = ? AND e1.deleted = 0 
			ORDER BY 2 ASC
		`, data.User.Default_accounts_id)

		//database.Db.Select(&expenses, "SELECT id, description FROM expenses WHERE accounts_id = ? AND deleted = 0 ORDER BY description ASC", data.User.Default_accounts_id)
		data.Expenses = expenses

		expensesadd := []util.Expense{}
		err = database.Db.Select(&expensesadd, `
			SELECT e1.id, e1.description, e1.amount, 
			CAST(case when e1.amount > 0 AND e1.exchange > 0 then e1.amount/e1.exchange else 0.00 end  AS Numeric(12,2)) AS amounte, 
			e1.expenses_id, coalesce(e2.description,'') AS expensedescription , e1.p_id 
			FROM expenses e1
			LEFT JOIN expenses e2 ON e1.expenses_id = e2.id 
			WHERE e1.accounts_id = ? AND e1.deleted = 0 
			UNION SELECT 0, ' ', 0.00, 0.00, 0 , '', ''
			FROM expenses
			ORDER BY 2 ASC
		`, data.User.Default_accounts_id)
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
		errsql := database.Db.Select(&expenses, `
			SELECT id, description, amount, 
				CAST(case when amount > 0 AND exchange > 0 then amount/exchange else 0 end  AS Numeric(12,2)) AS amounte, 
				expenses_id, p_id, '                                                        ' AS expenses_pid 
				FROM expenses 
				WHERE p_id = ? AND accounts_id = ? AND deleted = 0
				`, id, data.User.Default_accounts_id)
		fmt.Println("amounte:", expenses[0].Amounte)
		fmt.Println(errsql)

		if expenses[0].ExpensesId != 0 {
			expensesadd := []util.Expense{}
			errsql := database.Db.Select(&expensesadd, `
				SELECT p_id 
					FROM expenses 
					WHERE id = ? AND accounts_id = ? AND deleted = 0
					`, expenses[0].ExpensesId, data.User.Default_accounts_id)
			if !(errsql != nil || len(expensesadd) == 0) {
				expenses[0].ExpensesPid = expensesadd[0].Pid
			}

		}

		data.Expenses = expenses
		fmt.Println("expenses/show", data.Expenses)

		expensesadd := []util.Expense{}
		errsql = database.Db.Select(&expensesadd, `
			SELECT e1.id, e1.description, e1.amount, 
			CAST(case when e1.amount > 0 AND e1.exchange > 0 then e1.amount/e1.exchange else 0 end  AS Numeric(12,2)) AS amounte, 
			e1.expenses_id, 
			coalesce(e2.description,'') AS expensedescription,
			e1.p_id 
			FROM expenses e1
			LEFT JOIN expenses e2 ON e1.expenses_id = e2.id 
			WHERE e1.accounts_id = ? AND e1.deleted = 0 
			UNION SELECT 0, ' ', 0.00, 0.00, 0 , '', '' 
			FROM expenses
			ORDER BY 2 ASC
		`, data.User.Default_accounts_id)
		fmt.Println(errsql)
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

		if description == "" {
			util.Flash(`Invalid description!`, data, 0, ``, 0)
			return c.Redirect(http.StatusSeeOther, "/expenses")
		}

		expenses_idnum := 0
		if expenses_id != "" {
			expenses := []util.Expense{}
			errsql := database.Db.Select(&expenses, `
				SELECT id 
					FROM expenses 
					WHERE p_id = ? AND accounts_id = ? AND deleted = 0
					`, expenses_id, data.User.Default_accounts_id)
			if !(errsql != nil || len(expenses) == 0) {
				expenses_idnum = expenses[0].Id
			}
		}

		fmt.Println("expenses_idnum", expenses_idnum)

		amountnum, _ := strconv.ParseFloat(amount, 64)
		if amountnum == 0.00 {
			amounte, _ := strconv.ParseFloat(amounte, 64)
			if amounte != 0.00 {
				amountnum = util.ToFixed(amounte*data.Eur, 2)
			}
		}

		sql := `INSERT INTO expenses (description, accounts_id, amount, exchange, expenses_id, p_id) VALUES (?,?,?,?,?,?)`
		err := database.Db.MustExec(sql, description, data.User.Default_accounts_id, amountnum, data.Eur, expenses_idnum, util.Encrypt(util.CreateUUID()))
		fmt.Println(err)
		return c.Redirect(http.StatusSeeOther, "/expenses")
	}, auth)

	e.POST("/expenses/update", func(c echo.Context) error {
		data := c.Get("data").(*util.Data)
		id := c.FormValue("id")
		description := c.FormValue("description")
		expenses_id := c.FormValue("expense_id")
		amount := c.FormValue("amount")
		amounte := c.FormValue("amounte")

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
			errsql := database.Db.Select(&expenses, `
				SELECT id 
					FROM expenses 
					WHERE p_id = ? AND accounts_id = ? AND deleted = 0
					`, expenses_id, data.User.Default_accounts_id)
			if !(errsql != nil || len(expenses) == 0) {
				expenses_idnum = expenses[0].Id
			}
		}

		sql := `UPDATE expenses SET description = ?, amount = ?, exchange = ?, expenses_id = ? WHERE p_id = ? AND accounts_id = ?`
		err := database.Db.MustExec(sql, description, amountnum, data.Eur, expenses_idnum, id, data.User.Default_accounts_id)
		fmt.Println(err)

		return c.Redirect(http.StatusSeeOther, "/expenses")
	}, auth)

	e.POST("/expenses/delete", func(c echo.Context) error {
		data := c.Get("data").(*util.Data)
		id := c.FormValue("id")

		fmt.Println("expenses/delete", id)

		sql := `UPDATE expenses SET deleted = 1 WHERE p_id = ? AND accounts_id = ?`
		database.Db.MustExec(sql, id, data.User.Default_accounts_id)

		return c.Redirect(http.StatusSeeOther, "/expenses")
	}, auth)

}
