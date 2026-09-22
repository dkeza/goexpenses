package midware

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"

	"goexpenses/util"

	"github.com/labstack/echo/v4"
)

const (
	cspNonceContextKey = "csp_nonce"
	cspNonceSize       = 16
)

func newCSPNonce() (string, error) {
	randomBytes := make([]byte, cspNonceSize)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", fmt.Errorf("generate CSP nonce: %w", err)
	}
	return base64.RawStdEncoding.EncodeToString(randomBytes), nil
}

// SecurityHeaders protects browser responses while keeping Google Analytics
// and AdSense compatible with Google's nonce-based strict CSP guidance.
func SecurityHeaders(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		nonce, err := newCSPNonce()
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "could not secure response").SetInternal(err)
		}
		c.Set(cspNonceContextKey, nonce)

		headers := c.Response().Header()
		headers.Set("Content-Security-Policy", fmt.Sprintf(
			"object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'; script-src 'nonce-%s' 'unsafe-inline' 'unsafe-eval' 'strict-dynamic' https: http:",
			nonce,
		))
		headers.Set("X-Content-Type-Options", "nosniff")
		headers.Set("X-Frame-Options", "DENY")
		headers.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		headers.Set("Permissions-Policy", "camera=(), geolocation=(), microphone=()")
		if util.Settings.CookieSecure {
			headers.Set("Strict-Transport-Security", "max-age=31536000")
		}

		return next(c)
	}
}
