package routes

import (
	"database/sql"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestUnverifiedRegistrationPersistsTokenAndCannotSignIn(t *testing.T) {
	mock, _ := useMockRouteDatabase(t)
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	token, hash, err := newVerificationToken()
	if err != nil {
		t.Fatal(err)
	}
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO accounts (description) VALUES ($1) RETURNING id`)).
		WithArgs("My account").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(21))
	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO users (name, email, username, password, default_accounts_id, lang, email_verified, verification_token, verification_sent_at)`)).
		WithArgs("Test User", "test@example.com", "tester", "password-hash", 21, "EN", false, hash, now).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(22))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO accountsusers (accounts_id, users_id) VALUES ($1, $2)`)).
		WithArgs(21, 22).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	if err := createUnverifiedUserWithAccount("Test User", "test@example.com", "tester", "password-hash", "EN", hash, now); err != nil {
		t.Fatal(err)
	}
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, name, username, email, password FROM users WHERE lower(btrim(username)) = $1 AND email_verified = true`)).
		WithArgs("tester").WillReturnError(sql.ErrNoRows)
	if _, err := authenticateUser("tester", "password-hash"); !errors.Is(err, errInvalidCredentials) {
		t.Fatalf("unconfirmed login error = %v", err)
	}
	if token == "" {
		t.Fatal("empty confirmation token")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestConfirmationTokenIsSingleUseAndRejectsMalformedTokens(t *testing.T) {
	mock, _ := useMockRouteDatabase(t)
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	token, hash, err := newVerificationToken()
	if err != nil {
		t.Fatal(err)
	}
	query := regexp.QuoteMeta(`UPDATE users
		SET email_verified = true, verification_token = NULL, verification_sent_at = NULL
		WHERE verification_token = $1 AND email_verified = false AND verification_sent_at >= $2`)
	mock.ExpectExec(query).WithArgs(hash, now.Add(-verificationTokenDuration)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	confirmed, err := confirmEmail(token, now)
	if err != nil || !confirmed {
		t.Fatalf("confirmEmail = %v, %v", confirmed, err)
	}
	mock.ExpectExec(query).WithArgs(hash, now.Add(-verificationTokenDuration)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	confirmed, err = confirmEmail(token, now)
	if err != nil || confirmed {
		t.Fatalf("reused token = %v, %v", confirmed, err)
	}
	confirmed, err = confirmEmail("invalid", now)
	if err != nil || confirmed {
		t.Fatalf("malformed token = %v, %v", confirmed, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestVerificationResendEnforcesCooldown(t *testing.T) {
	mock, _ := useMockRouteDatabase(t)
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	query := regexp.QuoteMeta(`SELECT id, email, verification_sent_at FROM users
		WHERE lower(btrim(email)) = $1 AND email_verified = false FOR UPDATE`)
	mock.ExpectBegin()
	mock.ExpectQuery(query).WithArgs("test@example.com").
		WillReturnRows(sqlmock.NewRows([]string{"id", "email", "verification_sent_at"}).AddRow(7, "test@example.com", now.Add(-time.Minute)))
	mock.ExpectRollback()
	_, _, created, err := requestVerification(" TEST@example.com ", now)
	if err != nil || created {
		t.Fatalf("cooldown request = %v, %v", created, err)
	}
	mock.ExpectBegin()
	mock.ExpectQuery(query).WithArgs("test@example.com").
		WillReturnRows(sqlmock.NewRows([]string{"id", "email", "verification_sent_at"}).AddRow(7, "test@example.com", now.Add(-verificationResendInterval)))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE users SET verification_token = $1, verification_sent_at = $2 WHERE id = $3`)).
		WithArgs(sqlmock.AnyArg(), now, 7).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	recipient, token, created, err := requestVerification("test@example.com", now)
	if err != nil || !created || recipient != "test@example.com" || token == "" {
		t.Fatalf("resend = %q, %v, %v", recipient, created, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
