package routes

import (
	"database/sql"
	"errors"
	"html"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"goexpenses/database"
	"goexpenses/util"

	gomail "gopkg.in/gomail.v2"
)

const (
	verificationTokenDuration   = 24 * time.Hour
	verificationResendInterval  = 15 * time.Minute
	verificationResponseMessage = "If the E-Mail address is awaiting verification, a confirmation link has been sent."
)

func newVerificationToken() (string, string, error) {
	return util.NewPasswordResetToken()
}

func hashVerificationToken(token string) (string, error) {
	return util.HashPasswordResetToken(token)
}

func verificationTokenValid(token string, now time.Time) (bool, error) {
	hash, err := hashVerificationToken(token)
	if err != nil {
		return false, nil
	}
	var exists bool
	err = database.Db.Get(&exists, `SELECT EXISTS (
		SELECT 1 FROM users WHERE verification_token = $1 AND email_verified = false AND verification_sent_at >= $2
	)`, hash, now.Add(-verificationTokenDuration))
	return exists, err
}

func confirmEmail(token string, now time.Time) (bool, error) {
	hash, err := hashVerificationToken(token)
	if err != nil {
		return false, nil
	}
	result, err := database.Db.Exec(`UPDATE users
		SET email_verified = true, verification_token = NULL, verification_sent_at = NULL
		WHERE verification_token = $1 AND email_verified = false AND verification_sent_at >= $2`,
		hash, now.Add(-verificationTokenDuration))
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	return rows == 1, err
}

func requestVerification(email string, now time.Time) (recipient, token string, created bool, err error) {
	email = strings.ToLower(strings.TrimSpace(email))
	address, parseErr := mail.ParseAddress(email)
	if len(email) > registrationEmailMaxLength || parseErr != nil || address.Address != email {
		return "", "", false, nil
	}
	tx, err := database.Db.Beginx()
	if err != nil {
		return "", "", false, err
	}
	defer tx.Rollback()
	var user struct {
		ID     int          `db:"id"`
		Email  string       `db:"email"`
		SentAt sql.NullTime `db:"verification_sent_at"`
	}
	err = tx.Get(&user, `SELECT id, email, verification_sent_at FROM users
		WHERE lower(btrim(email)) = $1 AND email_verified = false FOR UPDATE`, email)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, err
	}
	if user.SentAt.Valid && now.Before(user.SentAt.Time.Add(verificationResendInterval)) {
		return "", "", false, nil
	}
	token, hash, err := newVerificationToken()
	if err != nil {
		return "", "", false, err
	}
	if _, err = tx.Exec(`UPDATE users SET verification_token = $1, verification_sent_at = $2 WHERE id = $3`, hash, now, user.ID); err != nil {
		return "", "", false, err
	}
	if err = tx.Commit(); err != nil {
		return "", "", false, err
	}
	return user.Email, token, true, nil
}

func sendVerificationEmail(recipient, token, lang string) error {
	link := strings.TrimRight(util.Settings.Host, "/") + "/verify-email?t=" + url.QueryEscape(token)
	message := gomail.NewMessage()
	message.SetHeader("From", util.Settings.MailFrom)
	message.SetHeader("To", recipient)
	message.SetHeader("Subject", "Goexpenses "+util.GetLangText("Confirm your E-Mail", lang))
	message.SetBody("text/html", util.GetLangText("Click this link to confirm your E-Mail:", lang)+` <a href="`+html.EscapeString(link)+`">`+util.GetLangText("Confirm E-Mail", lang)+`</a>`)
	return newPasswordResetDialer().DialAndSend(message)
}
