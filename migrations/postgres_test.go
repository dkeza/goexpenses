package migrations_test

import (
	"context"
	"testing"

	"goexpenses/internal/testdb"
	"goexpenses/migrations"
)

// TestPostsHistoryMigrationUpgradesExistingSchema rolls a fresh schema back to
// version 25 and applies migration 26 to it, as on an existing installation.
func TestPostsHistoryMigrationUpgradesExistingSchema(t *testing.T) {
	integration := testdb.Open(t)
	db := integration.DB
	if _, err := db.Exec(`
		DROP TABLE posts_history;
		DROP TRIGGER posts_history_trigger ON posts;
		DROP FUNCTION posts_history_record();
		ALTER TABLE accounts DROP COLUMN post_filter;
		DROP TABLE user_recovery_codes;
		ALTER TABLE users DROP COLUMN totp_secret, DROP COLUMN totp_enabled_at, DROP COLUMN totp_last_step;
		ALTER TABLE sessions
			DROP COLUMN pending_2fa_user_id, DROP COLUMN pending_2fa_at, DROP COLUMN pending_2fa_failures,
			DROP COLUMN pending_totp_secret, DROP COLUMN pending_totp_created_at;
		DROP TABLE account_invitations;
		ALTER TABLE accountsusers DROP COLUMN role, DROP COLUMN created_at, DROP COLUMN post_filter;
		UPDATE params SET build = 25 WHERE id = 1;
		INSERT INTO accounts (description) VALUES ('Account');
		INSERT INTO posts (description, amount, accounts_id, p_id)
			SELECT 'Before', 10, id, 'post-pid' FROM accounts;`); err != nil {
		t.Fatalf("prepare schema version 25: %v", err)
	}

	if err := migrations.Apply(context.Background(), db, integration.InitialSchema, migrations.SchemaVersion); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	if _, err := db.Exec(`UPDATE posts SET description = 'After' WHERE p_id = 'post-pid'`); err != nil {
		t.Fatalf("update post: %v", err)
	}
	// An update that changes nothing is not recorded.
	if _, err := db.Exec(`UPDATE posts SET description = 'After' WHERE p_id = 'post-pid'`); err != nil {
		t.Fatalf("repeat post update: %v", err)
	}

	var descriptions []string
	if err := db.Select(&descriptions, `SELECT description FROM posts_history WHERE p_id = 'post-pid' AND operation = 'update'`); err != nil {
		t.Fatalf("load post history: %v", err)
	}
	if len(descriptions) != 1 || descriptions[0] != "Before" {
		t.Fatalf("post history = %q, want [Before]", descriptions)
	}

	if _, err := db.Exec(`DELETE FROM posts WHERE p_id = 'post-pid'`); err != nil {
		t.Fatalf("delete post: %v", err)
	}
	var remaining int
	if err := db.Get(&remaining, `SELECT COUNT(*) FROM posts_history`); err != nil {
		t.Fatalf("count post history: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("%d history rows remain after the post was deleted, want 0", remaining)
	}
}

// TestPostFilterMigrationCarriesOverDateFilter rolls a fresh schema back to
// version 26 and checks that migration 27 keeps an account's date filter.
func TestPostFilterMigrationCarriesOverDateFilter(t *testing.T) {
	integration := testdb.Open(t)
	db := integration.DB
	if _, err := db.Exec(`
		ALTER TABLE accounts DROP COLUMN post_filter;
		DROP TABLE user_recovery_codes;
		ALTER TABLE users DROP COLUMN totp_secret, DROP COLUMN totp_enabled_at, DROP COLUMN totp_last_step;
		ALTER TABLE sessions
			DROP COLUMN pending_2fa_user_id, DROP COLUMN pending_2fa_at, DROP COLUMN pending_2fa_failures,
			DROP COLUMN pending_totp_secret, DROP COLUMN pending_totp_created_at;
		DROP TABLE account_invitations;
		ALTER TABLE accountsusers DROP COLUMN role, DROP COLUMN created_at, DROP COLUMN post_filter;
		UPDATE params SET build = 26 WHERE id = 1;
		INSERT INTO accounts (description, fromdate, todate) VALUES ('Filtered', '2026-09-01', '2026-09-30');
		INSERT INTO accounts (description) VALUES ('Unfiltered');`); err != nil {
		t.Fatalf("prepare schema version 26: %v", err)
	}

	if err := migrations.Apply(context.Background(), db, integration.InitialSchema, migrations.SchemaVersion); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	var filters []string
	if err := db.Select(&filters, `SELECT post_filter FROM accounts ORDER BY description`); err != nil {
		t.Fatalf("load post filters: %v", err)
	}
	want := []string{`{"from" : "2026-09-01", "to" : "2026-09-30"}`, ""}
	if len(filters) != 2 || filters[0] != want[0] || filters[1] != want[1] {
		t.Fatalf("post filters = %q, want %q", filters, want)
	}
}

// TestAccountMembersMigrationMakesMembersOwners rolls a fresh schema back to
// version 27 and checks that migration 28 makes existing members owners and
// gives each of them the account's saved filter.
func TestAccountMembersMigrationMakesMembersOwners(t *testing.T) {
	integration := testdb.Open(t)
	db := integration.DB
	if _, err := db.Exec(`
		DROP TABLE user_recovery_codes;
		ALTER TABLE users DROP COLUMN totp_secret, DROP COLUMN totp_enabled_at, DROP COLUMN totp_last_step;
		ALTER TABLE sessions
			DROP COLUMN pending_2fa_user_id, DROP COLUMN pending_2fa_at, DROP COLUMN pending_2fa_failures,
			DROP COLUMN pending_totp_secret, DROP COLUMN pending_totp_created_at;
		DROP TABLE account_invitations;
		ALTER TABLE accountsusers DROP COLUMN role, DROP COLUMN created_at, DROP COLUMN post_filter;
		UPDATE params SET build = 27 WHERE id = 1;
		INSERT INTO accounts (description, post_filter) VALUES ('Shared', '{"text":"rent"}');
		INSERT INTO users (name, email, username, default_accounts_id)
			SELECT name, name || '@example.com', name, a.id
			FROM (VALUES ('alice'), ('bob')) AS u(name), accounts a;
		INSERT INTO accountsusers (accounts_id, users_id) SELECT default_accounts_id, id FROM users;`); err != nil {
		t.Fatalf("prepare schema version 27: %v", err)
	}

	if err := migrations.Apply(context.Background(), db, integration.InitialSchema, migrations.SchemaVersion); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	type member struct {
		Role       string `db:"role"`
		PostFilter string `db:"post_filter"`
	}
	members := []member{}
	if err := db.Select(&members, `SELECT role, post_filter FROM accountsusers ORDER BY users_id`); err != nil {
		t.Fatalf("load members: %v", err)
	}
	if len(members) != 2 {
		t.Fatalf("%d members, want 2", len(members))
	}
	for _, m := range members {
		if m.Role != "owner" || m.PostFilter != `{"text":"rent"}` {
			t.Errorf("member = %+v, want owner with the account's filter", m)
		}
	}
	if _, err := db.Exec(`UPDATE accountsusers SET role = 'admin'`); err == nil {
		t.Error("an unknown member role was accepted")
	}
}

// TestTwoFactorMigrationUpgradesExistingSchema rolls a fresh schema back to
// version 29 and applies migration 30 to it, as on an existing installation.
func TestTwoFactorMigrationUpgradesExistingSchema(t *testing.T) {
	if migrations.SchemaVersion != 30 {
		t.Fatalf("SchemaVersion = %d, want 30", migrations.SchemaVersion)
	}
	integration := testdb.Open(t)
	db := integration.DB
	if _, err := db.Exec(`
		DROP TABLE user_recovery_codes;
		ALTER TABLE users DROP COLUMN totp_secret, DROP COLUMN totp_enabled_at, DROP COLUMN totp_last_step;
		ALTER TABLE sessions
			DROP COLUMN pending_2fa_user_id, DROP COLUMN pending_2fa_at, DROP COLUMN pending_2fa_failures,
			DROP COLUMN pending_totp_secret, DROP COLUMN pending_totp_created_at;
		UPDATE params SET build = 29 WHERE id = 1;
		INSERT INTO accounts (description) VALUES ('Account');
		INSERT INTO users (name, email, username, default_accounts_id)
			SELECT 'alice', 'alice@example.com', 'alice', id FROM accounts;
		INSERT INTO sessions (uuid, user_id) SELECT 'existing-session', id FROM users;`); err != nil {
		t.Fatalf("prepare schema version 29: %v", err)
	}

	if err := migrations.Apply(context.Background(), db, integration.InitialSchema, migrations.SchemaVersion); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	var build, lastStep, failures, enabledUsers int
	if err := db.Get(&build, `SELECT build FROM params WHERE id = 1`); err != nil || build != 30 {
		t.Fatalf("schema version = %d, %v; want 30", build, err)
	}
	// Existing users and sessions start without a second factor.
	if err := db.Get(&enabledUsers, `SELECT COUNT(*) FROM users WHERE totp_enabled_at IS NOT NULL OR totp_secret IS NOT NULL`); err != nil || enabledUsers != 0 {
		t.Fatalf("users with a second factor = %d, %v; want 0", enabledUsers, err)
	}
	if err := db.Get(&lastStep, `SELECT totp_last_step FROM users WHERE username = 'alice'`); err != nil || lastStep != 0 {
		t.Fatalf("totp_last_step = %d, %v; want 0", lastStep, err)
	}
	if err := db.Get(&failures, `SELECT pending_2fa_failures FROM sessions WHERE uuid = 'existing-session' AND pending_2fa_user_id IS NULL`); err != nil || failures != 0 {
		t.Fatalf("pending failures = %d, %v; want 0", failures, err)
	}
	if _, err := db.Exec(`INSERT INTO user_recovery_codes (user_id, code_hash) SELECT id, 'hash' FROM users`); err != nil {
		t.Fatalf("store recovery code: %v", err)
	}
	// Deleting a user removes their recovery codes.
	if _, err := db.Exec(`DELETE FROM sessions; DELETE FROM users`); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	var codes int
	if err := db.Get(&codes, `SELECT COUNT(*) FROM user_recovery_codes`); err != nil || codes != 0 {
		t.Fatalf("recovery codes after deleting the user = %d, %v; want 0", codes, err)
	}
}
