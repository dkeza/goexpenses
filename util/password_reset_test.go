package util

import "testing"

func TestPasswordResetToken(t *testing.T) {
	token, tokenHash, err := NewPasswordResetToken()
	if err != nil {
		t.Fatalf("NewPasswordResetToken: %v", err)
	}
	if token == tokenHash {
		t.Fatal("raw password reset token must not equal its stored hash")
	}

	calculatedHash, err := HashPasswordResetToken(token)
	if err != nil {
		t.Fatalf("HashPasswordResetToken: %v", err)
	}
	if calculatedHash != tokenHash {
		t.Fatalf("password reset hash = %q, want %q", calculatedHash, tokenHash)
	}
	if _, err := HashPasswordResetToken("invalid"); err == nil {
		t.Fatal("HashPasswordResetToken accepted an invalid token")
	}
}
