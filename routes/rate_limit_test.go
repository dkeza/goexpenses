package routes

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

func TestRequestLimiterBlocksUntilWindowExpires(t *testing.T) {
	limiter := newRequestLimiter()
	now := time.Date(2026, time.September, 22, 12, 0, 0, 0, time.UTC)
	rule := rateLimitRule{key: "login:user", limit: 2, window: time.Minute}

	for attempt := 0; attempt < rule.limit; attempt++ {
		allowed, retryAfter := limiter.allow(now.Add(time.Duration(attempt)*time.Second), rule)
		if !allowed || retryAfter != 0 {
			t.Fatalf("attempt %d = %v, %v; want allowed", attempt+1, allowed, retryAfter)
		}
	}

	allowed, retryAfter := limiter.allow(now.Add(2*time.Second), rule)
	if allowed || retryAfter != 58*time.Second {
		t.Fatalf("blocked attempt = %v, %v; want false, 58s", allowed, retryAfter)
	}

	// A rejected retry does not move the end of the existing block.
	allowed, retryAfter = limiter.allow(now.Add(30*time.Second), rule)
	if allowed || retryAfter != 30*time.Second {
		t.Fatalf("repeated blocked attempt = %v, %v; want false, 30s", allowed, retryAfter)
	}

	allowed, retryAfter = limiter.allow(now.Add(time.Minute), rule)
	if !allowed || retryAfter != 0 {
		t.Fatalf("attempt after window = %v, %v; want allowed", allowed, retryAfter)
	}
}

func TestRequestLimiterChecksRulesAtomically(t *testing.T) {
	limiter := newRequestLimiter()
	now := time.Now()
	ipRule := rateLimitRule{key: "login:ip", limit: 1, window: time.Hour}
	firstAccount := rateLimitRule{key: "login:first", limit: 5, window: time.Hour}
	secondAccount := rateLimitRule{key: "login:second", limit: 5, window: time.Hour}

	if allowed, _ := limiter.allow(now, ipRule, firstAccount); !allowed {
		t.Fatal("first request was unexpectedly blocked")
	}
	if allowed, _ := limiter.allow(now.Add(time.Second), ipRule, secondAccount); allowed {
		t.Fatal("request exceeding the IP limit was allowed")
	}
	if _, exists := limiter.attempts[secondAccount.key]; exists {
		t.Fatal("a rejected request consumed the account-specific allowance")
	}
}

func TestRequestLimiterEnforcesLimitConcurrently(t *testing.T) {
	limiter := newRequestLimiter()
	now := time.Now()
	rule := rateLimitRule{key: "login:concurrent", limit: 7, window: time.Minute}
	var allowedCount int32
	var workers sync.WaitGroup

	for range 50 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if allowed, _ := limiter.allow(now, rule); allowed {
				atomic.AddInt32(&allowedCount, 1)
			}
		}()
	}
	workers.Wait()

	if allowedCount != int32(rule.limit) {
		t.Fatalf("concurrent allowed count = %d, want %d", allowedCount, rule.limit)
	}
}

func TestRequestLimiterResetAndCleanup(t *testing.T) {
	limiter := newRequestLimiter()
	now := time.Now()
	rule := rateLimitRule{key: "login:user", limit: 1, window: time.Hour}

	limiter.allow(now, rule)
	limiter.reset(rule.key)
	if allowed, _ := limiter.allow(now, rule); !allowed {
		t.Fatal("reset key remained blocked")
	}

	limiter.cleanup(now.Add(2 * time.Hour))
	if len(limiter.attempts) != 0 {
		t.Fatalf("cleanup retained %d stale keys", len(limiter.attempts))
	}
}

func TestRateLimitIdentifierKeyNormalizesAndHidesValue(t *testing.T) {
	first := rateLimitIdentifierKey("login", "username", "  Test.User ")
	second := rateLimitIdentifierKey("login", "username", "test.user")
	if first != second {
		t.Fatalf("normalized keys differ: %q, %q", first, second)
	}
	if strings.Contains(first, "test.user") {
		t.Fatalf("rate limit key exposes identifier: %q", first)
	}
}

func TestClientIPExtractorTrustsOnlyLoopbackProxy(t *testing.T) {
	e := echo.New()
	e.IPExtractor = clientIPExtractor()

	directRequest := httptest.NewRequest(http.MethodPost, "/auth", nil)
	directRequest.RemoteAddr = "203.0.113.10:1234"
	directRequest.Header.Set(echo.HeaderXForwardedFor, "198.51.100.20")
	if got := e.NewContext(directRequest, httptest.NewRecorder()).RealIP(); got != "203.0.113.10" {
		t.Fatalf("direct request IP = %q, want network peer", got)
	}

	proxiedRequest := httptest.NewRequest(http.MethodPost, "/auth", nil)
	proxiedRequest.RemoteAddr = "127.0.0.1:1234"
	proxiedRequest.Header.Set(echo.HeaderXForwardedFor, "198.51.100.20")
	if got := e.NewContext(proxiedRequest, httptest.NewRecorder()).RealIP(); got != "198.51.100.20" {
		t.Fatalf("proxied request IP = %q, want forwarded client", got)
	}
}

func TestRateLimitMiddlewareReturnsRetryAfter(t *testing.T) {
	limiter := newRequestLimiter()
	e := echo.New()
	form := url.Values{"username": {"limited-user"}}
	rules := func(c echo.Context) []rateLimitRule {
		return []rateLimitRule{{key: rateLimitIdentifierKey("test", "username", c.FormValue("username")), limit: 1, window: time.Minute}}
	}
	handler := rateLimitMiddleware(limiter, rules)(func(c echo.Context) error {
		return c.NoContent(http.StatusNoContent)
	})

	firstRequest := httptest.NewRequest(http.MethodPost, "/auth", strings.NewReader(form.Encode()))
	firstRequest.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	if err := handler(e.NewContext(firstRequest, httptest.NewRecorder())); err != nil {
		t.Fatalf("first request: %v", err)
	}

	recorder := httptest.NewRecorder()
	secondRequest := httptest.NewRequest(http.MethodPost, "/auth", strings.NewReader(form.Encode()))
	secondRequest.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	err := handler(e.NewContext(secondRequest, recorder))
	var httpError *echo.HTTPError
	if !errors.As(err, &httpError) || httpError.Code != http.StatusTooManyRequests {
		t.Fatalf("second request error = %v; want HTTP 429", err)
	}
	retryAfter, conversionErr := strconv.Atoi(recorder.Header().Get("Retry-After"))
	if conversionErr != nil || retryAfter < 1 || retryAfter > 60 {
		t.Fatalf("Retry-After = %q; want 1..60 seconds", recorder.Header().Get("Retry-After"))
	}
}
