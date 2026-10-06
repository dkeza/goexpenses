package routes

import (
	"context"
	"crypto/tls"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"goexpenses/database"
	"goexpenses/util"

	mail "github.com/wneessen/go-mail"
)

const (
	// emailSendTimeout bounds a whole delivery, including a stalled server.
	emailSendTimeout = time.Minute
	// smtpConnectionTimeout is the deadline for each SMTP network operation.
	smtpConnectionTimeout = 30 * time.Second
	maxConcurrentEmails   = 2
)

var (
	errEmailTimeout = errors.New("email delivery timed out")

	deliverEmail = deliverWithSMTP
	emailWorkers sync.WaitGroup
	emailSlots   = make(chan struct{}, maxConcurrentEmails)
)

// emailMessage is an HTML email sent from the configured sender address.
type emailMessage struct {
	Recipient string
	Subject   string
	HTMLBody  string
}

// mailTLSConfig verifies the SMTP server certificate for the configured host.
func mailTLSConfig() *tls.Config {
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		ServerName: util.Settings.MailHost,
	}
}

// mailClientOptions keeps the behavior of the previous mail library: port
// 465 uses implicit TLS, other ports upgrade with STARTTLS when the server
// offers it, and the sender address is the SMTP user name.
func mailClientOptions() []mail.Option {
	options := []mail.Option{
		mail.WithPort(util.Settings.MailHostPort),
		mail.WithTimeout(smtpConnectionTimeout),
		mail.WithTLSConfig(mailTLSConfig()),
	}
	if mailUsesImplicitTLS(util.Settings.MailHostPort) {
		options = append(options, mail.WithSSL())
	} else {
		options = append(options, mail.WithTLSPolicy(mail.TLSOpportunistic))
	}
	if util.Settings.MailPassword != "" {
		options = append(options,
			mail.WithSMTPAuth(mail.SMTPAuthAutoDiscover),
			mail.WithUsername(util.Settings.MailFrom),
			mail.WithPassword(util.Settings.MailPassword),
		)
	}
	return options
}

// mailUsesImplicitTLS reports whether the SMTP port expects TLS from the
// first byte (SMTPS) instead of a STARTTLS upgrade.
func mailUsesImplicitTLS(port int) bool {
	return port == mail.DefaultPortSSL
}

func deliverWithSMTP(ctx context.Context, message emailMessage) error {
	msg := mail.NewMsg()
	if err := msg.From(util.Settings.MailFrom); err != nil {
		return fmt.Errorf("set email sender: %w", err)
	}
	if err := msg.To(message.Recipient); err != nil {
		return fmt.Errorf("set email recipient: %w", err)
	}
	msg.Subject(message.Subject)
	msg.SetBodyString(mail.TypeTextHTML, message.HTMLBody)

	client, err := mail.NewClient(util.Settings.MailHost, mailClientOptions()...)
	if err != nil {
		return fmt.Errorf("configure SMTP client: %w", err)
	}
	return client.DialAndSendWithContext(ctx, msg)
}

// queueTrackedEmail sends an email in the background. Responses therefore do
// not wait for SMTP, so their timing does not reveal whether an address or
// user name exists.
func queueTrackedEmail(kind string, message emailMessage) {
	emailWorkers.Go(func() {
		emailSlots <- struct{}{}
		defer func() { <-emailSlots }()
		if err := sendTrackedEmail(kind, message); err != nil {
			log.Printf("could not send %s email: %v", kind, err)
		}
	})
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

func deliverEmailWithTimeout(message emailMessage, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	result := make(chan error, 1)
	// Read deliverEmail before starting the goroutine: after a timeout the
	// delivery keeps running while callers may already have moved on.
	deliver := deliverEmail
	go func() { result <- deliver(ctx, message) }()
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		return errEmailTimeout
	}
}

func sendTrackedEmail(kind string, message emailMessage) error {
	var userID *int
	var id int
	lookupErr := database.Db.Get(&id, `SELECT id FROM users WHERE lower(btrim(email)) = $1`, strings.ToLower(strings.TrimSpace(message.Recipient)))
	if lookupErr == nil {
		userID = &id
	} else if !errors.Is(lookupErr, sql.ErrNoRows) {
		log.Printf("could not match email event to user: %v", lookupErr)
	}
	var eventID int64
	if err := database.Db.QueryRowx(`
		INSERT INTO admin_events (kind, status, user_id, subject, detail)
		VALUES ('email', 'smtp_pending', $1, $2, $3) RETURNING id`, userID, message.Recipient, kind).Scan(&eventID); err != nil {
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
