package midware

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"goexpenses/routes"
	"goexpenses/util"

	"github.com/labstack/echo/v4"
)

func TestSecurityHeadersUsesUniqueCSPNonce(t *testing.T) {
	originalCookieSecure := util.Settings.CookieSecure
	util.Settings.CookieSecure = true
	t.Cleanup(func() {
		util.Settings.CookieSecure = originalCookieSecure
	})

	e := echo.New()
	e.Use(SecurityHeaders)
	e.GET("/", func(c echo.Context) error {
		nonce, _ := c.Get(cspNonceContextKey).(string)
		return c.String(http.StatusOK, nonce)
	})

	var previousNonce string
	for requestNumber := 1; requestNumber <= 2; requestNumber++ {
		recorder := httptest.NewRecorder()
		e.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

		if recorder.Code != http.StatusOK {
			t.Fatalf("request %d status = %d, want 200", requestNumber, recorder.Code)
		}
		nonce := recorder.Body.String()
		decoded, err := base64.RawStdEncoding.DecodeString(nonce)
		if err != nil || len(decoded) != cspNonceSize {
			t.Fatalf("request %d nonce = %q, want %d random bytes", requestNumber, nonce, cspNonceSize)
		}
		if nonce == previousNonce {
			t.Fatal("consecutive responses reused the CSP nonce")
		}
		previousNonce = nonce

		headers := recorder.Header()
		policy := headers.Get("Content-Security-Policy")
		for _, expected := range []string{
			"script-src 'nonce-" + nonce + "'",
			"'strict-dynamic'",
			"object-src 'none'",
			"base-uri 'none'",
			"frame-ancestors 'none'",
			"form-action 'self'",
		} {
			if !strings.Contains(policy, expected) {
				t.Errorf("CSP %q does not contain %q", policy, expected)
			}
		}
		assertSecurityHeader(t, headers, "X-Content-Type-Options", "nosniff")
		assertSecurityHeader(t, headers, "X-Frame-Options", "DENY")
		assertSecurityHeader(t, headers, "Referrer-Policy", "strict-origin-when-cross-origin")
		assertSecurityHeader(t, headers, "Permissions-Policy", "camera=(), geolocation=(), microphone=()")
		assertSecurityHeader(t, headers, "Strict-Transport-Security", "max-age=31536000")
	}
}

func TestSecurityHeadersOmitsHSTSForInsecureDevelopment(t *testing.T) {
	originalCookieSecure := util.Settings.CookieSecure
	util.Settings.CookieSecure = false
	t.Cleanup(func() {
		util.Settings.CookieSecure = originalCookieSecure
	})

	e := echo.New()
	e.Use(SecurityHeaders)
	e.GET("/", func(c echo.Context) error {
		return c.NoContent(http.StatusNoContent)
	})
	recorder := httptest.NewRecorder()
	e.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

	if got := recorder.Header().Get("Strict-Transport-Security"); got != "" {
		t.Fatalf("insecure development response has HSTS %q", got)
	}
	if recorder.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("insecure development response is missing CSP")
	}
}

func TestSecurityHeadersWrapCSRFFailures(t *testing.T) {
	previousEcho := routes.E
	routes.E = echo.New()
	t.Cleanup(func() {
		routes.E = previousEcho
	})

	SetMiddleware()
	routes.E.POST("/protected", func(c echo.Context) error {
		return c.NoContent(http.StatusNoContent)
	})

	recorder := httptest.NewRecorder()
	routes.E.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/protected", nil))

	if recorder.Code < 400 {
		t.Fatalf("request without CSRF token returned %d, want an error", recorder.Code)
	}
	if recorder.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("CSRF failure is missing security headers")
	}
}

func assertSecurityHeader(t *testing.T, headers http.Header, name, want string) {
	t.Helper()
	if got := headers.Get(name); got != want {
		t.Errorf("%s = %q, want %q", name, got, want)
	}
}
