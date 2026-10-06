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
