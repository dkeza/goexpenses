package util

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
)

const passwordResetTokenSize = 32

func NewPasswordResetToken() (token string, tokenHash string, err error) {
	randomBytes := make([]byte, passwordResetTokenSize)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", "", err
	}

	token = base64.RawURLEncoding.EncodeToString(randomBytes)
	tokenHash = hashPasswordResetTokenBytes(randomBytes)
	return token, tokenHash, nil
}

func HashPasswordResetToken(token string) (string, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(decoded) != passwordResetTokenSize {
		return "", errors.New("invalid password reset token")
	}
	return hashPasswordResetTokenBytes(decoded), nil
}

func hashPasswordResetTokenBytes(token []byte) string {
	hash := sha256.Sum256(token)
	return hex.EncodeToString(hash[:])
}
