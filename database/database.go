package database

import (
	"github.com/jmoiron/sqlx"
)

var (
	Db                       *sqlx.DB
	DatabaseType             string
	DatabaseConnectionString string
)

func Connect() {
	if DatabaseType == "postgres" {
		Db = sqlx.MustConnect("postgres", DatabaseConnectionString)
	} else {
		//Db = sqlx.MustConnect("sqlite3", DatabaseConnectionString)
	}
}
