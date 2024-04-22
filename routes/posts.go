package routes

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"goexpenses/database"
	"goexpenses/util"

	"github.com/labstack/echo/v4"
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
			sql := fmt.Sprintf(`UPDATE accounts SET fromdate = %v, todate = %v WHERE id = %v`, util.SqlParam(1), util.SqlParam(2), util.SqlParam(3))
			database.Db.MustExec(sql, "", "", data.User.Default_accounts_id)
		} else {
			if cfrom == "" {
				account := util.Account{}
				sql := fmt.Sprintf(`
				SELECT fromdate, todate, id, description, deleted 
					FROM accounts 
					WHERE id = %v
				`, util.SqlParam(1))
				database.Db.Get(&account, sql, data.User.Default_accounts_id)
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
						sql := fmt.Sprintf(`UPDATE accounts SET fromdate = %v, todate = %v WHERE id = %v`, util.SqlParam(1), util.SqlParam(2), util.SqlParam(3))
						database.Db.MustExec(sql, cfrom, cto, data.User.Default_accounts_id)
					}
				}

			}
		}

		postsum := util.Postsum{}
		if ldatefilter {
			sql := fmt.Sprintf(`
			SELECT CAST(SUM(amount) AS Numeric(12,2)) AS saldo, 
				CAST(SUM(amount/exchange) AS Numeric(12,2)) AS saldoe 
				FROM posts 
				WHERE accounts_id = %v AND deleted = 0 AND created_at 
				BETWEEN %v AND %v
			`, util.SqlParam(1), util.SqlParam(2), util.SqlParam(3))
			database.Db.Get(&postsum, sql, data.User.Default_accounts_id, filterdatefrom, filterdateto)
		} else {
			sql := fmt.Sprintf(`
			SELECT CAST(SUM(amount) AS Numeric(12,2)) AS saldo, 
				CAST(SUM(amount/exchange) AS Numeric(12,2)) AS saldoe 
				FROM posts 
				WHERE accounts_id = %v AND deleted = 0
			`, util.SqlParam(1))
			database.Db.Get(&postsum, sql, data.User.Default_accounts_id)
		}
		data.Saldo = fmt.Sprintf("%.2f", postsum.Saldo)
		data.Saldoe = fmt.Sprintf("%.2f", postsum.Saldoe)

		incomesum := util.Incomessum{}
		if ldatefilter {
			sql := fmt.Sprintf(`
			SELECT CAST(SUM(amount) AS Numeric(12,2)) AS saldo, 
				CAST(SUM(amount/exchange) AS Numeric(12,2)) AS saldoe 
				FROM posts 
				WHERE incomes_id > 0 AND accounts_id = %v AND deleted = 0 AND created_at BETWEEN %v AND %v
			`, util.SqlParam(1), util.SqlParam(2), util.SqlParam(3))
			database.Db.Get(&incomesum, sql, data.User.Default_accounts_id, filterdatefrom, filterdateto)
		} else {
			sql := fmt.Sprintf(`
			SELECT CAST(SUM(amount) AS Numeric(12,2)) AS saldo, 
				CAST(SUM(amount/exchange) AS Numeric(12,2)) AS saldoe 
				FROM posts 
				WHERE incomes_id > 0 AND accounts_id = %v AND deleted = 0
			`, util.SqlParam(1))
			database.Db.Get(&incomesum, sql, data.User.Default_accounts_id)
		}
		data.Incomesum = fmt.Sprintf("%.2f", incomesum.Saldo)
		data.Incomesume = fmt.Sprintf("%.2f", incomesum.Saldoe)

		expensesum := util.Expensessum{}
		if ldatefilter {
			sql := fmt.Sprintf(`
			SELECT CAST(SUM(amount) AS Numeric(12,2)) AS saldo, 
				CAST(SUM(amount/exchange) AS Numeric(12,2)) AS saldoe 
				FROM posts 
				WHERE expenses_id > 0 AND accounts_id = %v AND deleted = 0 AND created_at BETWEEN %v AND %v
			`, util.SqlParam(1), util.SqlParam(2), util.SqlParam(3))
			database.Db.Get(&expensesum, sql, data.User.Default_accounts_id, filterdatefrom, filterdateto)
		} else {
			sql := fmt.Sprintf(`
			SELECT CAST(SUM(amount) AS Numeric(12,2)) AS saldo, 
				CAST(SUM(amount/exchange) AS Numeric(12,2)) AS saldoe 
				FROM posts WHERE expenses_id > 0 AND accounts_id = %v AND deleted = 0
			`, util.SqlParam(1))
			database.Db.Get(&expensesum, sql, data.User.Default_accounts_id)
		}
		data.Expensesum = fmt.Sprintf("%.2f", expensesum.Saldo)
		data.Expensesume = fmt.Sprintf("%.2f", expensesum.Saldoe)

		expenses := []util.Expense{}
		sql := fmt.Sprintf(`
		SELECT p_id, description 
			FROM expenses 
			WHERE accounts_id = %v AND deleted = 0 
			ORDER BY description ASC
		`, util.SqlParam(1))
		database.Db.Select(&expenses, sql, data.User.Default_accounts_id)
		data.Expenses = expenses

		posts := []util.Post{}
		var errsql error
		if ldatefilter {
			sql := ""
			if util.Settings.DatabaseType == "sqlite" {
				sql = fmt.Sprintf(`
				SELECT p.id, p.description, ifnull(e.description,'') AS expense, 
					ifnull(i.description,'') AS income, created_at AS date, created_ts, p.amount, 
					CAST(p.amount/p.exchange AS Numeric(12,2)) AS amounte, p.p_id 
					FROM posts p 
					LEFT JOIN expenses e ON p.expenses_id = e.id 
					LEFT JOIN incomes i ON p.incomes_id = i.id 
					WHERE p.accounts_id = %v AND p.deleted = 0 AND created_at BETWEEN %v AND %v 
					ORDER BY created_at DESC
				`, util.SqlParam(1), util.SqlParam(2), util.SqlParam(3))
			} else {
				sql = fmt.Sprintf(`
				SELECT p.id, p.description, COALESCE(e.description,'') AS expense, 
					COALESCE(i.description,'') AS income, created_at AS date, created_ts, p.amount, 
					CAST(p.amount/p.exchange AS Numeric(12,2)) AS amounte, p.p_id 
					FROM posts p 
					LEFT JOIN expenses e ON p.expenses_id = e.id 
					LEFT JOIN incomes i ON p.incomes_id = i.id 
					WHERE p.accounts_id = %v AND p.deleted = 0 AND created_at BETWEEN %v AND %v 
					ORDER BY created_at DESC
				`, util.SqlParam(1), util.SqlParam(2), util.SqlParam(3))
			}
			errsql = database.Db.Select(&posts, sql, data.User.Default_accounts_id, filterdatefrom, filterdateto)
		} else {
			sql := ""
			if util.Settings.DatabaseType == "sqlite" {
				sql = fmt.Sprintf(`
				SELECT p.id, p.description, ifnull(e.description,'') AS expense, 
					ifnull(i.description,'') AS income, created_at AS date, created_ts, p.amount, 
					CAST(p.amount/p.exchange AS Numeric(12,2)) AS amounte, p.p_id 
					FROM posts p 
					LEFT JOIN expenses e ON p.expenses_id = e.id 
					LEFT JOIN incomes i ON p.incomes_id = i.id 
					WHERE p.accounts_id = %v AND p.deleted = 0 
					ORDER BY created_at DESC
				`, util.SqlParam(1))
			} else {
				sql = fmt.Sprintf(`
				SELECT p.id, p.description, COALESCE(e.description,'') AS expense, 
					COALESCE(i.description,'') AS income, created_at AS date, created_ts, p.amount, 
					CAST(p.amount/p.exchange AS Numeric(12,2)) AS amounte, p.p_id 
					FROM posts p 
					LEFT JOIN expenses e ON p.expenses_id = e.id 
					LEFT JOIN incomes i ON p.incomes_id = i.id 
					WHERE p.accounts_id = %v AND p.deleted = 0 
					ORDER BY created_at DESC
				`, util.SqlParam(1))
			}
			errsql = database.Db.Select(&posts, sql, data.User.Default_accounts_id)
		}

		if errsql != nil {
			log.Println("/posts SQL Error: ", errsql.Error())
		}

		data.Posts = posts
		data.Date = time.Now().Format("2006-01-02")

		rerr := c.Render(http.StatusOK, "posts", data)
		if rerr != nil {
			log.Println("/posts Rendering Error: ", rerr.Error())
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
		date := c.FormValue("date")

		createdAt := time.Now()
		currentDate, errDate := time.Parse("2006-01-02", date)
		if errDate == nil {
			now := time.Now()
			createdAt = time.Date(currentDate.Year(), currentDate.Month(), currentDate.Day(), now.Hour(), now.Minute(), now.Second(), now.Nanosecond(), now.Location())
		}

		expenses_pid := expenses_id
		incomes_pid := incomes_id
		log.Println(incomes_pid)

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
			sql := fmt.Sprintf(`
				SELECT id, description, amount, expenses_id 
				FROM expenses 
				WHERE accounts_id = %v AND p_id = %v 
				ORDER BY description ASC
			`, util.SqlParam(1), util.SqlParam(2))
			errExpenses := database.Db.Select(&expenses, sql, data.User.Default_accounts_id, expenses_pid)
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
		log.Println("incomes_pid:", incomes_pid)
		if incomes_pid != "" {
			// Check if valid income is selected
			sql := fmt.Sprintf(`SELECT id, description FROM incomes WHERE accounts_id = %v AND p_id = %v ORDER BY description ASC`, util.SqlParam(1), util.SqlParam(2))
			errIncomes := database.Db.Select(&incomes, sql, data.User.Default_accounts_id, incomes_pid)
			log.Println("errIncomes", errIncomes)
			log.Println("incomes", incomes)
			if errIncomes != nil || len(incomes) == 0 {
				util.Flash(`Changes not saved, because of invalid input data!`, data, 0, "", 0)
				return c.Redirect(http.StatusSeeOther, "/posts")
			} else {
				incomes_idnum = incomes[0].Id
				amountnum = amountnum * -1
			}
		}

		sql := fmt.Sprintf(`INSERT INTO posts (description, expenses_id, incomes_id, amount, exchange, accounts_id, p_id, created_at) VALUES (%v,%v,%v,%v,%v,%v,%v,%v)`, util.SqlParam(1), util.SqlParam(2), util.SqlParam(3), util.SqlParam(4), util.SqlParam(5), util.SqlParam(6), util.SqlParam(7), util.SqlParam((8)))
		err := database.Db.MustExec(sql, description, expenses_idnum, incomes_idnum, amountnum, data.Eur, data.User.Default_accounts_id, util.Encrypt(util.CreateUUID()), createdAt)

		log.Println("/posts SQL Error:", err, "expenses_idnum:", expenses_idnum)

		if expenses_idnum > 0 && expenses[0].ExpensesId > 0 {
			expensesadd := []util.Expense{}
			sql := fmt.Sprintf(`SELECT e1.id, e1.amount FROM expenses e1 WHERE e1.id = %v ORDER BY 2 ASC`, util.SqlParam(1))
			errsql2 := database.Db.Select(&expensesadd, sql, expenses[0].ExpensesId)

			if errsql2 != nil {
				log.Println("/posts SQL Error errsql2: ", errsql2)
			}

			addexp := expenses[0].ExpensesId
			addamount := expensesadd[0].Amount

			if addexp != 0 {
				sql := fmt.Sprintf(`INSERT INTO posts (description, expenses_id, incomes_id, amount, exchange, accounts_id, p_id) VALUES (%v,%v,%v,%v,%v,%v,%v)`, util.SqlParam(1), util.SqlParam(2), util.SqlParam(3), util.SqlParam(4), util.SqlParam(5), util.SqlParam(6), util.SqlParam(7))
				err = database.Db.MustExec(sql, description, addexp, 0, addamount, data.Eur, data.User.Default_accounts_id, util.Encrypt(util.CreateUUID()))
			}
		}

		util.Flash(`Saved`, data, 1, description, expenses_idnum)
		return c.Redirect(http.StatusSeeOther, "/posts")
	}, auth)

	e.POST("/posts/delete", func(c echo.Context) error {
		data := c.Get("data").(*util.Data)
		id := c.FormValue("id")

		sql := fmt.Sprintf(`UPDATE posts SET deleted = 1 WHERE p_id = %v AND accounts_id = %v`, util.SqlParam(1), util.SqlParam(2))
		database.Db.MustExec(sql, id, data.User.Default_accounts_id)

		return c.Redirect(http.StatusSeeOther, "/posts")
	}, auth)

	e.GET("/posts/show", func(c echo.Context) error {
		data := c.Get("data").(*util.Data)
		data.Active = "posts"

		id := c.QueryParam("id")
		posts := []util.Post{}
		sql := ""
		if util.Settings.DatabaseType == "sqlite" {
			sql = fmt.Sprintf(`
			SELECT p.id, p.p_id, p.description, ifnull(e.description,'') AS expense, 
				ifnull(i.description,'') AS income, created_at AS date, created_at AS datetime, created_ts, p.amount 
				FROM posts p 
				LEFT JOIN expenses e ON p.expenses_id = e.id 
				LEFT JOIN incomes i ON p.incomes_id = i.id 
				WHERE p.p_id = %v AND p.accounts_id = %v AND p.deleted = 0
			`, util.SqlParam(1), util.SqlParam(2))
		} else {
			sql = fmt.Sprintf(`
			SELECT p.id, p.p_id, p.description, COALESCE(e.description,'') AS expense, 
				COALESCE(i.description,'') AS income, created_at AS date, created_at AS datetime, created_ts, p.amount 
				FROM posts p 
				LEFT JOIN expenses e ON p.expenses_id = e.id 
				LEFT JOIN incomes i ON p.incomes_id = i.id 
				WHERE p.p_id = %v AND p.accounts_id = %v AND p.deleted = 0
			`, util.SqlParam(1), util.SqlParam(2))
		}
		errsql := database.Db.Select(&posts, sql, id, data.User.Default_accounts_id)
		if errsql != nil {
			log.Println("/posts/show errsql:", errsql)
		}
		for _, post := range posts {
			post.DateOnly = post.DateTime.Format("2006-01-02")
			post.TimeOnly = post.DateTime.Format("15:04")
			data.Posts = append(data.Posts, post)
		}

		errrender := c.Render(http.StatusOK, "postsshow", data)
		if errrender != nil {
			log.Println("/posts/show Rendering error:", errrender)
		}
		return errrender
	}, auth)

	e.POST("/posts/update", func(c echo.Context) error {
		data := c.Get("data").(*util.Data)
		id := c.FormValue("id")
		description := c.FormValue("description")
		amount := c.FormValue("amount")
		dateOnly := c.FormValue("dateonly")

		storedDate := time.Now()

		posts := []util.Post{}
		sql := ""
		if util.Settings.DatabaseType == "sqlite" {
			sql = fmt.Sprintf(`
			SELECT created_at AS datetime 
				FROM posts p 
				LEFT JOIN expenses e ON p.expenses_id = e.id 
				LEFT JOIN incomes i ON p.incomes_id = i.id 
				WHERE p.p_id = %v AND p.accounts_id = %v AND p.deleted = 0
			`, util.SqlParam(1), util.SqlParam(2))
		} else {
			sql = fmt.Sprintf(`
			SELECT created_at AS datetime 
				FROM posts p 
				LEFT JOIN expenses e ON p.expenses_id = e.id 
				LEFT JOIN incomes i ON p.incomes_id = i.id 
				WHERE p.p_id = %v AND p.accounts_id = %v AND p.deleted = 0
			`, util.SqlParam(1), util.SqlParam(2))
		}
		errsql1 := database.Db.Select(&posts, sql, id, data.User.Default_accounts_id)
		if errsql1 != nil {
			log.Println("/posts/update errsql1:", errsql1)
		}
		for _, post := range posts {
			storedDate = post.DateTime
			break
		}

		createdAt := storedDate
		enteredDate, errDate := time.Parse("2006-01-02", dateOnly)
		if errDate == nil {
			createdAt = time.Date(enteredDate.Year(), enteredDate.Month(), enteredDate.Day(), storedDate.Hour(), storedDate.Minute(), storedDate.Second(), storedDate.Nanosecond(), storedDate.Location())
		}

		sql = fmt.Sprintf(`UPDATE posts SET description = %v, amount = %v, created_at = %v WHERE p_id = %v AND accounts_id = %v`, util.SqlParam(1), util.SqlParam(2), util.SqlParam(3), util.SqlParam(4), util.SqlParam(5))

		errsql2 := database.Db.MustExec(sql, description, amount, createdAt, id, data.User.Default_accounts_id)
		if errsql2 != nil {
			log.Printf("/posts/update errsql2: %+v\n", errsql2)
		}

		return c.Redirect(http.StatusSeeOther, "/posts")
	}, auth)

	e.GET("/posts/newincomepost", func(c echo.Context) error {
		data := c.Get("data").(*util.Data)
		data.Active = "posts"

		incomes := []util.Income{}
		sql := fmt.Sprintf(`
		SELECT id, p_id, description 
			FROM incomes 
			WHERE accounts_id = %v AND deleted = 0 
			ORDER BY description ASC
		`, util.SqlParam(1))
		database.Db.Select(&incomes, sql, data.User.Default_accounts_id)
		data.Incomes = incomes
		data.Date = time.Now().Format("2006-01-02")

		return c.Render(http.StatusOK, "newincomepostshow", data)
	}, auth)
}
