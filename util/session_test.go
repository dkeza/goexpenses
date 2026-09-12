package util

import (
	"net/http"
	"testing"
)

func TestSessionToken(t *testing.T) {
	token, tokenHash, err := NewSessionToken()
	if err != nil {
		t.Fatalf("NewSessionToken: %v", err)
	}
	if token == tokenHash {
		t.Fatal("raw session token must not equal its stored hash")
	}

	calculatedHash, err := HashSessionToken(token)
	if err != nil {
		t.Fatalf("HashSessionToken: %v", err)
	}
	if calculatedHash != tokenHash {
		t.Fatalf("session hash = %q, want %q", calculatedHash, tokenHash)
	}
	if _, err := HashSessionToken("invalid"); err == nil {
		t.Fatal("HashSessionToken accepted an invalid token")
	}
}

func TestSessionCookieSecurityAttributes(t *testing.T) {
	Settings.CookieSecure = true
	cookie := NewSessionCookie("token")

	if !cookie.Secure || !cookie.HttpOnly {
		t.Fatalf("cookie Secure/HttpOnly = %v/%v, want true/true", cookie.Secure, cookie.HttpOnly)
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("cookie SameSite = %v, want Lax", cookie.SameSite)
	}
	if cookie.Path != "/" || cookie.MaxAge != int(SessionDuration.Seconds()) {
		t.Fatalf("cookie Path/MaxAge = %q/%d", cookie.Path, cookie.MaxAge)
	}
}

func TestExpiredSessionCookie(t *testing.T) {
	Settings.CookieSecure = true
	cookie := ExpiredSessionCookie()
	if cookie.MaxAge != -1 || cookie.Expires.Unix() != 1 {
		t.Fatalf("session cookie was not expired: %+v", cookie)
	}
}
