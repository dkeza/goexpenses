package util

import (
	"context"
	"fmt"
	"log"
	"time"

	"goexpenses/database"
)

func Flash(message string, data *Data, success int, description string, expense_id int) {
	sql := `UPDATE sessions SET message = $1, message_success = $2, last_post_description = $3, expenses_id = $4 WHERE uuid = $5`
	if _, err := database.Db.Exec(sql, GetLangText(message, data.Lang), success, description, expense_id, data.CookieId); err != nil {
		log.Printf("Could not store flash message: %v", err)
	}
}

func DeleteOldSessions(ctx context.Context) error {
	currentTime := time.Now()
	oneMonthBefore := currentTime.AddDate(0, -1, 0)
	sql := `DELETE FROM sessions WHERE created_at < $1`
	result, err := database.Db.ExecContext(ctx, sql, oneMonthBefore)
	if err != nil {
		_ = RecordAdminEvent(ctx, AdminEvent{Kind: "session_cleanup", Status: "failed", Detail: "Database delete failed"})
		return fmt.Errorf("delete old sessions: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count deleted sessions: %w", err)
	}
	deleted := int(count)
	return RecordAdminEvent(ctx, AdminEvent{Kind: "session_cleanup", Status: "success", ItemCount: &deleted})
}
