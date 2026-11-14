package util

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// TOTP parameters from RFC 6238 as authenticator apps expect them.
const (
	totpDigits     = 6
	totpPeriod     = 30
	totpSecretSize = 20
	totpKeySize    = 32

	recoveryCodeLength = 10
	// Crockford base32: no I, L, O or U, so codes survive being read aloud or
	// copied by hand. Ten characters carry 50 bits.
	recoveryCodeAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
)

var (
	totpSecretEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

	ErrTOTPKeyMissing = errors.New("totpkey is not configured")
	ErrTOTPKeyInvalid = errors.New("totpkey must be 32 bytes encoded as base64")
	errTOTPCiphertext = errors.New("invalid encrypted two-factor secret")
)

// NewTOTPSecret returns 20 random bytes as unpadded base32.
func NewTOTPSecret() (string, error) {
	secret := make([]byte, totpSecretSize)
	if _, err := rand.Read(secret); err != nil {
		return "", fmt.Errorf("generate two-factor secret: %w", err)
	}
	return totpSecretEncoding.EncodeToString(secret), nil
}

// TOTPStep returns the number of the 30-second step that contains t.
func TOTPStep(t time.Time) int64 {
	return t.Unix() / totpPeriod
}

// TOTPCode returns the six-digit code of a base32 secret for one step.
func TOTPCode(secret string, step int64) (string, error) {
	normalized := strings.ToUpper(strings.TrimRight(strings.ReplaceAll(secret, " ", ""), "="))
	key, err := totpSecretEncoding.DecodeString(normalized)
	if err != nil || len(key) == 0 {
		return "", errors.New("invalid two-factor secret")
	}
	if step < 0 {
		return "", errors.New("invalid two-factor step")
	}
	counter := make([]byte, 8)
	binary.BigEndian.PutUint64(counter, uint64(step))
	mac := hmac.New(sha1.New, key)
	mac.Write(counter)
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	value := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%0*d", totpDigits, value%1000000), nil
}

// MatchTOTP checks code against the previous, current and next step and
// returns the step it belongs to. It does not prevent reuse: the caller
// stores the step with a conditional update, so a step is accepted once.
func MatchTOTP(secret, code string, now time.Time) (step int64, ok bool) {
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	if len(code) != totpDigits {
		return 0, false
	}
	current := TOTPStep(now)
	for _, candidate := range []int64{current - 1, current, current + 1} {
		expected, err := TOTPCode(secret, candidate)
		if err != nil {
			return 0, false
		}
		if subtle.ConstantTimeCompare([]byte(expected), []byte(code)) == 1 && !ok {
			step, ok = candidate, true
		}
	}
	return step, ok
}

// TOTPURI returns the otpauth:// URI that authenticator apps read from a QR code.
func TOTPURI(issuer, account, secret string) string {
	query := url.Values{}
	query.Set("secret", secret)
	query.Set("issuer", issuer)
	query.Set("algorithm", "SHA1")
	query.Set("digits", fmt.Sprint(totpDigits))
	query.Set("period", fmt.Sprint(totpPeriod))
	return "otpauth://totp/" + url.PathEscape(issuer+":"+account) + "?" + query.Encode()
}

// TOTPKeyError reports why two-factor secrets cannot be encrypted or
// decrypted, or nil when the configured key is usable.
func TOTPKeyError() error {
	_, err := totpKey()
	return err
}

func totpKey() ([]byte, error) {
	if Settings.TOTPKey == "" {
		return nil, ErrTOTPKeyMissing
	}
	key, err := base64.StdEncoding.DecodeString(Settings.TOTPKey)
	if err != nil || len(key) != totpKeySize {
		return nil, ErrTOTPKeyInvalid
	}
	return key, nil
}

func totpCipher() (cipher.AEAD, error) {
	key, err := totpKey()
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// EncryptTOTPSecret seals a secret with AES-GCM for storage. The result is
// base64 of the random nonce followed by the ciphertext.
func EncryptTOTPSecret(secret string) (string, error) {
	aead, err := totpCipher()
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}
	return base64.StdEncoding.EncodeToString(aead.Seal(nonce, nonce, []byte(secret), nil)), nil
}

// DecryptTOTPSecret opens a value produced by EncryptTOTPSecret.
func DecryptTOTPSecret(encrypted string) (string, error) {
	aead, err := totpCipher()
	if err != nil {
		return "", err
	}
	sealed, err := base64.StdEncoding.DecodeString(encrypted)
	if err != nil || len(sealed) <= aead.NonceSize() {
		return "", errTOTPCiphertext
	}
	secret, err := aead.Open(nil, sealed[:aead.NonceSize()], sealed[aead.NonceSize():], nil)
	if err != nil {
		return "", errTOTPCiphertext
	}
	return string(secret), nil
}

// NewRecoveryCodes returns n random recovery codes formatted as XXXXX-XXXXX.
// Store NormalizeRecoveryCode of each code hashed with HashPassword.
func NewRecoveryCodes(n int) ([]string, error) {
	codes := make([]string, 0, n)
	random := make([]byte, recoveryCodeLength)
	for len(codes) < n {
		if _, err := rand.Read(random); err != nil {
			return nil, fmt.Errorf("generate recovery code: %w", err)
		}
		code := make([]byte, 0, recoveryCodeLength+1)
		for i, value := range random {
			if i == recoveryCodeLength/2 {
				code = append(code, '-')
			}
			// 256 is a multiple of the alphabet size, so every character is
			// equally likely.
			code = append(code, recoveryCodeAlphabet[int(value)%len(recoveryCodeAlphabet)])
		}
		codes = append(codes, string(code))
	}
	return codes, nil
}

// NormalizeRecoveryCode upper-cases an entered recovery code and removes
// dashes and spaces, so it compares equal to the stored form.
func NormalizeRecoveryCode(s string) string {
	return strings.NewReplacer("-", "", " ", "").Replace(strings.ToUpper(strings.TrimSpace(s)))
}
