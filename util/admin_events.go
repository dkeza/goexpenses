package util

import (
	"context"
	"errors"
	"strings"

	"goexpenses/database"
)

type AdminEvent struct {
	Kind        string
	Status      string
	UserID      *int
	ActorUserID *int
	Subject     string
	Detail      string
	ItemCount   *int
}

func RecordAdminEvent(ctx context.Context, event AdminEvent) error {
	if database.Db == nil {
		return errors.New("record admin event: database is not connected")
	}
	_, err := database.Db.ExecContext(ctx, `
		INSERT INTO admin_events (kind, status, user_id, actor_user_id, subject, detail, item_count)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		event.Kind, event.Status, event.UserID, event.ActorUserID,
		truncateEventField(event.Subject, 254), truncateEventField(event.Detail, 500), event.ItemCount)
	return err
}

func truncateEventField(value string, limit int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}
