package main

import (
	"strings"
	"testing"
)

func TestEmbeddedPostgresSchemaIsCurrentAndNonDestructive(t *testing.T) {
	schema, err := embeddedFiles.ReadFile("db/pg_structure.sql")
	if err != nil {
		t.Fatalf("read embedded PostgreSQL schema: %v", err)
	}
	schemaText := string(schema)

	if strings.Contains(strings.ToUpper(schemaText), "DROP TABLE") {
		t.Fatal("initial PostgreSQL schema contains a destructive DROP TABLE statement")
	}
	for _, required := range []string{
		"CREATE TABLE public.params",
		"created_at timestamp NOT NULL DEFAULT NOW()",
		"created_ts timestamp NOT NULL DEFAULT NOW()",
	} {
		if !strings.Contains(schemaText, required) {
			t.Fatalf("initial PostgreSQL schema does not contain %q", required)
		}
	}
}
