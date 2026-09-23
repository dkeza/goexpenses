package routes

import (
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"goexpenses/database"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo/v4"
)

func useMockRouteDatabase(t *testing.T) (sqlmock.Sqlmock, *sqlx.DB) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create mock database: %v", err)
	}
	sqlxDB := sqlx.NewDb(db, "sqlmock")
	previousDB := database.Db
	database.Db = sqlxDB
	t.Cleanup(func() {
		database.Db = previousDB
		db.Close()
	})
	return mock, sqlxDB
}

func TestExecuteExactlyOneRejectsMissingRecord(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create mock database: %v", err)
	}
	defer db.Close()
	sqlxDB := sqlx.NewDb(db, "sqlmock")

	query := "UPDATE posts SET deleted = 1 WHERE p_id = $1 AND accounts_id = $2"
	mock.ExpectExec(regexp.QuoteMeta(query)).
		WithArgs("missing", 12).
		WillReturnResult(sqlmock.NewResult(0, 0))

	err = executeExactlyOne(sqlxDB, query, "missing", 12)
	if !errors.Is(err, errRecordNotFound) {
		t.Fatalf("executeExactlyOne error = %v, want record not found", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("database expectations: %v", err)
	}
}

func TestDatabaseRecordReadErrorReturnsNotFound(t *testing.T) {
	context := echo.New().NewContext(
		httptest.NewRequest(http.MethodGet, "/items/missing", nil),
		httptest.NewRecorder(),
	)

	err := databaseRecordReadError(context, "load item", sql.ErrNoRows)
	httpError, ok := err.(*echo.HTTPError)
	if !ok || httpError.Code != http.StatusNotFound {
		t.Fatalf("databaseRecordReadError = %v, want HTTP 404", err)
	}
}

func TestRunTransactionRollsBackFailedOperation(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create mock database: %v", err)
	}
	defer db.Close()
	sqlxDB := sqlx.NewDb(db, "sqlmock")
	query := "INSERT INTO posts (description) VALUES ($1)"

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(query)).
		WithArgs("first").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(regexp.QuoteMeta(query)).
		WithArgs("second").
		WillReturnError(errors.New("write failed"))
	mock.ExpectRollback()

	err = runTransaction(sqlxDB, func(transaction *sqlx.Tx) error {
		if err := executeExactlyOne(transaction, query, "first"); err != nil {
			return err
		}
		return executeExactlyOne(transaction, query, "second")
	})
	if err == nil {
		t.Fatal("runTransaction accepted a failed operation")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("database expectations: %v", err)
	}
}

func TestCreateUserWithAccountRollsBackWhenUserInsertFails(t *testing.T) {
	mock, _ := useMockRouteDatabase(t)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO accounts (description) VALUES ($1) RETURNING id`)).
		WithArgs("My account").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(21))
	mock.ExpectQuery(regexp.QuoteMeta(`
			INSERT INTO users (name, email, username, password, default_accounts_id, lang, email_verified, verification_token, verification_sent_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			RETURNING id`)).
		WithArgs("Test User", "test@example.com", "tester", "password-hash", 21, "EN", true, nil, nil).
		WillReturnError(errors.New("user insert failed"))
	mock.ExpectRollback()

	err := createUserWithAccount(" Test User ", " test@example.com ", " tester ", "password-hash", "EN")
	if err == nil {
		t.Fatal("createUserWithAccount accepted a failed user insert")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("database expectations: %v", err)
	}
}

func TestCreateAccountRollsBackWhenMembershipInsertFails(t *testing.T) {
	mock, _ := useMockRouteDatabase(t)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO accounts (description) VALUES ($1) RETURNING id`)).
		WithArgs("Shared account").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(22))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO accountsusers (accounts_id, users_id) VALUES ($1, $2)`)).
		WithArgs(22, 7).
		WillReturnError(errors.New("membership insert failed"))
	mock.ExpectRollback()

	err := createAccount(7, " Shared account ")
	if err == nil {
		t.Fatal("createAccount accepted a failed membership insert")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("database expectations: %v", err)
	}
}

func TestCreatePostsRollsBackWhenLinkedInsertFails(t *testing.T) {
	mock, _ := useMockRouteDatabase(t)
	createdAt := time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC)
	query := `
			INSERT INTO posts (description, expenses_id, incomes_id, amount, exchange, accounts_id, p_id, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(query)).
		WithArgs("Primary", 3, 0, 125.5, 117.2, 8, "primary-id", createdAt).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(regexp.QuoteMeta(query)).
		WithArgs("Linked", 4, 0, 125.5, 117.2, 8, "linked-id", createdAt).
		WillReturnError(errors.New("linked insert failed"))
	mock.ExpectRollback()

	err := createPosts([]postWrite{
		{Description: "Primary", ExpenseID: 3, Amount: 125.5, Exchange: 117.2, AccountID: 8, PublicID: "primary-id", CreatedAt: createdAt},
		{Description: "Linked", ExpenseID: 4, Amount: 125.5, Exchange: 117.2, AccountID: 8, PublicID: "linked-id", CreatedAt: createdAt},
	})
	if err == nil {
		t.Fatal("createPosts accepted a failed linked insert")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("database expectations: %v", err)
	}
}
