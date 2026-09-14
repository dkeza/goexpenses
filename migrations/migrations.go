package migrations

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jmoiron/sqlx"
)

const advisoryLockID int64 = 6754112174613617249

//go:embed sql/*.sql
var migrationFiles embed.FS

var migrationPaths = map[int]string{
	1:  "sql/001_public_ids.sql",
	8:  "sql/008_session_created_at.sql",
	11: "sql/011_post_created_ts.sql",
	12: "sql/012_current_version.sql",
	13: "sql/013_current_version.sql",
}

func Apply(ctx context.Context, db *sqlx.DB, initialSchema []byte, targetVersion int) error {
	if db == nil {
		return errors.New("apply migrations: database is not connected")
	}
	if targetVersion < 1 {
		return fmt.Errorf("apply migrations: invalid target version %d", targetVersion)
	}
	if _, ok := migrationPaths[targetVersion]; !ok {
		return fmt.Errorf("apply migrations: migration for target version %d is missing", targetVersion)
	}

	connection, err := db.Connx(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration connection: %w", err)
	}
	defer connection.Close()

	if _, err := connection.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, advisoryLockID); err != nil {
		return fmt.Errorf("lock database migrations: %w", err)
	}
	defer func() {
		unlockContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		connection.ExecContext(unlockContext, `SELECT pg_advisory_unlock($1)`, advisoryLockID)
	}()

	initialized, err := schemaInitialized(ctx, connection)
	if err != nil {
		return err
	}
	if !initialized {
		return initializeSchema(ctx, connection, initialSchema, targetVersion)
	}

	currentVersion, err := schemaVersion(ctx, connection)
	if err != nil {
		return err
	}
	if currentVersion > targetVersion {
		return fmt.Errorf("database schema version %d is newer than application version %d", currentVersion, targetVersion)
	}

	versions := make([]int, 0, len(migrationPaths))
	for version := range migrationPaths {
		if version > currentVersion && version <= targetVersion {
			versions = append(versions, version)
		}
	}
	sort.Ints(versions)

	for _, version := range versions {
		if err := applyMigration(ctx, connection, version, migrationPaths[version]); err != nil {
			return err
		}
	}
	return nil
}

func schemaInitialized(ctx context.Context, connection *sqlx.Conn) (bool, error) {
	var initialized bool
	err := connection.GetContext(ctx, &initialized, `
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.tables
			WHERE table_schema = current_schema()
			  AND table_name = 'params'
		)`)
	if err != nil {
		return false, fmt.Errorf("check database schema: %w", err)
	}
	return initialized, nil
}

func schemaVersion(ctx context.Context, connection *sqlx.Conn) (int, error) {
	var version int
	err := connection.GetContext(ctx, &version, `SELECT build FROM params WHERE id = 1`)
	if errors.Is(err, sql.ErrNoRows) {
		if _, err := connection.ExecContext(ctx, `INSERT INTO params (id, build) VALUES (1, 0)`); err != nil {
			return 0, fmt.Errorf("initialize database schema version: %w", err)
		}
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read database schema version: %w", err)
	}
	return version, nil
}

func initializeSchema(ctx context.Context, connection *sqlx.Conn, initialSchema []byte, targetVersion int) error {
	if len(initialSchema) == 0 {
		return errors.New("initialize database schema: embedded schema is empty")
	}

	transaction, err := connection.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin database initialization: %w", err)
	}
	defer transaction.Rollback()

	if _, err := transaction.ExecContext(ctx, string(initialSchema)); err != nil {
		return fmt.Errorf("create database schema: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `INSERT INTO params (id, build) VALUES (1, $1)`, targetVersion); err != nil {
		return fmt.Errorf("record database schema version: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit database initialization: %w", err)
	}
	return nil
}

func applyMigration(ctx context.Context, connection *sqlx.Conn, version int, path string) error {
	script, err := migrationFiles.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read migration %d: %w", version, err)
	}

	transaction, err := connection.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration %d: %w", version, err)
	}
	defer transaction.Rollback()

	if _, err := transaction.ExecContext(ctx, string(script)); err != nil {
		return fmt.Errorf("apply migration %d: %w", version, err)
	}
	result, err := transaction.ExecContext(ctx, `UPDATE params SET build = $1 WHERE id = 1`, version)
	if err != nil {
		return fmt.Errorf("record migration %d: %w", version, err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("verify migration %d version: %w", version, err)
	}
	if rowsAffected != 1 {
		return fmt.Errorf("record migration %d: schema version row is missing", version)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit migration %d: %w", version, err)
	}
	return nil
}
