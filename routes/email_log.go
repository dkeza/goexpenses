package routes

import (
	"database/sql"
	"errors"
	"log"
	"strings"

	"goexpenses/database"

	gomail "gopkg.in/gomail.v2"
)

func sendTrackedEmail(kind, recipient string, message *gomail.Message) error {
	var userID *int
	var id int
	lookupErr := database.Db.Get(&id, `SELECT id FROM users WHERE lower(btrim(email)) = $1`, strings.ToLower(strings.TrimSpace(recipient)))
	if lookupErr == nil {
		userID = &id
	} else if !errors.Is(lookupErr, sql.ErrNoRows) {
		log.Printf("could not match email event to user: %v", lookupErr)
	}
	var eventID int64
	if err := database.Db.QueryRowx(`
		INSERT INTO admin_events (kind, status, user_id, subject, detail)
		VALUES ('email', 'smtp_pending', $1, $2, $3) RETURNING id`, userID, recipient, kind).Scan(&eventID); err != nil {
		return err
	}
	err := newPasswordResetDialer().DialAndSend(message)
	status := "smtp_accepted"
	if err != nil {
		status = "smtp_unconfirmed"
	}
	if _, updateErr := database.Db.Exec(`UPDATE admin_events SET status = $1 WHERE id = $2`, status, eventID); updateErr != nil {
		log.Printf("could not update email event: %v", updateErr)
	}
	return err
}
