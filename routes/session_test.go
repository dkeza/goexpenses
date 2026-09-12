package routes

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"goexpenses/database"
	"goexpenses/util"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo/v4"
)

func TestRotateSession(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create mock database: %v", err)
	}
	defer db.Close()

	database.Db = sqlx.NewDb(db, "sqlmock")
	util.Settings.DatabaseType = "postgres"
	util.Settings.CookieSecure = true
	const currentHash = "old-session-hash"
	query := "UPDATE sessions SET uuid = $1, user_id = $2, created_at = $3 WHERE uuid = $4"
	mock.ExpectExec(regexp.QuoteMeta(query)).
		WithArgs(sqlmock.AnyArg(), 12, sqlmock.AnyArg(), currentHash).
		WillReturnResult(sqlmock.NewResult(0, 1))

	req := httptest.NewRequest(http.MethodPost, "/auth", nil)
	recorder := httptest.NewRecorder()
	context := echo.New().NewContext(req, recorder)
	data := &util.Data{CookieId: currentHash}
	context.Set("_id", currentHash)
	context.Set("data", data)

	if err := rotateSession(context, 12); err != nil {
		t.Fatalf("rotateSession: %v", err)
	}
	newHash := context.Get("_id").(string)
	if newHash == currentHash || data.CookieId != newHash {
		t.Fatalf("session was not rotated: context=%q data=%q", newHash, data.CookieId)
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("response cookies = %d, want 1", len(cookies))
	}
	storedHash, err := util.HashSessionToken(cookies[0].Value)
	if err != nil || storedHash != newHash {
		t.Fatal("rotated cookie token does not match stored hash")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("database expectations: %v", err)
	}
}

func TestLogoutDeletesSessionAndCookie(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create mock database: %v", err)
	}
	defer db.Close()

	database.Db = sqlx.NewDb(db, "sqlmock")
	util.Settings.DatabaseType = "postgres"
	util.Settings.CookieSecure = true
	const sessionHash = "current-session-hash"
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM sessions WHERE uuid = $1")).
		WithArgs(sessionHash).
		WillReturnResult(sqlmock.NewResult(0, 1))

	req := httptest.NewRequest(http.MethodPost, "/logout", nil)
	recorder := httptest.NewRecorder()
	context := echo.New().NewContext(req, recorder)
	context.Set("_id", sessionHash)

	if err := logout(context); err != nil {
		t.Fatalf("logout: %v", err)
	}
	if recorder.Code != http.StatusSeeOther || recorder.Header().Get(echo.HeaderLocation) != "/" {
		t.Fatalf("logout response = %d %q", recorder.Code, recorder.Header().Get(echo.HeaderLocation))
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 || cookies[0].MaxAge != -1 {
		t.Fatalf("logout cookie was not expired: %+v", cookies)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("database expectations: %v", err)
	}
}
