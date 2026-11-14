package routes

import (
	"database/sql"
	"database/sql/driver"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"goexpenses/util"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/labstack/echo/v4"
)

const (
	twoFactorTestUser     = 7
	twoFactorTestSession  = "current-session-hash"
	twoFactorTestPassword = "correct-password-1"
)

var twoFactorTestKey = base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))

// twoFactorTestPasswordHash is hashed once: bcrypt is slow on purpose.
var twoFactorTestPasswordHash = sync.OnceValue(func() string {
	hash, err := util.HashPassword(twoFactorTestPassword)
	if err != nil {
		panic(err)
	}
	return hash
})

func useTwoFactorKey(t *testing.T, key string) {
	t.Helper()
	previous := util.Settings.TOTPKey
	util.Settings.TOTPKey = key
	t.Cleanup(func() { util.Settings.TOTPKey = previous })
}

type twoFactorFixture struct {
	t         *testing.T
	mock      sqlmock.Sqlmock
	now       time.Time
	secret    string
	encrypted string
}

// newTwoFactorFixture mocks the database and prepares an encrypted secret.
// Unmet or unexpected database calls fail the test.
func newTwoFactorFixture(t *testing.T) *twoFactorFixture {
	t.Helper()
	mock, _ := useMockRouteDatabase(t)
	useTwoFactorKey(t, twoFactorTestKey)
	secret, err := util.NewTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := util.EncryptTOTPSecret(secret)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("database expectations: %v", err)
		}
	})
	return &twoFactorFixture{t: t, mock: mock, now: time.Unix(1_800_000_000, 0), secret: secret, encrypted: encrypted}
}

func (fx *twoFactorFixture) step() int64 { return util.TOTPStep(fx.now) }

func (fx *twoFactorFixture) code() string {
	fx.t.Helper()
	code, err := util.TOTPCode(fx.secret, fx.step())
	if err != nil {
		fx.t.Fatal(err)
	}
	return code
}

// wrongCode differs from the codes of all three accepted steps.
func (fx *twoFactorFixture) wrongCode() string {
	for _, candidate := range []string{"000000", "111111", "222222", "333333"} {
		if _, ok := util.MatchTOTP(fx.secret, candidate, fx.now); !ok {
			return candidate
		}
	}
	fx.t.Fatal("no wrong code found")
	return ""
}

func (fx *twoFactorFixture) expectPending(query string, userID any, failures int, fresh bool) {
	fx.mock.ExpectQuery(regexp.QuoteMeta(query)).WithArgs(twoFactorTestSession).
		WillReturnRows(sqlmock.NewRows([]string{"pending_2fa_user_id", "pending_2fa_failures", "fresh"}).AddRow(userID, failures, fresh))
}

// expectLoginLocks expects the start of the sign-in transaction: the pending
// step read without a lock, then the user row and the session row locked in
// that order.
func (fx *twoFactorFixture) expectLoginLocks(failures int, fresh bool) {
	fx.expectPending(pendingLoginQuery, twoFactorTestUser, failures, fresh)
	fx.mock.ExpectBegin()
	fx.mock.ExpectQuery(regexp.QuoteMeta(lockTwoFactorLoginUserQuery)).WithArgs(twoFactorTestUser).
		WillReturnRows(sqlmock.NewRows([]string{"totp_secret"}).AddRow(fx.encrypted))
	fx.expectPending(lockPendingLoginQuery, twoFactorTestUser, failures, fresh)
}

func (fx *twoFactorFixture) expectExec(query string, rows int64, args ...driver.Value) {
	fx.mock.ExpectExec(regexp.QuoteMeta(query)).WithArgs(args...).WillReturnResult(sqlmock.NewResult(0, rows))
}

func (fx *twoFactorFixture) expectEvent(kind, status string) {
	fx.expectExec(recordTwoFactorEventQuery, 1, kind, status, twoFactorTestUser)
}

func (fx *twoFactorFixture) expectStoredPassword() {
	fx.mock.ExpectQuery(regexp.QuoteMeta(`SELECT password FROM users WHERE id = $1 AND blocked_at IS NULL`)).
		WithArgs(twoFactorTestUser).
		WillReturnRows(sqlmock.NewRows([]string{"password"}).AddRow(twoFactorTestPasswordHash()))
}

func (fx *twoFactorFixture) expectLockedUser(enabled bool) {
	var secret any
	if enabled {
		secret = fx.encrypted
	}
	fx.mock.ExpectQuery(regexp.QuoteMeta(lockTwoFactorUserQuery)).WithArgs(twoFactorTestUser).
		WillReturnRows(sqlmock.NewRows([]string{"password", "totp_secret", "totp_enabled"}).AddRow(twoFactorTestPasswordHash(), secret, enabled))
}

func (fx *twoFactorFixture) expectSetupSecret(query string, fresh bool) {
	fx.mock.ExpectQuery(regexp.QuoteMeta(query)).WithArgs(twoFactorTestSession).
		WillReturnRows(sqlmock.NewRows([]string{"pending_totp_secret", "fresh"}).AddRow(fx.encrypted, fresh))
}

func (fx *twoFactorFixture) expectNewRecoveryCodes() {
	fx.expectExec(deleteRecoveryCodesQuery, 0, twoFactorTestUser)
	for i := 0; i < recoveryCodeCount; i++ {
		fx.expectExec(insertRecoveryCodeQuery, 1, twoFactorTestUser, sqlmock.AnyArg())
	}
}

func loginTestUser() util.User {
	return util.User{Id: twoFactorTestUser, Username: "user", Password: "verified-hash"}
}

func TestSessionQueriesCoverPendingState(t *testing.T) {
	// A pending session is signed out, whatever the session held before.
	if !strings.Contains(rotatePendingSessionQuery, "user_id = 0") {
		t.Error("the pending rotation does not sign the session out")
	}
	for _, column := range []string{"pending_2fa_user_id = NULL", "pending_2fa_at = NULL", "pending_2fa_failures = 0", "pending_totp_secret = NULL", "pending_totp_created_at = NULL"} {
		if !strings.Contains(rotateSessionQuery, column) {
			t.Errorf("signing in does not reset %q", column)
		}
	}
	// Without the parentheses the current session would be revoked too.
	if want := `DELETE FROM sessions WHERE (user_id = $1 OR pending_2fa_user_id = $1) AND uuid <> $2`; deleteOtherUserSessionsQuery != want {
		t.Errorf("other-sessions query = %q, want %q", deleteOtherUserSessionsQuery, want)
	}
	if want := `DELETE FROM sessions WHERE (user_id = $1 OR pending_2fa_user_id = $1)`; deleteUserSessionsQuery != want {
		t.Errorf("all-sessions query = %q, want %q", deleteUserSessionsQuery, want)
	}
}

func TestBeginLoginWithTwoFactorOnlyMakesSessionPending(t *testing.T) {
	fx := newTwoFactorFixture(t)
	fx.mock.ExpectBegin()
	fx.mock.ExpectQuery(regexp.QuoteMeta(lockLoginUserQuery)).WithArgs(twoFactorTestUser).
		WillReturnRows(sqlmock.NewRows([]string{"password", "totp_enabled_at"}).AddRow("verified-hash", time.Now()))
	fx.expectExec(rotatePendingSessionQuery, 1, sqlmock.AnyArg(), sqlmock.AnyArg(), twoFactorTestUser, twoFactorTestSession)
	fx.mock.ExpectCommit()

	start, err := beginLogin(twoFactorTestSession, loginTestUser())
	if err != nil {
		t.Fatal(err)
	}
	if !start.Pending || start.TokenHash == "" || start.TokenHash == twoFactorTestSession {
		t.Fatalf("login start = %+v, want a pending session with a rotated token", start)
	}
	if hash, err := util.HashSessionToken(start.Token); err != nil || hash != start.TokenHash {
		t.Fatal("the returned token does not match the stored hash")
	}
}

func TestBeginLoginWithoutTwoFactorSignsIn(t *testing.T) {
	fx := newTwoFactorFixture(t)
	fx.mock.ExpectBegin()
	fx.mock.ExpectQuery(regexp.QuoteMeta(lockLoginUserQuery)).WithArgs(twoFactorTestUser).
		WillReturnRows(sqlmock.NewRows([]string{"password", "totp_enabled_at"}).AddRow("verified-hash", nil))
	// The same statement clears a pending step left by a sign-in attempt for
	// another account (see TestSessionQueriesCoverPendingState).
	fx.expectExec(rotateSessionQuery, 1, sqlmock.AnyArg(), twoFactorTestUser, sqlmock.AnyArg(), twoFactorTestSession)
	fx.expectExec(recordLoginQuery, 1, twoFactorTestUser)
	fx.mock.ExpectCommit()

	start, err := beginLogin(twoFactorTestSession, loginTestUser())
	if err != nil {
		t.Fatal(err)
	}
	if start.Pending || start.TokenHash == twoFactorTestSession {
		t.Fatalf("login start = %+v, want a signed-in session with a rotated token", start)
	}
}

func TestBeginLoginRefusesPasswordChangedAfterCheck(t *testing.T) {
	for name, rows := range map[string]*sqlmock.Rows{
		"password was reset": sqlmock.NewRows([]string{"password", "totp_enabled_at"}).AddRow("new-hash", nil),
		"user was blocked":   sqlmock.NewRows([]string{"password", "totp_enabled_at"}),
	} {
		t.Run(name, func(t *testing.T) {
			fx := newTwoFactorFixture(t)
			fx.mock.ExpectBegin()
			fx.mock.ExpectQuery(regexp.QuoteMeta(lockLoginUserQuery)).WithArgs(twoFactorTestUser).WillReturnRows(rows)
			fx.mock.ExpectRollback()
			if _, err := beginLogin(twoFactorTestSession, loginTestUser()); !errors.Is(err, errInvalidCredentials) {
				t.Fatalf("error = %v, want invalid credentials", err)
			}
		})
	}
}

func TestBeginLoginRollsBackWhenSessionIsGone(t *testing.T) {
	fx := newTwoFactorFixture(t)
	fx.mock.ExpectBegin()
	fx.mock.ExpectQuery(regexp.QuoteMeta(lockLoginUserQuery)).WithArgs(twoFactorTestUser).
		WillReturnRows(sqlmock.NewRows([]string{"password", "totp_enabled_at"}).AddRow("verified-hash", time.Now()))
	fx.expectExec(rotatePendingSessionQuery, 0, sqlmock.AnyArg(), sqlmock.AnyArg(), twoFactorTestUser, twoFactorTestSession)
	fx.mock.ExpectRollback()
	if start, err := beginLogin(twoFactorTestSession, loginTestUser()); err == nil || start.Token != "" {
		t.Fatalf("login start = %+v, %v; want an error and no token", start, err)
	}
}

func TestTwoFactorLoginAcceptsCodeAndRotatesSession(t *testing.T) {
	fx := newTwoFactorFixture(t)
	fx.expectLoginLocks(0, true)
	fx.expectExec(consumeTOTPStepQuery, 1, fx.step(), twoFactorTestUser)
	fx.expectExec(rotateSessionQuery, 1, sqlmock.AnyArg(), twoFactorTestUser, sqlmock.AnyArg(), twoFactorTestSession)
	fx.expectExec(recordLoginQuery, 1, twoFactorTestUser)
	fx.mock.ExpectCommit()

	result, err := completeTwoFactorLogin(twoFactorTestSession, fx.code(), "", fx.now)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != twoFactorOK || result.UserID != twoFactorTestUser || result.TokenHash == "" || result.TokenHash == twoFactorTestSession {
		t.Fatalf("result = %+v, want a sign-in with a rotated token", result)
	}
}

func TestTwoFactorLoginCountsWrongCode(t *testing.T) {
	fx := newTwoFactorFixture(t)
	fx.expectLoginLocks(1, true)
	fx.expectExec(countPendingFailureQuery, 1, twoFactorTestSession)
	fx.expectEvent("auth_2fa_failed", "failed")
	// The failure is committed, not rolled back.
	fx.mock.ExpectCommit()

	result, err := completeTwoFactorLogin(twoFactorTestSession, fx.wrongCode(), "", fx.now)
	if err != nil || result.Outcome != twoFactorWrongCode || result.Token != "" {
		t.Fatalf("result = %+v, %v; want a wrong code and no token", result, err)
	}
}

func TestTwoFactorLoginDropsPendingAfterFifthWrongCode(t *testing.T) {
	fx := newTwoFactorFixture(t)
	fx.expectLoginLocks(twoFactorMaxFailures-1, true)
	fx.expectExec(countPendingFailureQuery, 1, twoFactorTestSession)
	fx.expectExec(clearPendingLoginQuery, 1, twoFactorTestSession)
	fx.expectEvent("auth_2fa_failed", "failed")
	fx.mock.ExpectCommit()

	result, err := completeTwoFactorLogin(twoFactorTestSession, fx.wrongCode(), "", fx.now)
	if err != nil || result.Outcome != twoFactorLockedOut || result.Token != "" {
		t.Fatalf("result = %+v, %v; want a lock-out and no token", result, err)
	}
}

func TestTwoFactorLoginRefusesUnusablePending(t *testing.T) {
	for name, tc := range map[string]struct {
		failures int
		fresh    bool
	}{
		"older than five minutes": {0, false},
		"attempts used up":        {twoFactorMaxFailures, true},
	} {
		t.Run(name, func(t *testing.T) {
			fx := newTwoFactorFixture(t)
			fx.expectLoginLocks(tc.failures, tc.fresh)
			fx.expectExec(clearPendingLoginQuery, 1, twoFactorTestSession)
			fx.mock.ExpectCommit()
			// Even the right code does not help.
			result, err := completeTwoFactorLogin(twoFactorTestSession, fx.code(), "", fx.now)
			if err != nil || result.Outcome != twoFactorExpired || result.Token != "" {
				t.Fatalf("result = %+v, %v; want an expired sign-in", result, err)
			}
		})
	}
}

func TestTwoFactorLoginWithoutPendingStepDoesNothing(t *testing.T) {
	fx := newTwoFactorFixture(t)
	fx.expectPending(pendingLoginQuery, nil, 0, false)
	result, err := completeTwoFactorLogin(twoFactorTestSession, fx.code(), "", fx.now)
	if err != nil || result.Outcome != twoFactorExpired {
		t.Fatalf("result = %+v, %v; want an expired sign-in", result, err)
	}
}

// A user who was blocked, or whose two-factor authentication was removed,
// after the password step is not found by the locking query.
func TestTwoFactorLoginRefusesBlockedUserOrUserWithoutTwoFactor(t *testing.T) {
	fx := newTwoFactorFixture(t)
	fx.expectPending(pendingLoginQuery, twoFactorTestUser, 0, true)
	fx.mock.ExpectBegin()
	fx.mock.ExpectQuery(regexp.QuoteMeta(lockTwoFactorLoginUserQuery)).WithArgs(twoFactorTestUser).WillReturnError(sql.ErrNoRows)
	fx.expectPending(lockPendingLoginQuery, twoFactorTestUser, 0, true)
	fx.expectExec(clearPendingLoginQuery, 1, twoFactorTestSession)
	fx.mock.ExpectCommit()

	result, err := completeTwoFactorLogin(twoFactorTestSession, fx.code(), "", fx.now)
	if err != nil || result.Outcome != twoFactorExpired || result.Token != "" {
		t.Fatalf("result = %+v, %v; want an expired sign-in", result, err)
	}
	if !strings.Contains(lockTwoFactorLoginUserQuery, "blocked_at IS NULL AND email_verified = true AND totp_enabled_at IS NOT NULL") {
		t.Error("the locking query does not check block, verification and two-factor state")
	}
}

// A session that meanwhile started a sign-in for another user is left alone.
func TestTwoFactorLoginLeavesPendingOfAnotherUser(t *testing.T) {
	fx := newTwoFactorFixture(t)
	fx.expectPending(pendingLoginQuery, twoFactorTestUser, 0, true)
	fx.mock.ExpectBegin()
	fx.mock.ExpectQuery(regexp.QuoteMeta(lockTwoFactorLoginUserQuery)).WithArgs(twoFactorTestUser).
		WillReturnRows(sqlmock.NewRows([]string{"totp_secret"}).AddRow(fx.encrypted))
	fx.expectPending(lockPendingLoginQuery, twoFactorTestUser+1, 0, true)
	fx.mock.ExpectCommit()

	result, err := completeTwoFactorLogin(twoFactorTestSession, fx.code(), "", fx.now)
	if err != nil || result.Outcome != twoFactorExpired {
		t.Fatalf("result = %+v, %v; want an expired sign-in", result, err)
	}
}

func TestTwoFactorLoginRefusesRepeatedStep(t *testing.T) {
	fx := newTwoFactorFixture(t)
	fx.expectLoginLocks(0, true)
	// The step is not newer than totp_last_step: the update changes nothing.
	fx.expectExec(consumeTOTPStepQuery, 0, fx.step(), twoFactorTestUser)
	fx.expectExec(countPendingFailureQuery, 1, twoFactorTestSession)
	fx.expectEvent("auth_2fa_failed", "failed")
	fx.mock.ExpectCommit()

	result, err := completeTwoFactorLogin(twoFactorTestSession, fx.code(), "", fx.now)
	if err != nil || result.Outcome != twoFactorWrongCode {
		t.Fatalf("result = %+v, %v; want the repeated code refused", result, err)
	}
}

func (fx *twoFactorFixture) expectUnusedRecoveryCodes(codes ...string) {
	rows := sqlmock.NewRows([]string{"id", "code_hash"})
	for i, code := range codes {
		hash, err := util.HashPassword(util.NormalizeRecoveryCode(code))
		if err != nil {
			fx.t.Fatal(err)
		}
		rows.AddRow(41+i, hash)
	}
	fx.mock.ExpectQuery(regexp.QuoteMeta(unusedRecoveryCodesQuery)).WithArgs(twoFactorTestUser).WillReturnRows(rows)
}

func TestTwoFactorLoginAcceptsRecoveryCodeOnce(t *testing.T) {
	for name, spent := range map[string]int64{"first use": 1, "second use": 0} {
		t.Run(name, func(t *testing.T) {
			fx := newTwoFactorFixture(t)
			fx.expectPending(pendingLoginQuery, twoFactorTestUser, 0, true)
			// bcrypt runs before the transaction and finds the second code.
			fx.expectUnusedRecoveryCodes("AAAAA-AAAAA", "ABCDE-FGHJK")
			fx.mock.ExpectBegin()
			fx.mock.ExpectQuery(regexp.QuoteMeta(lockTwoFactorLoginUserQuery)).WithArgs(twoFactorTestUser).
				WillReturnRows(sqlmock.NewRows([]string{"totp_secret"}).AddRow(fx.encrypted))
			fx.expectPending(lockPendingLoginQuery, twoFactorTestUser, 0, true)
			fx.expectExec(consumeRecoveryCodeQuery, spent, 42, twoFactorTestUser)
			want := twoFactorOK
			if spent == 1 {
				fx.expectExec(rotateSessionQuery, 1, sqlmock.AnyArg(), twoFactorTestUser, sqlmock.AnyArg(), twoFactorTestSession)
				fx.expectExec(recordLoginQuery, 1, twoFactorTestUser)
				fx.expectEvent("auth_2fa_recovery_used", "success")
			} else {
				// A concurrent request spent the code first.
				want = twoFactorWrongCode
				fx.expectExec(countPendingFailureQuery, 1, twoFactorTestSession)
				fx.expectEvent("auth_2fa_failed", "failed")
			}
			fx.mock.ExpectCommit()

			result, err := completeTwoFactorLogin(twoFactorTestSession, "", " abcde fghjk ", fx.now)
			if err != nil || result.Outcome != want {
				t.Fatalf("result = %+v, %v; want outcome %d", result, err, want)
			}
		})
	}
}

func TestTwoFactorLoginCountsUnknownRecoveryCode(t *testing.T) {
	fx := newTwoFactorFixture(t)
	fx.expectPending(pendingLoginQuery, twoFactorTestUser, 0, true)
	fx.expectUnusedRecoveryCodes("AAAAA-AAAAA")
	fx.mock.ExpectBegin()
	fx.mock.ExpectQuery(regexp.QuoteMeta(lockTwoFactorLoginUserQuery)).WithArgs(twoFactorTestUser).
		WillReturnRows(sqlmock.NewRows([]string{"totp_secret"}).AddRow(fx.encrypted))
	fx.expectPending(lockPendingLoginQuery, twoFactorTestUser, 0, true)
	fx.expectExec(countPendingFailureQuery, 1, twoFactorTestSession)
	fx.expectEvent("auth_2fa_failed", "failed")
	fx.mock.ExpectCommit()

	result, err := completeTwoFactorLogin(twoFactorTestSession, "", "ZZZZZ-ZZZZZ", fx.now)
	if err != nil || result.Outcome != twoFactorWrongCode {
		t.Fatalf("result = %+v, %v; want a wrong code", result, err)
	}
}

// If rotating the session or recording the sign-in fails, the transaction is
// rolled back: the code stays unspent and no token is handed out.
func TestTwoFactorLoginRollsBackWhenSignInCannotBeCompleted(t *testing.T) {
	failure := errors.New("database failure")
	for name, failAtEvent := range map[string]bool{"session rotation fails": false, "sign-in event fails": true} {
		t.Run(name, func(t *testing.T) {
			fx := newTwoFactorFixture(t)
			fx.expectLoginLocks(0, true)
			fx.expectExec(consumeTOTPStepQuery, 1, fx.step(), twoFactorTestUser)
			rotation := fx.mock.ExpectExec(regexp.QuoteMeta(rotateSessionQuery))
			if failAtEvent {
				rotation.WillReturnResult(sqlmock.NewResult(0, 1))
				fx.mock.ExpectExec(regexp.QuoteMeta(recordLoginQuery)).WillReturnError(failure)
			} else {
				rotation.WillReturnError(failure)
			}
			fx.mock.ExpectRollback()

			result, err := completeTwoFactorLogin(twoFactorTestSession, fx.code(), "", fx.now)
			if !errors.Is(err, failure) || result.Outcome == twoFactorOK || result.Token != "" {
				t.Fatalf("result = %+v, %v; want the failure and no token", result, err)
			}
		})
	}
}

func TestTwoFactorLoginHandlerSetsCookieOnlyAfterSuccess(t *testing.T) {
	fx := newTwoFactorFixture(t)
	fx.expectLoginLocks(0, true)
	fx.mock.ExpectExec(regexp.QuoteMeta(consumeTOTPStepQuery)).WillReturnResult(sqlmock.NewResult(0, 1))
	fx.mock.ExpectExec(regexp.QuoteMeta(rotateSessionQuery)).WillReturnError(errors.New("database failure"))
	fx.mock.ExpectRollback()

	code, err := util.TOTPCode(fx.secret, util.TOTPStep(time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	c, recorder := twoFactorTestContext(http.MethodPost, "/login/2fa", url.Values{"code": {code}}, util.User{})
	if err := submitTwoFactorLogin(c); err == nil {
		t.Fatal("a failed sign-in returned no error")
	}
	if cookies := recorder.Result().Cookies(); len(cookies) != 0 {
		t.Fatalf("a failed sign-in set cookies: %+v", cookies)
	}
}

// Without a usable key no TOTP code can be checked. Sign-in fails closed: the
// second factor is not skipped, but recovery codes do not need the key.
func TestTwoFactorLoginFailsClosedWithoutKey(t *testing.T) {
	for _, key := range []string{"", "bm90IGEga2V5"} {
		fx := newTwoFactorFixture(t)
		util.Settings.TOTPKey = key
		fx.expectLoginLocks(0, true)
		fx.expectEvent("auth_2fa_key_error", "failed")
		fx.mock.ExpectCommit()

		result, err := completeTwoFactorLogin(twoFactorTestSession, fx.code(), "", fx.now)
		if err != nil || result.Outcome != twoFactorKeyError || result.Token != "" {
			t.Fatalf("key %q: result = %+v, %v; want a key error and no token", key, result, err)
		}
	}
}

func TestTwoFactorLoginAcceptsRecoveryCodeWithoutKey(t *testing.T) {
	fx := newTwoFactorFixture(t)
	util.Settings.TOTPKey = ""
	fx.expectPending(pendingLoginQuery, twoFactorTestUser, 0, true)
	fx.expectUnusedRecoveryCodes("ABCDE-FGHJK")
	fx.mock.ExpectBegin()
	fx.mock.ExpectQuery(regexp.QuoteMeta(lockTwoFactorLoginUserQuery)).WithArgs(twoFactorTestUser).
		WillReturnRows(sqlmock.NewRows([]string{"totp_secret"}).AddRow(fx.encrypted))
	fx.expectPending(lockPendingLoginQuery, twoFactorTestUser, 0, true)
	fx.expectExec(consumeRecoveryCodeQuery, 1, 41, twoFactorTestUser)
	fx.expectExec(rotateSessionQuery, 1, sqlmock.AnyArg(), twoFactorTestUser, sqlmock.AnyArg(), twoFactorTestSession)
	fx.expectExec(recordLoginQuery, 1, twoFactorTestUser)
	fx.expectEvent("auth_2fa_recovery_used", "success")
	fx.mock.ExpectCommit()

	result, err := completeTwoFactorLogin(twoFactorTestSession, "", "ABCDE-FGHJK", fx.now)
	if err != nil || result.Outcome != twoFactorOK {
		t.Fatalf("result = %+v, %v; want a sign-in", result, err)
	}
}

func TestEnableTwoFactorKeepsOnlyCurrentSession(t *testing.T) {
	fx := newTwoFactorFixture(t)
	fx.expectStoredPassword()
	fx.expectSetupSecret(setupSecretQuery, true)
	fx.mock.ExpectBegin()
	fx.expectLockedUser(false)
	// The stored secret stays encrypted; the step of the first code is spent.
	fx.expectExec(enableTwoFactorQuery, 1, fx.encrypted, fx.step(), twoFactorTestUser)
	fx.expectSetupSecret(lockSetupSecretQuery, true)
	fx.expectExec(clearSetupSecretQuery, 1, twoFactorTestSession)
	fx.expectExec(deleteOtherUserSessionsQuery, 2, twoFactorTestUser, twoFactorTestSession)
	fx.expectNewRecoveryCodes()
	fx.expectEvent("2fa_enabled", "success")
	fx.mock.ExpectCommit()

	codes, err := enableTwoFactor(twoFactorTestUser, twoFactorTestSession, twoFactorTestPassword, fx.code(), fx.now)
	if err != nil {
		t.Fatal(err)
	}
	if len(codes) != recoveryCodeCount {
		t.Fatalf("%d recovery codes, want %d", len(codes), recoveryCodeCount)
	}
}

func TestEnableTwoFactorRefusals(t *testing.T) {
	t.Run("setup secret expired", func(t *testing.T) {
		fx := newTwoFactorFixture(t)
		fx.expectStoredPassword()
		fx.expectSetupSecret(setupSecretQuery, false)
		if _, err := enableTwoFactor(twoFactorTestUser, twoFactorTestSession, twoFactorTestPassword, fx.code(), fx.now); !errors.Is(err, errTwoFactorSetupExpired) {
			t.Fatalf("error = %v, want an expired setup", err)
		}
	})
	t.Run("wrong code", func(t *testing.T) {
		fx := newTwoFactorFixture(t)
		fx.expectStoredPassword()
		fx.expectSetupSecret(setupSecretQuery, true)
		if _, err := enableTwoFactor(twoFactorTestUser, twoFactorTestSession, twoFactorTestPassword, fx.wrongCode(), fx.now); !errors.Is(err, errTwoFactorCode) {
			t.Fatalf("error = %v, want an invalid code", err)
		}
	})
	t.Run("already enabled", func(t *testing.T) {
		fx := newTwoFactorFixture(t)
		fx.expectStoredPassword()
		fx.expectSetupSecret(setupSecretQuery, true)
		fx.mock.ExpectBegin()
		fx.expectLockedUser(true)
		fx.expectExec(enableTwoFactorQuery, 0, fx.encrypted, fx.step(), twoFactorTestUser)
		fx.mock.ExpectRollback()
		if _, err := enableTwoFactor(twoFactorTestUser, twoFactorTestSession, twoFactorTestPassword, fx.code(), fx.now); !errors.Is(err, errTwoFactorState) {
			t.Fatalf("error = %v, want a state error", err)
		}
	})
	t.Run("setup secret replaced before the lock", func(t *testing.T) {
		fx := newTwoFactorFixture(t)
		fx.expectStoredPassword()
		fx.expectSetupSecret(setupSecretQuery, true)
		fx.mock.ExpectBegin()
		fx.expectLockedUser(false)
		fx.expectExec(enableTwoFactorQuery, 1, fx.encrypted, fx.step(), twoFactorTestUser)
		fx.mock.ExpectQuery(regexp.QuoteMeta(lockSetupSecretQuery)).WithArgs(twoFactorTestSession).
			WillReturnRows(sqlmock.NewRows([]string{"pending_totp_secret", "fresh"}).AddRow("another-secret", true))
		fx.mock.ExpectRollback()
		if _, err := enableTwoFactor(twoFactorTestUser, twoFactorTestSession, twoFactorTestPassword, fx.code(), fx.now); !errors.Is(err, errTwoFactorSetupExpired) {
			t.Fatalf("error = %v, want an expired setup", err)
		}
	})
	t.Run("password changed before the lock", func(t *testing.T) {
		fx := newTwoFactorFixture(t)
		fx.expectStoredPassword()
		fx.expectSetupSecret(setupSecretQuery, true)
		fx.mock.ExpectBegin()
		fx.mock.ExpectQuery(regexp.QuoteMeta(lockTwoFactorUserQuery)).WithArgs(twoFactorTestUser).
			WillReturnRows(sqlmock.NewRows([]string{"password", "totp_secret", "totp_enabled"}).AddRow("new-hash", nil, false))
		fx.mock.ExpectRollback()
		if _, err := enableTwoFactor(twoFactorTestUser, twoFactorTestSession, twoFactorTestPassword, fx.code(), fx.now); !errors.Is(err, errInvalidCredentials) {
			t.Fatalf("error = %v, want invalid credentials", err)
		}
	})
}

// Enable, disable and regenerate stop at a wrong password before they open a
// transaction or look at the code.
func TestTwoFactorChangesRequireCurrentPassword(t *testing.T) {
	for name, change := range map[string]func(fx *twoFactorFixture) error{
		"enable": func(fx *twoFactorFixture) error {
			_, err := enableTwoFactor(twoFactorTestUser, twoFactorTestSession, "wrong-password-1", fx.code(), fx.now)
			return err
		},
		"disable": func(fx *twoFactorFixture) error {
			return disableTwoFactor(twoFactorTestUser, twoFactorTestSession, "wrong-password-1", fx.code(), "", fx.now)
		},
		"regenerate": func(fx *twoFactorFixture) error {
			_, err := regenerateRecoveryCodes(twoFactorTestUser, twoFactorTestSession, "wrong-password-1", fx.code(), fx.now)
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			fx := newTwoFactorFixture(t)
			fx.expectStoredPassword()
			if err := change(fx); !errors.Is(err, errInvalidCredentials) {
				t.Fatalf("error = %v, want invalid credentials", err)
			}
		})
	}
}

func TestDisableTwoFactorWithCodeKeepsOnlyCurrentSession(t *testing.T) {
	fx := newTwoFactorFixture(t)
	fx.expectStoredPassword()
	fx.mock.ExpectBegin()
	fx.expectLockedUser(true)
	fx.expectExec(consumeTOTPStepQuery, 1, fx.step(), twoFactorTestUser)
	fx.expectExec(removeTwoFactorQuery, 1, twoFactorTestUser)
	fx.expectExec(deleteOtherUserSessionsQuery, 1, twoFactorTestUser, twoFactorTestSession)
	fx.expectExec(deleteRecoveryCodesQuery, 9, twoFactorTestUser)
	fx.expectEvent("2fa_disabled", "success")
	fx.mock.ExpectCommit()

	if err := disableTwoFactor(twoFactorTestUser, twoFactorTestSession, twoFactorTestPassword, fx.code(), "", fx.now); err != nil {
		t.Fatal(err)
	}
}

func TestDisableTwoFactorWithRecoveryCode(t *testing.T) {
	fx := newTwoFactorFixture(t)
	fx.expectStoredPassword()
	fx.expectUnusedRecoveryCodes("ABCDE-FGHJK")
	fx.mock.ExpectBegin()
	fx.expectLockedUser(true)
	fx.expectExec(removeTwoFactorQuery, 1, twoFactorTestUser)
	fx.expectExec(deleteOtherUserSessionsQuery, 0, twoFactorTestUser, twoFactorTestSession)
	fx.expectExec(consumeRecoveryCodeQuery, 1, 41, twoFactorTestUser)
	fx.expectExec(deleteRecoveryCodesQuery, 1, twoFactorTestUser)
	fx.expectEvent("2fa_disabled", "success")
	fx.mock.ExpectCommit()

	if err := disableTwoFactor(twoFactorTestUser, twoFactorTestSession, twoFactorTestPassword, "", "abcde-fghjk", fx.now); err != nil {
		t.Fatal(err)
	}
}

func TestDisableTwoFactorRefusesWrongCode(t *testing.T) {
	fx := newTwoFactorFixture(t)
	fx.expectStoredPassword()
	fx.mock.ExpectBegin()
	fx.expectLockedUser(true)
	fx.mock.ExpectRollback()
	if err := disableTwoFactor(twoFactorTestUser, twoFactorTestSession, twoFactorTestPassword, fx.wrongCode(), "", fx.now); !errors.Is(err, errTwoFactorCode) {
		t.Fatalf("error = %v, want an invalid code", err)
	}
}

func TestRegenerateRecoveryCodesKeepsOnlyCurrentSession(t *testing.T) {
	fx := newTwoFactorFixture(t)
	fx.expectStoredPassword()
	fx.mock.ExpectBegin()
	fx.expectLockedUser(true)
	fx.expectExec(consumeTOTPStepQuery, 1, fx.step(), twoFactorTestUser)
	fx.expectExec(deleteOtherUserSessionsQuery, 1, twoFactorTestUser, twoFactorTestSession)
	fx.expectNewRecoveryCodes()
	fx.expectEvent("2fa_recovery_regenerated", "success")
	fx.mock.ExpectCommit()

	codes, err := regenerateRecoveryCodes(twoFactorTestUser, twoFactorTestSession, twoFactorTestPassword, fx.code(), fx.now)
	if err != nil || len(codes) != recoveryCodeCount {
		t.Fatalf("%d codes, %v; want %d", len(codes), err, recoveryCodeCount)
	}
}

func TestRegenerateRecoveryCodesRefusesRepeatedStep(t *testing.T) {
	fx := newTwoFactorFixture(t)
	fx.expectStoredPassword()
	fx.mock.ExpectBegin()
	fx.expectLockedUser(true)
	fx.expectExec(consumeTOTPStepQuery, 0, fx.step(), twoFactorTestUser)
	fx.mock.ExpectRollback()
	if _, err := regenerateRecoveryCodes(twoFactorTestUser, twoFactorTestSession, twoFactorTestPassword, fx.code(), fx.now); !errors.Is(err, errTwoFactorCode) {
		t.Fatalf("error = %v, want an invalid code", err)
	}
}

func TestSetupSecretIsReusedForTenMinutes(t *testing.T) {
	fx := newTwoFactorFixture(t)
	fx.expectSetupSecret(setupSecretQuery, true)
	secret, err := currentSetupSecret(twoFactorTestSession)
	if err != nil || secret != fx.secret {
		t.Fatalf("a fresh setup secret was not reused: %v", err)
	}

	fx.expectSetupSecret(setupSecretQuery, false)
	fx.expectExec(storeSetupSecretQuery, 1, sqlmock.AnyArg(), twoFactorTestSession)
	secret, err = currentSetupSecret(twoFactorTestSession)
	if err != nil || secret == fx.secret || len(secret) != len(fx.secret) {
		t.Fatalf("an expired setup secret was not replaced: %v", err)
	}
	if !strings.Contains(setupSecretQuery, "INTERVAL '10 minutes'") || !strings.Contains(pendingLoginQuery, "INTERVAL '5 minutes'") {
		t.Error("setup secret or pending step has an unexpected lifetime")
	}
}

func TestAdminResetTwoFactorRecordsActorAndReason(t *testing.T) {
	fx := newTwoFactorFixture(t)
	fx.mock.ExpectBegin()
	fx.mock.ExpectQuery(regexp.QuoteMeta(lockTwoFactorTargetQuery)).WithArgs(12).
		WillReturnRows(sqlmock.NewRows([]string{"username", "is_admin", "totp_enabled"}).AddRow("target", false, true))
	fx.expectExec(removeTwoFactorQuery, 1, 12)
	fx.expectExec(deleteUserSessionsQuery, 3, 12)
	fx.expectExec(deleteRecoveryCodesQuery, 10, 12)
	fx.expectExec(recordAdminResetQuery, 1, 12, twoFactorTestUser, "target", "Lost phone")
	fx.mock.ExpectCommit()

	if err := resetUserTwoFactor(twoFactorTestUser, 12, "Lost phone"); err != nil {
		t.Fatal(err)
	}
}

func TestAdminResetTwoFactorDoesNotApplyToAdmins(t *testing.T) {
	fx := newTwoFactorFixture(t)
	fx.mock.ExpectBegin()
	fx.mock.ExpectQuery(regexp.QuoteMeta(lockTwoFactorTargetQuery)).WithArgs(12).
		WillReturnRows(sqlmock.NewRows([]string{"username", "is_admin", "totp_enabled"}).AddRow("other-admin", true, true))
	fx.mock.ExpectRollback()

	err := resetUserTwoFactor(twoFactorTestUser, 12, "Lost phone")
	if httpError, ok := err.(*echo.HTTPError); !ok || httpError.Code != http.StatusForbidden {
		t.Fatalf("error = %v, want 403", err)
	}
}

type capturedPage struct {
	name string
	data any
}

func (page *capturedPage) Render(_ io.Writer, name string, data interface{}, _ echo.Context) error {
	page.name, page.data = name, data
	return nil
}

func twoFactorTestContext(method, target string, form url.Values, user util.User) (echo.Context, *httptest.ResponseRecorder) {
	request := httptest.NewRequest(method, target, strings.NewReader(form.Encode()))
	request.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	recorder := httptest.NewRecorder()
	e := echo.New()
	e.Renderer = &capturedPage{}
	c := e.NewContext(request, recorder)
	c.Set("_id", twoFactorTestSession)
	c.Set("data", &util.Data{CookieId: twoFactorTestSession, User: user, Lang: "EN"})
	return c, recorder
}

func TestAdminResetTwoFactorRequiresAdminPasswordAndReason(t *testing.T) {
	admin := util.User{Id: twoFactorTestUser, IsAdmin: true}
	resetContext := func(form url.Values, user util.User) (echo.Context, *httptest.ResponseRecorder) {
		c, recorder := twoFactorTestContext(http.MethodPost, "/admin/users/12/2fa/reset", form, user)
		c.SetParamNames("id")
		c.SetParamValues("12")
		return c, recorder
	}

	t.Run("not an admin", func(t *testing.T) {
		newTwoFactorFixture(t)
		c, _ := resetContext(url.Values{"reason": {"Lost phone"}, "password": {twoFactorTestPassword}}, util.User{Id: 4})
		err := adminOnly(adminResetTwoFactor)(c)
		if httpError, ok := err.(*echo.HTTPError); !ok || httpError.Code != http.StatusForbidden {
			t.Fatalf("error = %v, want 403", err)
		}
	})
	t.Run("no reason", func(t *testing.T) {
		newTwoFactorFixture(t)
		c, _ := resetContext(url.Values{"reason": {" "}, "password": {twoFactorTestPassword}}, admin)
		err := adminOnly(adminResetTwoFactor)(c)
		if httpError, ok := err.(*echo.HTTPError); !ok || httpError.Code != http.StatusBadRequest {
			t.Fatalf("error = %v, want 400", err)
		}
	})
	t.Run("wrong password", func(t *testing.T) {
		fx := newTwoFactorFixture(t)
		fx.expectStoredPassword()
		// Nothing but the flash message is written.
		fx.mock.ExpectExec(`UPDATE sessions SET message`).WillReturnResult(sqlmock.NewResult(0, 1))
		c, recorder := resetContext(url.Values{"reason": {"Lost phone"}, "password": {"wrong-password-1"}}, admin)
		if err := adminOnly(adminResetTwoFactor)(c); err != nil {
			t.Fatal(err)
		}
		if recorder.Code != http.StatusSeeOther || recorder.Header().Get(echo.HeaderLocation) != "/admin/users/12" {
			t.Fatalf("response = %d to %q", recorder.Code, recorder.Header().Get(echo.HeaderLocation))
		}
	})
}

// assertProtectedPage checks that a page with second-factor secrets is kept
// out of caches and free of analytics and ad scripts.
func assertProtectedPage(t *testing.T, c echo.Context, recorder *httptest.ResponseRecorder, wantTemplate string) {
	t.Helper()
	page := c.Echo().Renderer.(*capturedPage)
	if page.name != wantTemplate {
		t.Fatalf("rendered %q, want %q", page.name, wantTemplate)
	}
	if !c.Get("data").(*util.Data).HideThirdPartyScripts {
		t.Errorf("%s loads third-party scripts", wantTemplate)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("%s Cache-Control = %q, want no-store", wantTemplate, got)
	}
}

func TestTwoFactorPagesHideThirdPartyScripts(t *testing.T) {
	t.Run("second sign-in step", func(t *testing.T) {
		fx := newTwoFactorFixture(t)
		fx.expectPending(pendingLoginQuery, twoFactorTestUser, 0, true)
		c, recorder := twoFactorTestContext(http.MethodGet, "/login/2fa?recovery=1&next=/admin", nil, util.User{})
		if err := showTwoFactorLogin(c); err != nil {
			t.Fatal(err)
		}
		assertProtectedPage(t, c, recorder, "login-2fa")
		page := c.Echo().Renderer.(*capturedPage).data.(twoFactorLoginPage)
		if !page.Recovery || page.KeyUnavailable || page.LoginNext != "/admin" {
			t.Fatalf("page = %+v", page)
		}
	})
	t.Run("second sign-in step without pending sign-in", func(t *testing.T) {
		fx := newTwoFactorFixture(t)
		fx.expectPending(pendingLoginQuery, twoFactorTestUser, 0, false)
		c, recorder := twoFactorTestContext(http.MethodGet, "/login/2fa", nil, util.User{})
		if err := showTwoFactorLogin(c); err != nil {
			t.Fatal(err)
		}
		if recorder.Code != http.StatusSeeOther || recorder.Header().Get(echo.HeaderLocation) != "/login" {
			t.Fatalf("response = %d to %q, want a redirect to /login", recorder.Code, recorder.Header().Get(echo.HeaderLocation))
		}
	})
	t.Run("setup", func(t *testing.T) {
		fx := newTwoFactorFixture(t)
		fx.mock.ExpectQuery(regexp.QuoteMeta(twoFactorEnabledQuery)).WithArgs(twoFactorTestUser).
			WillReturnRows(sqlmock.NewRows([]string{"enabled"}).AddRow(false))
		fx.expectSetupSecret(setupSecretQuery, true)
		c, recorder := twoFactorTestContext(http.MethodGet, "/account/2fa", nil, util.User{Id: twoFactorTestUser, Email: "user@example.com"})
		if err := renderTwoFactorPage(c, nil); err != nil {
			t.Fatal(err)
		}
		assertProtectedPage(t, c, recorder, "two-factor")
		page := c.Echo().Renderer.(*capturedPage).data.(twoFactorPage)
		if strings.ReplaceAll(page.Secret, " ", "") != fx.secret || !strings.HasPrefix(string(page.QRCode), "data:image/png;base64,") {
			t.Fatalf("setup page shows secret %q and QR code %.30q", page.Secret, page.QRCode)
		}
	})
	t.Run("status and new recovery codes", func(t *testing.T) {
		fx := newTwoFactorFixture(t)
		fx.mock.ExpectQuery(regexp.QuoteMeta(twoFactorEnabledQuery)).WithArgs(twoFactorTestUser).
			WillReturnRows(sqlmock.NewRows([]string{"enabled"}).AddRow(true))
		fx.mock.ExpectQuery(regexp.QuoteMeta(countRecoveryCodesQuery)).WithArgs(twoFactorTestUser).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(10))
		c, recorder := twoFactorTestContext(http.MethodPost, "/account/2fa/enable", nil, util.User{Id: twoFactorTestUser})
		if err := renderTwoFactorPage(c, []string{"ABCDE-FGHJK"}); err != nil {
			t.Fatal(err)
		}
		assertProtectedPage(t, c, recorder, "two-factor")
		page := c.Echo().Renderer.(*capturedPage).data.(twoFactorPage)
		// Once enabled, the secret is never shown again.
		if !page.Enabled || page.Secret != "" || page.QRCode != "" || page.RemainingCodeCount != 10 || len(page.RecoveryCodes) != 1 {
			t.Fatalf("page = %+v", page)
		}
	})
	t.Run("setup without key", func(t *testing.T) {
		fx := newTwoFactorFixture(t)
		util.Settings.TOTPKey = ""
		fx.mock.ExpectQuery(regexp.QuoteMeta(twoFactorEnabledQuery)).WithArgs(twoFactorTestUser).
			WillReturnRows(sqlmock.NewRows([]string{"enabled"}).AddRow(false))
		c, recorder := twoFactorTestContext(http.MethodGet, "/account/2fa", nil, util.User{Id: twoFactorTestUser})
		if err := renderTwoFactorPage(c, nil); err != nil {
			t.Fatal(err)
		}
		assertProtectedPage(t, c, recorder, "two-factor")
		if page := c.Echo().Renderer.(*capturedPage).data.(twoFactorPage); !page.KeyUnavailable || page.Secret != "" {
			t.Fatalf("page = %+v", page)
		}
	})
}

func TestTwoFactorRateLimitRules(t *testing.T) {
	fx := newTwoFactorFixture(t)
	fx.expectPending(pendingLoginQuery, twoFactorTestUser, 0, true)
	c, _ := twoFactorTestContext(http.MethodPost, "/login/2fa", url.Values{"code": {"123456"}}, util.User{})
	rules := twoFactorLoginRateLimitRules(c)
	// The user key comes from the session's pending step, not from the form
	// or the client address.
	if len(rules) != 2 || rules[0].key != "2fa:ip:"+c.RealIP() || rules[0].limit != 30 ||
		rules[1].key != "2fa:user:7" || rules[1].limit != 10 || rules[1].window != 15*time.Minute {
		t.Fatalf("rules = %+v", rules)
	}

	fx.expectPending(pendingLoginQuery, nil, 0, false)
	if rules := twoFactorLoginRateLimitRules(c); len(rules) != 1 || rules[0].key != "2fa:ip:"+c.RealIP() {
		t.Fatalf("rules without a pending step = %+v, want only the address rule", rules)
	}

	c, _ = twoFactorTestContext(http.MethodPost, "/account/2fa/disable", nil, util.User{Id: twoFactorTestUser})
	if rules := twoFactorManageRateLimitRules(c); len(rules) != 1 || rules[0].key != "2fa-manage:user:7" || rules[0].limit != 10 || rules[0].window != 15*time.Minute {
		t.Fatalf("manage rules = %+v", rules)
	}
}
