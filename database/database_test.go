package database

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
)

func TestConnectPingsDatabaseBeforePublishingConnection(t *testing.T) {
	sqlDB, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	if err != nil {
		t.Fatalf("create mock database: %v", err)
	}
	defer sqlDB.Close()

	wantDB := sqlx.NewDb(sqlDB, "sqlmock")
	previousOpen := openDatabase
	previousDB := Db
	previousType := DatabaseType
	previousConnectionString := DatabaseConnectionString
	t.Cleanup(func() {
		openDatabase = previousOpen
		Db = previousDB
		DatabaseType = previousType
		DatabaseConnectionString = previousConnectionString
	})

	openDatabase = func(driverName, connectionString string) (*sqlx.DB, error) {
		if driverName != "postgres" || connectionString != "postgres://configured-database" {
			t.Fatalf("openDatabase arguments = %q, %q", driverName, connectionString)
		}
		return wantDB, nil
	}
	DatabaseType = "postgres"
	DatabaseConnectionString = "postgres://configured-database"
	mock.ExpectPing()

	if err := Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if Db != wantDB {
		t.Fatal("Connect published a different database handle")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("database expectations: %v", err)
	}
}

func TestConnectDoesNotPublishConnectionWhenPingFails(t *testing.T) {
	sqlDB, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	if err != nil {
		t.Fatalf("create mock database: %v", err)
	}

	wantDB := sqlx.NewDb(sqlDB, "sqlmock")
	previousOpen := openDatabase
	previousDB := Db
	previousType := DatabaseType
	previousConnectionString := DatabaseConnectionString
	t.Cleanup(func() {
		openDatabase = previousOpen
		Db = previousDB
		DatabaseType = previousType
		DatabaseConnectionString = previousConnectionString
	})

	openDatabase = func(string, string) (*sqlx.DB, error) {
		return wantDB, nil
	}
	Db = nil
	DatabaseType = "postgres"
	DatabaseConnectionString = "postgres://user:sensitive-password@database.example.com/app"
	mock.ExpectPing().WillReturnError(errors.New("connection refused"))
	mock.ExpectClose()

	err = Connect(context.Background())
	if err == nil {
		t.Fatal("Connect accepted a failed database ping")
	}
	if Db != nil {
		t.Fatal("Connect published a database handle after a failed ping")
	}
	if strings.Contains(err.Error(), "sensitive-password") || strings.Contains(err.Error(), DatabaseConnectionString) {
		t.Fatalf("Connect error exposed the database connection string: %q", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("database expectations: %v", err)
	}
}

func TestConnectRejectsUnsupportedDatabaseType(t *testing.T) {
	previousType := DatabaseType
	t.Cleanup(func() { DatabaseType = previousType })

	DatabaseType = "sqlite"
	if err := Connect(context.Background()); err == nil {
		t.Fatal("Connect accepted an unsupported database type")
	}
}
