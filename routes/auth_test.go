package routes

import (
	"errors"
	"regexp"
	"testing"

	"goexpenses/database"
	"goexpenses/util"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
)

func TestAuthenticateUserMigratesLegacyPassword(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create mock database: %v", err)
	}
	defer db.Close()

	database.Db = sqlx.NewDb(db, "sqlmock")
	util.Settings.DatabaseType = "postgres"

	const (
		username = "legacy-user"
		password = "legacy-password"
		userID   = 17
	)
	legacyHash := util.Encrypt(password)
	selectQuery := `SELECT id, name, username, email, password FROM users WHERE lower(btrim(username)) = $1 AND email_verified = true`
	updateQuery := `UPDATE users SET password = $1 WHERE id = $2 AND password = $3`

	mock.ExpectQuery(regexp.QuoteMeta(selectQuery)).
		WithArgs(username).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "username", "email", "password"}).
			AddRow(userID, "Legacy User", username, "legacy@example.com", legacyHash))
	mock.ExpectExec(regexp.QuoteMeta(updateQuery)).
		WithArgs(sqlmock.AnyArg(), userID, legacyHash).
		WillReturnResult(sqlmock.NewResult(0, 1))

	user, err := authenticateUser(username, password)
	if err != nil {
		t.Fatalf("authenticateUser: %v", err)
	}
	valid, needsRehash := util.VerifyPassword(user.Password, password)
	if !valid || needsRehash {
		t.Fatalf("migrated password verification = %v, %v; want true, false", valid, needsRehash)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("database expectations: %v", err)
	}
}

func TestAuthenticateUserAcceptsBcryptPassword(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create mock database: %v", err)
	}
	defer db.Close()

	database.Db = sqlx.NewDb(db, "sqlmock")
	util.Settings.DatabaseType = "postgres"

	const (
		username = "bcrypt-user"
		password = "bcrypt-password"
	)
	hash, err := util.HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	const selectQuery = `SELECT id, name, username, email, password FROM users WHERE lower(btrim(username)) = $1 AND email_verified = true`
	mock.ExpectQuery(regexp.QuoteMeta(selectQuery)).
		WithArgs(username).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "username", "email", "password"}).
			AddRow(18, "Bcrypt User", username, "bcrypt@example.com", hash))

	user, err := authenticateUser(username, password)
	if err != nil {
		t.Fatalf("authenticateUser: %v", err)
	}
	if user.Id != 18 {
		t.Fatalf("authenticated user ID = %d, want 18", user.Id)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("database expectations: %v", err)
	}
}

func TestAuthenticateUserRejectsWrongPassword(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create mock database: %v", err)
	}
	defer db.Close()

	database.Db = sqlx.NewDb(db, "sqlmock")
	util.Settings.DatabaseType = "postgres"

	hash, err := util.HashPassword("correct-password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	const selectQuery = `SELECT id, name, username, email, password FROM users WHERE lower(btrim(username)) = $1 AND email_verified = true`
	mock.ExpectQuery(regexp.QuoteMeta(selectQuery)).
		WithArgs("bcrypt-user").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "username", "email", "password"}).
			AddRow(18, "Bcrypt User", "bcrypt-user", "bcrypt@example.com", hash))

	_, err = authenticateUser("bcrypt-user", "wrong-password")
	if !errors.Is(err, errInvalidCredentials) {
		t.Fatalf("authenticateUser error = %v, want invalid credentials", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("database expectations: %v", err)
	}
}
