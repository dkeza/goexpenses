package routes

import (
	"fmt"
	"net/http"
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
			if err := executeExactlyOne(database.Db, sql, "", "", data.User.Default_accounts_id); err != nil {
				return databaseWriteError(c, "reset account date filter", err)
			}
		} else {
			if cfrom == "" {
				account := util.Account{}
				sql := fmt.Sprintf(`
				SELECT fromdate, todate, id, description, deleted 
					FROM accounts 
					WHERE id = %v
				`, util.SqlParam(1))
				if err := database.Db.Get(&account, sql, data.User.Default_accounts_id); err != nil {
					return databaseRecordReadError(c, "load account date filter", err)
				}
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
						if err := executeExactlyOne(database.Db, sql, cfrom, cto, data.User.Default_accounts_id); err != nil {
							return databaseWriteError(c, "save account date filter", err)
						}
					}
				}

			}
		}

		postsum := util.Postsum{}
		var errsql error
		if ldatefilter {
			sql := fmt.Sprintf(`
			SELECT COALESCE(CAST(SUM(amount) AS Numeric(12,2)), 0) AS saldo,
				COALESCE(CAST(SUM(amount/NULLIF(exchange, 0)) AS Numeric(12,2)), 0) AS saldoe
				FROM posts 
				WHERE accounts_id = %v AND deleted = 0 AND created_at 
				BETWEEN %v AND %v
			`, util.SqlParam(1), util.SqlParam(2), util.SqlParam(3))
			errsql = database.Db.Get(&postsum, sql, data.User.Default_accounts_id, filterdatefrom, filterdateto)
		} else {
			sql := fmt.Sprintf(`
			SELECT COALESCE(CAST(SUM(amount) AS Numeric(12,2)), 0) AS saldo,
				COALESCE(CAST(SUM(amount/NULLIF(exchange, 0)) AS Numeric(12,2)), 0) AS saldoe
				FROM posts 
				WHERE accounts_id = %v AND deleted = 0
			`, util.SqlParam(1))
			errsql = database.Db.Get(&postsum, sql, data.User.Default_accounts_id)
		}
		if errsql != nil {
			return databaseReadError(c, "load post totals", errsql)
		}
		data.Saldo = fmt.Sprintf("%.2f", postsum.Saldo)
		data.Saldoe = fmt.Sprintf("%.2f", postsum.Saldoe)

		incomesum := util.Incomessum{}
		if ldatefilter {
			sql := fmt.Sprintf(`
			SELECT COALESCE(CAST(SUM(amount) AS Numeric(12,2)), 0) AS saldo,
				COALESCE(CAST(SUM(amount/NULLIF(exchange, 0)) AS Numeric(12,2)), 0) AS saldoe
				FROM posts 
				WHERE incomes_id > 0 AND accounts_id = %v AND deleted = 0 AND created_at BETWEEN %v AND %v
			`, util.SqlParam(1), util.SqlParam(2), util.SqlParam(3))
			errsql = database.Db.Get(&incomesum, sql, data.User.Default_accounts_id, filterdatefrom, filterdateto)
		} else {
			sql := fmt.Sprintf(`
			SELECT COALESCE(CAST(SUM(amount) AS Numeric(12,2)), 0) AS saldo,
				COALESCE(CAST(SUM(amount/NULLIF(exchange, 0)) AS Numeric(12,2)), 0) AS saldoe
				FROM posts 
				WHERE incomes_id > 0 AND accounts_id = %v AND deleted = 0
			`, util.SqlParam(1))
			errsql = database.Db.Get(&incomesum, sql, data.User.Default_accounts_id)
		}
		if errsql != nil {
			return databaseReadError(c, "load income totals", errsql)
		}
		data.Incomesum = fmt.Sprintf("%.2f", incomesum.Saldo)
		data.Incomesume = fmt.Sprintf("%.2f", incomesum.Saldoe)

		expensesum := util.Expensessum{}
		if ldatefilter {
			sql := fmt.Sprintf(`
			SELECT COALESCE(CAST(SUM(amount) AS Numeric(12,2)), 0) AS saldo,
				COALESCE(CAST(SUM(amount/NULLIF(exchange, 0)) AS Numeric(12,2)), 0) AS saldoe
				FROM posts 
				WHERE expenses_id > 0 AND accounts_id = %v AND deleted = 0 AND created_at BETWEEN %v AND %v
			`, util.SqlParam(1), util.SqlParam(2), util.SqlParam(3))
			errsql = database.Db.Get(&expensesum, sql, data.User.Default_accounts_id, filterdatefrom, filterdateto)
		} else {
			sql := fmt.Sprintf(`
			SELECT COALESCE(CAST(SUM(amount) AS Numeric(12,2)), 0) AS saldo,
				COALESCE(CAST(SUM(amount/NULLIF(exchange, 0)) AS Numeric(12,2)), 0) AS saldoe
				FROM posts WHERE expenses_id > 0 AND accounts_id = %v AND deleted = 0
			`, util.SqlParam(1))
			errsql = database.Db.Get(&expensesum, sql, data.User.Default_accounts_id)
		}
		if errsql != nil {
			return databaseReadError(c, "load expense totals", errsql)
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
		if err := database.Db.Select(&expenses, sql, data.User.Default_accounts_id); err != nil {
			return databaseReadError(c, "load expenses", err)
		}
		data.Expenses = expenses

		posts := []util.Post{}
		if ldatefilter {
			sql := ""
			if util.Settings.DatabaseType == "sqlite" {
				sql = fmt.Sprintf(`
				SELECT p.id, p.description, ifnull(e.description,'') AS expense, 
					ifnull(i.description,'') AS income, created_at AS date, created_ts, p.amount, 
					COALESCE(CAST(p.amount/NULLIF(p.exchange, 0) AS Numeric(12,2)), 0) AS amounte, p.p_id
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
					COALESCE(CAST(p.amount/NULLIF(p.exchange, 0) AS Numeric(12,2)), 0) AS amounte, p.p_id
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
					COALESCE(CAST(p.amount/NULLIF(p.exchange, 0) AS Numeric(12,2)), 0) AS amounte, p.p_id
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
					COALESCE(CAST(p.amount/NULLIF(p.exchange, 0) AS Numeric(12,2)), 0) AS amounte, p.p_id
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
			return databaseReadError(c, "load posts", errsql)
		}

		data.Posts = posts
		data.Date = time.Now().Format("2006-01-02")

		return c.Render(http.StatusOK, "posts", data)
	}, auth)

	e.POST("/posts/save", func(c echo.Context) error {
		data := c.Get("data").(*util.Data)

		description := c.FormValue("description")
		expenses_id := c.FormValue("expense_id")
		incomes_id := c.FormValue("income_id")
		amount := c.FormValue("amount")
		amounte := c.FormValue("amounte")
		date := c.FormValue("date")
		amountnum, err := parseAmountInputs(amount, amounte, data.Eur)
		if err != nil {
			util.Flash(`Changes not saved, because of invalid input data!`, data, 0, "", 0)
			return c.Redirect(http.StatusSeeOther, "/posts")
		}

		createdAt, err := dateWithTime(date, time.Now())
		if err != nil {
			util.Flash(`Invalid date!`, data, 0, description, 0)
			return c.Redirect(http.StatusSeeOther, "/posts")
		}
		if amountnum == 0 {
			util.Flash(`Invalid amount!`, data, 0, description, 0)
			return c.Redirect(http.StatusSeeOther, "/posts")
		}

		expenses_pid := expenses_id
		incomes_pid := incomes_id

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
				WHERE accounts_id = %v AND p_id = %v AND deleted = 0
				ORDER BY description ASC
			`, util.SqlParam(1), util.SqlParam(2))
			errExpenses := database.Db.Select(&expenses, sql, data.User.Default_accounts_id, expenses_pid)
			if errExpenses != nil {
				return databaseReadError(c, "validate expense", errExpenses)
			}
			if len(expenses) == 0 {
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

		}

		incomes_idnum := 0
		incomes := []util.Income{}
		if incomes_pid != "" {
			// Check if valid income is selected
			sql := fmt.Sprintf(`SELECT id, description FROM incomes WHERE accounts_id = %v AND p_id = %v AND deleted = 0 ORDER BY description ASC`, util.SqlParam(1), util.SqlParam(2))
			errIncomes := database.Db.Select(&incomes, sql, data.User.Default_accounts_id, incomes_pid)
			if errIncomes != nil {
				return databaseReadError(c, "validate income", errIncomes)
			}
			if len(incomes) == 0 {
				util.Flash(`Changes not saved, because of invalid input data!`, data, 0, "", 0)
				return c.Redirect(http.StatusSeeOther, "/posts")
			}
			incomes_idnum = incomes[0].Id
			amountnum = amountnum * -1
		}

		records := []postWrite{{
			Description: description,
			ExpenseID:   expenses_idnum,
			IncomeID:    incomes_idnum,
			Amount:      amountnum,
			Exchange:    data.Eur,
			AccountID:   data.User.Default_accounts_id,
			PublicID:    util.Encrypt(util.CreateUUID()),
			CreatedAt:   createdAt,
		}}
		if expenses_idnum > 0 && expenses[0].ExpensesId > 0 {
			expensesadd := []util.Expense{}
			sql := fmt.Sprintf(`SELECT e1.id, e1.amount FROM expenses e1 WHERE e1.id = %v AND e1.accounts_id = %v AND e1.deleted = 0`, util.SqlParam(1), util.SqlParam(2))
			errsql2 := database.Db.Select(&expensesadd, sql, expenses[0].ExpensesId, data.User.Default_accounts_id)
			if errsql2 != nil {
				return databaseReadError(c, "load linked expense", errsql2)
			}
			if len(expensesadd) == 0 {
				util.Flash(`Changes not saved, because of invalid input data!`, data, 0, "", 0)
				return c.Redirect(http.StatusSeeOther, "/posts")
			}

			addexp := expenses[0].ExpensesId
			addamount := expensesadd[0].Amount
			if addexp != 0 {
				records = append(records, postWrite{
					Description: description,
					ExpenseID:   addexp,
					Amount:      addamount,
					Exchange:    data.Eur,
					AccountID:   data.User.Default_accounts_id,
					PublicID:    util.Encrypt(util.CreateUUID()),
					CreatedAt:   time.Now(),
				})
			}
		}
		if err := createPosts(records); err != nil {
			return databaseWriteError(c, "create post", err)
		}

		util.Flash(`Saved`, data, 1, description, expenses_idnum)
		return c.Redirect(http.StatusSeeOther, "/posts")
	}, auth)

	e.POST("/posts/delete", func(c echo.Context) error {
		data := c.Get("data").(*util.Data)
		id := c.FormValue("id")

		sql := fmt.Sprintf(`UPDATE posts SET deleted = 1 WHERE p_id = %v AND accounts_id = %v AND deleted = 0`, util.SqlParam(1), util.SqlParam(2))
		if err := executeExactlyOne(database.Db, sql, id, data.User.Default_accounts_id); err != nil {
			return databaseWriteError(c, "delete post", err)
		}

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
			return databaseReadError(c, "load post", errsql)
		}
		if len(posts) == 0 {
			return echo.NewHTTPError(http.StatusNotFound, "record not found")
		}
		for _, post := range posts {
			post.DateOnly = post.DateTime.Format("2006-01-02")
			post.TimeOnly = post.DateTime.Format("15:04")
			data.Posts = append(data.Posts, post)
		}

		return c.Render(http.StatusOK, "postsshow", data)
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
			return databaseReadError(c, "load post for update", errsql1)
		}
		if len(posts) == 0 {
			return echo.NewHTTPError(http.StatusNotFound, "record not found")
		}
		for _, post := range posts {
			storedDate = post.DateTime
			break
		}

		amountNumber, err := parseDatabaseAmount(amount)
		if err != nil || amountNumber == 0 {
			util.Flash(`Invalid amount!`, data, 0, description, 0)
			return c.Redirect(http.StatusSeeOther, "/posts")
		}
		createdAt, err := dateWithTime(dateOnly, storedDate)
		if err != nil {
			util.Flash(`Invalid date!`, data, 0, description, 0)
			return c.Redirect(http.StatusSeeOther, "/posts")
		}
		sql = fmt.Sprintf(`UPDATE posts SET description = %v, amount = %v, created_at = %v WHERE p_id = %v AND accounts_id = %v AND deleted = 0`, util.SqlParam(1), util.SqlParam(2), util.SqlParam(3), util.SqlParam(4), util.SqlParam(5))

		if err := executeExactlyOne(database.Db, sql, description, amountNumber, createdAt, id, data.User.Default_accounts_id); err != nil {
			return databaseWriteError(c, "update post", err)
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
		if err := database.Db.Select(&incomes, sql, data.User.Default_accounts_id); err != nil {
			return databaseReadError(c, "load incomes", err)
		}
		data.Incomes = incomes
		data.Date = time.Now().Format("2006-01-02")

		return c.Render(http.StatusOK, "newincomepostshow", data)
	}, auth)
}
