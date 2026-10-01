package midware

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

func serveWithRequestLogger(t *testing.T, target string, handler echo.HandlerFunc) (int, string) {
	t.Helper()
	var output bytes.Buffer
	e := echo.New()
	e.Use(RequestLogger(slog.New(slog.NewTextHandler(&output, nil))))
	e.Use(middleware.Recover())
	e.GET("/target", handler)

	recorder := httptest.NewRecorder()
	e.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))
	return recorder.Code, output.String()
}

func TestRequestLoggerLogsServerErrorWithoutQuery(t *testing.T) {
	status, logged := serveWithRequestLogger(t, "/target?t=secret-token", func(c echo.Context) error {
		return echo.NewHTTPError(http.StatusInternalServerError, "could not load data").SetInternal(errors.New("database unavailable"))
	})

	if status != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", status)
	}
	for _, want := range []string{"level=ERROR", "path=/target", "status=500", "database unavailable"} {
		if !strings.Contains(logged, want) {
			t.Errorf("log %q does not contain %q", logged, want)
		}
	}
	if strings.Contains(logged, "secret-token") {
		t.Errorf("log leaks the query string: %q", logged)
	}
}

func TestRequestLoggerLogsRecoveredPanic(t *testing.T) {
	status, logged := serveWithRequestLogger(t, "/target", func(c echo.Context) error {
		panic("template failed")
	})

	if status != http.StatusInternalServerError || !strings.Contains(logged, "status=500") {
		t.Fatalf("status = %d, log = %q", status, logged)
	}
}

func TestRequestLoggerSkipsFastAndClientErrorRequests(t *testing.T) {
	for _, handler := range []echo.HandlerFunc{
		func(c echo.Context) error { return c.NoContent(http.StatusOK) },
		func(c echo.Context) error { return echo.NewHTTPError(http.StatusNotFound, "record not found") },
	} {
		if _, logged := serveWithRequestLogger(t, "/target", handler); logged != "" {
			t.Errorf("unexpected log entry: %q", logged)
		}
	}
}
