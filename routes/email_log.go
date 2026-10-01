package routes

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"strings"
	"sync"
	"time"

	"goexpenses/database"

	gomail "gopkg.in/gomail.v2"
)

const (
	// emailSendTimeout bounds a whole SMTP delivery; gomail only limits the
	// initial connection.
	emailSendTimeout    = time.Minute
	maxConcurrentEmails = 2
)

var (
	errEmailTimeout = errors.New("email delivery timed out")

	deliverEmail = func(message *gomail.Message) error {
		return newPasswordResetDialer().DialAndSend(message)
	}
	emailWorkers sync.WaitGroup
	emailSlots   = make(chan struct{}, maxConcurrentEmails)
)

// queueTrackedEmail sends an email in the background. Responses therefore do
// not wait for SMTP, so their timing does not reveal whether an address or
// user name exists.
func queueTrackedEmail(kind, recipient string, message *gomail.Message) {
	emailWorkers.Add(1)
	go func() {
		defer emailWorkers.Done()
		emailSlots <- struct{}{}
		defer func() { <-emailSlots }()
		if err := sendTrackedEmail(kind, recipient, message); err != nil {
			log.Printf("could not send %s email: %v", kind, err)
		}
	}()
}

// WaitForEmails waits for queued emails to finish, or until ctx ends.
func WaitForEmails(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		emailWorkers.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func deliverEmailWithTimeout(message *gomail.Message, timeout time.Duration) error {
	result := make(chan error, 1)
	go func() { result <- deliverEmail(message) }()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case err := <-result:
		return err
	case <-timer.C:
		return errEmailTimeout
	}
}

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
	err := deliverEmailWithTimeout(message, emailSendTimeout)
	status := "smtp_accepted"
	if err != nil {
		status = "smtp_unconfirmed"
	}
	if _, updateErr := database.Db.Exec(`UPDATE admin_events SET status = $1 WHERE id = $2`, status, eventID); updateErr != nil {
		log.Printf("could not update email event: %v", updateErr)
	}
	return err
}
