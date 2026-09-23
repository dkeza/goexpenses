package util

import (
	"context"
	"regexp"
	"testing"

	"goexpenses/database"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
)

func TestDeleteOldSessionsRecordsDeletedCount(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	previousDB := database.Db
	database.Db = sqlx.NewDb(db, "sqlmock")
	t.Cleanup(func() { database.Db = previousDB })
	previousType := Settings.DatabaseType
	Settings.DatabaseType = "postgres"
	t.Cleanup(func() { Settings.DatabaseType = previousType })

	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM sessions WHERE created_at < $1`)).
		WithArgs(sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 3))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO admin_events (kind, status, user_id, actor_user_id, subject, detail, item_count)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`)).
		WithArgs("session_cleanup", "success", nil, nil, "", "", 3).
		WillReturnResult(sqlmock.NewResult(1, 1))
	if err := DeleteOldSessions(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
