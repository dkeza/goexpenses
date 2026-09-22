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
		t.Skip("TEST_DATABASE_URL is not set")
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
		ALTER TABLE users
			DROP CONSTRAINT users_name_not_blank,
			DROP CONSTRAINT users_username_not_blank,
			DROP CONSTRAINT users_email_not_blank,
			DROP CONSTRAINT users_default_account_fk;
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

	duplicateErr := createUserWithAccount("Duplicate", "other@example.com", "INTEGRATION-USER", passwordHash, "EN")
	if registrationConflictMessage(duplicateErr) != registrationResponseMessage {
		t.Fatalf("duplicate username error = %v", duplicateErr)
	}

	_, err = testDB.ExecContext(ctx, `INSERT INTO posts (description, amount, exchange, accounts_id, p_id) VALUES ('orphan', 1, 1, -1, 'orphan-post')`)
	var postgresError *pq.Error
	if !errors.As(err, &postgresError) || postgresError.Code != "23503" {
		t.Fatalf("orphan post error = %v, want foreign-key violation", err)
	}
}
