// Package testdb provides an isolated PostgreSQL schema for integration
// tests. Tests are skipped when no test database is configured.
package testdb

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"goexpenses/migrations"

	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
)

// Database is a connection limited to one temporary schema.
type Database struct {
	DB            *sqlx.DB
	InitialSchema []byte
}

// Open creates a uniquely named schema with the current database structure
// and drops it when the test ends. The database URL comes from
// TEST_DATABASE_URL or the Git-ignored .test-db-url file in the repository
// root; without either the test is skipped.
func Open(t testing.TB) *Database {
	t.Helper()
	root := repositoryRoot(t)
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		localURL, err := os.ReadFile(filepath.Join(root, ".test-db-url"))
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
	t.Cleanup(func() { adminDB.Close() })

	schemaName := fmt.Sprintf("goexpenses_test_%d", time.Now().UnixNano())
	quotedSchema := pq.QuoteIdentifier(schemaName)
	if _, err := adminDB.ExecContext(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		t.Fatalf("create test schema: %v", err)
	}
	t.Cleanup(func() {
		if _, err := adminDB.ExecContext(context.Background(), "DROP SCHEMA "+quotedSchema+" CASCADE"); err != nil {
			t.Errorf("drop test schema: %v", err)
		}
	})

	parsedURL, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatalf("parse test database URL: %v", err)
	}
	query := parsedURL.Query()
	query.Set("search_path", schemaName)
	parsedURL.RawQuery = query.Encode()

	testDB, err := sqlx.ConnectContext(ctx, "postgres", parsedURL.String())
	if err != nil {
		t.Fatalf("connect to isolated test schema: %v", err)
	}
	testDB.SetMaxOpenConns(1)
	// Registered after the schema cleanup, so it runs first and releases the
	// connection before the schema is dropped.
	t.Cleanup(func() { testDB.Close() })

	initialSchema, err := os.ReadFile(filepath.Join(root, "db", "pg_structure.sql"))
	if err != nil {
		t.Fatalf("read initial schema: %v", err)
	}
	initialSchema = []byte(strings.ReplaceAll(string(initialSchema), "public.", quotedSchema+"."))
	if err := migrations.Apply(ctx, testDB, initialSchema, migrations.SchemaVersion); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	return &Database{DB: testDB, InitialSchema: initialSchema}
}

func repositoryRoot(t testing.TB) string {
	t.Helper()
	directory, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			return directory
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			t.Fatal("repository root with go.mod not found")
		}
		directory = parent
	}
}
