package routes

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"goexpenses/database"
	"goexpenses/internal/testdb"
	"goexpenses/migrations"
	"goexpenses/util"

	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo/v4"
	"github.com/lib/pq"
)

func TestPostgresRegistrationLoginAndPost(t *testing.T) {
	integration := testdb.Open(t)
	testDB, initialSchema := integration.DB, integration.InitialSchema

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Recreate the version-14 shape so CI executes the upgrade SQL as well as
	// checking that a newly initialized database contains the same protections.
	const removeDataIntegrityMigration = `
		DROP INDEX users_username_lower_uidx;
		DROP INDEX users_email_lower_uidx;
		DROP INDEX users_verification_token_uidx;
		DROP INDEX posts_public_id_uidx;
		DROP INDEX expenses_public_id_uidx;
		DROP INDEX incomes_public_id_uidx;
		DROP INDEX accountsusers_account_user_uidx;
		DROP INDEX posts_active_account_date_id_idx;
		DROP INDEX expenses_active_account_description_idx;
		DROP INDEX incomes_active_account_description_idx;
		DROP INDEX sessions_created_at_idx;
		DROP INDEX passwordresets_active_email_created_idx;
		DROP INDEX accountsusers_user_idx;
		DROP TABLE user_recovery_codes;
		ALTER TABLE users DROP COLUMN totp_secret, DROP COLUMN totp_enabled_at, DROP COLUMN totp_last_step;
		ALTER TABLE sessions
			DROP COLUMN pending_2fa_user_id, DROP COLUMN pending_2fa_at, DROP COLUMN pending_2fa_failures,
			DROP COLUMN pending_totp_secret, DROP COLUMN pending_totp_created_at;
		DROP TABLE account_invitations;
		DROP TABLE admin_events;
		DROP TABLE posts_history;
		DROP TRIGGER posts_history_trigger ON posts;
		DROP FUNCTION posts_history_record();
		ALTER TABLE accounts DROP COLUMN post_filter;
		ALTER TABLE accountsusers
			DROP COLUMN role,
			DROP COLUMN created_at,
			DROP COLUMN post_filter;
		ALTER TABLE users
			DROP CONSTRAINT users_name_not_blank,
			DROP CONSTRAINT users_username_not_blank,
			DROP CONSTRAINT users_email_not_blank,
			DROP CONSTRAINT users_default_account_fk,
			DROP COLUMN email_verified,
			DROP COLUMN verification_token,
			DROP COLUMN verification_sent_at;
		ALTER TABLE users
			DROP COLUMN is_admin,
			DROP COLUMN blocked_at,
			DROP COLUMN blocked_reason;
		ALTER TABLE posts
			DROP CONSTRAINT posts_public_id_not_blank,
			DROP CONSTRAINT posts_account_fk;
		ALTER TABLE expenses
			DROP CONSTRAINT expenses_public_id_not_blank,
			DROP CONSTRAINT expenses_account_fk;
		ALTER TABLE incomes
			DROP CONSTRAINT incomes_public_id_not_blank,
			DROP CONSTRAINT incomes_account_fk;
		ALTER TABLE accountsusers
			DROP CONSTRAINT accountsusers_account_fk,
			DROP CONSTRAINT accountsusers_user_fk;
		UPDATE params SET build = 14 WHERE id = 1;`
	if _, err := testDB.ExecContext(ctx, removeDataIntegrityMigration); err != nil {
		t.Fatalf("prepare version-14 schema: %v", err)
	}
	if err := migrations.Apply(ctx, testDB, initialSchema, migrations.SchemaVersion); err != nil {
		t.Fatalf("upgrade version-14 schema: %v", err)
	}

	previousDB := database.Db
	previousDatabaseType := util.Settings.DatabaseType
	database.Db = testDB
	util.Settings.DatabaseType = "postgres"
	defer func() {
		database.Db = previousDB
		util.Settings.DatabaseType = previousDatabaseType
	}()

	passwordHash, err := util.HashPassword("integration-password")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if err := createUserWithAccount("Integration User", "integration@example.com", "integration-user", passwordHash, "EN"); err != nil {
		t.Fatalf("register user: %v", err)
	}

	user, err := authenticateUser("  INTEGRATION-USER ", "integration-password")
	if err != nil {
		t.Fatalf("authenticate user: %v", err)
	}
	if user.Id == 0 {
		t.Fatal("authenticated user has no ID")
	}

	verificationNow := time.Now().UTC()
	verificationToken, verificationHash, err := newVerificationToken()
	if err != nil {
		t.Fatalf("create confirmation token: %v", err)
	}
	if err := createUnverifiedUserWithAccount("Pending User", "pending@example.com", "pending-user", passwordHash, "EN", verificationHash, verificationNow); err != nil {
		t.Fatalf("register pending user: %v", err)
	}
	if _, err := authenticateUser("pending-user", "integration-password"); !errors.Is(err, errInvalidCredentials) {
		t.Fatalf("pending user signed in: %v", err)
	}
	valid, err := verificationTokenValid(verificationToken, verificationNow.Add(verificationTokenDuration+time.Second))
	if err != nil || valid {
		t.Fatalf("expired confirmation link = %v, %v", valid, err)
	}
	valid, err = verificationTokenValid(verificationToken, verificationNow)
	if err != nil || !valid {
		t.Fatalf("fresh confirmation link = %v, %v", valid, err)
	}
	confirmed, err := confirmEmail(verificationToken, verificationNow)
	if err != nil || !confirmed {
		t.Fatalf("confirm pending user = %v, %v", confirmed, err)
	}
	confirmed, err = confirmEmail(verificationToken, verificationNow)
	if err != nil || confirmed {
		t.Fatalf("reused confirmation link = %v, %v", confirmed, err)
	}
	if _, err := authenticateUser("pending-user", "integration-password"); err != nil {
		t.Fatalf("confirmed user cannot sign in: %v", err)
	}

	var accountID int
	if err := testDB.GetContext(ctx, &accountID, `SELECT default_accounts_id FROM users WHERE id = $1`, user.Id); err != nil {
		t.Fatalf("load default account: %v", err)
	}
	if err := createPosts([]postWrite{{
		Description: "Integration expense",
		Amount:      -125.50,
		Exchange:    117.20,
		AccountID:   accountID,
		PublicID:    "integration-post",
		CreatedAt:   time.Now(),
	}}); err != nil {
		t.Fatalf("create post: %v", err)
	}

	filterFrom := time.Now().AddDate(0, 0, -7)
	filterTo := time.Now().AddDate(0, 0, 1)
	if err := createPosts([]postWrite{
		{Description: "Totals expense", ExpenseID: 1, Amount: 200, Exchange: 100, AccountID: accountID, PublicID: "totals-expense", CreatedAt: time.Now()},
		{Description: "Totals income", IncomeID: 1, Amount: -50, Exchange: 100, AccountID: accountID, PublicID: "totals-income", CreatedAt: time.Now()},
		{Description: "Old expense", ExpenseID: 1, Amount: 30, Exchange: 100, AccountID: accountID, PublicID: "totals-old-expense", CreatedAt: time.Now().AddDate(0, -2, 0)},
	}); err != nil {
		t.Fatalf("create totals posts: %v", err)
	}
	totals, err := loadPostTotals(accountID, postFilterQuery{})
	if err != nil {
		t.Fatalf("load post totals: %v", err)
	}
	if want := (postTotals{Saldo: 54.5, Saldoe: 0.73, IncomeSaldo: -50, IncomeSaldoe: -0.5, ExpenseSaldo: 230, ExpenseSaldoe: 2.3}); totals != want {
		t.Fatalf("post totals = %+v, want %+v", totals, want)
	}
	totals, err = loadPostTotals(accountID, postFilterQuery{From: &filterFrom, To: &filterTo})
	if err != nil {
		t.Fatalf("load filtered post totals: %v", err)
	}
	if want := (postTotals{Saldo: 24.5, Saldoe: 0.43, IncomeSaldo: -50, IncomeSaldoe: -0.5, ExpenseSaldo: 200, ExpenseSaldoe: 2}); totals != want {
		t.Fatalf("filtered post totals = %+v, want %+v", totals, want)
	}
	totals, err = loadPostTotals(accountID, postFilterQuery{Text: "totals", Kind: "expense"})
	if err != nil {
		t.Fatalf("load searched post totals: %v", err)
	}
	if want := (postTotals{Saldo: 200, Saldoe: 2, ExpenseSaldo: 200, ExpenseSaldoe: 2}); totals != want {
		t.Fatalf("searched post totals = %+v, want %+v", totals, want)
	}
	posts, _, err := loadPostsPage(accountID, postFilterQuery{Text: "%_"}, postPageRequest{})
	if err != nil || len(posts) != 0 {
		t.Fatalf("search with LIKE wildcards = %d posts, %v; want none", len(posts), err)
	}

	duplicateErr := createUserWithAccount("Duplicate", "other@example.com", "INTEGRATION-USER", passwordHash, "EN")
	if registrationConflictMessage(duplicateErr) != registrationResponseMessage {
		t.Fatalf("duplicate username error = %v", duplicateErr)
	}

	_, err = testDB.ExecContext(ctx, `INSERT INTO posts (description, amount, exchange, accounts_id, p_id) VALUES ('orphan', 1, 1, -1, 'orphan-post')`)
	var postgresError *pq.Error
	if !errors.As(err, &postgresError) || postgresError.Code != "23503" {
		t.Fatalf("orphan post error = %v, want foreign-key violation", err)
	}

	// The confirmed user has a shared default account and a private extra account.
	var targetID, sharedAccountID, privateAccountID int
	if err := testDB.GetContext(ctx, &targetID, `SELECT id FROM users WHERE username = 'pending-user'`); err != nil {
		t.Fatalf("load target user: %v", err)
	}
	if err := testDB.GetContext(ctx, &sharedAccountID, `SELECT default_accounts_id FROM users WHERE id = $1`, targetID); err != nil {
		t.Fatalf("load shared account: %v", err)
	}
	if _, err := testDB.ExecContext(ctx, `UPDATE users SET is_admin = true WHERE id = $1`, user.Id); err != nil {
		t.Fatalf("promote test admin: %v", err)
	}
	if _, err := testDB.ExecContext(ctx, `INSERT INTO accountsusers (accounts_id, users_id) VALUES ($1, $2)`, sharedAccountID, user.Id); err != nil {
		t.Fatalf("share account: %v", err)
	}
	if _, err := testDB.ExecContext(ctx, `INSERT INTO posts (description, accounts_id, p_id, deleted) VALUES ('shared post', $1, 'shared-post', 1)`, sharedAccountID); err != nil {
		t.Fatalf("create shared post: %v", err)
	}
	if _, err := testDB.ExecContext(ctx, `INSERT INTO expenses (description, accounts_id, p_id) VALUES ('shared expense', $1, 'shared-expense')`, sharedAccountID); err != nil {
		t.Fatalf("create shared expense: %v", err)
	}
	if _, err := testDB.ExecContext(ctx, `INSERT INTO incomes (description, accounts_id, p_id) VALUES ('shared income', $1, 'shared-income')`, sharedAccountID); err != nil {
		t.Fatalf("create shared income: %v", err)
	}
	if err := createAccount(targetID, "Private account"); err != nil {
		t.Fatalf("create private account: %v", err)
	}
	if err := testDB.GetContext(ctx, &privateAccountID, `SELECT a.id FROM accounts a JOIN accountsusers au ON au.accounts_id = a.id WHERE au.users_id = $1 AND a.description = 'Private account'`, targetID); err != nil {
		t.Fatalf("load private account: %v", err)
	}
	if _, err := testDB.ExecContext(ctx, `INSERT INTO posts (description, accounts_id, p_id) VALUES ('private post', $1, 'private-post')`, privateAccountID); err != nil {
		t.Fatalf("create private post: %v", err)
	}
	if _, err := testDB.ExecContext(ctx, `INSERT INTO expenses (description, accounts_id, p_id) VALUES ('private expense', $1, 'private-expense')`, privateAccountID); err != nil {
		t.Fatalf("create private expense: %v", err)
	}
	if _, err := testDB.ExecContext(ctx, `INSERT INTO incomes (description, accounts_id, p_id) VALUES ('private income', $1, 'private-income')`, privateAccountID); err != nil {
		t.Fatalf("create private income: %v", err)
	}
	counts, err := loadAdminUserCounts(targetID)
	if err != nil {
		t.Fatalf("load user data counts: %v", err)
	}
	if counts != (adminUserCounts{
		Accounts: 2, DeletedAccounts: 1,
		Posts: 2, DeletedPosts: 1,
		Expenses: 2, DeletedExpenses: 1,
		Incomes: 2, DeletedIncomes: 1,
	}) {
		t.Fatalf("user data counts = %+v", counts)
	}
	if _, err := testDB.ExecContext(ctx, `INSERT INTO passwordresets (email, token) VALUES ('pending@example.com', 'test-reset')`); err != nil {
		t.Fatalf("create reset token: %v", err)
	}
	if _, err := testDB.ExecContext(ctx, `INSERT INTO sessions (uuid, user_id) VALUES ('test-session', $1)`, targetID); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := testDB.ExecContext(ctx, `INSERT INTO admin_events (kind, status, user_id, subject, detail) VALUES ('email', 'smtp_accepted', $1, 'pending@example.com', 'password_reset')`, targetID); err != nil {
		t.Fatalf("create email event: %v", err)
	}
	if err := deleteUser(deleteUserTestContext(fmt.Sprint(targetID), "pending-user", user.Id)); err != nil {
		t.Fatalf("delete target user: %v", err)
	}
	for _, check := range []struct {
		name  string
		query string
		args  []any
		want  int
	}{
		{"target user", `SELECT COUNT(*) FROM users WHERE id = $1`, []any{targetID}, 0},
		{"shared account", `SELECT COUNT(*) FROM accounts WHERE id = $1`, []any{sharedAccountID}, 1},
		{"shared membership", `SELECT COUNT(*) FROM accountsusers WHERE accounts_id = $1 AND users_id = $2`, []any{sharedAccountID, user.Id}, 1},
		{"shared posts", `SELECT COUNT(*) FROM posts WHERE accounts_id = $1`, []any{sharedAccountID}, 1},
		{"shared expenses", `SELECT COUNT(*) FROM expenses WHERE accounts_id = $1`, []any{sharedAccountID}, 1},
		{"shared incomes", `SELECT COUNT(*) FROM incomes WHERE accounts_id = $1`, []any{sharedAccountID}, 1},
		{"private account", `SELECT COUNT(*) FROM accounts WHERE id = $1`, []any{privateAccountID}, 0},
		{"private posts", `SELECT COUNT(*) FROM posts WHERE accounts_id = $1`, []any{privateAccountID}, 0},
		{"private expenses", `SELECT COUNT(*) FROM expenses WHERE accounts_id = $1`, []any{privateAccountID}, 0},
		{"private incomes", `SELECT COUNT(*) FROM incomes WHERE accounts_id = $1`, []any{privateAccountID}, 0},
		{"sessions", `SELECT COUNT(*) FROM sessions WHERE user_id = $1`, []any{targetID}, 0},
		{"reset tokens", `SELECT COUNT(*) FROM passwordresets WHERE email = 'pending@example.com'`, nil, 0},
		{"personal event text", `SELECT COUNT(*) FROM admin_events WHERE user_id = $1 AND (subject <> '' OR detail <> '')`, []any{targetID}, 0},
		{"deletion event", `SELECT COUNT(*) FROM admin_events WHERE kind = 'user_delete' AND user_id = $1 AND actor_user_id = $2`, []any{targetID, user.Id}, 1},
	} {
		var count int
		if err := testDB.GetContext(ctx, &count, check.query, check.args...); err != nil || count != check.want {
			t.Errorf("%s count = %d, %v; want %d", check.name, count, err, check.want)
		}
	}
}

const twoFactorIntegrationPassword = "integration-password"

// openTwoFactorDatabase connects the routes to an isolated schema with
// several connections, so transactions of concurrent requests really overlap.
func openTwoFactorDatabase(t *testing.T, connections int) *sqlx.DB {
	t.Helper()
	integration := testdb.Open(t, testdb.WithMaxOpenConns(connections))
	previousDB, previousSettings := database.Db, util.Settings
	t.Cleanup(func() { database.Db, util.Settings = previousDB, previousSettings })
	database.Db = integration.DB
	util.Settings.DatabaseType = "postgres"
	util.Settings.TOTPKey = twoFactorTestKey
	return integration.DB
}

type twoFactorIntegrationUser struct {
	id     int
	secret string
	// recoveryCode is the user's single unused recovery code.
	recoveryCode string
}

func createTwoFactorIntegrationUser(t *testing.T, db *sqlx.DB, username string) twoFactorIntegrationUser {
	t.Helper()
	if err := createUserWithAccount(username, username+"@example.com", username, twoFactorTestIntegrationHash(t), "EN"); err != nil {
		t.Fatalf("create user: %v", err)
	}
	user := twoFactorIntegrationUser{recoveryCode: "ABCDE-FGHJK"}
	var err error
	if user.secret, err = util.NewTOTPSecret(); err != nil {
		t.Fatal(err)
	}
	encrypted, err := util.EncryptTOTPSecret(user.secret)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Get(&user.id, `UPDATE users SET totp_secret = $1, totp_enabled_at = NOW() WHERE username = $2 RETURNING id`, encrypted, username); err != nil {
		t.Fatalf("enable two-factor authentication: %v", err)
	}
	codeHash, err := util.HashPassword(util.NormalizeRecoveryCode(user.recoveryCode))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(insertRecoveryCodeQuery, user.id, codeHash); err != nil {
		t.Fatalf("store recovery code: %v", err)
	}
	return user
}

var twoFactorIntegrationHashOnce = sync.OnceValues(func() (string, error) {
	return util.HashPassword(twoFactorIntegrationPassword)
})

func twoFactorTestIntegrationHash(t *testing.T) string {
	t.Helper()
	hash, err := twoFactorIntegrationHashOnce()
	if err != nil {
		t.Fatal(err)
	}
	return hash
}

// addPendingSession stores a session that passed the password step.
func addPendingSession(t *testing.T, db *sqlx.DB, sessionHash string, userID int) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO sessions (uuid, pending_2fa_user_id, pending_2fa_at) VALUES ($1, $2, NOW())`, sessionHash, userID); err != nil {
		t.Fatalf("create pending session: %v", err)
	}
}

func countIntegrationRows(t *testing.T, db *sqlx.DB, query string, args ...any) int {
	t.Helper()
	count := 0
	if err := db.Get(&count, query, args...); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	return count
}

func userSessionCount(t *testing.T, db *sqlx.DB, userID int) int {
	t.Helper()
	return countIntegrationRows(t, db, `SELECT COUNT(*) FROM sessions WHERE user_id = $1 OR pending_2fa_user_id = $1`, userID)
}

func addPasswordResetToken(t *testing.T, db *sqlx.DB, email string) string {
	t.Helper()
	token, tokenHash, err := util.NewPasswordResetToken()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO passwordresets (email, token) VALUES ($1, $2)`, email, tokenHash); err != nil {
		t.Fatalf("create reset token: %v", err)
	}
	return token
}

func blockUserTestContext(userID, actorID int) echo.Context {
	form := url.Values{"reason": {"Policy violation"}}
	request := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/admin/users/%d/block", userID), strings.NewReader(form.Encode()))
	request.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	c := echo.New().NewContext(request, httptest.NewRecorder())
	c.SetParamNames("id")
	c.SetParamValues(fmt.Sprint(userID))
	c.Set("data", &util.Data{User: util.User{Id: actorID, IsAdmin: true}})
	return c
}

// runBlockedOnUser makes the overlap of concurrent requests deterministic: a
// separate transaction locks the user row, every operation is started and
// seen waiting for that lock, and only then the lock is released so they
// race. A timeout turns a deadlock or a hang into a test failure.
func runBlockedOnUser(t *testing.T, db *sqlx.DB, userID int, operations ...func() error) []error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	locker, err := db.BeginTxx(ctx, nil)
	if err != nil {
		t.Fatalf("begin locking transaction: %v", err)
	}
	defer locker.Rollback()
	if _, err := locker.ExecContext(ctx, `SELECT 1 FROM users WHERE id = $1 FOR UPDATE`, userID); err != nil {
		t.Fatalf("lock user: %v", err)
	}

	results := make([]error, len(operations))
	done := make(chan struct{})
	var group sync.WaitGroup
	for i, operation := range operations {
		group.Add(1)
		go func() {
			defer group.Done()
			results[i] = operation()
		}()
	}
	go func() {
		group.Wait()
		close(done)
	}()

	for {
		waiting := 0
		if err := db.GetContext(ctx, &waiting, `
			SELECT COUNT(*) FROM pg_stat_activity
			WHERE datname = current_database() AND pid <> pg_backend_pid()
			  AND wait_event_type = 'Lock' AND query ILIKE '%users%'`); err != nil {
			t.Fatalf("count waiting requests: %v", err)
		}
		if waiting >= len(operations) {
			break
		}
		select {
		case <-done:
			t.Fatal("the requests finished without waiting for the user lock")
		case <-ctx.Done():
			t.Fatalf("only %d of %d requests reached the user lock", waiting, len(operations))
		case <-time.After(20 * time.Millisecond):
		}
	}
	if err := locker.Rollback(); err != nil {
		t.Fatalf("release user lock: %v", err)
	}
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("concurrent requests did not finish: deadlock?")
	}
	return results
}

// TestPostgresRevokingSessionsCoversPendingSignIns checks every place that
// revokes a user's sessions against a session waiting for the second factor,
// which has user_id = 0.
func TestPostgresRevokingSessionsCoversPendingSignIns(t *testing.T) {
	db := openTwoFactorDatabase(t, 1)
	user := createTwoFactorIntegrationUser(t, db, "revoked-user")
	other := createTwoFactorIntegrationUser(t, db, "other-user")

	seed := func() {
		t.Helper()
		if _, err := db.Exec(`DELETE FROM sessions`); err != nil {
			t.Fatal(err)
		}
		addPendingSession(t, db, "pending-session", user.id)
		addPendingSession(t, db, "other-pending-session", other.id)
		if _, err := db.Exec(`INSERT INTO sessions (uuid, user_id) VALUES ('current-session', $1), ('second-session', $1), ('other-session', $2)`, user.id, other.id); err != nil {
			t.Fatal(err)
		}
	}
	assertSessions := func(action string, want int) {
		t.Helper()
		if got := userSessionCount(t, db, user.id); got != want {
			t.Errorf("%s left %d sessions of the user, want %d", action, got, want)
		}
		if got := userSessionCount(t, db, other.id); got != 2 {
			t.Errorf("%s left %d sessions of another user, want 2", action, got)
		}
	}

	seed()
	newHash, err := util.HashPassword("changed-password-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := changeOwnPassword(user.id, twoFactorIntegrationPassword, newHash, "current-session"); err != nil {
		t.Fatalf("change password: %v", err)
	}
	assertSessions("password change", 1)
	if countIntegrationRows(t, db, `SELECT COUNT(*) FROM sessions WHERE uuid = 'current-session' AND user_id = $1`, user.id) != 1 {
		t.Error("password change revoked the current session")
	}

	seed()
	changed, err := resetPasswordWithToken(addPasswordResetToken(t, db, "revoked-user@example.com"), twoFactorTestIntegrationHash(t), time.Now())
	if err != nil || !changed {
		t.Fatalf("reset password = %v, %v", changed, err)
	}
	assertSessions("password reset", 0)

	seed()
	if err := resetUserTwoFactor(other.id, user.id, "Lost phone"); err != nil {
		t.Fatalf("admin reset: %v", err)
	}
	assertSessions("admin two-factor reset", 0)
	if countIntegrationRows(t, db, `SELECT COUNT(*) FROM users WHERE id = $1 AND totp_secret IS NULL AND totp_enabled_at IS NULL`, user.id) != 1 ||
		countIntegrationRows(t, db, `SELECT COUNT(*) FROM user_recovery_codes WHERE user_id = $1`, user.id) != 0 {
		t.Error("admin reset left the secret or recovery codes")
	}
	if countIntegrationRows(t, db, `SELECT COUNT(*) FROM admin_events WHERE kind = 'admin_2fa_reset' AND user_id = $1 AND actor_user_id = $2 AND detail = 'Lost phone'`, user.id, other.id) != 1 {
		t.Error("admin reset was not recorded with actor and reason")
	}

	seed()
	if err := changeUserBlock(blockUserTestContext(user.id, other.id), true); err != nil {
		t.Fatalf("block user: %v", err)
	}
	assertSessions("blocking", 0)
}

// TestPostgresTwoFactorChangesKeepCurrentSession runs enable, regenerate and
// disable against PostgreSQL: each revokes the user's other sessions and
// pending sign-ins, but not the session that made the change.
func TestPostgresTwoFactorChangesKeepCurrentSession(t *testing.T) {
	db := openTwoFactorDatabase(t, 1)
	if err := createUserWithAccount("Owner", "owner@example.com", "owner", twoFactorTestIntegrationHash(t), "EN"); err != nil {
		t.Fatal(err)
	}
	userID := 0
	if err := db.Get(&userID, `SELECT id FROM users WHERE username = 'owner'`); err != nil {
		t.Fatal(err)
	}
	seed := func() {
		t.Helper()
		if _, err := db.Exec(`DELETE FROM sessions WHERE uuid <> 'current-session'`); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO sessions (uuid, user_id) VALUES ('current-session', $1), ('second-session', $1) ON CONFLICT (uuid) DO NOTHING`, userID); err != nil {
			t.Fatal(err)
		}
		addPendingSession(t, db, "pending-session", userID)
	}
	assertOnlyCurrent := func(action string) {
		t.Helper()
		if userSessionCount(t, db, userID) != 1 || countIntegrationRows(t, db, `SELECT COUNT(*) FROM sessions WHERE uuid = 'current-session' AND user_id = $1`, userID) != 1 {
			t.Errorf("%s did not leave exactly the current session", action)
		}
	}
	now := time.Now()
	codeAt := func(secret string, stepOffset int64) string {
		t.Helper()
		code, err := util.TOTPCode(secret, util.TOTPStep(now)+stepOffset)
		if err != nil {
			t.Fatal(err)
		}
		return code
	}

	seed()
	secret, err := currentSetupSecret("current-session")
	if err != nil {
		t.Fatalf("prepare setup: %v", err)
	}
	if again, err := currentSetupSecret("current-session"); err != nil || again != secret {
		t.Fatalf("the setup secret changed on reload: %v", err)
	}
	if _, err := db.Exec(`UPDATE sessions SET pending_totp_created_at = NOW() - INTERVAL '11 minutes' WHERE uuid = 'current-session'`); err != nil {
		t.Fatal(err)
	}
	if _, err := enableTwoFactor(userID, "current-session", twoFactorIntegrationPassword, codeAt(secret, 0), now); !errors.Is(err, errTwoFactorSetupExpired) {
		t.Fatalf("enabling with an expired setup secret = %v", err)
	}
	expired := secret
	if secret, err = currentSetupSecret("current-session"); err != nil || secret == expired {
		t.Fatalf("an expired setup secret was shown again: %v", err)
	}

	codes, err := enableTwoFactor(userID, "current-session", twoFactorIntegrationPassword, codeAt(secret, -1), now)
	if err != nil || len(codes) != recoveryCodeCount {
		t.Fatalf("enable = %d codes, %v", len(codes), err)
	}
	assertOnlyCurrent("enable")
	if countIntegrationRows(t, db, `SELECT COUNT(*) FROM sessions WHERE pending_totp_secret IS NOT NULL`) != 0 {
		t.Error("the setup secret stayed in the session")
	}
	var stored string
	if err := db.Get(&stored, `SELECT totp_secret FROM users WHERE id = $1`, userID); err != nil || stored == secret || strings.Contains(stored, secret) {
		t.Fatalf("the secret is not stored encrypted: %v", err)
	}
	// The code that enabled two-factor authentication cannot be used again.
	if _, err := regenerateRecoveryCodes(userID, "current-session", twoFactorIntegrationPassword, codeAt(secret, -1), now); !errors.Is(err, errTwoFactorCode) {
		t.Fatalf("regenerating with a used code = %v", err)
	}

	seed()
	newCodes, err := regenerateRecoveryCodes(userID, "current-session", twoFactorIntegrationPassword, codeAt(secret, 0), now)
	if err != nil || len(newCodes) != recoveryCodeCount {
		t.Fatalf("regenerate = %d codes, %v", len(newCodes), err)
	}
	assertOnlyCurrent("regenerate")
	if id, err := findRecoveryCode(userID, codes[0]); err != nil || id != 0 {
		t.Errorf("an old recovery code still works: %d, %v", id, err)
	}
	if countIntegrationRows(t, db, countRecoveryCodesQuery, userID) != recoveryCodeCount {
		t.Error("the user does not have ten new recovery codes")
	}

	seed()
	if err := disableTwoFactor(userID, "current-session", twoFactorIntegrationPassword, "", "00000-00000", now); !errors.Is(err, errTwoFactorCode) {
		t.Fatalf("disabling with an unknown recovery code = %v", err)
	}
	if err := disableTwoFactor(userID, "current-session", twoFactorIntegrationPassword, "", newCodes[3], now); err != nil {
		t.Fatalf("disable: %v", err)
	}
	assertOnlyCurrent("disable")
	if countIntegrationRows(t, db, `SELECT COUNT(*) FROM users WHERE id = $1 AND totp_secret IS NULL AND totp_enabled_at IS NULL`, userID) != 1 ||
		countIntegrationRows(t, db, `SELECT COUNT(*) FROM user_recovery_codes WHERE user_id = $1`, userID) != 0 {
		t.Error("disable left the secret or recovery codes")
	}
	for _, kind := range []string{"2fa_enabled", "2fa_recovery_regenerated", "2fa_disabled"} {
		if countIntegrationRows(t, db, `SELECT COUNT(*) FROM admin_events WHERE kind = $1 AND user_id = $2`, kind, userID) != 1 {
			t.Errorf("event %s was not recorded once", kind)
		}
	}
}

func TestPostgresPendingSignInExpires(t *testing.T) {
	db := openTwoFactorDatabase(t, 1)
	user := createTwoFactorIntegrationUser(t, db, "slow-user")
	addPendingSession(t, db, "pending-session", user.id)
	if _, err := db.Exec(`UPDATE sessions SET pending_2fa_at = NOW() - INTERVAL '6 minutes' WHERE uuid = 'pending-session'`); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	code, err := util.TOTPCode(user.secret, util.TOTPStep(now))
	if err != nil {
		t.Fatal(err)
	}
	result, err := completeTwoFactorLogin("pending-session", code, "", now)
	if err != nil || result.Outcome != twoFactorExpired {
		t.Fatalf("result = %+v, %v; want an expired sign-in", result, err)
	}
	if userSessionCount(t, db, user.id) != 0 {
		t.Error("the expired pending sign-in was not cleared")
	}
}

// The same TOTP code, or the same recovery code, sent from two sessions at
// once signs in exactly one of them.
func TestPostgresConcurrentCodeIsAcceptedOnce(t *testing.T) {
	for _, method := range []string{"TOTP code", "recovery code"} {
		t.Run(method, func(t *testing.T) {
			db := openTwoFactorDatabase(t, 4)
			user := createTwoFactorIntegrationUser(t, db, "racing-user")
			now := time.Now()
			code, recoveryCode := "", user.recoveryCode
			if method == "TOTP code" {
				recoveryCode = ""
				var err error
				if code, err = util.TOTPCode(user.secret, util.TOTPStep(now)); err != nil {
					t.Fatal(err)
				}
			}
			outcomes := make([]twoFactorLoginResult, 2)
			operations := []func() error{}
			for i, session := range []string{"first-session", "second-session"} {
				addPendingSession(t, db, session, user.id)
				operations = append(operations, func() (err error) {
					outcomes[i], err = completeTwoFactorLogin(session, code, recoveryCode, now)
					return err
				})
			}
			for _, err := range runBlockedOnUser(t, db, user.id, operations...) {
				if err != nil {
					t.Fatalf("sign-in failed: %v", err)
				}
			}
			accepted := 0
			for _, result := range outcomes {
				switch result.Outcome {
				case twoFactorOK:
					accepted++
				case twoFactorWrongCode:
				default:
					t.Errorf("unexpected outcome %d", result.Outcome)
				}
			}
			if accepted != 1 {
				t.Fatalf("%d sessions signed in with the same %s, want 1", accepted, method)
			}
			if signedIn := countIntegrationRows(t, db, `SELECT COUNT(*) FROM sessions WHERE user_id = $1`, user.id); signedIn != 1 {
				t.Errorf("%d signed-in sessions, want 1", signedIn)
			}
			if logins := countIntegrationRows(t, db, `SELECT COUNT(*) FROM admin_events WHERE kind = 'auth_login' AND user_id = $1`, user.id); logins != 1 {
				t.Errorf("%d sign-in events, want 1", logins)
			}
		})
	}
}

// Wrong codes sent at once to one pending sign-in cannot gain attempts:
// exactly five are counted, then the pending sign-in is gone.
func TestPostgresConcurrentWrongCodesGetFiveAttempts(t *testing.T) {
	const requests = 8
	db := openTwoFactorDatabase(t, requests+2)
	user := createTwoFactorIntegrationUser(t, db, "guessed-user")
	addPendingSession(t, db, "pending-session", user.id)
	now := time.Now()
	wrong := "000000"
	for _, candidate := range []string{"111111", "222222", "333333"} {
		if _, ok := util.MatchTOTP(user.secret, wrong, now); !ok {
			break
		}
		wrong = candidate
	}

	outcomes := make([]twoFactorLoginResult, requests)
	operations := []func() error{}
	for i := 0; i < requests; i++ {
		operations = append(operations, func() (err error) {
			outcomes[i], err = completeTwoFactorLogin("pending-session", wrong, "", now)
			return err
		})
	}
	for _, err := range runBlockedOnUser(t, db, user.id, operations...) {
		if err != nil {
			t.Fatalf("sign-in failed: %v", err)
		}
	}
	counted := 0
	for _, result := range outcomes {
		switch result.Outcome {
		case twoFactorWrongCode, twoFactorLockedOut:
			counted++
		case twoFactorExpired:
		default:
			t.Errorf("unexpected outcome %d", result.Outcome)
		}
	}
	if counted != twoFactorMaxFailures {
		t.Errorf("%d wrong codes were taken as attempts, want %d", counted, twoFactorMaxFailures)
	}
	if events := countIntegrationRows(t, db, `SELECT COUNT(*) FROM admin_events WHERE kind = 'auth_2fa_failed' AND user_id = $1`, user.id); events != twoFactorMaxFailures {
		t.Errorf("%d failure events, want %d", events, twoFactorMaxFailures)
	}
	if userSessionCount(t, db, user.id) != 0 {
		t.Error("the pending sign-in survived five wrong codes")
	}
	// The right code no longer helps: the password is needed again.
	code, err := util.TOTPCode(user.secret, util.TOTPStep(now))
	if err != nil {
		t.Fatal(err)
	}
	if result, err := completeTwoFactorLogin("pending-session", code, "", now); err != nil || result.Outcome != twoFactorExpired {
		t.Fatalf("result after lock-out = %+v, %v", result, err)
	}
}

// The second sign-in step locks rows in the same order as blocking a user
// and resetting a password, so running them together cannot deadlock.
// Whichever commits first, the user ends up signed out.
func TestPostgresSecondStepRacesBlockAndPasswordReset(t *testing.T) {
	for _, rival := range []string{"blocking", "password reset"} {
		t.Run(rival, func(t *testing.T) {
			db := openTwoFactorDatabase(t, 4)
			user := createTwoFactorIntegrationUser(t, db, "contested-user")
			addPendingSession(t, db, "pending-session", user.id)
			now := time.Now()
			code, err := util.TOTPCode(user.secret, util.TOTPStep(now))
			if err != nil {
				t.Fatal(err)
			}
			rivalOperation := func() error { return changeUserBlock(blockUserTestContext(user.id, 999999), true) }
			if rival == "password reset" {
				token := addPasswordResetToken(t, db, "contested-user@example.com")
				newHash, err := util.HashPassword("changed-password-1")
				if err != nil {
					t.Fatal(err)
				}
				rivalOperation = func() error {
					changed, err := resetPasswordWithToken(token, newHash, time.Now())
					if err == nil && !changed {
						err = errors.New("the reset token was refused")
					}
					return err
				}
			}
			var login twoFactorLoginResult
			results := runBlockedOnUser(t, db, user.id,
				func() (err error) {
					login, err = completeTwoFactorLogin("pending-session", code, "", now)
					return err
				},
				rivalOperation,
			)
			for _, err := range results {
				if err != nil {
					t.Fatalf("request failed (deadlock?): %v", err)
				}
			}
			if login.Outcome != twoFactorOK && login.Outcome != twoFactorExpired {
				t.Errorf("sign-in outcome = %d", login.Outcome)
			}
			if userSessionCount(t, db, user.id) != 0 {
				t.Errorf("the user is still signed in or pending after %s", rival)
			}
		})
	}
}

// A sign-in with the old password that overlaps a password reset never
// leaves a session behind: either it loses the race and is refused, or the
// reset revokes what it created.
func TestPostgresOldPasswordSignInRacesPasswordReset(t *testing.T) {
	for _, tc := range []struct {
		name      string
		twoFactor bool
	}{{"with two-factor authentication", true}, {"without two-factor authentication", false}} {
		t.Run(tc.name, func(t *testing.T) {
			db := openTwoFactorDatabase(t, 4)
			user := createTwoFactorIntegrationUser(t, db, "resetting-user")
			if !tc.twoFactor {
				if _, err := db.Exec(removeTwoFactorQuery, user.id); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := db.Exec(`INSERT INTO sessions (uuid) VALUES ('anonymous-session')`); err != nil {
				t.Fatal(err)
			}
			token := addPasswordResetToken(t, db, "resetting-user@example.com")
			newHash, err := util.HashPassword("changed-password-1")
			if err != nil {
				t.Fatal(err)
			}
			// The password is checked before the reset, as in a slow request.
			verified, err := authenticateUser("resetting-user", twoFactorIntegrationPassword)
			if err != nil {
				t.Fatalf("check old password: %v", err)
			}
			var loginErr error
			results := runBlockedOnUser(t, db, user.id,
				func() error {
					_, loginErr = beginLogin("anonymous-session", verified)
					return nil
				},
				func() error {
					changed, err := resetPasswordWithToken(token, newHash, time.Now())
					if err == nil && !changed {
						err = errors.New("the reset token was refused")
					}
					return err
				},
			)
			if results[1] != nil {
				t.Fatalf("password reset failed (deadlock?): %v", results[1])
			}
			if loginErr != nil && !errors.Is(loginErr, errInvalidCredentials) {
				t.Fatalf("sign-in failed (deadlock?): %v", loginErr)
			}
			if userSessionCount(t, db, user.id) != 0 {
				t.Error("a session of the old password survived the reset")
			}
		})
	}
}
