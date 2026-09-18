package routes

import (
	"errors"
	"net/mail"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/lib/pq"
)

const (
	registrationNameMaxLength     = 100
	registrationUsernameMaxLength = 64
	registrationEmailMaxLength    = 254
	registrationPasswordMinLength = 10
	registrationPasswordMaxBytes  = 72
)

var registrationUsernamePattern = regexp.MustCompile(`^[a-z0-9._-]{3,64}$`)

type registrationInput struct {
	Name     string
	Email    string
	Username string
	Password string
}

func validateRegistration(name, email, username, password string) (registrationInput, string) {
	input := registrationInput{
		Name:     strings.TrimSpace(name),
		Email:    strings.ToLower(strings.TrimSpace(email)),
		Username: strings.ToLower(strings.TrimSpace(username)),
		Password: password,
	}

	if input.Name == "" || utf8.RuneCountInString(input.Name) > registrationNameMaxLength {
		return registrationInput{}, "Invalid name!"
	}
	if len(input.Email) > registrationEmailMaxLength {
		return registrationInput{}, "Invalid E-Mail!"
	}
	address, err := mail.ParseAddress(input.Email)
	if err != nil || address.Address != input.Email {
		return registrationInput{}, "Invalid E-Mail!"
	}
	if len(input.Username) > registrationUsernameMaxLength || !registrationUsernamePattern.MatchString(input.Username) {
		return registrationInput{}, "Invalid user name!"
	}
	if !validNewPassword(input.Password) {
		return registrationInput{}, "Password must contain between 10 and 72 characters."
	}

	return input, ""
}

func validNewPassword(password string) bool {
	return utf8.RuneCountInString(password) >= registrationPasswordMinLength && len(password) <= registrationPasswordMaxBytes
}

func registrationConflictMessage(err error) string {
	var postgresError *pq.Error
	if !errors.As(err, &postgresError) || postgresError.Code != "23505" {
		return ""
	}

	switch postgresError.Constraint {
	case "users_username_lower_uidx":
		return "User name is already in use."
	case "users_email_lower_uidx":
		return "E-Mail is already in use."
	default:
		return ""
	}
}
