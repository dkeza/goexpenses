package midware

import (
	"errors"
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

func TestInvalidCookieCreatesNewSession(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create mock database: %v", err)
	}
	defer db.Close()

	database.Db = sqlx.NewDb(db, "sqlmock")
	util.Settings.DatabaseType = "postgres"
	util.Settings.CookieSecure = true
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO sessions (uuid) VALUES ($1)")).
		WithArgs(sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: util.SessionCookieName, Value: "invalid"})
	recorder := httptest.NewRecorder()
	context := echo.New().NewContext(req, recorder)

	session, tokenHash, err := getOrCreateSession(context)
	if err != nil {
		t.Fatalf("getOrCreateSession: %v", err)
	}
	if session.Id != 0 || tokenHash == "" {
		t.Fatalf("new session = %+v, hash = %q", session, tokenHash)
	}

	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("response cookies = %d, want 1", len(cookies))
	}
	storedHash, err := util.HashSessionToken(cookies[0].Value)
	if err != nil || storedHash != tokenHash {
		t.Fatal("cookie token does not match stored hash")
	}
	if !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode {
		t.Fatalf("insecure session cookie: %+v", cookies[0])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("database expectations: %v", err)
	}
}

func TestValidCookieLoadsSession(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create mock database: %v", err)
	}
	defer db.Close()

	database.Db = sqlx.NewDb(db, "sqlmock")
	util.Settings.DatabaseType = "postgres"
	token, tokenHash, err := util.NewSessionToken()
	if err != nil {
		t.Fatalf("NewSessionToken: %v", err)
	}
	query := "SELECT id, uuid, user_id, lang, message, expenses_id, last_post_description, message_success FROM sessions WHERE uuid = $1 AND created_at >= $2"
	mock.ExpectQuery(regexp.QuoteMeta(query)).
		WithArgs(tokenHash, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "uuid", "user_id", "lang", "message", "expenses_id", "last_post_description", "message_success"}).
			AddRow(9, tokenHash, 4, "EN", "", 0, "", 0))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: util.SessionCookieName, Value: token})
	recorder := httptest.NewRecorder()
	context := echo.New().NewContext(req, recorder)

	session, actualHash, err := getOrCreateSession(context)
	if err != nil {
		t.Fatalf("getOrCreateSession: %v", err)
	}
	if session.Id != 9 || session.User_id != 4 || actualHash != tokenHash {
		t.Fatalf("loaded session = %+v, hash = %q", session, actualHash)
	}
	if len(recorder.Result().Cookies()) != 0 {
		t.Fatal("valid session unexpectedly replaced its cookie")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("database expectations: %v", err)
	}
}

func TestExpiredCookieCreatesNewSession(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create mock database: %v", err)
	}
	defer db.Close()

	database.Db = sqlx.NewDb(db, "sqlmock")
	util.Settings.DatabaseType = "postgres"
	oldToken, oldHash, err := util.NewSessionToken()
	if err != nil {
		t.Fatalf("NewSessionToken: %v", err)
	}
	query := "SELECT id, uuid, user_id, lang, message, expenses_id, last_post_description, message_success FROM sessions WHERE uuid = $1 AND created_at >= $2"
	mock.ExpectQuery(regexp.QuoteMeta(query)).
		WithArgs(oldHash, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "uuid", "user_id", "lang", "message", "expenses_id", "last_post_description", "message_success"}))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO sessions (uuid) VALUES ($1)")).
		WithArgs(sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: util.SessionCookieName, Value: oldToken})
	recorder := httptest.NewRecorder()
	context := echo.New().NewContext(req, recorder)

	_, newHash, err := getOrCreateSession(context)
	if err != nil {
		t.Fatalf("getOrCreateSession: %v", err)
	}
	if newHash == oldHash {
		t.Fatal("expired session token was not replaced")
	}
	if len(recorder.Result().Cookies()) != 1 {
		t.Fatal("replacement session cookie was not set")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("database expectations: %v", err)
	}
}

func TestSessionDatabaseFailureIsReturned(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create mock database: %v", err)
	}
	defer db.Close()

	database.Db = sqlx.NewDb(db, "sqlmock")
	token, tokenHash, err := util.NewSessionToken()
	if err != nil {
		t.Fatalf("NewSessionToken: %v", err)
	}
	query := "SELECT id, uuid, user_id, lang, message, expenses_id, last_post_description, message_success FROM sessions WHERE uuid = $1 AND created_at >= $2"
	mock.ExpectQuery(regexp.QuoteMeta(query)).
		WithArgs(tokenHash, sqlmock.AnyArg()).
		WillReturnError(errors.New("database unavailable"))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: util.SessionCookieName, Value: token})
	context := echo.New().NewContext(req, httptest.NewRecorder())

	if _, _, err := getOrCreateSession(context); err == nil {
		t.Fatal("getOrCreateSession ignored a database failure")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("database expectations: %v", err)
	}
}

func TestCheckCookieClearsSessionForMissingUser(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create mock database: %v", err)
	}
	defer db.Close()

	database.Db = sqlx.NewDb(db, "sqlmock")
	util.Settings.DatabaseType = "postgres"
	token, tokenHash, err := util.NewSessionToken()
	if err != nil {
		t.Fatalf("NewSessionToken: %v", err)
	}
	sessionQuery := "SELECT id, uuid, user_id, lang, message, expenses_id, last_post_description, message_success FROM sessions WHERE uuid = $1 AND created_at >= $2"
	mock.ExpectQuery(regexp.QuoteMeta(sessionQuery)).
		WithArgs(tokenHash, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "uuid", "user_id", "lang", "message", "expenses_id", "last_post_description", "message_success"}).
			AddRow(9, tokenHash, 404, "EN", "", 0, "", 0))
	userQuery := "SELECT id, name, username, email, default_accounts_id, lang, is_admin FROM users WHERE id = $1 AND email_verified = true AND blocked_at IS NULL"
	mock.ExpectQuery(regexp.QuoteMeta(userQuery)).
		WithArgs(404).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "username", "email", "default_accounts_id", "lang"}))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE sessions SET user_id = $1 WHERE uuid = $2")).
		WithArgs(0, tokenHash).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, code, rate, date FROM currencies WHERE code = $1")).
		WithArgs("EUR").
		WillReturnRows(sqlmock.NewRows([]string{"id", "code", "rate", "date"}).
			AddRow(1, "EUR", 117.2, "2026-09-13 12:00:00"))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: util.SessionCookieName, Value: token})
	context := echo.New().NewContext(req, httptest.NewRecorder())
	context.Set("csrf", "test-token")
	called := false
	handler := CheckCookie(func(c echo.Context) error {
		called = true
		data := c.Get("data").(*util.Data)
		if data.User.Id != 0 || data.Username != "" || c.Get("id") != 0 {
			t.Fatalf("stale session still authenticated: %+v", data.User)
		}
		return nil
	})

	if err := handler(context); err != nil {
		t.Fatalf("CheckCookie: %v", err)
	}
	if !called {
		t.Fatal("next handler was not called")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("database expectations: %v", err)
	}
}
