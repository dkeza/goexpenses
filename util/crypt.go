package util

import (
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"fmt"
	"log"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

func CreateUUID() (uuid string) {
	u := new([16]byte)
	_, err := rand.Read(u[:])
	if err != nil {
		log.Fatalln("Cannot generate UUID", err)
	}

	// 0x40 is reserved variant from RFC 4122
	u[8] = (u[8] | 0x40) & 0x7F
	// Set the four most significant bits (bits 12 through 15) of the
	// time_hi_and_version field to the 4-bit version number.
	u[6] = (u[6] & 0xF) | (0x4 << 4)
	uuid = fmt.Sprintf("%x-%x-%x-%x-%x", u[0:4], u[4:6], u[6:8], u[8:10], u[10:])
	return
}

// Encrypt returns a legacy SHA-1 identifier. It must not be used for passwords.
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
