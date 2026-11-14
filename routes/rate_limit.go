package routes

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"goexpenses/database"
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
	twoFactorRateLimitWindow      = 15 * time.Minute
	twoFactorIPRateLimit          = 30
	twoFactorUserRateLimit        = 10
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

// loginAccountKey limits password guesses for one user name from one client
// address. Keying by user name alone would let anyone who knows a user name
// lock that user out of signing in.
func loginAccountKey(c echo.Context, username string) string {
	return rateLimitIdentifierKey("login", "username", username) + ":ip:" + c.RealIP()
}

func loginRateLimitRules(c echo.Context) []rateLimitRule {
	return []rateLimitRule{
		{key: rateLimitIPKey("login", c), limit: loginIPRateLimit, window: loginRateLimitWindow},
		{key: loginAccountKey(c, c.FormValue("username")), limit: loginAccountRateLimit, window: loginRateLimitWindow},
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

// twoFactorLoginRateLimitRules limits second-factor attempts per client
// address and per user. The user comes from the server-side pending state of
// the session, not from the form or the address, so the limit also holds
// across new sessions and repeated password sign-ins.
func twoFactorLoginRateLimitRules(c echo.Context) []rateLimitRule {
	rules := []rateLimitRule{
		{key: rateLimitIPKey("2fa", c), limit: twoFactorIPRateLimit, window: twoFactorRateLimitWindow},
	}
	sessionHash, _ := c.Get("_id").(string)
	if pending, err := loadPendingLogin(database.Db, pendingLoginQuery, sessionHash); err == nil && pending.UserID != nil {
		rules = append(rules, rateLimitRule{key: "2fa:user:" + strconv.Itoa(*pending.UserID), limit: twoFactorUserRateLimit, window: twoFactorRateLimitWindow})
	}
	return rules
}

// twoFactorManageRateLimitRules limits password and code guesses on the
// pages that change a signed-in user's second factor.
func twoFactorManageRateLimitRules(c echo.Context) []rateLimitRule {
	userID := 0
	if data, ok := c.Get("data").(*util.Data); ok {
		userID = data.User.Id
	}
	return []rateLimitRule{
		{key: "2fa-manage:user:" + strconv.Itoa(userID), limit: twoFactorUserRateLimit, window: twoFactorRateLimitWindow},
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
	case "/accounts/members/invite":
		return "/accounts/members"
	case "/login/2fa":
		return "/login/2fa"
	case "/account/2fa/enable", "/account/2fa/disable", "/account/2fa/recovery-codes":
		return "/account/2fa"
	case "/admin/users/:id/2fa/reset":
		return "/admin"
	default:
		return "/login"
	}
}

var publicRequestLimiter = newRequestLimiter()

// ResetRateLimits forgets all counted requests. Tests that drive the whole
// application use it, because users of separate test databases share IDs.
func ResetRateLimits() {
	publicRequestLimiter.mu.Lock()
	defer publicRequestLimiter.mu.Unlock()
	clear(publicRequestLimiter.attempts)
}
