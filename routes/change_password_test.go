package routes

import (
	"errors"
	"regexp"
	"testing"

	"goexpenses/util"

	"github.com/DATA-DOG/go-sqlmock"
)

const (
	changePasswordUserID      = 42
	changePasswordSessionHash = "current-session-hash"
	changePasswordCurrent     = "current-password-123"
	changePasswordNewHash     = "new-password-hash"
)

func expectStoredPassword(t *testing.T, mock sqlmock.Sqlmock) string {
	t.Helper()
	storedHash, err := util.HashPassword(changePasswordCurrent)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	mock.ExpectQuery(regexp.QuoteMeta("SELECT password FROM users WHERE id = $1 AND blocked_at IS NULL")).
		WithArgs(changePasswordUserID).
		WillReturnRows(sqlmock.NewRows([]string{"password"}).AddRow(storedHash))
	return storedHash
}

func TestChangeOwnPasswordRevokesOtherSessions(t *testing.T) {
	mock, cleanup := newPasswordResetMock(t)
	defer cleanup()

	storedHash := expectStoredPassword(t, mock)
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("UPDATE users SET password = $1 WHERE id = $2 AND password = $3")).
		WithArgs(changePasswordNewHash, changePasswordUserID, storedHash).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM sessions WHERE user_id = $1 AND uuid <> $2")).
		WithArgs(changePasswordUserID, changePasswordSessionHash).
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()

	if err := changeOwnPassword(changePasswordUserID, changePasswordCurrent, changePasswordNewHash, changePasswordSessionHash); err != nil {
		t.Fatalf("changeOwnPassword: %v", err)
	}
}

func TestChangeOwnPasswordRejectsWrongCurrentPassword(t *testing.T) {
	mock, cleanup := newPasswordResetMock(t)
	defer cleanup()

	expectStoredPassword(t, mock)

	err := changeOwnPassword(changePasswordUserID, "wrong-password-123", changePasswordNewHash, changePasswordSessionHash)
	if !errors.Is(err, errInvalidCredentials) {
		t.Fatalf("changeOwnPassword error = %v, want errInvalidCredentials", err)
	}
}

func TestChangeOwnPasswordRejectsConcurrentChange(t *testing.T) {
	mock, cleanup := newPasswordResetMock(t)
	defer cleanup()

	storedHash := expectStoredPassword(t, mock)
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("UPDATE users SET password = $1 WHERE id = $2 AND password = $3")).
		WithArgs(changePasswordNewHash, changePasswordUserID, storedHash).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

	err := changeOwnPassword(changePasswordUserID, changePasswordCurrent, changePasswordNewHash, changePasswordSessionHash)
	if !errors.Is(err, errInvalidCredentials) {
		t.Fatalf("changeOwnPassword error = %v, want errInvalidCredentials", err)
	}
}
