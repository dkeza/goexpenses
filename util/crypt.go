package util

import (
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

const publicIDByteSize = 20

// NewPublicID returns a cryptographically random, URL-safe identifier while
// preserving the 40-character hexadecimal format used by existing records.
func NewPublicID() (string, error) {
	return newPublicID(rand.Reader)
}

func newPublicID(random io.Reader) (string, error) {
	value := make([]byte, publicIDByteSize)
	if _, err := io.ReadFull(random, value); err != nil {
		return "", fmt.Errorf("generate public ID: %w", err)
	}
	return hex.EncodeToString(value), nil
}

// Encrypt returns a legacy SHA-1 password hash. It is retained only to migrate
// existing passwords and must not be used for new credentials or identifiers.
func Encrypt(plaintext string) (cryptext string) {
	cryptext = fmt.Sprintf("%x", sha1.Sum([]byte(plaintext)))
	return
}

// HashPassword creates a salted, adaptive password hash.
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// VerifyPassword accepts bcrypt hashes and legacy SHA-1 hashes. The second
// return value reports whether a valid password should be rehashed.
func VerifyPassword(storedHash, password string) (valid bool, needsRehash bool) {
	if strings.HasPrefix(storedHash, "$2") {
		if bcrypt.CompareHashAndPassword([]byte(storedHash), []byte(password)) != nil {
			return false, false
		}
		cost, err := bcrypt.Cost([]byte(storedHash))
		return true, err == nil && cost < bcrypt.DefaultCost
	}

	legacyHash := Encrypt(password)
	valid = subtle.ConstantTimeCompare([]byte(storedHash), []byte(legacyHash)) == 1
	return valid, valid
}
