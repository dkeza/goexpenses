package midware

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
