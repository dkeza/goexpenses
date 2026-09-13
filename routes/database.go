package routes

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"goexpenses/database"
	"goexpenses/util"

	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo/v4"
)

var errRecordNotFound = errors.New("record not found")

type sqlExecer interface {
	Exec(query string, args ...interface{}) (sql.Result, error)
}

func executeExactlyOne(execer sqlExecer, query string, args ...interface{}) error {
	result, err := execer.Exec(query, args...)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected != 1 {
		return errRecordNotFound
	}
	return nil
}

func runTransaction(db *sqlx.DB, operation func(*sqlx.Tx) error) error {
	transaction, err := db.Beginx()
	if err != nil {
		return err
	}
	defer transaction.Rollback()

	if err := operation(transaction); err != nil {
		return err
	}
	return transaction.Commit()
}

func databaseWriteError(c echo.Context, operation string, err error) error {
	if errors.Is(err, errRecordNotFound) {
		return echo.NewHTTPError(http.StatusNotFound, "record not found")
	}
	c.Logger().Errorf("%s: %v", operation, err)
	return echo.NewHTTPError(http.StatusInternalServerError, "could not save changes").SetInternal(err)
}

func databaseReadError(c echo.Context, operation string, err error) error {
	c.Logger().Errorf("%s: %v", operation, err)
	return echo.NewHTTPError(http.StatusInternalServerError, "could not load data").SetInternal(err)
}

func createUserWithAccount(name, email, username, passwordHash, lang string) error {
	return runTransaction(database.Db, func(transaction *sqlx.Tx) error {
		accountID := 0
		if err := transaction.QueryRowx(
			`INSERT INTO accounts (description) VALUES ($1) RETURNING id`,
			util.GetLangText(`My account`, lang),
		).Scan(&accountID); err != nil {
			return err
		}

		userID := 0
		if err := transaction.QueryRowx(`
			INSERT INTO users (name, email, username, password, default_accounts_id, lang)
			VALUES ($1, $2, $3, $4, $5, $6)
			RETURNING id`,
			strings.TrimSpace(name), strings.TrimSpace(email), strings.TrimSpace(username), passwordHash, accountID, lang,
		).Scan(&userID); err != nil {
			return err
		}

		return executeExactlyOne(transaction,
			`INSERT INTO accountsusers (accounts_id, users_id) VALUES ($1, $2)`,
			accountID, userID,
		)
	})
}

func createAccount(userID int, description string) error {
	return runTransaction(database.Db, func(transaction *sqlx.Tx) error {
		accountID := 0
		if err := transaction.QueryRowx(
			`INSERT INTO accounts (description) VALUES ($1) RETURNING id`,
			strings.TrimSpace(description),
		).Scan(&accountID); err != nil {
			return err
		}

		return executeExactlyOne(transaction,
			`INSERT INTO accountsusers (accounts_id, users_id) VALUES ($1, $2)`,
			accountID, userID,
		)
	})
}

type postWrite struct {
	Description string
	ExpenseID   int
	IncomeID    int
	Amount      float64
	Exchange    float64
	AccountID   int
	PublicID    string
	CreatedAt   time.Time
}

func createPosts(records []postWrite) error {
	if len(records) == 0 {
		return errors.New("no post records to create")
	}
	return runTransaction(database.Db, func(transaction *sqlx.Tx) error {
		const query = `
			INSERT INTO posts (description, expenses_id, incomes_id, amount, exchange, accounts_id, p_id, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`
		for _, record := range records {
			if err := executeExactlyOne(transaction, query,
				record.Description,
				record.ExpenseID,
				record.IncomeID,
				record.Amount,
				record.Exchange,
				record.AccountID,
				record.PublicID,
				record.CreatedAt,
			); err != nil {
				return err
			}
		}
		return nil
	})
}
