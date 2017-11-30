package database

import (
	"github.com/jmoiron/sqlx"
)

var Db *sqlx.DB

func Connect() {
	Db = sqlx.MustConnect("sqlite3", "./db/database.db")
}
