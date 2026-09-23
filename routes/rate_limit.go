package routes

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"goexpenses/util"

	"github.com/labstack/echo/v4"
)

const (
	loginRateLimitWindow          = 15 * time.Minute
	loginIPRateLimit              = 30
	loginAccountRateLimit         = 5
	passwordResetRateLimitWindow  = time.Hour
	passwordResetIPRateLimit      = 10
	passwordResetAccountRateLimit = 3
	registrationRateLimitWindow   = time.Hour
	registrationIPRateLimit       = 5
	registrationAccountRateLimit  = 3
	rateLimitCleanupInterval      = 10 * time.Minute
)

type rateLimitRule struct {
	key    string
	limit  int
	window time.Duration
}

type requestLimiter struct {
	mu          sync.Mutex
	attempts    map[string][]time.Time
	nextCleanup time.Time
}

func newRequestLimiter() *requestLimiter {
	return &requestLimiter{attempts: make(map[string][]time.Time)}
}

// allow atomically checks and records all rules. A rejected request is not
// recorded again, so repeatedly retrying while blocked cannot extend a block.
func (limiter *requestLimiter) allow(now time.Time, rules ...rateLimitRule) (bool, time.Duration) {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()

	if limiter.nextCleanup.IsZero() || !now.Before(limiter.nextCleanup) {
		limiter.cleanup(now)
		limiter.nextCleanup = now.Add(rateLimitCleanupInterval)
	}

	retryAfter := time.Duration(0)
	for _, rule := range rules {
		attempts := pruneRateLimitAttempts(limiter.attempts[rule.key], now.Add(-rule.window))
		if len(attempts) == 0 {
			delete(limiter.attempts, rule.key)
		} else {
			limiter.attempts[rule.key] = attempts
		}
		if len(attempts) >= rule.limit {
			retry := attempts[0].Add(rule.window).Sub(now)
			if retry > retryAfter {
				retryAfter = retry
			}
		}
	}
	if retryAfter > 0 {
		return false, retryAfter
	}

	for _, rule := range rules {
		limiter.attempts[rule.key] = append(limiter.attempts[rule.key], now)
	}
	return true, 0
}

func (limiter *requestLimiter) reset(keys ...string) {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	for _, key := range keys {
		delete(limiter.attempts, key)
	}
}

func (limiter *requestLimiter) cleanup(now time.Time) {
	for key, attempts := range limiter.attempts {
		// All endpoint windows are at most one hour. Rules also prune with their
		// exact window when they are checked.
		attempts = pruneRateLimitAttempts(attempts, now.Add(-time.Hour))
		if len(attempts) == 0 {
			delete(limiter.attempts, key)
		} else {
			limiter.attempts[key] = attempts
		}
	}
}

func pruneRateLimitAttempts(attempts []time.Time, cutoff time.Time) []time.Time {
	firstCurrent := 0
	for firstCurrent < len(attempts) && !attempts[firstCurrent].After(cutoff) {
		firstCurrent++
	}
	return attempts[firstCurrent:]
}

func rateLimitIdentifierKey(endpoint, field, value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	digest := sha256.Sum256([]byte(normalized))
	return endpoint + ":" + field + ":" + hex.EncodeToString(digest[:])
}

func rateLimitIPKey(endpoint string, c echo.Context) string {
	return endpoint + ":ip:" + c.RealIP()
}

func clientIPExtractor() echo.IPExtractor {
	// Production traffic arrives through cloudflared on loopback. Only trust
	// forwarded addresses from that local proxy, not from direct clients.
	return echo.ExtractIPFromXFFHeader(
		echo.TrustLoopback(true),
		echo.TrustLinkLocal(false),
		echo.TrustPrivateNet(false),
	)
}

func loginRateLimitRules(c echo.Context) []rateLimitRule {
	return []rateLimitRule{
		{key: rateLimitIPKey("login", c), limit: loginIPRateLimit, window: loginRateLimitWindow},
		{key: rateLimitIdentifierKey("login", "username", c.FormValue("username")), limit: loginAccountRateLimit, window: loginRateLimitWindow},
	}
}

func passwordResetRateLimitRules(c echo.Context) []rateLimitRule {
	return []rateLimitRule{
		{key: rateLimitIPKey("password-reset", c), limit: passwordResetIPRateLimit, window: passwordResetRateLimitWindow},
		{key: rateLimitIdentifierKey("password-reset", "email", c.FormValue("email")), limit: passwordResetAccountRateLimit, window: passwordResetRateLimitWindow},
	}
}

func registrationRateLimitRules(c echo.Context) []rateLimitRule {
	return []rateLimitRule{
		{key: rateLimitIPKey("registration", c), limit: registrationIPRateLimit, window: registrationRateLimitWindow},
		{key: rateLimitIdentifierKey("registration", "email", c.FormValue("email")), limit: registrationAccountRateLimit, window: registrationRateLimitWindow},
		{key: rateLimitIdentifierKey("registration", "username", c.FormValue("username")), limit: registrationAccountRateLimit, window: registrationRateLimitWindow},
	}
}

func verificationRateLimitRules(c echo.Context) []rateLimitRule {
	return []rateLimitRule{
		{key: rateLimitIPKey("verification", c), limit: passwordResetIPRateLimit, window: passwordResetRateLimitWindow},
		{key: rateLimitIdentifierKey("verification", "email", c.FormValue("email")), limit: passwordResetAccountRateLimit, window: passwordResetRateLimitWindow},
	}
}

func rateLimitMiddleware(limiter *requestLimiter, rules func(echo.Context) []rateLimitRule) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			allowed, retryAfter := limiter.allow(time.Now(), rules(c)...)
			if !allowed {
				seconds := int((retryAfter + time.Second - 1) / time.Second)
				c.Response().Header().Set("Retry-After", strconv.Itoa(seconds))
				if data, ok := c.Get("data").(*util.Data); ok && c.Echo().Renderer != nil {
					data.RateLimitRetryAfter = seconds
					data.RateLimitBackURL = rateLimitBackURL(c.Path())
					return c.Render(http.StatusTooManyRequests, "rate-limit", data)
				}
				return echo.NewHTTPError(http.StatusTooManyRequests, "too many requests; please try again later")
			}
			return next(c)
		}
	}
}

func rateLimitBackURL(path string) string {
	switch path {
	case "/register":
		return "/register"
	case "/resend-verification":
		return "/resend-verification"
	case "/reset":
		return "/reset"
	default:
		return "/login"
	}
}

var publicRequestLimiter = newRequestLimiter()
