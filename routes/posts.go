package routes

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"goexpenses/database"
	"goexpenses/util"

	"github.com/labstack/echo"
)

func DefinePosts() {
	e := E
	auth := Auth

	e.GET("/posts", func(c echo.Context) error {
		data := c.Get("data").(*util.Data)
		data.Active = "posts"
		cfrom := c.QueryParam("from")
		cto := c.QueryParam("to")
		creset := c.QueryParam("reset")

		var filterdatefrom time.Time
		var filterdateto time.Time

		data.Filter = ""
		ldatefilter := false

		if creset != "" {
			database.Db.MustExec(`UPDATE accounts SET fromdate = ?, todate = ? WHERE id = ?`, "", "", data.User.Default_accounts_id)
		} else {
			if cfrom == "" {
				account := util.Account{}
				database.Db.Get(&account, `
					SELECT fromdate, todate, id, description, deleted 
					FROM accounts 
					WHERE id = ?
					`, data.User.Default_accounts_id)
				if account.Fromdate != "" {
					cfrom = account.Fromdate
					cto = account.Todate
				}
			}

			if cfrom != "" {
				layout := "02.01.2006T15:04:05.000Z"
				clfrom := cfrom + "T00:00:00.000Z"
				tfrom, err := time.Parse(layout, clfrom)
				if err == nil {
					clto := cto + "T23:59:59.999Z"
					tto, err := time.Parse(layout, clto)
					if err == nil {
						ldatefilter = true
						filterdatefrom = tfrom
						filterdateto = tto
						data.Filter = filterdatefrom.Format("02-01-2006") + " - " + filterdateto.Format("02-01-2006")
						database.Db.MustExec(`UPDATE accounts SET fromdate = ?, todate = ? WHERE id = ?`, cfrom, cto, data.User.Default_accounts_id)
					}
				}

			}
		}

		postsum := util.Postsum{}
		if ldatefilter {
			database.Db.Get(&postsum, `
				SELECT CAST(SUM(amount) AS Numeric(12,2)) AS saldo, 
				CAST(SUM(amount/exchange) AS Numeric(12,2)) AS saldoe 
				FROM posts 
				WHERE accounts_id = ? AND deleted = 0 AND created_at 
				BETWEEN ? AND ?
				`, data.User.Default_accounts_id, filterdatefrom, filterdateto)
		} else {
			database.Db.Get(&postsum, `
				SELECT CAST(SUM(amount) AS Numeric(12,2)) AS saldo, 
					CAST(SUM(amount/exchange) AS Numeric(12,2)) AS saldoe 
					FROM posts 
					WHERE accounts_id = ? AND deleted = 0
					`, data.User.Default_accounts_id)
		}
		data.Saldo = fmt.Sprintf("%.2f", postsum.Saldo)
		data.Saldoe = fmt.Sprintf("%.2f", postsum.Saldoe)

		incomesum := util.Incomessum{}
		if ldatefilter {
			database.Db.Get(&incomesum, `
				SELECT CAST(SUM(amount) AS Numeric(12,2)) AS saldo, 
					CAST(SUM(amount/exchange) AS Numeric(12,2)) AS saldoe 
					FROM posts 
					WHERE incomes_id > 0 AND accounts_id = ? AND deleted = 0 AND created_at BETWEEN ? AND ?
					`, data.User.Default_accounts_id, filterdatefrom, filterdateto)
		} else {
			database.Db.Get(&incomesum, `
				SELECT CAST(SUM(amount) AS Numeric(12,2)) AS saldo, 
					CAST(SUM(amount/exchange) AS Numeric(12,2)) AS saldoe 
					FROM posts 
					WHERE incomes_id > 0 AND accounts_id = ? AND deleted = 0
					`, data.User.Default_accounts_id)
		}
		data.Incomesum = fmt.Sprintf("%.2f", incomesum.Saldo)
		data.Incomesume = fmt.Sprintf("%.2f", incomesum.Saldoe)

		expensesum := util.Expensessum{}
		if ldatefilter {
			database.Db.Get(&expensesum, `
				SELECT CAST(SUM(amount) AS Numeric(12,2)) AS saldo, 
					CAST(SUM(amount/exchange) AS Numeric(12,2)) AS saldoe 
					FROM posts 
					WHERE expenses_id > 0 AND accounts_id = ? AND deleted = 0 AND created_at BETWEEN ? AND ?
					`, data.User.Default_accounts_id, filterdatefrom, filterdateto)
		} else {
			database.Db.Get(&expensesum, `
				SELECT CAST(SUM(amount) AS Numeric(12,2)) AS saldo, 
					CAST(SUM(amount/exchange) AS Numeric(12,2)) AS saldoe 
					FROM posts WHERE expenses_id > 0 AND accounts_id = ? AND deleted = 0
					`, data.User.Default_accounts_id)
		}
		data.Expensesum = fmt.Sprintf("%.2f", expensesum.Saldo)
		data.Expensesume = fmt.Sprintf("%.2f", expensesum.Saldoe)

		expenses := []util.Expense{}
		database.Db.Select(&expenses, `
			SELECT p_id, description 
				FROM expenses 
				WHERE accounts_id = ? AND deleted = 0 
				ORDER BY description ASC
				`, data.User.Default_accounts_id)
		data.Expenses = expenses

		posts := []util.Post{}
		var errsql error
		if ldatefilter {
			errsql = database.Db.Select(&posts, `
				SELECT p.id, p.description, ifnull(e.description,'') AS expense, 
					ifnull(i.description,'') AS income, created_at AS date, p.amount, 
					CAST(p.amount/p.exchange AS Numeric(12,2)) AS amounte, p.p_id 
					FROM posts p 
					LEFT JOIN expenses e ON p.expenses_id = e.id 
					LEFT JOIN incomes i ON p.incomes_id = i.id 
					WHERE p.accounts_id = ? AND p.deleted = 0 AND created_at BETWEEN ? AND ? 
					ORDER BY created_at DESC
					`, data.User.Default_accounts_id, filterdatefrom, filterdateto)
		} else {
			errsql = database.Db.Select(&posts, `
				SELECT p.id, p.description, ifnull(e.description,'') AS expense, 
					ifnull(i.description,'') AS income, created_at AS date, p.amount, 
					CAST(p.amount/p.exchange AS Numeric(12,2)) AS amounte, p.p_id 
					FROM posts p 
					LEFT JOIN expenses e ON p.expenses_id = e.id 
					LEFT JOIN incomes i ON p.incomes_id = i.id 
					WHERE p.accounts_id = ? AND p.deleted = 0 
					ORDER BY created_at DESC
					`, data.User.Default_accounts_id)
		}

		if errsql != nil {
			fmt.Println("/posts SQL Error: ", errsql.Error())
		}

		data.Posts = posts

		rerr := c.Render(http.StatusOK, "posts", data)
		if rerr != nil {
			fmt.Println("/posts Rendering Error: ", rerr.Error())
		}

		return rerr
	}, auth)

	e.POST("/posts/save", func(c echo.Context) error {
		data := c.Get("data").(*util.Data)

		description := c.FormValue("description")
		expenses_id := c.FormValue("expense_id")
		incomes_id := c.FormValue("income_id")
		amount := c.FormValue("amount")
		amounte := c.FormValue("amounte")

		expenses_pid := expenses_id
		incomes_pid := incomes_id
		fmt.Println(incomes_pid)

		amountnum, _ := strconv.ParseFloat(amount, 64)
		if amountnum == 0.00 {
			amounte, _ := strconv.ParseFloat(amounte, 64)
			if amounte != 0.00 {
				amountnum = util.ToFixed(amounte*data.Eur, 2)
			}
		}

		expenses := []util.Expense{}

		if expenses_pid == "" {
			if incomes_pid == "" {
				util.Flash(`Invalid expense!`, data, 0, "", 0)
				return c.Redirect(http.StatusSeeOther, "/posts")
			}
		} else {
			// Check if valid expense is selected

			errExpenses := database.Db.Select(&expenses, `
				SELECT id, description, amount, expenses_id 
					FROM expenses 
					WHERE accounts_id = ? AND p_id = ? 
					ORDER BY description ASC
					`, data.User.Default_accounts_id, expenses_pid)
			if errExpenses != nil || len(expenses) == 0 {
				util.Flash(`Changes not saved, because of invalid input data!`, data, 0, "", 0)
				return c.Redirect(http.StatusSeeOther, "/posts")
			}
		}
		expenses_idnum := 0
		if incomes_pid == "" {
			expenses_idnum = expenses[0].Id
			if description == "" {
				util.Flash(`Invalid description!`, data, 0, ``, expenses_idnum)
				return c.Redirect(http.StatusSeeOther, "/posts")
			}

			if amountnum == 0.00 {
				util.Flash(`Invalid amount!`, data, 0, description, expenses_idnum)
				return c.Redirect(http.StatusSeeOther, "/posts")
			}
		}

		//incomes_idnum, _ := strconv.Atoi(incomes_id)

		incomes_idnum := 0
		incomes := []util.Income{}
		fmt.Println("incomes_pid:", incomes_pid)
		if incomes_pid != "" {
			// Check if valid income is selected

			errIncomes := database.Db.Select(&incomes, "SELECT id, description FROM incomes WHERE accounts_id = ? AND p_id = ? ORDER BY description ASC", data.User.Default_accounts_id, incomes_pid)
			fmt.Println("errIncomes", errIncomes)
			fmt.Println("incomes", incomes)
			if errIncomes != nil || len(incomes) == 0 {
				util.Flash(`Changes not saved, because of invalid input data!`, data, 0, "", 0)
				return c.Redirect(http.StatusSeeOther, "/posts")
			} else {
				incomes_idnum = incomes[0].Id
				amountnum = amountnum * -1
			}
		}

		sql := `INSERT INTO posts (description, expenses_id, incomes_id, amount, exchange, accounts_id, p_id) VALUES (?,?,?,?,?,?,?)`
		err := database.Db.MustExec(sql, description, expenses_idnum, incomes_idnum, amountnum, data.Eur, data.User.Default_accounts_id, util.Encrypt(util.CreateUUID()))

		fmt.Println("/posts SQL Error:", err, "expenses_idnum:", expenses_idnum)

		if expenses_idnum > 0 && expenses[0].ExpensesId > 0 {
			expensesadd := []util.Expense{}
			errsql2 := database.Db.Select(&expensesadd, `
				SELECT e1.id, e1.amount FROM expenses e1 WHERE e1.id = ? ORDER BY 2 ASC
			`, expenses[0].ExpensesId)

			if errsql2 != nil {
				fmt.Println("/posts SQL Error errsql2: ", errsql2)
			}

			addexp := expenses[0].ExpensesId
			addamount := expensesadd[0].Amount

			if addexp != 0 {
				sql = `INSERT INTO posts (description, expenses_id, incomes_id, amount, exchange, accounts_id, p_id) VALUES (?,?,?,?,?,?,?)`
				err = database.Db.MustExec(sql, description, addexp, 0, addamount, data.Eur, data.User.Default_accounts_id, util.Encrypt(util.CreateUUID()))
			}
		}

		util.Flash(`Saved`, data, 1, description, expenses_idnum)
		return c.Redirect(http.StatusSeeOther, "/posts")
	}, auth)

	e.POST("/posts/delete", func(c echo.Context) error {
		data := c.Get("data").(*util.Data)
		id := c.FormValue("id")

		sql := `UPDATE posts SET deleted = 1 WHERE p_id = ? AND accounts_id = ?`
		database.Db.MustExec(sql, id, data.User.Default_accounts_id)

		return c.Redirect(http.StatusSeeOther, "/posts")
	}, auth)

	e.GET("/posts/show", func(c echo.Context) error {
		data := c.Get("data").(*util.Data)
		data.Active = "posts"

		id := c.QueryParam("id")
		posts := []util.Post{}
		errsql := database.Db.Select(&posts, `
			SELECT p.id, p.p_id, p.description, ifnull(e.description,'') AS expense, 
				ifnull(i.description,'') AS income, created_at AS date, p.amount 
				FROM posts p 
				LEFT JOIN expenses e ON p.expenses_id = e.id 
				LEFT JOIN incomes i ON p.incomes_id = i.id 
				WHERE p.p_id = ? AND p.accounts_id = ? AND p.deleted = 0
				`, id, data.User.Default_accounts_id)
		if errsql != nil {
			fmt.Println("/posts/show errsql:", errsql)
		}
		data.Posts = posts
		errrender := c.Render(http.StatusOK, "postsshow", data)
		if errrender != nil {
			fmt.Println("/posts/show Rendering error:", errrender)
		}
		return errrender
	}, auth)

	e.POST("/posts/update", func(c echo.Context) error {
		data := c.Get("data").(*util.Data)
		id := c.FormValue("id")
		description := c.FormValue("description")
		amount := c.FormValue("amount")

		sql := `UPDATE posts SET description = ?, amount = ? WHERE p_id = ? AND accounts_id = ?`
		database.Db.MustExec(sql, description, amount, id, data.User.Default_accounts_id)

		return c.Redirect(http.StatusSeeOther, "/posts")
	}, auth)

	e.GET("/posts/newincomepost", func(c echo.Context) error {
		data := c.Get("data").(*util.Data)
		data.Active = "posts"

		incomes := []util.Income{}
		database.Db.Select(&incomes, `
			SELECT id, p_id, description 
				FROM incomes 
				WHERE accounts_id = ? AND deleted = 0 
				ORDER BY description ASC
				`, data.User.Default_accounts_id)
		data.Incomes = incomes

		return c.Render(http.StatusOK, "newincomepostshow", data)
	}, auth)
}
