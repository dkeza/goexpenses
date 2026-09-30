package midware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"goexpenses/database"
	"goexpenses/routes"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo/v4"
)

func TestOversizedRequestBodyIsRejected(t *testing.T) {
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
	routes.E.POST("/posts/save", func(c echo.Context) error {
		t.Error("handler ran for an oversized request body")
		return c.NoContent(http.StatusOK)
	})

	body := "description=" + strings.Repeat("a", 65*1024)
	request := httptest.NewRequest(http.MethodPost, "/posts/save", strings.NewReader(body))
	request.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	recorder := httptest.NewRecorder()
	routes.E.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", recorder.Code)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unexpected database access: %v", err)
	}
}
