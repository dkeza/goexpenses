package database

import (
	"context"
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"
)

var (
	Db                       *sqlx.DB
	DatabaseType             string
	DatabaseConnectionString string
	openDatabase             = sqlx.Open
)

func Connect(ctx context.Context) error {
	if DatabaseType != "postgres" {
		return fmt.Errorf("unsupported database type %q", DatabaseType)
	}

	db, err := openDatabase("postgres", DatabaseConnectionString)
	if err != nil {
		return errors.New("open database connection")
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return fmt.Errorf("ping database: %w", err)
	}

	Db = db
	return nil
}
