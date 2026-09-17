package migrations

import (
	"context"
	"errors"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
)

const schemaInitializedQuery = `
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.tables
			WHERE table_schema = current_schema()
			  AND table_name = 'params'
		)`

func newMigrationMock(t *testing.T) (*sqlx.DB, sqlmock.Sqlmock) {
	t.Helper()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create mock database: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("database expectations: %v", err)
		}
		db.Close()
	})
	return sqlx.NewDb(db, "sqlmock"), mock
}

func expectMigrationLock(mock sqlmock.Sqlmock) {
	mock.ExpectExec(regexp.QuoteMeta("SELECT pg_advisory_lock($1)")).
		WithArgs(advisoryLockID).
		WillReturnResult(sqlmock.NewResult(0, 1))
}

func expectMigrationUnlock(mock sqlmock.Sqlmock) {
	mock.ExpectExec(regexp.QuoteMeta("SELECT pg_advisory_unlock($1)")).
		WithArgs(advisoryLockID).
		WillReturnResult(sqlmock.NewResult(0, 1))
}

func TestApplyInitializesFreshSchemaAtTargetVersion(t *testing.T) {
	db, mock := newMigrationMock(t)
	initialSchema := []byte("CREATE TABLE params (id integer, build integer)")

	expectMigrationLock(mock)
	mock.ExpectQuery(regexp.QuoteMeta(schemaInitializedQuery)).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(string(initialSchema))).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO params (id, build) VALUES (1, $1)")).
		WithArgs(CurrentVersion).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	expectMigrationUnlock(mock)

	if err := Apply(context.Background(), db, initialSchema, CurrentVersion); err != nil {
		t.Fatalf("Apply: %v", err)
	}
}

func TestApplyDoesNothingWhenSchemaIsCurrent(t *testing.T) {
	db, mock := newMigrationMock(t)

	expectMigrationLock(mock)
	mock.ExpectQuery(regexp.QuoteMeta(schemaInitializedQuery)).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT build FROM params WHERE id = 1")).
		WillReturnRows(sqlmock.NewRows([]string{"build"}).AddRow(CurrentVersion))
	expectMigrationUnlock(mock)

	if err := Apply(context.Background(), db, []byte("unused"), CurrentVersion); err != nil {
		t.Fatalf("Apply: %v", err)
	}
}

func TestApplyRecordsVersionInMigrationTransaction(t *testing.T) {
	db, mock := newMigrationMock(t)
	script, err := migrationFiles.ReadFile(migrationPaths[CurrentVersion])
	if err != nil {
		t.Fatalf("read test migration: %v", err)
	}

	expectMigrationLock(mock)
	mock.ExpectQuery(regexp.QuoteMeta(schemaInitializedQuery)).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT build FROM params WHERE id = 1")).
		WillReturnRows(sqlmock.NewRows([]string{"build"}).AddRow(CurrentVersion - 1))
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(string(script))).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE params SET build = $1 WHERE id = 1")).
		WithArgs(CurrentVersion).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	expectMigrationUnlock(mock)

	if err := Apply(context.Background(), db, []byte("unused"), CurrentVersion); err != nil {
		t.Fatalf("Apply: %v", err)
	}
}

func TestApplyRollsBackFailedMigrationWithoutUpdatingVersion(t *testing.T) {
	db, mock := newMigrationMock(t)
	script, err := migrationFiles.ReadFile(migrationPaths[11])
	if err != nil {
		t.Fatalf("read test migration: %v", err)
	}

	expectMigrationLock(mock)
	mock.ExpectQuery(regexp.QuoteMeta(schemaInitializedQuery)).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT build FROM params WHERE id = 1")).
		WillReturnRows(sqlmock.NewRows([]string{"build"}).AddRow(8))
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(string(script))).
		WillReturnError(errors.New("migration failed"))
	mock.ExpectRollback()
	expectMigrationUnlock(mock)

	if err := Apply(context.Background(), db, []byte("unused"), CurrentVersion); err == nil {
		t.Fatal("Apply accepted a failed migration")
	}
}

func TestApplyRejectsNewerDatabaseSchema(t *testing.T) {
	db, mock := newMigrationMock(t)

	expectMigrationLock(mock)
	mock.ExpectQuery(regexp.QuoteMeta(schemaInitializedQuery)).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT build FROM params WHERE id = 1")).
		WillReturnRows(sqlmock.NewRows([]string{"build"}).AddRow(CurrentVersion + 1))
	expectMigrationUnlock(mock)

	if err := Apply(context.Background(), db, []byte("unused"), CurrentVersion); err == nil {
		t.Fatal("Apply accepted a database schema newer than the application")
	}
}
