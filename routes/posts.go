package routes

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	"goexpenses/database"
	"goexpenses/util"

	"github.com/labstack/echo/v4"
)

const postsPageSize = 50

type postPageCursor struct {
	CreatedAt time.Time `json:"created_at"`
	ID        int       `json:"id"`
}

type postPageRequest struct {
	Cursor    postPageCursor
	Direction string
}

func parsePostFilterRange(from, to string) (time.Time, time.Time, error) {
	var fromDate, toDate time.Time
	var err error
	for _, layout := range []string{"2006-01-02", "02.01.2006"} {
		fromDate, err = time.Parse(layout, from)
		if err == nil {
			toDate, err = time.Parse(layout, to)
			if err == nil {
				return fromDate, toDate.AddDate(0, 0, 1).Add(-time.Nanosecond), nil
			}
		}
	}
	return time.Time{}, time.Time{}, errors.New("invalid post filter date range")
}

func encodePostCursor(post util.Post) (string, error) {
	encoded, err := json.Marshal(postPageCursor{CreatedAt: post.DateTime, ID: post.Id})
	if err != nil {
		return "", fmt.Errorf("encode post cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(encoded), nil
}

func decodePostCursor(encoded string) (postPageCursor, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return postPageCursor{}, errors.New("invalid post cursor encoding")
	}

	cursor := postPageCursor{}
	if err := json.Unmarshal(decoded, &cursor); err != nil || cursor.ID <= 0 || cursor.CreatedAt.IsZero() {
		return postPageCursor{}, errors.New("invalid post cursor")
	}
	return cursor, nil
}

func parsePostPageRequest(c echo.Context) (postPageRequest, error) {
	after := c.QueryParam("after")
	before := c.QueryParam("before")
	if after != "" && before != "" {
		return postPageRequest{}, errors.New("only one post cursor may be specified")
	}

	request := postPageRequest{}
	encoded := after
	if before != "" {
		request.Direction = "before"
		encoded = before
	} else if after != "" {
		request.Direction = "after"
	}
	if encoded == "" {
		return request, nil
	}

	cursor, err := decodePostCursor(encoded)
	if err != nil {
		return postPageRequest{}, err
	}
	request.Cursor = cursor
	return request, nil
}

func setPostPagination(data *util.Data, posts []util.Post, request postPageRequest, hasMore bool) error {
	data.Pagination = util.Pagination{}
	if len(posts) == 0 {
		return nil
	}

	data.Pagination.HasPrev = request.Direction == "after" || (request.Direction == "before" && hasMore)
	data.Pagination.HasNext = request.Direction == "before" || (request.Direction != "before" && hasMore)

	var err error
	if data.Pagination.HasPrev {
		data.Pagination.PrevCursor, err = encodePostCursor(posts[0])
		if err != nil {
			return err
		}
	}
	if data.Pagination.HasNext {
		data.Pagination.NextCursor, err = encodePostCursor(posts[len(posts)-1])
		if err != nil {
			return err
		}
	}
	return nil
}

type postTotals struct {
	Saldo         float64 `db:"saldo"`
	Saldoe        float64 `db:"saldoe"`
	IncomeSaldo   float64 `db:"income_saldo"`
	IncomeSaldoe  float64 `db:"income_saldoe"`
	ExpenseSaldo  float64 `db:"expense_saldo"`
	ExpenseSaldoe float64 `db:"expense_saldoe"`
}

// loadPostTotals sums all, income and expense posts of an account in one
// query, optionally limited to a date range. Each sum is rounded like before.
func loadPostTotals(accountID int, filterDateFrom, filterDateTo *time.Time) (postTotals, error) {
	queryArgs := []any{accountID}
	where := "accounts_id = $1 AND deleted = 0"
	if filterDateFrom != nil && filterDateTo != nil {
		queryArgs = append(queryArgs, *filterDateFrom, *filterDateTo)
		where += " AND created_at BETWEEN $2 AND $3"
	}
	totals := postTotals{}
	err := database.Db.Get(&totals, `
		SELECT
			COALESCE(CAST(SUM(amount) AS Numeric(12,2)), 0) AS saldo,
			COALESCE(CAST(SUM(amount/NULLIF(exchange, 0)) AS Numeric(12,2)), 0) AS saldoe,
			COALESCE(CAST(SUM(amount) FILTER (WHERE incomes_id > 0) AS Numeric(12,2)), 0) AS income_saldo,
			COALESCE(CAST(SUM(amount/NULLIF(exchange, 0)) FILTER (WHERE incomes_id > 0) AS Numeric(12,2)), 0) AS income_saldoe,
			COALESCE(CAST(SUM(amount) FILTER (WHERE expenses_id > 0) AS Numeric(12,2)), 0) AS expense_saldo,
			COALESCE(CAST(SUM(amount/NULLIF(exchange, 0)) FILTER (WHERE expenses_id > 0) AS Numeric(12,2)), 0) AS expense_saldoe
		FROM posts
		WHERE `+where, queryArgs...)
	return totals, err
}

func loadPostsPage(accountID int, filterDateFrom, filterDateTo *time.Time, request postPageRequest) ([]util.Post, bool, error) {
	posts := []util.Post{}
	queryArgs := []any{accountID}
	where := "p.accounts_id = $1 AND p.deleted = 0"
	if filterDateFrom != nil && filterDateTo != nil {
		queryArgs = append(queryArgs, *filterDateFrom, *filterDateTo)
		where += fmt.Sprintf(" AND p.created_at BETWEEN $%d AND $%d", len(queryArgs)-1, len(queryArgs))
	}

	order := "p.created_at DESC, p.id DESC"
	if request.Direction != "" {
		queryArgs = append(queryArgs, request.Cursor.CreatedAt, request.Cursor.CreatedAt, request.Cursor.ID)
		operator := "<"
		if request.Direction == "before" {
			operator = ">"
			order = "p.created_at ASC, p.id ASC"
		}
		where += fmt.Sprintf(
			" AND (p.created_at %s $%d OR (p.created_at = $%d AND p.id %s $%d))",
			operator,
			len(queryArgs)-2,
			len(queryArgs)-1,
			operator,
			len(queryArgs),
		)
	}

	queryArgs = append(queryArgs, postsPageSize+1)
	query := fmt.Sprintf(`
		SELECT p.id, p.description, COALESCE(e.description,'') AS expense,
			COALESCE(i.description,'') AS income, p.created_at AS date,
			p.created_at AS datetime, p.created_ts, p.amount,
			COALESCE(CAST(p.amount/NULLIF(p.exchange, 0) AS Numeric(12,2)), 0) AS amounte, p.p_id
		FROM posts p
		LEFT JOIN expenses e ON p.expenses_id = e.id
		LEFT JOIN incomes i ON p.incomes_id = i.id
		WHERE %s
		ORDER BY %s
		LIMIT $%d
	`, where, order, len(queryArgs))
	if err := database.Db.Select(&posts, query, queryArgs...); err != nil {
		return nil, false, err
	}

	hasMore := len(posts) > postsPageSize
	if hasMore {
		posts = posts[:postsPageSize]
	}
	if request.Direction == "before" {
		slices.Reverse(posts)
	}
	return posts, hasMore, nil
}

// linkedPostWrite builds the automatic post for an expense's linked expense.
// It shares the primary post's description, account, rate and date.
func linkedPostWrite(primary postWrite, expenseID int, amount float64, publicID string) postWrite {
	return postWrite{
		Description: primary.Description,
		ExpenseID:   expenseID,
		Amount:      amount,
		Exchange:    primary.Exchange,
		AccountID:   primary.AccountID,
		PublicID:    publicID,
		CreatedAt:   primary.CreatedAt,
	}
}

func DefinePosts() {
	e := E
	auth := Auth

	e.GET("/posts", func(c echo.Context) error {
		data := c.Get("data").(*util.Data)
		data.Active = "posts"
		pageRequest, err := parsePostPageRequest(c)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid paging cursor").SetInternal(err)
		}
		cfrom := c.QueryParam("from")
		cto := c.QueryParam("to")
		creset := c.QueryParam("reset")

		var filterdatefrom time.Time
		var filterdateto time.Time

		data.Filter = ""
		ldatefilter := false

		if creset != "" {
			sql := `UPDATE accounts SET fromdate = $1, todate = $2 WHERE id = $3`
			if err := executeExactlyOne(database.Db, sql, "", "", data.User.Default_accounts_id); err != nil {
				return databaseWriteError(c, "reset account date filter", err)
			}
		} else {
			// A filter from the query string is new and gets saved; otherwise the
			// account's saved filter is applied without writing it back.
			filterSubmitted := cfrom != ""
			if !filterSubmitted {
				account := util.Account{}
				sql := `
				SELECT fromdate, todate, id, description, deleted 
					FROM accounts 
					WHERE id = $1
				`
				if err := database.Db.Get(&account, sql, data.User.Default_accounts_id); err != nil {
					return databaseRecordReadError(c, "load account date filter", err)
				}
				if account.Fromdate != "" {
					cfrom = account.Fromdate
					cto = account.Todate
				}
			}

			if cfrom != "" {
				tfrom, tto, err := parsePostFilterRange(cfrom, cto)
				if err == nil {
					ldatefilter = true
					filterdatefrom = tfrom
					filterdateto = tto
					data.Filter = filterdatefrom.Format("02-01-2006") + " - " + filterdateto.Format("02-01-2006")
					if filterSubmitted {
						sql := `UPDATE accounts SET fromdate = $1, todate = $2 WHERE id = $3`
						if err := executeExactlyOne(database.Db, sql, cfrom, cto, data.User.Default_accounts_id); err != nil {
							return databaseWriteError(c, "save account date filter", err)
						}
					}
				}
			}
		}

		var totalsFrom, totalsTo *time.Time
		if ldatefilter {
			totalsFrom, totalsTo = &filterdatefrom, &filterdateto
		}
		totals, err := loadPostTotals(data.User.Default_accounts_id, totalsFrom, totalsTo)
		if err != nil {
			return databaseReadError(c, "load post totals", err)
		}
		data.Saldo = fmt.Sprintf("%.2f", totals.Saldo)
		data.Saldoe = fmt.Sprintf("%.2f", totals.Saldoe)
		data.Incomesum = fmt.Sprintf("%.2f", totals.IncomeSaldo)
		data.Incomesume = fmt.Sprintf("%.2f", totals.IncomeSaldoe)
		data.Expensesum = fmt.Sprintf("%.2f", totals.ExpenseSaldo)
		data.Expensesume = fmt.Sprintf("%.2f", totals.ExpenseSaldoe)

		expenses := []util.Expense{}
		sql := `
		SELECT p_id, description 
			FROM expenses 
			WHERE accounts_id = $1 AND deleted = 0 
			ORDER BY description ASC
		`
		if err := database.Db.Select(&expenses, sql, data.User.Default_accounts_id); err != nil {
			return databaseReadError(c, "load expenses", err)
		}
		data.Expenses = expenses

		var pageFilterFrom, pageFilterTo *time.Time
		if ldatefilter {
			pageFilterFrom = &filterdatefrom
			pageFilterTo = &filterdateto
		}
		posts, hasMore, err := loadPostsPage(data.User.Default_accounts_id, pageFilterFrom, pageFilterTo, pageRequest)
		if err != nil {
			return databaseReadError(c, "load posts", err)
		}
		if err := setPostPagination(data, posts, pageRequest, hasMore); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "could not build paging links").SetInternal(err)
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
		if descriptionTooLongFor(description) {
			util.Flash(descriptionTooLong, data, 0, "", 0)
			return c.Redirect(http.StatusSeeOther, "/posts")
		}
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
			sql := `
				SELECT id, description, amount, expenses_id 
				FROM expenses 
				WHERE accounts_id = $1 AND p_id = $2 AND deleted = 0
				ORDER BY description ASC
			`
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
			sql := `SELECT id, description FROM incomes WHERE accounts_id = $1 AND p_id = $2 AND deleted = 0 ORDER BY description ASC`
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
		publicID, err := util.NewPublicID()
		if err != nil {
			return databaseWriteError(c, "generate post public ID", err)
		}

		records := []postWrite{{
			Description: description,
			ExpenseID:   expenses_idnum,
			IncomeID:    incomes_idnum,
			Amount:      amountnum,
			Exchange:    data.Eur,
			AccountID:   data.User.Default_accounts_id,
			PublicID:    publicID,
			CreatedAt:   createdAt,
		}}
		if expenses_idnum > 0 && expenses[0].ExpensesId > 0 {
			expensesadd := []util.Expense{}
			sql := `SELECT e1.id, e1.amount FROM expenses e1 WHERE e1.id = $1 AND e1.accounts_id = $2 AND e1.deleted = 0`
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
				linkedPublicID, err := util.NewPublicID()
				if err != nil {
					return databaseWriteError(c, "generate linked post public ID", err)
				}
				records = append(records, linkedPostWrite(records[0], addexp, addamount, linkedPublicID))
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

		sql := `UPDATE posts SET deleted = 1 WHERE p_id = $1 AND accounts_id = $2 AND deleted = 0`
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
		sql := `
			SELECT p.id, p.p_id, p.description, COALESCE(e.description,'') AS expense,
				COALESCE(i.description,'') AS income, created_at AS date, created_at AS datetime, created_ts, p.amount
				FROM posts p
				LEFT JOIN expenses e ON p.expenses_id = e.id
				LEFT JOIN incomes i ON p.incomes_id = i.id
				WHERE p.p_id = $1 AND p.accounts_id = $2 AND p.deleted = 0
			`
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
		if descriptionTooLongFor(description) {
			util.Flash(descriptionTooLong, data, 0, "", 0)
			return c.Redirect(http.StatusSeeOther, "/posts")
		}

		storedDate := time.Now()

		posts := []util.Post{}
		sql := `SELECT created_at AS datetime FROM posts WHERE p_id = $1 AND accounts_id = $2 AND deleted = 0`
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
		sql = `UPDATE posts SET description = $1, amount = $2, created_at = $3 WHERE p_id = $4 AND accounts_id = $5 AND deleted = 0`

		if err := executeExactlyOne(database.Db, sql, description, amountNumber, createdAt, id, data.User.Default_accounts_id); err != nil {
			return databaseWriteError(c, "update post", err)
		}

		return c.Redirect(http.StatusSeeOther, "/posts")
	}, auth)

	e.GET("/posts/newincomepost", func(c echo.Context) error {
		data := c.Get("data").(*util.Data)
		data.Active = "posts"

		incomes := []util.Income{}
		sql := `
		SELECT id, p_id, description 
			FROM incomes 
			WHERE accounts_id = $1 AND deleted = 0 
			ORDER BY description ASC
		`
		if err := database.Db.Select(&incomes, sql, data.User.Default_accounts_id); err != nil {
			return databaseReadError(c, "load incomes", err)
		}
		data.Incomes = incomes
		data.Date = time.Now().Format("2006-01-02")

		return c.Render(http.StatusOK, "newincomepostshow", data)
	}, auth)
}
