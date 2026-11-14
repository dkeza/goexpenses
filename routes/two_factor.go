package routes

import (
	stdsql "database/sql"
	"encoding/base64"
	"errors"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"goexpenses/database"
	"goexpenses/util"

	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo/v4"
	qrcode "github.com/skip2/go-qrcode"
)

// Two-factor authentication (TOTP).
//
// Every flow locks rows in the same order as the older code that blocks,
// deletes or resets a user: users, then sessions, then user_recovery_codes.
// Taking them in another order could deadlock concurrent requests.

const (
	twoFactorIssuer       = "Goexpenses"
	twoFactorMaxFailures  = 5
	recoveryCodeCount     = 10
	twoFactorQRCodePixels = 220
)

const (
	// A pending second step is valid for five minutes.
	pendingLoginQuery     = `SELECT pending_2fa_user_id, pending_2fa_failures, COALESCE(pending_2fa_at > NOW() - INTERVAL '5 minutes', false) AS fresh FROM sessions WHERE uuid = $1`
	lockPendingLoginQuery = pendingLoginQuery + ` FOR UPDATE`

	lockTwoFactorLoginUserQuery = `SELECT totp_secret FROM users WHERE id = $1 AND blocked_at IS NULL AND email_verified = true AND totp_enabled_at IS NOT NULL FOR UPDATE`
	clearPendingLoginQuery      = `UPDATE sessions SET pending_2fa_user_id = NULL, pending_2fa_at = NULL, pending_2fa_failures = 0 WHERE uuid = $1`
	countPendingFailureQuery    = `UPDATE sessions SET pending_2fa_failures = pending_2fa_failures + 1 WHERE uuid = $1`

	// A step or a recovery code is spent by a conditional update: of two
	// concurrent requests with the same code only one changes a row.
	consumeTOTPStepQuery     = `UPDATE users SET totp_last_step = $1 WHERE id = $2 AND totp_last_step < $1`
	consumeRecoveryCodeQuery = `UPDATE user_recovery_codes SET used_at = NOW() WHERE id = $1 AND user_id = $2 AND used_at IS NULL`
	unusedRecoveryCodesQuery = `SELECT id, code_hash FROM user_recovery_codes WHERE user_id = $1 AND used_at IS NULL ORDER BY id`
	countRecoveryCodesQuery  = `SELECT COUNT(*) FROM user_recovery_codes WHERE user_id = $1 AND used_at IS NULL`
	insertRecoveryCodeQuery  = `INSERT INTO user_recovery_codes (user_id, code_hash) VALUES ($1, $2)`
	deleteRecoveryCodesQuery = `DELETE FROM user_recovery_codes WHERE user_id = $1`

	// The setup secret is shown for ten minutes before a new one is made.
	setupSecretQuery      = `SELECT pending_totp_secret, COALESCE(pending_totp_created_at > NOW() - INTERVAL '10 minutes', false) AS fresh FROM sessions WHERE uuid = $1`
	lockSetupSecretQuery  = setupSecretQuery + ` FOR UPDATE`
	storeSetupSecretQuery = `UPDATE sessions SET pending_totp_secret = $1, pending_totp_created_at = NOW() WHERE uuid = $2`
	clearSetupSecretQuery = `UPDATE sessions SET pending_totp_secret = NULL, pending_totp_created_at = NULL WHERE uuid = $1`

	twoFactorEnabledQuery     = `SELECT totp_enabled_at IS NOT NULL FROM users WHERE id = $1`
	lockTwoFactorUserQuery    = `SELECT password, totp_secret, totp_enabled_at IS NOT NULL AS totp_enabled FROM users WHERE id = $1 AND blocked_at IS NULL FOR UPDATE`
	enableTwoFactorQuery      = `UPDATE users SET totp_secret = $1, totp_enabled_at = NOW(), totp_last_step = $2 WHERE id = $3 AND totp_enabled_at IS NULL`
	removeTwoFactorQuery      = `UPDATE users SET totp_secret = NULL, totp_enabled_at = NULL WHERE id = $1`
	lockTwoFactorTargetQuery  = `SELECT username, is_admin, totp_enabled_at IS NOT NULL AS totp_enabled FROM users WHERE id = $1 FOR UPDATE`
	recordTwoFactorEventQuery = `INSERT INTO admin_events (kind, status, user_id) VALUES ($1, $2, $3)`
	recordAdminResetQuery     = `INSERT INTO admin_events (kind, status, user_id, actor_user_id, subject, detail) VALUES ('admin_2fa_reset', 'success', $1, $2, $3, $4)`
)

var (
	errTwoFactorCode         = errors.New("invalid two-factor code")
	errTwoFactorKey          = errors.New("two-factor secret cannot be decrypted")
	errTwoFactorSetupExpired = errors.New("two-factor setup expired")
	errTwoFactorState        = errors.New("two-factor authentication is not in the expected state")
)

// pendingLogin is the second sign-in step a session is waiting for.
type pendingLogin struct {
	UserID   *int `db:"pending_2fa_user_id"`
	Failures int  `db:"pending_2fa_failures"`
	Fresh    bool `db:"fresh"`
}

func (pending pendingLogin) usable() bool {
	return pending.UserID != nil && pending.Fresh && pending.Failures < twoFactorMaxFailures
}

// loadPendingLogin reads a session's pending step. A missing session reads
// as no pending step.
func loadPendingLogin(queryer sqlx.Queryer, query, sessionHash string) (pendingLogin, error) {
	pending := pendingLogin{}
	if err := sqlx.Get(queryer, &pending, query, sessionHash); err != nil && !errors.Is(err, stdsql.ErrNoRows) {
		return pendingLogin{}, err
	}
	return pending, nil
}

// executedOne runs a conditional update and reports whether it changed its row.
func executedOne(execer sqlExecer, query string, args ...interface{}) (bool, error) {
	err := executeExactlyOne(execer, query, args...)
	if errors.Is(err, errRecordNotFound) {
		return false, nil
	}
	return err == nil, err
}

// findRecoveryCode returns the ID of the user's unused recovery code that
// matches input, or 0. bcrypt is slow, so callers run this before their
// transaction and spend the code by ID inside it.
func findRecoveryCode(userID int, input string) (int, error) {
	normalized := util.NormalizeRecoveryCode(input)
	if normalized == "" {
		return 0, nil
	}
	codes := []struct {
		ID   int    `db:"id"`
		Hash string `db:"code_hash"`
	}{}
	if err := database.Db.Select(&codes, unusedRecoveryCodesQuery, userID); err != nil {
		return 0, err
	}
	found := 0
	for _, code := range codes {
		if valid, _ := util.VerifyPassword(code.Hash, normalized); valid && found == 0 {
			found = code.ID
		}
	}
	return found, nil
}

// newRecoveryCodeSet returns fresh recovery codes and their bcrypt hashes.
func newRecoveryCodeSet() (codes []string, hashes []string, err error) {
	codes, err = util.NewRecoveryCodes(recoveryCodeCount)
	if err != nil {
		return nil, nil, err
	}
	for _, code := range codes {
		hash, err := util.HashPassword(util.NormalizeRecoveryCode(code))
		if err != nil {
			return nil, nil, err
		}
		hashes = append(hashes, hash)
	}
	return codes, hashes, nil
}

func replaceRecoveryCodes(tx *sqlx.Tx, userID int, hashes []string) error {
	if _, err := tx.Exec(deleteRecoveryCodesQuery, userID); err != nil {
		return err
	}
	for _, hash := range hashes {
		if _, err := tx.Exec(insertRecoveryCodeQuery, userID, hash); err != nil {
			return err
		}
	}
	return nil
}

// consumeTOTP checks a code against an encrypted secret and spends its step.
func consumeTOTP(tx *sqlx.Tx, userID int, encryptedSecret *string, code string, now time.Time) error {
	if encryptedSecret == nil {
		return errTwoFactorState
	}
	secret, err := util.DecryptTOTPSecret(*encryptedSecret)
	if err != nil {
		return errTwoFactorKey
	}
	step, ok := util.MatchTOTP(secret, code, now)
	if !ok {
		return errTwoFactorCode
	}
	fresh, err := executedOne(tx, consumeTOTPStepQuery, step, userID)
	if err != nil {
		return err
	}
	if !fresh {
		return errTwoFactorCode
	}
	return nil
}

type twoFactorOutcome int

const (
	// twoFactorExpired: there is no usable pending step; sign in again.
	twoFactorExpired twoFactorOutcome = iota
	twoFactorOK
	twoFactorWrongCode
	// twoFactorLockedOut: a wrong code used up the last attempt.
	twoFactorLockedOut
	// twoFactorKeyError: the secret cannot be decrypted, so no TOTP code can
	// be checked. Sign-in fails closed; recovery codes still work.
	twoFactorKeyError
)

type twoFactorLoginResult struct {
	Outcome   twoFactorOutcome
	UserID    int
	Token     string
	TokenHash string
}

// completeTwoFactorLogin finishes a pending sign-in with a TOTP code or, when
// recoveryCode is set, a recovery code. Spending the code, rotating the
// session and recording the sign-in are one transaction: if any part fails
// everything is rolled back and the code stays unspent. A wrong code is
// counted under the same locks that guard the counter and then committed,
// so concurrent requests cannot gain extra attempts.
func completeTwoFactorLogin(sessionHash, code, recoveryCode string, now time.Time) (twoFactorLoginResult, error) {
	pending, err := loadPendingLogin(database.Db, pendingLoginQuery, sessionHash)
	if err != nil {
		return twoFactorLoginResult{}, err
	}
	if pending.UserID == nil {
		return twoFactorLoginResult{}, nil
	}
	userID := *pending.UserID
	useRecovery := strings.TrimSpace(recoveryCode) != ""
	recoveryCodeID := 0
	if useRecovery {
		if recoveryCodeID, err = findRecoveryCode(userID, recoveryCode); err != nil {
			return twoFactorLoginResult{}, err
		}
	}
	token, tokenHash, err := util.NewSessionToken()
	if err != nil {
		return twoFactorLoginResult{}, err
	}

	outcome := twoFactorExpired
	err = runTransaction(database.Db, func(tx *sqlx.Tx) error {
		var user struct {
			Secret *string `db:"totp_secret"`
		}
		userUsable := true
		if err := tx.Get(&user, lockTwoFactorLoginUserQuery, userID); err != nil {
			if !errors.Is(err, stdsql.ErrNoRows) {
				return err
			}
			userUsable = false
		}
		locked, err := loadPendingLogin(tx, lockPendingLoginQuery, sessionHash)
		if err != nil {
			return err
		}
		if locked.UserID == nil || *locked.UserID != userID {
			// The session moved on to another sign-in; that one is not ours to touch.
			return nil
		}
		if !userUsable || !locked.usable() {
			_, err := tx.Exec(clearPendingLoginQuery, sessionHash)
			return err
		}

		accepted := false
		if useRecovery {
			if recoveryCodeID != 0 {
				if accepted, err = executedOne(tx, consumeRecoveryCodeQuery, recoveryCodeID, userID); err != nil {
					return err
				}
			}
		} else {
			switch err := consumeTOTP(tx, userID, user.Secret, code, now); {
			case err == nil:
				accepted = true
			case errors.Is(err, errTwoFactorKey), errors.Is(err, errTwoFactorState):
				outcome = twoFactorKeyError
				_, err := tx.Exec(recordTwoFactorEventQuery, "auth_2fa_key_error", "failed", userID)
				return err
			case !errors.Is(err, errTwoFactorCode):
				return err
			}
		}

		if !accepted {
			outcome = twoFactorWrongCode
			if _, err := tx.Exec(countPendingFailureQuery, sessionHash); err != nil {
				return err
			}
			if locked.Failures+1 >= twoFactorMaxFailures {
				outcome = twoFactorLockedOut
				if _, err := tx.Exec(clearPendingLoginQuery, sessionHash); err != nil {
					return err
				}
			}
			_, err := tx.Exec(recordTwoFactorEventQuery, "auth_2fa_failed", "failed", userID)
			return err
		}

		if err := executeExactlyOne(tx, rotateSessionQuery, tokenHash, userID, time.Now(), sessionHash); err != nil {
			return err
		}
		if _, err := tx.Exec(recordLoginQuery, userID); err != nil {
			return err
		}
		if useRecovery {
			if _, err := tx.Exec(recordTwoFactorEventQuery, "auth_2fa_recovery_used", "success", userID); err != nil {
				return err
			}
		}
		outcome = twoFactorOK
		return nil
	})
	if err != nil {
		return twoFactorLoginResult{}, err
	}
	result := twoFactorLoginResult{Outcome: outcome, UserID: userID}
	if outcome == twoFactorOK {
		result.Token, result.TokenHash = token, tokenHash
	}
	return result, nil
}

// loadSetupSecret returns the session's encrypted setup secret while it is
// not older than ten minutes.
func loadSetupSecret(queryer sqlx.Queryer, query, sessionHash string) (encrypted string, ok bool, err error) {
	var setup struct {
		Secret *string `db:"pending_totp_secret"`
		Fresh  bool    `db:"fresh"`
	}
	if err := sqlx.Get(queryer, &setup, query, sessionHash); err != nil {
		if errors.Is(err, stdsql.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	if setup.Secret == nil || !setup.Fresh {
		return "", false, nil
	}
	return *setup.Secret, true, nil
}

// currentSetupSecret returns the secret the setup page shows. Reloading the
// page within ten minutes shows the same secret; after that a new one is made.
func currentSetupSecret(sessionHash string) (string, error) {
	encrypted, ok, err := loadSetupSecret(database.Db, setupSecretQuery, sessionHash)
	if err != nil {
		return "", err
	}
	if ok {
		if secret, err := util.DecryptTOTPSecret(encrypted); err == nil {
			return secret, nil
		}
	}
	secret, err := util.NewTOTPSecret()
	if err != nil {
		return "", err
	}
	encrypted, err = util.EncryptTOTPSecret(secret)
	if err != nil {
		return "", err
	}
	if err := executeExactlyOne(database.Db, storeSetupSecretQuery, encrypted, sessionHash); err != nil {
		return "", err
	}
	return secret, nil
}

type twoFactorUser struct {
	Password string  `db:"password"`
	Secret   *string `db:"totp_secret"`
	Enabled  bool    `db:"totp_enabled"`
}

// lockTwoFactorUser locks the user row and checks that the password verified
// before the transaction is still the user's password.
func lockTwoFactorUser(tx *sqlx.Tx, userID int, verifiedHash string) (twoFactorUser, error) {
	user := twoFactorUser{}
	if err := tx.Get(&user, lockTwoFactorUserQuery, userID); err != nil {
		if errors.Is(err, stdsql.ErrNoRows) {
			return twoFactorUser{}, errInvalidCredentials
		}
		return twoFactorUser{}, err
	}
	if user.Password != verifiedHash {
		return twoFactorUser{}, errInvalidCredentials
	}
	return user, nil
}

// enableTwoFactor turns two-factor authentication on after the current
// password and a code from the setup secret. It returns the recovery codes,
// which are shown once.
func enableTwoFactor(userID int, sessionHash, password, code string, now time.Time) ([]string, error) {
	passwordHash, err := verifiedPasswordHash(userID, password)
	if err != nil {
		return nil, err
	}
	encrypted, ok, err := loadSetupSecret(database.Db, setupSecretQuery, sessionHash)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errTwoFactorSetupExpired
	}
	secret, err := util.DecryptTOTPSecret(encrypted)
	if err != nil {
		return nil, errTwoFactorKey
	}
	step, ok := util.MatchTOTP(secret, code, now)
	if !ok {
		return nil, errTwoFactorCode
	}
	codes, hashes, err := newRecoveryCodeSet()
	if err != nil {
		return nil, err
	}

	err = runTransaction(database.Db, func(tx *sqlx.Tx) error {
		if _, err := lockTwoFactorUser(tx, userID, passwordHash); err != nil {
			return err
		}
		if enabled, err := executedOne(tx, enableTwoFactorQuery, encrypted, step, userID); err != nil {
			return err
		} else if !enabled {
			return errTwoFactorState
		}
		// The secret was read before the transaction; make sure the locked
		// session still holds the same, unexpired one.
		lockedSecret, ok, err := loadSetupSecret(tx, lockSetupSecretQuery, sessionHash)
		if err != nil {
			return err
		}
		if !ok || lockedSecret != encrypted {
			return errTwoFactorSetupExpired
		}
		if _, err := tx.Exec(clearSetupSecretQuery, sessionHash); err != nil {
			return err
		}
		if _, err := tx.Exec(deleteOtherUserSessionsQuery, userID, sessionHash); err != nil {
			return err
		}
		if err := replaceRecoveryCodes(tx, userID, hashes); err != nil {
			return err
		}
		_, err = tx.Exec(recordTwoFactorEventQuery, "2fa_enabled", "success", userID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return codes, nil
}

// disableTwoFactor turns two-factor authentication off after the current
// password and a TOTP code or, when recoveryCode is set, a recovery code.
func disableTwoFactor(userID int, sessionHash, password, code, recoveryCode string, now time.Time) error {
	passwordHash, err := verifiedPasswordHash(userID, password)
	if err != nil {
		return err
	}
	useRecovery := strings.TrimSpace(recoveryCode) != ""
	recoveryCodeID := 0
	if useRecovery {
		if recoveryCodeID, err = findRecoveryCode(userID, recoveryCode); err != nil {
			return err
		}
		if recoveryCodeID == 0 {
			return errTwoFactorCode
		}
	}

	return runTransaction(database.Db, func(tx *sqlx.Tx) error {
		user, err := lockTwoFactorUser(tx, userID, passwordHash)
		if err != nil {
			return err
		}
		if !user.Enabled {
			return errTwoFactorState
		}
		if !useRecovery {
			if err := consumeTOTP(tx, userID, user.Secret, code, now); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(removeTwoFactorQuery, userID); err != nil {
			return err
		}
		if _, err := tx.Exec(deleteOtherUserSessionsQuery, userID, sessionHash); err != nil {
			return err
		}
		if useRecovery {
			if spent, err := executedOne(tx, consumeRecoveryCodeQuery, recoveryCodeID, userID); err != nil {
				return err
			} else if !spent {
				return errTwoFactorCode
			}
		}
		if _, err := tx.Exec(deleteRecoveryCodesQuery, userID); err != nil {
			return err
		}
		_, err = tx.Exec(recordTwoFactorEventQuery, "2fa_disabled", "success", userID)
		return err
	})
}

// regenerateRecoveryCodes replaces the user's recovery codes after the
// current password and a TOTP code. The new codes are shown once.
func regenerateRecoveryCodes(userID int, sessionHash, password, code string, now time.Time) ([]string, error) {
	passwordHash, err := verifiedPasswordHash(userID, password)
	if err != nil {
		return nil, err
	}
	codes, hashes, err := newRecoveryCodeSet()
	if err != nil {
		return nil, err
	}
	err = runTransaction(database.Db, func(tx *sqlx.Tx) error {
		user, err := lockTwoFactorUser(tx, userID, passwordHash)
		if err != nil {
			return err
		}
		if !user.Enabled {
			return errTwoFactorState
		}
		if err := consumeTOTP(tx, userID, user.Secret, code, now); err != nil {
			return err
		}
		if _, err := tx.Exec(deleteOtherUserSessionsQuery, userID, sessionHash); err != nil {
			return err
		}
		if err := replaceRecoveryCodes(tx, userID, hashes); err != nil {
			return err
		}
		_, err = tx.Exec(recordTwoFactorEventQuery, "2fa_recovery_regenerated", "success", userID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return codes, nil
}

// resetUserTwoFactor removes another user's second factor, for a user who
// lost both the device and the recovery codes. As with blocking, it does not
// apply to admin accounts: an admin changes their own on /account/2fa.
func resetUserTwoFactor(actorID, userID int, reason string) error {
	return runTransaction(database.Db, func(tx *sqlx.Tx) error {
		var user struct {
			Username string `db:"username"`
			IsAdmin  bool   `db:"is_admin"`
			Enabled  bool   `db:"totp_enabled"`
		}
		if err := tx.Get(&user, lockTwoFactorTargetQuery, userID); err != nil {
			if errors.Is(err, stdsql.ErrNoRows) {
				return echo.NewHTTPError(http.StatusNotFound, "user not found")
			}
			return err
		}
		if user.IsAdmin {
			return echo.NewHTTPError(http.StatusForbidden, "two-factor authentication of admin accounts cannot be reset here")
		}
		if !user.Enabled {
			return echo.NewHTTPError(http.StatusConflict, "user has no two-factor authentication")
		}
		if _, err := tx.Exec(removeTwoFactorQuery, userID); err != nil {
			return err
		}
		if _, err := tx.Exec(deleteUserSessionsQuery, userID); err != nil {
			return err
		}
		if _, err := tx.Exec(deleteRecoveryCodesQuery, userID); err != nil {
			return err
		}
		_, err := tx.Exec(recordAdminResetQuery, userID, actorID, user.Username, reason)
		return err
	})
}

func adminResetTwoFactor(c echo.Context) error {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id < 1 {
		return echo.NewHTTPError(http.StatusNotFound, "user not found")
	}
	data := c.Get("data").(*util.Data)
	reason := strings.TrimSpace(c.FormValue("reason"))
	if reason == "" || len([]rune(reason)) > 500 {
		return echo.NewHTTPError(http.StatusBadRequest, "a reason up to 500 characters is required")
	}
	if _, err := verifiedPasswordHash(data.User.Id, c.FormValue("password")); err != nil {
		if errors.Is(err, errInvalidCredentials) {
			util.Flash(`Current password is incorrect!`, data, 0, "", 0)
			return c.Redirect(http.StatusSeeOther, "/admin/users/"+strconv.Itoa(id))
		}
		return databaseReadError(c, "verify admin password", err)
	}
	if err := resetUserTwoFactor(data.User.Id, id, reason); err != nil {
		var httpError *echo.HTTPError
		if errors.As(err, &httpError) {
			return err
		}
		return databaseWriteError(c, "reset two-factor authentication", err)
	}
	util.Flash(`Two-factor authentication has been reset.`, data, 1, "", 0)
	return c.Redirect(http.StatusSeeOther, "/admin/users/"+strconv.Itoa(id))
}

// protectTwoFactorPage keeps pages that show or accept second-factor secrets
// out of caches and free of analytics and ad scripts.
func protectTwoFactorPage(c echo.Context, data *util.Data) {
	c.Response().Header().Set("Cache-Control", "no-store")
	data.HideThirdPartyScripts = true
}

func twoFactorLoginURL(toAdmin, recovery bool) string {
	query := []string{}
	if recovery {
		query = append(query, "recovery=1")
	}
	if toAdmin {
		query = append(query, "next=/admin")
	}
	if len(query) == 0 {
		return "/login/2fa"
	}
	return "/login/2fa?" + strings.Join(query, "&")
}

type twoFactorLoginPage struct {
	*util.Data
	Recovery       bool
	KeyUnavailable bool
}

func showTwoFactorLogin(c echo.Context) error {
	data := c.Get("data").(*util.Data)
	sessionHash, _ := c.Get("_id").(string)
	pending, err := loadPendingLogin(database.Db, pendingLoginQuery, sessionHash)
	if err != nil {
		return databaseReadError(c, "load pending sign-in", err)
	}
	if !pending.usable() {
		return c.Redirect(http.StatusSeeOther, "/login")
	}
	protectTwoFactorPage(c, data)
	data.Active = "login"
	if c.QueryParam("next") == "/admin" {
		data.LoginNext = "/admin"
	}
	return c.Render(http.StatusOK, "login-2fa", twoFactorLoginPage{
		Data:           data,
		Recovery:       c.QueryParam("recovery") == "1",
		KeyUnavailable: util.TOTPKeyError() != nil,
	})
}

func submitTwoFactorLogin(c echo.Context) error {
	data := c.Get("data").(*util.Data)
	sessionHash, _ := c.Get("_id").(string)
	toAdmin := c.FormValue("next") == "/admin"
	recoveryCode := c.FormValue("recovery_code")
	useRecovery := strings.TrimSpace(recoveryCode) != ""

	result, err := completeTwoFactorLogin(sessionHash, c.FormValue("code"), recoveryCode, time.Now())
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "could not sign in").SetInternal(err)
	}
	switch result.Outcome {
	case twoFactorOK:
		setSessionCookie(c, result.Token, result.TokenHash)
		if toAdmin {
			return c.Redirect(http.StatusSeeOther, "/admin")
		}
		return c.Redirect(http.StatusSeeOther, "/posts")
	case twoFactorWrongCode:
		util.Flash(`Invalid code!`, data, 0, "", 0)
		return c.Redirect(http.StatusSeeOther, twoFactorLoginURL(toAdmin, useRecovery))
	case twoFactorKeyError:
		// The secret itself is never logged.
		c.Logger().Errorf("two-factor sign-in for user %d: secret cannot be decrypted; check totpkey", result.UserID)
		util.Flash(`Signing in with a code is currently not possible. Use a recovery code.`, data, 0, "", 0)
		return c.Redirect(http.StatusSeeOther, twoFactorLoginURL(toAdmin, true))
	case twoFactorLockedOut:
		util.Flash(`Too many invalid codes. Sign in again.`, data, 0, "", 0)
	}
	if toAdmin {
		return c.Redirect(http.StatusSeeOther, "/login?next=/admin")
	}
	return c.Redirect(http.StatusSeeOther, "/login")
}

type twoFactorPage struct {
	*util.Data
	Enabled        bool
	KeyUnavailable bool
	// Secret and QRCode are the setup secret of a user who has not enabled
	// two-factor authentication yet.
	Secret string
	QRCode template.URL
	// RecoveryCodes are set only in the response that created them.
	RecoveryCodes      []string
	RemainingCodeCount int
}

// groupSecret splits a base32 secret into groups of four for typing by hand.
func groupSecret(secret string) string {
	groups := []string{}
	for len(secret) > 4 {
		groups = append(groups, secret[:4])
		secret = secret[4:]
	}
	return strings.Join(append(groups, secret), " ")
}

func renderTwoFactorPage(c echo.Context, recoveryCodes []string) error {
	data := c.Get("data").(*util.Data)
	sessionHash, _ := c.Get("_id").(string)
	protectTwoFactorPage(c, data)
	view := twoFactorPage{Data: data, RecoveryCodes: recoveryCodes}
	if err := database.Db.Get(&view.Enabled, twoFactorEnabledQuery, data.User.Id); err != nil {
		return databaseReadError(c, "load two-factor status", err)
	}
	switch {
	case view.Enabled:
		if err := database.Db.Get(&view.RemainingCodeCount, countRecoveryCodesQuery, data.User.Id); err != nil {
			return databaseReadError(c, "count recovery codes", err)
		}
	case util.TOTPKeyError() != nil:
		view.KeyUnavailable = true
	default:
		secret, err := currentSetupSecret(sessionHash)
		if err != nil {
			return databaseWriteError(c, "prepare two-factor setup", err)
		}
		image, err := qrcode.Encode(util.TOTPURI(twoFactorIssuer, data.User.Email, secret), qrcode.Medium, twoFactorQRCodePixels)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "could not create QR code").SetInternal(err)
		}
		view.Secret = groupSecret(secret)
		view.QRCode = template.URL("data:image/png;base64," + base64.StdEncoding.EncodeToString(image))
	}
	return c.Render(http.StatusOK, "two-factor", view)
}

// twoFactorChangeFailed reports a refused change on /account/2fa.
func twoFactorChangeFailed(c echo.Context, err error) error {
	data := c.Get("data").(*util.Data)
	message := ""
	switch {
	case errors.Is(err, errInvalidCredentials):
		message = `Current password is incorrect!`
	case errors.Is(err, errTwoFactorCode):
		message = `Invalid code!`
	case errors.Is(err, errTwoFactorSetupExpired):
		message = `The setup code has expired. Scan the new QR code.`
	case errors.Is(err, errTwoFactorKey):
		c.Logger().Errorf("two-factor change for user %d: secret cannot be decrypted; check totpkey", data.User.Id)
		message = `Signing in with a code is currently not possible. Use a recovery code.`
	case errors.Is(err, errTwoFactorState):
		message = `Two-factor authentication settings have changed. Try again.`
	default:
		return databaseWriteError(c, "change two-factor authentication", err)
	}
	util.Flash(message, data, 0, "", 0)
	return c.Redirect(http.StatusSeeOther, "/account/2fa")
}

func DefineTwoFactorRoutes() {
	e := E
	auth := Auth
	manageLimit := rateLimitMiddleware(publicRequestLimiter, twoFactorManageRateLimitRules)

	e.GET("/login/2fa", showTwoFactorLogin)
	e.POST("/login/2fa", submitTwoFactorLogin, rateLimitMiddleware(publicRequestLimiter, twoFactorLoginRateLimitRules))

	e.GET("/account/2fa", func(c echo.Context) error {
		return renderTwoFactorPage(c, nil)
	}, auth)

	e.POST("/account/2fa/enable", func(c echo.Context) error {
		data := c.Get("data").(*util.Data)
		sessionHash, _ := c.Get("_id").(string)
		codes, err := enableTwoFactor(data.User.Id, sessionHash, c.FormValue("password"), c.FormValue("code"), time.Now())
		if err != nil {
			return twoFactorChangeFailed(c, err)
		}
		// The recovery codes are part of this response only; a redirect
		// would have to keep them somewhere.
		return renderTwoFactorPage(c, codes)
	}, auth, manageLimit)

	e.POST("/account/2fa/disable", func(c echo.Context) error {
		data := c.Get("data").(*util.Data)
		sessionHash, _ := c.Get("_id").(string)
		err := disableTwoFactor(data.User.Id, sessionHash, c.FormValue("password"), c.FormValue("code"), c.FormValue("recovery_code"), time.Now())
		if err != nil {
			return twoFactorChangeFailed(c, err)
		}
		util.Flash(`Two-factor authentication is off.`, data, 1, "", 0)
		return c.Redirect(http.StatusSeeOther, "/account/2fa")
	}, auth, manageLimit)

	e.POST("/account/2fa/recovery-codes", func(c echo.Context) error {
		data := c.Get("data").(*util.Data)
		sessionHash, _ := c.Get("_id").(string)
		codes, err := regenerateRecoveryCodes(data.User.Id, sessionHash, c.FormValue("password"), c.FormValue("code"), time.Now())
		if err != nil {
			return twoFactorChangeFailed(c, err)
		}
		return renderTwoFactorPage(c, codes)
	}, auth, manageLimit)
}
