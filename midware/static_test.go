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

func TestStaticFilesSkipSessionAndCSRFMiddleware(t *testing.T) {
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
	serveOK := func(c echo.Context) error { return c.NoContent(http.StatusOK) }
	routes.E.GET("/static/*", serveOK)
	routes.E.GET("/favicon.ico", serveOK)
	routes.E.GET("/ads.txt", serveOK)

	for _, path := range []string{"/static/css/main.css", "/favicon.ico", "/ads.txt"} {
		recorder := httptest.NewRecorder()
		routes.E.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))

		if recorder.Code != http.StatusOK {
			t.Errorf("%s status = %d, want 200", path, recorder.Code)
		}
		if cookies := recorder.Result().Cookies(); len(cookies) != 0 {
			t.Errorf("%s set %d cookies, want none", path, len(cookies))
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unexpected session database access: %v", err)
	}
}

func TestSkipsSession(t *testing.T) {
	tests := map[string]bool{
		routes.HealthPath:     true,
		"/static/js/app.js":   true,
		"/favicon.ico":        true,
		"/ads.txt":            true,
		"/":                   false,
		"/posts":              false,
		"/static":             false,
		"/staticfiles/app.js": false,
		"/favicon.ico/extra":  false,
	}
	for path, want := range tests {
		if got := skipsSession(path); got != want {
			t.Errorf("skipsSession(%q) = %v, want %v", path, got, want)
		}
	}
}
