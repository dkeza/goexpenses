package routes

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"goexpenses/database"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo/v4"
)

func useHealthCheckDatabase(t *testing.T) sqlmock.Sqlmock {
	t.Helper()
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	if err != nil {
		t.Fatalf("create mock database: %v", err)
	}
	previousDB := database.Db
	database.Db = sqlx.NewDb(db, "sqlmock")
	t.Cleanup(func() {
		database.Db = previousDB
		db.Close()
	})
	return mock
}

func TestHealthCheckReturnsOKWhenDatabaseResponds(t *testing.T) {
	mock := useHealthCheckDatabase(t)
	mock.ExpectPing()

	recorder := httptest.NewRecorder()
	context := echo.New().NewContext(
		httptest.NewRequest(http.MethodGet, HealthPath, nil),
		recorder,
	)

	if err := healthCheck(context); err != nil {
		t.Fatalf("healthCheck: %v", err)
	}
	if recorder.Code != http.StatusOK || recorder.Body.String() != "ok\n" {
		t.Fatalf("health check response = %d %q, want 200 %q", recorder.Code, recorder.Body.String(), "ok\n")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("database expectations: %v", err)
	}
}

func TestHealthCheckReturnsUnavailableWithoutDatabaseDetails(t *testing.T) {
	mock := useHealthCheckDatabase(t)
	mock.ExpectPing().WillReturnError(errors.New("secret database failure"))

	recorder := httptest.NewRecorder()
	context := echo.New().NewContext(
		httptest.NewRequest(http.MethodGet, HealthPath, nil),
		recorder,
	)

	if err := healthCheck(context); err != nil {
		t.Fatalf("healthCheck: %v", err)
	}
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("health check status = %d, want 503", recorder.Code)
	}
	if recorder.Body.String() != "service unavailable\n" {
		t.Fatalf("health check exposed unexpected response %q", recorder.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("database expectations: %v", err)
	}
}

func TestHealthCheckReturnsUnavailableBeforeDatabaseInitialization(t *testing.T) {
	previousDB := database.Db
	database.Db = nil
	t.Cleanup(func() { database.Db = previousDB })

	recorder := httptest.NewRecorder()
	context := echo.New().NewContext(
		httptest.NewRequest(http.MethodGet, HealthPath, nil),
		recorder,
	)

	if err := healthCheck(context); err != nil {
		t.Fatalf("healthCheck: %v", err)
	}
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("health check status = %d, want 503", recorder.Code)
	}
}
