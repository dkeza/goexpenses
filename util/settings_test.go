package util

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadSettingsFromINI(t *testing.T) {
	configPath := writeSettingsFile(t, `[settings]
host=https://app.example.com/
port=8081
mailhost=smtp.example.com
mailhostport=587
mailfrom=app@example.com
mailpassword=ini-secret
openexchangeratesid=exchange-id
cookiesecure=false
databasetype=postgres
DATABASE_URL=postgres://user:database-secret@db.example.com/app
`)

	settings, err := loadSettings(configPath, mapEnvironment(nil))
	if err != nil {
		t.Fatalf("loadSettings: %v", err)
	}
	if settings.Build != 13 || settings.Host != "https://app.example.com" || settings.Port != "8081" {
		t.Fatal("INI application settings were not loaded")
	}
	if settings.MailHost != "smtp.example.com" || settings.MailHostPort != 587 || settings.MailFrom != "app@example.com" || settings.MailPassword != "ini-secret" {
		t.Fatal("INI SMTP settings were not loaded")
	}
	if settings.OpenExchangeRatesId != "exchange-id" || settings.CookieSecure {
		t.Fatal("INI exchange ID or secure-cookie setting was not loaded")
	}
	if settings.DatabaseType != "postgres" || settings.DatabaseConnectionString != "postgres://user:database-secret@db.example.com/app" {
		t.Fatal("INI database settings were not loaded")
	}
}

func TestLoadSettingsUsesEnvironmentOverrides(t *testing.T) {
	configPath := writeSettingsFile(t, `[settings]
host=https://ini.example.com
port=8081
mailhost=smtp.ini.example.com
mailhostport=465
mailfrom=ini@example.com
mailpassword=ini-secret
openexchangeratesid=ini-exchange-id
cookiesecure=false
databasetype=postgres
databaseurl=postgres://ini-user:ini-secret@db.ini.example.com/app
`)

	environment := map[string]string{
		"HOST":          "https://env.example.com/",
		"PORT":          "9090",
		"MAIL_HOST":     "smtp.env.example.com",
		"MAIL_PORT":     "587",
		"MAIL_FROM":     "env@example.com",
		"MAIL_PASSWORD": "env-secret",
		"EXCHANGE_ID":   "env-exchange-id",
		"COOKIE_SECURE": "true",
		"DATABASE_TYPE": "POSTGRES",
		"DATABASE_URL":  "postgresql://env-user:env-secret@db.env.example.com/app",
	}

	settings, err := loadSettings(configPath, mapEnvironment(environment))
	if err != nil {
		t.Fatalf("loadSettings: %v", err)
	}

	if settings.Host != "https://env.example.com" || settings.Port != "9090" {
		t.Fatalf("host/port = %q/%q; environment did not override INI", settings.Host, settings.Port)
	}
	if settings.MailHost != "smtp.env.example.com" || settings.MailHostPort != 587 || settings.MailFrom != "env@example.com" || settings.MailPassword != "env-secret" {
		t.Fatal("environment did not override SMTP settings")
	}
	if settings.OpenExchangeRatesId != "env-exchange-id" || !settings.CookieSecure {
		t.Fatal("environment did not override exchange ID or secure-cookie setting")
	}
	if settings.DatabaseType != "postgres" || settings.DatabaseConnectionString != environment["DATABASE_URL"] {
		t.Fatal("environment did not override database settings")
	}
}

func TestEnvironmentCanOverrideInvalidINIValue(t *testing.T) {
	configPath := writeSettingsFile(t, `[settings]
host=https://example.com
port=8080
mailhost=smtp.example.com
mailhostport=not-a-port
mailfrom=app@example.com
openexchangeratesid=exchange-id
cookiesecure=not-a-boolean
databasetype=postgres
DATABASE_URL=postgres://user:secret@db.example.com/app
`)
	environment := map[string]string{
		"MAIL_PORT":     "587",
		"COOKIE_SECURE": "true",
	}

	settings, err := loadSettings(configPath, mapEnvironment(environment))
	if err != nil {
		t.Fatalf("loadSettings: %v", err)
	}
	if settings.MailHostPort != 587 || !settings.CookieSecure {
		t.Fatal("valid environment values did not override invalid INI values")
	}
}

func TestLoadSettingsReportsAllMissingRequiredValues(t *testing.T) {
	settings, err := loadSettings(filepath.Join(t.TempDir(), "missing.ini"), mapEnvironment(nil))
	if err == nil {
		t.Fatalf("loadSettings returned settings without required values: host=%q port=%q database_type=%q", settings.Host, settings.Port, settings.DatabaseType)
	}

	message := err.Error()
	for _, settingName := range []string{"DATABASE_URL", "HOST", "MAIL_HOST", "MAIL_PORT", "MAIL_FROM", "EXCHANGE_ID"} {
		if !strings.Contains(message, settingName) {
			t.Errorf("configuration error does not mention %s: %q", settingName, message)
		}
	}
}

func TestLoadSettingsErrorsDoNotExposeSecrets(t *testing.T) {
	const secret = "do-not-print-this-secret"
	configPath := writeSettingsFile(t, "[settings]\nmailpassword "+secret+"\n")

	_, err := loadSettings(configPath, mapEnvironment(map[string]string{
		"MAIL_PASSWORD": secret,
	}))
	if err == nil {
		t.Fatal("loadSettings accepted malformed INI configuration")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("configuration error exposed a secret: %q", err)
	}
}

func TestLoadSettingsRejectsInvalidEnvironmentValuesWithoutExposingSecrets(t *testing.T) {
	const secret = "environment-secret"
	_, err := loadSettings(filepath.Join(t.TempDir(), "missing.ini"), mapEnvironment(map[string]string{
		"MAIL_PORT":     "invalid",
		"MAIL_PASSWORD": secret,
	}))
	if err == nil {
		t.Fatal("loadSettings accepted invalid MAIL_PORT")
	}
	if !strings.Contains(err.Error(), "MAIL_PORT") || strings.Contains(err.Error(), secret) {
		t.Fatalf("unsafe or unclear configuration error: %q", err)
	}
}

func writeSettingsFile(t *testing.T, contents string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "goexpenses.ini")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write settings file: %v", err)
	}
	return path
}

func mapEnvironment(values map[string]string) environmentLookup {
	return func(name string) (string, bool) {
		value, ok := values[name]
		return value, ok
	}
}
