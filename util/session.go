package util

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"time"
)

const (
	SessionCookieName = "_id"
	SessionDuration   = 30 * 24 * time.Hour
	sessionTokenSize  = 32
)

func NewSessionToken() (token string, tokenHash string, err error) {
	randomBytes := make([]byte, sessionTokenSize)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", "", err
	}

	token = base64.RawURLEncoding.EncodeToString(randomBytes)
	tokenHash = hashSessionTokenBytes(randomBytes)
	return token, tokenHash, nil
}

func HashSessionToken(token string) (string, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(decoded) != sessionTokenSize {
		return "", errors.New("invalid session token")
	}
	return hashSessionTokenBytes(decoded), nil
}

func hashSessionTokenBytes(token []byte) string {
	hash := sha256.Sum256(token)
	return hex.EncodeToString(hash[:])
}

func NewSessionCookie(token string) *http.Cookie {
	return &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  time.Now().Add(SessionDuration),
		MaxAge:   int(SessionDuration.Seconds()),
		Secure:   Settings.CookieSecure,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}
}

func ExpiredSessionCookie() *http.Cookie {
	cookie := NewSessionCookie("")
	cookie.Expires = time.Unix(1, 0)
	cookie.MaxAge = -1
	return cookie
}
