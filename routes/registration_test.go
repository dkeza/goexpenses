package routes

import (
	"errors"
	"testing"

	"github.com/lib/pq"
)

func TestValidateRegistrationNormalizesInput(t *testing.T) {
	input, message := validateRegistration(
		"  Test User  ",
		"  Test.User@Example.COM ",
		"  Test_User  ",
		"long-enough-password",
	)
	if message != "" {
		t.Fatalf("validateRegistration message = %q, want empty", message)
	}
	if input.Name != "Test User" || input.Email != "test.user@example.com" || input.Username != "test_user" {
		t.Fatalf("validateRegistration input = %#v, want normalized values", input)
	}
}

func TestValidateRegistrationRejectsInvalidFields(t *testing.T) {
	tests := []struct {
		name     string
		fullName string
		email    string
		username string
		password string
		message  string
	}{
		{name: "blank name", email: "user@example.com", username: "tester", password: "long-password", message: "Invalid name!"},
		{name: "invalid email", fullName: "Test User", email: "not-an-email", username: "tester", password: "long-password", message: "Invalid E-Mail!"},
		{name: "short username", fullName: "Test User", email: "user@example.com", username: "ab", password: "long-password", message: "Invalid user name!"},
		{name: "unsafe username", fullName: "Test User", email: "user@example.com", username: "test user", password: "long-password", message: "Invalid user name!"},
		{name: "short password", fullName: "Test User", email: "user@example.com", username: "tester", password: "short", message: "Password must contain between 10 and 72 characters."},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, message := validateRegistration(test.fullName, test.email, test.username, test.password)
			if message != test.message {
				t.Fatalf("validateRegistration message = %q, want %q", message, test.message)
			}
		})
	}
}

func TestRegistrationConflictMessage(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "username", err: &pq.Error{Code: "23505", Constraint: "users_username_lower_uidx"}, want: registrationResponseMessage},
		{name: "email", err: &pq.Error{Code: "23505", Constraint: "users_email_lower_uidx"}, want: registrationResponseMessage},
		{name: "wrapped", err: errors.Join(errors.New("insert user"), &pq.Error{Code: "23505", Constraint: "users_email_lower_uidx"}), want: registrationResponseMessage},
		{name: "other constraint", err: &pq.Error{Code: "23505", Constraint: "other"}},
		{name: "other error", err: errors.New("database unavailable")},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := registrationConflictMessage(test.err); got != test.want {
				t.Fatalf("registrationConflictMessage = %q, want %q", got, test.want)
			}
		})
	}
}

func TestValidNewPassword(t *testing.T) {
	if validNewPassword("short") {
		t.Fatal("validNewPassword accepted a short password")
	}
	if !validNewPassword("long-enough-password") {
		t.Fatal("validNewPassword rejected a valid password")
	}
	if validNewPassword(string(make([]byte, registrationPasswordMaxBytes+1))) {
		t.Fatal("validNewPassword accepted a password longer than bcrypt supports")
	}
}
