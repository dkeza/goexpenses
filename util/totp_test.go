package util

import (
	"encoding/base32"
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

// rfc6238Secret is the SHA1 test secret from RFC 6238, appendix B.
var rfc6238Secret = base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString([]byte("12345678901234567890"))

func useTOTPKey(t *testing.T, key string) {
	t.Helper()
	previous := Settings.TOTPKey
	Settings.TOTPKey = key
	t.Cleanup(func() { Settings.TOTPKey = previous })
}

func testTOTPKey(fill byte) string {
	return base64.StdEncoding.EncodeToString([]byte(strings.Repeat(string(fill), 32)))
}

func TestTOTPCodeMatchesRFC6238Vectors(t *testing.T) {
	// The RFC lists eight digits; a six-digit code is their last six.
	for unix, want := range map[int64]string{
		59:          "287082",
		1111111109:  "081804",
		1111111111:  "050471",
		1234567890:  "005924",
		2000000000:  "279037",
		20000000000: "353130",
	} {
		code, err := TOTPCode(rfc6238Secret, TOTPStep(time.Unix(unix, 0)))
		if err != nil || code != want {
			t.Errorf("code at %d = %q, %v; want %q", unix, code, err, want)
		}
	}
	if _, err := TOTPCode("not base32!", 1); err == nil {
		t.Error("an invalid secret produced a code")
	}
}

func TestMatchTOTPAcceptsOneStepOfDrift(t *testing.T) {
	now := time.Unix(1111111111, 0)
	current := TOTPStep(now)
	for offset := int64(-2); offset <= 2; offset++ {
		code, err := TOTPCode(rfc6238Secret, current+offset)
		if err != nil {
			t.Fatal(err)
		}
		step, ok := MatchTOTP(rfc6238Secret, code, now)
		wantOK := offset >= -1 && offset <= 1
		if ok != wantOK || (ok && step != current+offset) {
			t.Errorf("offset %d: step %d, ok %v; want ok %v", offset, step, ok, wantOK)
		}
	}
	code, _ := TOTPCode(rfc6238Secret, current)
	if _, ok := MatchTOTP(rfc6238Secret, " "+code[:3]+" "+code[3:]+" ", now); !ok {
		t.Error("a code typed with spaces was rejected")
	}
	for _, invalid := range []string{"", "12345", "1234567", "abcdef"} {
		if _, ok := MatchTOTP(rfc6238Secret, invalid, now); ok {
			t.Errorf("code %q was accepted", invalid)
		}
	}
}

func TestNewTOTPSecretIsRandomBase32(t *testing.T) {
	first, err := NewTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	second, _ := NewTOTPSecret()
	if len(first) != 32 || first == second {
		t.Fatalf("secrets %q and %q: want two different 32-character values", first, second)
	}
	if _, err := TOTPCode(first, 1); err != nil {
		t.Fatalf("generated secret is not usable: %v", err)
	}
}

func TestTOTPURI(t *testing.T) {
	got := TOTPURI("Goexpenses", "user@example.com", "ABC")
	want := "otpauth://totp/Goexpenses:user@example.com?algorithm=SHA1&digits=6&issuer=Goexpenses&period=30&secret=ABC"
	if got != want {
		t.Fatalf("URI = %q, want %q", got, want)
	}
}

func TestTOTPSecretEncryptionRoundTrip(t *testing.T) {
	useTOTPKey(t, testTOTPKey('k'))
	first, err := EncryptTOTPSecret(rfc6238Secret)
	if err != nil {
		t.Fatal(err)
	}
	second, _ := EncryptTOTPSecret(rfc6238Secret)
	if first == second || strings.Contains(first, rfc6238Secret) {
		t.Fatal("encryption is not randomized or leaks the secret")
	}
	secret, err := DecryptTOTPSecret(first)
	if err != nil || secret != rfc6238Secret {
		t.Fatalf("decrypted %q, %v", secret, err)
	}
}

func TestTOTPSecretEncryptionRejectsBadKeyOrCiphertext(t *testing.T) {
	useTOTPKey(t, testTOTPKey('k'))
	encrypted, err := EncryptTOTPSecret(rfc6238Secret)
	if err != nil {
		t.Fatal(err)
	}
	tampered := []byte(encrypted)
	if tampered[len(tampered)-3] == 'A' {
		tampered[len(tampered)-3] = 'B'
	} else {
		tampered[len(tampered)-3] = 'A'
	}
	for _, ciphertext := range []string{"", "%%%", "c2hvcnQ=", string(tampered)} {
		if _, err := DecryptTOTPSecret(ciphertext); err == nil {
			t.Errorf("ciphertext %q was decrypted", ciphertext)
		}
	}

	Settings.TOTPKey = testTOTPKey('x')
	if _, err := DecryptTOTPSecret(encrypted); err == nil {
		t.Error("a different key decrypted the secret")
	}
	for key, want := range map[string]error{
		"":                        ErrTOTPKeyMissing,
		"not base64":              ErrTOTPKeyInvalid,
		"c2hvcnQ=":                ErrTOTPKeyInvalid,
		testTOTPKey('k') + "AAAA": ErrTOTPKeyInvalid,
	} {
		Settings.TOTPKey = key
		if err := TOTPKeyError(); err != want {
			t.Errorf("key %q: error %v, want %v", key, err, want)
		}
		if _, err := EncryptTOTPSecret(rfc6238Secret); err != want {
			t.Errorf("key %q: encrypt error %v, want %v", key, err, want)
		}
		if _, err := DecryptTOTPSecret(encrypted); err != want {
			t.Errorf("key %q: decrypt error %v, want %v", key, err, want)
		}
	}
}

func TestRecoveryCodes(t *testing.T) {
	codes, err := NewRecoveryCodes(10)
	if err != nil || len(codes) != 10 {
		t.Fatalf("%d codes, %v; want 10", len(codes), err)
	}
	seen := map[string]bool{}
	for _, code := range codes {
		if len(code) != 11 || code[5] != '-' {
			t.Fatalf("code %q is not formatted as XXXXX-XXXXX", code)
		}
		normalized := NormalizeRecoveryCode(code)
		if len(normalized) != 10 || strings.Trim(normalized, recoveryCodeAlphabet) != "" {
			t.Fatalf("code %q has characters outside the alphabet", code)
		}
		if seen[normalized] {
			t.Fatalf("code %q was generated twice", code)
		}
		seen[normalized] = true
	}
	// 32 symbols carry 5 bits each: 10 characters give 50 bits.
	if len(recoveryCodeAlphabet) != 32 || strings.ContainsAny(recoveryCodeAlphabet, "ILOU") {
		t.Fatalf("alphabet %q is not Crockford base32", recoveryCodeAlphabet)
	}
	if got := NormalizeRecoveryCode("  abcde-fgh jk "); got != "ABCDEFGHJK" {
		t.Fatalf("normalized code = %q", got)
	}
}
