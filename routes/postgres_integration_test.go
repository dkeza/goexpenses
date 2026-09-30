package routes

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"goexpenses/database"
	"goexpenses/migrations"
	"goexpenses/util"

	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
)

func TestPostgresRegistrationLoginAndPost(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		localURL, err := os.ReadFile("../.test-db-url")
		if errors.Is(err, os.ErrNotExist) {
			t.Skip("TEST_DATABASE_URL is not set and .test-db-url is absent")
		}
		if err != nil {
			t.Fatalf("read local test database URL: %v", err)
		}
		databaseURL = strings.TrimSpace(string(localURL))
		if databaseURL == "" {
			t.Fatal(".test-db-url is empty")
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	adminDB, err := sqlx.ConnectContext(ctx, "postgres", databaseURL)
	if err != nil {
		t.Fatalf("connect to integration database: %v", err)
	}
	defer adminDB.Close()

	schemaName := fmt.Sprintf("goexpenses_test_%d", time.Now().UnixNano())
	quotedSchema := pq.QuoteIdentifier(schemaName)
	if _, err := adminDB.ExecContext(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		t.Fatalf("create test schema: %v", err)
	}
	defer func() {
		if _, err := adminDB.ExecContext(context.Background(), "DROP SCHEMA "+quotedSchema+" CASCADE"); err != nil {
			t.Errorf("drop test schema: %v", err)
		}
	}()

	parsedURL, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatalf("parse TEST_DATABASE_URL: %v", err)
	}
	query := parsedURL.Query()
	query.Set("search_path", schemaName)
	parsedURL.RawQuery = query.Encode()

	testDB, err := sqlx.ConnectContext(ctx, "postgres", parsedURL.String())
	if err != nil {
		t.Fatalf("connect to isolated test schema: %v", err)
	}
	testDB.SetMaxOpenConns(1)
	defer testDB.Close()

	initialSchema, err := os.ReadFile("../db/pg_structure.sql")
	if err != nil {
		t.Fatalf("read initial schema: %v", err)
	}
	initialSchema = []byte(strings.ReplaceAll(string(initialSchema), "public.", quotedSchema+"."))
	if err := migrations.Apply(ctx, testDB, initialSchema, migrations.CurrentVersion); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
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
		DROP TABLE admin_events;
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
	if err := migrations.Apply(ctx, testDB, initialSchema, migrations.CurrentVersion); err != nil {
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
	totals, err := loadPostTotals(accountID, nil, nil)
	if err != nil {
		t.Fatalf("load post totals: %v", err)
	}
	if want := (postTotals{Saldo: 54.5, Saldoe: 0.73, IncomeSaldo: -50, IncomeSaldoe: -0.5, ExpenseSaldo: 230, ExpenseSaldoe: 2.3}); totals != want {
		t.Fatalf("post totals = %+v, want %+v", totals, want)
	}
	totals, err = loadPostTotals(accountID, &filterFrom, &filterTo)
	if err != nil {
		t.Fatalf("load filtered post totals: %v", err)
	}
	if want := (postTotals{Saldo: 24.5, Saldoe: 0.43, IncomeSaldo: -50, IncomeSaldoe: -0.5, ExpenseSaldo: 200, ExpenseSaldoe: 2}); totals != want {
		t.Fatalf("filtered post totals = %+v, want %+v", totals, want)
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
