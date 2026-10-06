package routes

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

var testEmail = emailMessage{Recipient: "user@example.com", Subject: "Test", HTMLBody: "<p>Test</p>"}

func stubEmailDelivery(t *testing.T, deliver func(context.Context, emailMessage) error) {
	t.Helper()
	previous := deliverEmail
	deliverEmail = deliver
	t.Cleanup(func() { deliverEmail = previous })
}

func expectEmailEvent(mock sqlmock.Sqlmock, status string) {
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id FROM users WHERE lower(btrim(email)) = $1")).
		WithArgs("user@example.com").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(42))
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO admin_events (kind, status, user_id, subject, detail)")).
		WithArgs(42, "user@example.com", "password_reset").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(7))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE admin_events SET status = $1 WHERE id = $2")).
		WithArgs(status, 7).
		WillReturnResult(sqlmock.NewResult(0, 1))
}

func TestQueuedEmailDoesNotBlockCaller(t *testing.T) {
	mock, _ := useMockRouteDatabase(t)
	expectEmailEvent(mock, "smtp_accepted")
	release := make(chan struct{})
	stubEmailDelivery(t, func(context.Context, emailMessage) error {
		<-release
		return nil
	})

	started := time.Now()
	queueTrackedEmail("password_reset", testEmail)
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("queueing waited %v for SMTP delivery", elapsed)
	}

	close(release)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := WaitForEmails(ctx); err != nil {
		t.Fatalf("WaitForEmails: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("database expectations: %v", err)
	}
}

func TestWaitForEmailsStopsAtDeadline(t *testing.T) {
	mock, _ := useMockRouteDatabase(t)
	expectEmailEvent(mock, "smtp_accepted")
	release := make(chan struct{})
	stubEmailDelivery(t, func(context.Context, emailMessage) error {
		<-release
		return nil
	})
	queueTrackedEmail("password_reset", testEmail)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := WaitForEmails(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("WaitForEmails error = %v, want deadline exceeded", err)
	}

	close(release)
	if err := WaitForEmails(context.Background()); err != nil {
		t.Fatalf("WaitForEmails after release: %v", err)
	}
}

func TestEmailDeliveryTimeoutIsRecordedAsUnconfirmed(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	stubEmailDelivery(t, func(context.Context, emailMessage) error {
		<-release
		return nil
	})

	if err := deliverEmailWithTimeout(testEmail, 10*time.Millisecond); !errors.Is(err, errEmailTimeout) {
		t.Fatalf("deliverEmailWithTimeout error = %v, want errEmailTimeout", err)
	}
}

func TestFailedEmailIsRecordedAsUnconfirmed(t *testing.T) {
	mock, _ := useMockRouteDatabase(t)
	expectEmailEvent(mock, "smtp_unconfirmed")
	stubEmailDelivery(t, func(context.Context, emailMessage) error { return errors.New("smtp unavailable") })

	if err := sendTrackedEmail("password_reset", testEmail); err == nil {
		t.Fatal("sendTrackedEmail ignored a delivery failure")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("database expectations: %v", err)
	}
}
