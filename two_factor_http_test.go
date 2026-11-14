package main

import (
	"encoding/base64"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"goexpenses/util"
)

var (
	setupSecretPattern  = regexp.MustCompile(`id="totp-secret"[^>]*>([A-Z2-7 ]+)<`)
	recoveryCodePattern = regexp.MustCompile(`<li>([0-9A-Z]{5}-[0-9A-Z]{5})</li>`)
)

func totpCodeAt(t *testing.T, secret string, stepOffset int64) string {
	t.Helper()
	code, err := util.TOTPCode(secret, util.TOTPStep(time.Now())+stepOffset)
	if err != nil {
		t.Fatalf("compute code: %v", err)
	}
	return code
}

func assertNoThirdPartyScripts(t *testing.T, page string, response testResponse) {
	t.Helper()
	if response.status != http.StatusOK {
		t.Fatalf("%s = %d to %q", page, response.status, response.location)
	}
	if strings.Contains(response.body, "googletagmanager.com") || strings.Contains(response.body, "googlesyndication.com") {
		t.Errorf("%s loads analytics or ads", page)
	}
}

// TestHTTPTwoFactorSignIn drives the whole feature through the real router,
// middleware and templates: turning two-factor authentication on, signing in
// with a code and with a recovery code, and turning it off.
func TestHTTPTwoFactorSignIn(t *testing.T) {
	server, db := startTestApp(t)
	util.Settings.TOTPKey = base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	user := createTestUser(t, db, "careful")
	credentials := url.Values{"username": {user.username}, "password": {integrationPassword}}

	client := newTestClient(t, server)
	client.login(user)
	otherDevice := newTestClient(t, server)
	otherDevice.login(user)

	setup := client.get("/account/2fa")
	assertNoThirdPartyScripts(t, "setup page", setup)
	match := setupSecretPattern.FindStringSubmatch(setup.body)
	if match == nil {
		t.Fatal("setup page shows no secret")
	}
	secret := strings.ReplaceAll(match[1], " ", "")
	if !strings.Contains(setup.body, `src="data:image/png;base64,`) {
		t.Error("setup page shows no QR code")
	}
	if again := client.get("/account/2fa"); !strings.Contains(again.body, match[1]) {
		t.Error("reloading the setup page changed the secret")
	}

	refused := client.post("/account/2fa/enable", url.Values{"password": {"wrong-password-1"}, "code": {totpCodeAt(t, secret, -1)}})
	if refused.status != http.StatusSeeOther || refused.location != "/account/2fa" ||
		countRows(t, db, `SELECT COUNT(*) FROM users WHERE id = $1 AND totp_enabled_at IS NOT NULL`, user.id) != 0 {
		t.Fatalf("enabling with a wrong password = %d to %q", refused.status, refused.location)
	}

	// Kept to send again later: computing "the previous step" a second time
	// would give a newer, still unused code once the clock passes a step.
	enablingCode := totpCodeAt(t, secret, -1)
	enabled := client.post("/account/2fa/enable", url.Values{"password": {integrationPassword}, "code": {enablingCode}})
	assertNoThirdPartyScripts(t, "recovery codes page", enabled)
	codes := recoveryCodePattern.FindAllStringSubmatch(enabled.body, -1)
	if len(codes) != 10 {
		t.Fatalf("%d recovery codes shown after enabling, want 10", len(codes))
	}
	if strings.Contains(enabled.body, match[1]) {
		t.Error("the secret is shown after two-factor authentication was enabled")
	}
	// The recovery codes are shown once only.
	status := client.get("/account/2fa")
	assertNoThirdPartyScripts(t, "status page", status)
	if strings.Contains(status.body, codes[0][1]) || !strings.Contains(status.body, `id="recovery-codes-left">10<`) {
		t.Error("status page repeats recovery codes or miscounts them")
	}
	// Enabling signs the user's other devices out.
	if response := otherDevice.get("/posts"); response.location != "/login" {
		t.Errorf("another device stayed signed in: %d to %q", response.status, response.location)
	}
	client.post("/logout", nil)

	// The password alone does not sign in.
	client.get("/")
	if response := client.post("/auth", credentials); response.status != http.StatusSeeOther || response.location != "/login/2fa" {
		t.Fatalf("password step = %d to %q, want a redirect to /login/2fa", response.status, response.location)
	}
	if response := client.get("/posts"); response.location != "/login" {
		t.Fatalf("posts after the password step = %d to %q, want a redirect to /login", response.status, response.location)
	}
	assertNoThirdPartyScripts(t, "code page", client.get("/login/2fa"))
	assertNoThirdPartyScripts(t, "recovery code page", client.get("/login/2fa?recovery=1"))

	// The code that enabled two-factor authentication is not accepted again.
	for _, wrong := range []string{enablingCode, "12345"} {
		if response := client.post("/login/2fa", url.Values{"code": {wrong}}); response.location != "/login/2fa" {
			t.Fatalf("code %q = %d to %q, want back to /login/2fa", wrong, response.status, response.location)
		}
	}
	if response := client.get("/posts"); response.location != "/login" {
		t.Fatal("a refused code signed the user in")
	}
	if response := client.post("/login/2fa", url.Values{"code": {totpCodeAt(t, secret, 0)}}); response.status != http.StatusSeeOther || response.location != "/posts" {
		t.Fatalf("right code = %d to %q, want a redirect to /posts", response.status, response.location)
	}
	if response := client.get("/posts"); response.status != http.StatusOK {
		t.Fatalf("posts after the second step = %d", response.status)
	}
	if countRows(t, db, `SELECT COUNT(*) FROM admin_events WHERE kind = 'auth_2fa_failed' AND user_id = $1`, user.id) != 2 {
		t.Error("refused codes were not recorded")
	}

	// Signing in as someone with two-factor authentication from a session
	// that is signed in ends that sign-in until the second step is passed.
	if response := client.post("/auth", credentials); response.location != "/login/2fa" {
		t.Fatalf("password step from a signed-in session = %d to %q", response.status, response.location)
	}
	if response := client.get("/posts"); response.location != "/login" {
		t.Fatal("the earlier sign-in survived a new password step")
	}

	// A recovery code works once.
	recovery := url.Values{"recovery_code": {strings.ToLower(codes[0][1])}}
	if response := client.post("/login/2fa", recovery); response.location != "/posts" {
		t.Fatalf("recovery code = %d to %q, want a redirect to /posts", response.status, response.location)
	}
	client.post("/logout", nil)
	client.get("/")
	client.post("/auth", credentials)
	if response := client.post("/login/2fa", recovery); response.location != "/login/2fa?recovery=1" {
		t.Fatalf("used recovery code = %d to %q, want back to the recovery form", response.status, response.location)
	}

	// A password step for an account without two-factor authentication
	// replaces the pending one.
	plain := createTestUser(t, db, "plain")
	if response := client.post("/auth", url.Values{"username": {plain.username}, "password": {integrationPassword}}); response.location != "/posts" {
		t.Fatalf("sign-in without two-factor authentication = %d to %q", response.status, response.location)
	}
	if countRows(t, db, `SELECT COUNT(*) FROM sessions WHERE pending_2fa_user_id IS NOT NULL OR pending_2fa_at IS NOT NULL OR pending_2fa_failures <> 0`) != 0 {
		t.Error("the pending sign-in of the first account survived")
	}
	if response := client.get("/login/2fa"); response.location != "/login" {
		t.Errorf("code page without a pending sign-in = %d to %q", response.status, response.location)
	}
	client.post("/logout", nil)

	// Turning it off needs the password and a second factor.
	client.get("/")
	client.post("/auth", credentials)
	if response := client.post("/login/2fa", url.Values{"recovery_code": {codes[1][1]}}); response.location != "/posts" {
		t.Fatalf("second recovery code = %d to %q", response.status, response.location)
	}
	client.post("/account/2fa/disable", url.Values{"password": {integrationPassword}, "code": {"000000"}, "recovery_code": {""}})
	if countRows(t, db, `SELECT COUNT(*) FROM users WHERE id = $1 AND totp_enabled_at IS NOT NULL`, user.id) != 1 {
		t.Fatal("two-factor authentication was turned off without a valid code")
	}
	if response := client.post("/account/2fa/disable", url.Values{"password": {integrationPassword}, "recovery_code": {codes[2][1]}}); response.location != "/account/2fa" {
		t.Fatalf("disable = %d to %q", response.status, response.location)
	}
	if countRows(t, db, `SELECT COUNT(*) FROM users WHERE id = $1 AND totp_secret IS NULL AND totp_enabled_at IS NULL`, user.id) != 1 ||
		countRows(t, db, `SELECT COUNT(*) FROM user_recovery_codes WHERE user_id = $1`, user.id) != 0 {
		t.Fatal("turning off left the secret or recovery codes")
	}
	client.post("/logout", nil)
	client.login(user)
}

// TestHTTPPasswordResetDoesNotBypassTwoFactor resets the password from the
// browser that is waiting for the second factor.
func TestHTTPPasswordResetDoesNotBypassTwoFactor(t *testing.T) {
	server, db := startTestApp(t)
	util.Settings.TOTPKey = base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	user := createTestUser(t, db, "forgetful")
	secret, err := util.NewTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := util.EncryptTOTPSecret(secret)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE users SET totp_secret = $1, totp_enabled_at = NOW() WHERE id = $2`, encrypted, user.id); err != nil {
		t.Fatal(err)
	}
	token, tokenHash, err := util.NewPasswordResetToken()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO passwordresets (email, token) VALUES ($1, $2)`, user.username+"@example.com", tokenHash); err != nil {
		t.Fatal(err)
	}

	client := newTestClient(t, server)
	client.get("/")
	if response := client.post("/auth", url.Values{"username": {user.username}, "password": {integrationPassword}}); response.location != "/login/2fa" {
		t.Fatalf("password step = %d to %q", response.status, response.location)
	}
	const newPassword = "a-new-password-1"
	reset := client.post("/changepassword", url.Values{"_token": {token}, "password": {newPassword}, "repeatpassword": {newPassword}})
	if reset.status != http.StatusSeeOther || reset.location != "/login" {
		t.Fatalf("password reset = %d to %q, want a redirect to /login", reset.status, reset.location)
	}
	// The reset signs nobody in and drops the pending second step.
	if response := client.get("/posts"); response.location != "/login" {
		t.Fatalf("posts after the reset = %d to %q", response.status, response.location)
	}
	if response := client.post("/login/2fa", url.Values{"code": {totpCodeAt(t, secret, 0)}}); response.location != "/login" {
		t.Fatalf("code after the reset = %d to %q, want a redirect to /login", response.status, response.location)
	}
	// The new password still needs the second factor.
	if response := client.post("/auth", url.Values{"username": {user.username}, "password": {newPassword}}); response.location != "/login/2fa" {
		t.Fatalf("new password = %d to %q, want a redirect to /login/2fa", response.status, response.location)
	}
	if response := client.post("/login/2fa", url.Values{"code": {totpCodeAt(t, secret, 0)}}); response.location != "/posts" {
		t.Fatalf("code after the new password = %d to %q", response.status, response.location)
	}
}
