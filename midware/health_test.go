package midware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"goexpenses/database"
	"goexpenses/routes"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo/v4"
)

func TestHealthCheckSkipsSessionAndCSRFMiddleware(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create mock database: %v", err)
	}
	previousDB := database.Db
	previousEcho := routes.E
	database.Db = sqlx.NewDb(db, "sqlmock")
	routes.E = echo.New()
	t.Cleanup(func() {
		database.Db = previousDB
		routes.E = previousEcho
		db.Close()
	})

	SetMiddleware()
	routes.E.GET(routes.HealthPath, func(c echo.Context) error {
		return c.NoContent(http.StatusOK)
	})

	recorder := httptest.NewRecorder()
	routes.E.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, routes.HealthPath, nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("health check status = %d, want 200", recorder.Code)
	}
	if cookies := recorder.Result().Cookies(); len(cookies) != 0 {
		t.Fatalf("health check set %d cookies, want none", len(cookies))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unexpected session database access: %v", err)
	}
}
