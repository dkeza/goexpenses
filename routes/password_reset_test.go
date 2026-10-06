package routes

import (
	"crypto/tls"
	"database/sql/driver"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"testing"
	"time"

	"goexpenses/database"
	"goexpenses/util"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo/v4"
	mail "github.com/wneessen/go-mail"
)

type captureStringArgument struct {
	value *string
}

func (argument captureStringArgument) Match(value driver.Value) bool {
	stringValue, ok := value.(string)
	if ok {
		*argument.value = stringValue
	}
	return ok
}

func newPasswordResetMock(t *testing.T) (sqlmock.Sqlmock, func()) {
	t.Helper()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create mock database: %v", err)
	}
	database.Db = sqlx.NewDb(db, "sqlmock")
	database.DatabaseType = "postgres"
	util.Settings.DatabaseType = "postgres"

	return mock, func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("database expectations: %v", err)
		}
		db.Close()
	}
}

func TestCreatePasswordResetStoresOnlyTokenHash(t *testing.T) {
	mock, cleanup := newPasswordResetMock(t)
	defer cleanup()

	const email = "user@example.com"
	var storedToken string
	now := time.Date(2026, time.September, 12, 12, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, email FROM users WHERE lower(btrim(email)) = $1 AND email_verified = true AND blocked_at IS NULL FOR UPDATE")).
		WithArgs(email).
		WillReturnRows(sqlmock.NewRows([]string{"id", "email"}).AddRow(42, email))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) FROM passwordresets WHERE email = $1 AND created_at >= $2")).
		WithArgs(email, now.Add(-passwordResetRequestInterval)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE passwordresets SET done = 1 WHERE email = $1 AND done = 0")).
		WithArgs(email).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO passwordresets (email, token) VALUES ($1, $2)")).
		WithArgs(email, captureStringArgument{value: &storedToken}).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	recipient, token, created, err := createPasswordReset("  USER@EXAMPLE.COM  ", now)
	if err != nil {
		t.Fatalf("createPasswordReset: %v", err)
	}
	if !created || recipient != email || token == "" {
		t.Fatalf("password reset = %q, %q, %v; want recipient, token, true", recipient, token, created)
	}
	wantHash, err := util.HashPasswordResetToken(token)
	if err != nil {
		t.Fatalf("HashPasswordResetToken: %v", err)
	}
	if storedToken != wantHash || storedToken == token {
		t.Fatalf("stored token = %q; want only hash %q", storedToken, wantHash)
	}
}

func TestCreatePasswordResetIsRateLimited(t *testing.T) {
	mock, cleanup := newPasswordResetMock(t)
	defer cleanup()

	const email = "user@example.com"
	now := time.Date(2026, time.September, 12, 12, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, email FROM users WHERE lower(btrim(email)) = $1 AND email_verified = true AND blocked_at IS NULL FOR UPDATE")).
		WithArgs(email).
		WillReturnRows(sqlmock.NewRows([]string{"id", "email"}).AddRow(42, email))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) FROM passwordresets WHERE email = $1 AND created_at >= $2")).
		WithArgs(email, now.Add(-passwordResetRequestInterval)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectRollback()

	_, token, created, err := createPasswordReset(email, now)
	if err != nil {
		t.Fatalf("createPasswordReset: %v", err)
	}
	if created || token != "" {
		t.Fatalf("rate-limited reset created token %q", token)
	}
}

func TestCreatePasswordResetDoesNotRevealUnknownEmail(t *testing.T) {
	mock, cleanup := newPasswordResetMock(t)
	defer cleanup()

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, email FROM users WHERE lower(btrim(email)) = $1 AND email_verified = true AND blocked_at IS NULL FOR UPDATE")).
		WithArgs("unknown@example.com").
		WillReturnRows(sqlmock.NewRows([]string{"id", "email"}))
	mock.ExpectRollback()

	recipient, token, created, err := createPasswordReset("unknown@example.com", time.Now())
	if err != nil {
		t.Fatalf("createPasswordReset: %v", err)
	}
	if created || recipient != "" || token != "" {
		t.Fatalf("unknown email reset = %q, %q, %v; want empty, empty, false", recipient, token, created)
	}
}

func TestResetPasswordConsumesTokenAndDeletesSessions(t *testing.T) {
	mock, cleanup := newPasswordResetMock(t)
	defer cleanup()

	const (
		email        = "user@example.com"
		passwordHash = "new-password-hash"
		userID       = 42
	)
	token, tokenHash, err := util.NewPasswordResetToken()
	if err != nil {
		t.Fatalf("NewPasswordResetToken: %v", err)
	}
	now := time.Date(2026, time.September, 12, 12, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, email FROM passwordresets WHERE token = $1 AND created_at >= $2 AND done = 0 FOR UPDATE")).
		WithArgs(tokenHash, now.Add(-passwordResetDuration)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "email"}).AddRow(7, email))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id FROM users WHERE email = $1 AND blocked_at IS NULL")).
		WithArgs(email).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(userID))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE passwordresets SET done = 1 WHERE token = $1 AND done = 0")).
		WithArgs(tokenHash).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE passwordresets SET done = 1 WHERE email = $1 AND done = 0")).
		WithArgs(email).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE users SET password = $1 WHERE id = $2")).
		WithArgs(passwordHash, userID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM sessions WHERE user_id = $1")).
		WithArgs(userID).
		WillReturnResult(sqlmock.NewResult(0, 3))
	mock.ExpectCommit()

	changed, err := resetPasswordWithToken(token, passwordHash, now)
	if err != nil {
		t.Fatalf("resetPasswordWithToken: %v", err)
	}
	if !changed {
		t.Fatal("resetPasswordWithToken rejected a valid token")
	}
}

func TestResetPasswordRejectsConsumedToken(t *testing.T) {
	mock, cleanup := newPasswordResetMock(t)
	defer cleanup()

	token, tokenHash, err := util.NewPasswordResetToken()
	if err != nil {
		t.Fatalf("NewPasswordResetToken: %v", err)
	}
	now := time.Date(2026, time.September, 12, 12, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, email FROM passwordresets WHERE token = $1 AND created_at >= $2 AND done = 0 FOR UPDATE")).
		WithArgs(tokenHash, now.Add(-passwordResetDuration)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "email"}))
	mock.ExpectRollback()

	changed, err := resetPasswordWithToken(token, "unused-password-hash", now)
	if err != nil {
		t.Fatalf("resetPasswordWithToken: %v", err)
	}
	if changed {
		t.Fatal("resetPasswordWithToken accepted an already consumed token")
	}
}

func TestMailTLSConfigVerifiesCertificate(t *testing.T) {
	previous := util.Settings
	t.Cleanup(func() { util.Settings = previous })
	util.Settings.MailHost = "smtp.example.com"

	config := mailTLSConfig()
	if config.InsecureSkipVerify {
		t.Fatal("mail TLS configuration disables certificate verification")
	}
	if config.ServerName != util.Settings.MailHost {
		t.Fatalf("TLS server name = %q, want %q", config.ServerName, util.Settings.MailHost)
	}
	if config.MinVersion != tls.VersionTLS12 {
		t.Fatalf("TLS minimum version = %x, want TLS 1.2", config.MinVersion)
	}
}

func TestMailClientTLSPolicyFollowsPort(t *testing.T) {
	previous := util.Settings
	t.Cleanup(func() { util.Settings = previous })
	util.Settings.MailHost = "smtp.example.com"
	util.Settings.MailFrom = "app@example.com"

	for _, test := range []struct {
		port   int
		policy string
	}{
		{587, "TLSOpportunistic"},
		{25, "TLSOpportunistic"},
	} {
		util.Settings.MailHostPort = test.port
		client, err := mail.NewClient(util.Settings.MailHost, mailClientOptions()...)
		if err != nil {
			t.Fatalf("port %d: NewClient: %v", test.port, err)
		}
		if got := client.TLSPolicy(); got != test.policy {
			t.Errorf("port %d: TLS policy = %q, want %q", test.port, got, test.policy)
		}
		if got := client.ServerAddr(); got != "smtp.example.com:"+strconv.Itoa(test.port) {
			t.Errorf("port %d: server address = %q", test.port, got)
		}
	}
}

func TestResetPasswordPageHidesTokenFromThirdParties(t *testing.T) {
	mock, _ := useMockRouteDatabase(t)
	token, tokenHash, err := util.NewPasswordResetToken()
	if err != nil {
		t.Fatalf("NewPasswordResetToken: %v", err)
	}
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id FROM passwordresets WHERE token = $1 AND created_at >= $2 AND done = 0")).
		WithArgs(tokenHash, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(7))

	previousEcho := E
	E = echo.New()
	E.Renderer = discardRenderer{}
	t.Cleanup(func() { E = previousEcho })
	data := &util.Data{}
	E.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Set("data", data)
			return next(c)
		}
	})
	DefinePasswordRoutes()

	recorder := httptest.NewRecorder()
	E.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/resetpassword?t="+token, nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	if !data.HideThirdPartyScripts || data.Token != token {
		t.Fatalf("page data = hide %v, token set %v", data.HideThirdPartyScripts, data.Token == token)
	}
	if got := recorder.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Errorf("Referrer-Policy = %q", got)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q", got)
	}
}
