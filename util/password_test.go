package util

import (
	"strings"
	"testing"
)

func TestPasswordHashAndVerification(t *testing.T) {
	const password = "correct horse battery staple"

	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if hash == password || !strings.HasPrefix(hash, "$2") {
		t.Fatalf("HashPassword returned an invalid bcrypt hash")
	}

	valid, needsRehash := VerifyPassword(hash, password)
	if !valid || needsRehash {
		t.Fatalf("VerifyPassword(valid bcrypt) = %v, %v; want true, false", valid, needsRehash)
	}

	valid, needsRehash = VerifyPassword(hash, "wrong password")
	if valid || needsRehash {
		t.Fatalf("VerifyPassword(wrong password) = %v, %v; want false, false", valid, needsRehash)
	}
}

func TestLegacyPasswordRequiresRehash(t *testing.T) {
	const password = "legacy password"
	legacyHash := Encrypt(password)

	valid, needsRehash := VerifyPassword(legacyHash, password)
	if !valid || !needsRehash {
		t.Fatalf("VerifyPassword(legacy hash) = %v, %v; want true, true", valid, needsRehash)
	}

	valid, needsRehash = VerifyPassword(legacyHash, "wrong password")
	if valid || needsRehash {
		t.Fatalf("VerifyPassword(wrong legacy password) = %v, %v; want false, false", valid, needsRehash)
	}
}
