package util

import (
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"os"
	"strconv"
	"strings"

	"goexpenses/database"

	ini "github.com/vaughan0/go-ini"
)

const defaultSettingsPath = "goexpenses.ini"

var Settings AppSettings

// Application settings
type AppSettings struct {
	Build                    int
	Host                     string
	Port                     string
	MailHost                 string
	MailHostPort             int
	MailFrom                 string
	MailPassword             string
	OpenExchangeRatesId      string
	CookieSecure             bool
	DatabaseType             string
	DatabaseConnectionString string
}

type environmentLookup func(string) (string, bool)

func defaultSettings() AppSettings {
	return AppSettings{
		Build:        13,
		Port:         "8080",
		CookieSecure: true,
		DatabaseType: "postgres",
	}
}

func ReadSettings() error {
	settings, err := loadSettings(defaultSettingsPath, os.LookupEnv)
	if err != nil {
		return err
	}

	Settings = settings
	database.DatabaseType = settings.DatabaseType
	database.DatabaseConnectionString = settings.DatabaseConnectionString
	return nil
}

func loadSettings(path string, lookupEnv environmentLookup) (AppSettings, error) {
	settings := defaultSettings()

	file, err := ini.LoadFile(path)
	if err == nil {
		if err := applyINISettings(&settings, file, lookupEnv); err != nil {
			return AppSettings{}, err
		}
	} else if !os.IsNotExist(err) {
		var syntaxError ini.ErrSyntax
		if errors.As(err, &syntaxError) {
			return AppSettings{}, fmt.Errorf("read configuration file %q: invalid INI syntax on line %d", path, syntaxError.Line)
		}
		return AppSettings{}, fmt.Errorf("read configuration file %q: %w", path, err)
	}

	if err := applyEnvironmentSettings(&settings, lookupEnv); err != nil {
		return AppSettings{}, err
	}
	normalizeSettings(&settings)
	if err := validateSettings(settings); err != nil {
		return AppSettings{}, err
	}

	return settings, nil
}

func applyINISettings(settings *AppSettings, file ini.File, lookupEnv environmentLookup) error {
	setINIString(file, "host", &settings.Host)
	setINIString(file, "port", &settings.Port)
	setINIString(file, "mailhost", &settings.MailHost)
	setINIString(file, "mailfrom", &settings.MailFrom)
	setINIString(file, "mailpassword", &settings.MailPassword)
	setINIString(file, "openexchangeratesid", &settings.OpenExchangeRatesId)
	setINIString(file, "databasetype", &settings.DatabaseType)
	if value, ok := file.Get("settings", "databaseurl"); ok {
		settings.DatabaseConnectionString = value
	}
	if value, ok := file.Get("settings", "DATABASE_URL"); ok {
		settings.DatabaseConnectionString = value
	}

	_, mailPortOverridden := lookupEnv("MAIL_PORT")
	if value, ok := file.Get("settings", "mailhostport"); ok && !mailPortOverridden {
		port, err := parsePort("mailhostport", value)
		if err != nil {
			return err
		}
		settings.MailHostPort = port
	}
	_, cookieSecureOverridden := lookupEnv("COOKIE_SECURE")
	if value, ok := file.Get("settings", "cookiesecure"); ok && !cookieSecureOverridden {
		secure, err := strconv.ParseBool(strings.TrimSpace(value))
		if err != nil {
			return fmt.Errorf("invalid configuration: cookiesecure must be true or false")
		}
		settings.CookieSecure = secure
	}
	return nil
}

func setINIString(file ini.File, key string, destination *string) {
	if value, ok := file.Get("settings", key); ok {
		*destination = value
	}
}

func applyEnvironmentSettings(settings *AppSettings, lookupEnv environmentLookup) error {
	setEnvironmentString(lookupEnv, "HOST", &settings.Host)
	setEnvironmentString(lookupEnv, "PORT", &settings.Port)
	setEnvironmentString(lookupEnv, "MAIL_HOST", &settings.MailHost)
	setEnvironmentString(lookupEnv, "MAIL_FROM", &settings.MailFrom)
	setEnvironmentString(lookupEnv, "MAIL_PASSWORD", &settings.MailPassword)
	setEnvironmentString(lookupEnv, "EXCHANGE_ID", &settings.OpenExchangeRatesId)
	setEnvironmentString(lookupEnv, "DATABASE_TYPE", &settings.DatabaseType)
	setEnvironmentString(lookupEnv, "DATABASE_URL", &settings.DatabaseConnectionString)

	if value, ok := lookupEnv("MAIL_PORT"); ok {
		port, err := parsePort("MAIL_PORT", value)
		if err != nil {
			return err
		}
		settings.MailHostPort = port
	}
	if value, ok := lookupEnv("COOKIE_SECURE"); ok {
		secure, err := strconv.ParseBool(strings.TrimSpace(value))
		if err != nil {
			return fmt.Errorf("invalid configuration: COOKIE_SECURE must be true or false")
		}
		settings.CookieSecure = secure
	}
	return nil
}

func setEnvironmentString(lookupEnv environmentLookup, name string, destination *string) {
	if value, ok := lookupEnv(name); ok {
		*destination = value
	}
}

func normalizeSettings(settings *AppSettings) {
	settings.Host = strings.TrimRight(strings.TrimSpace(settings.Host), "/")
	settings.Port = strings.TrimSpace(settings.Port)
	settings.MailHost = strings.TrimSpace(settings.MailHost)
	settings.MailFrom = strings.TrimSpace(settings.MailFrom)
	settings.OpenExchangeRatesId = strings.TrimSpace(settings.OpenExchangeRatesId)
	settings.DatabaseType = strings.ToLower(strings.TrimSpace(settings.DatabaseType))
	settings.DatabaseConnectionString = strings.TrimSpace(settings.DatabaseConnectionString)
}

func validateSettings(settings AppSettings) error {
	problems := make([]string, 0)

	if _, err := parsePort("PORT", settings.Port); err != nil {
		problems = append(problems, "PORT must be a number between 1 and 65535")
	}
	if settings.DatabaseType != "postgres" {
		problems = append(problems, "DATABASE_TYPE must be postgres")
	}
	if !validPostgresURL(settings.DatabaseConnectionString) {
		problems = append(problems, "DATABASE_URL must be a valid postgres:// or postgresql:// URL")
	}
	if !validApplicationURL(settings.Host) {
		problems = append(problems, "HOST must be an absolute http:// or https:// URL without a path")
	}
	if settings.MailHost == "" {
		problems = append(problems, "MAIL_HOST is required")
	}
	if settings.MailHostPort < 1 || settings.MailHostPort > 65535 {
		problems = append(problems, "MAIL_PORT must be a number between 1 and 65535")
	}
	if !validEmailAddress(settings.MailFrom) {
		problems = append(problems, "MAIL_FROM must be a valid email address")
	}
	if settings.OpenExchangeRatesId == "" {
		problems = append(problems, "EXCHANGE_ID is required")
	}

	if len(problems) != 0 {
		return fmt.Errorf("invalid configuration: %s", strings.Join(problems, "; "))
	}
	return nil
}

func parsePort(name, value string) (int, error) {
	port, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || port < 1 || port > 65535 {
		return 0, fmt.Errorf("invalid configuration: %s must be a number between 1 and 65535", name)
	}
	return port, nil
}

func validPostgresURL(value string) bool {
	parsed, err := url.Parse(value)
	if err != nil {
		return false
	}
	return (parsed.Scheme == "postgres" || parsed.Scheme == "postgresql") && parsed.Host != ""
}

func validApplicationURL(value string) bool {
	parsed, err := url.Parse(value)
	if err != nil {
		return false
	}
	return (parsed.Scheme == "http" || parsed.Scheme == "https") &&
		parsed.Host != "" && parsed.User == nil &&
		(parsed.Path == "" || parsed.Path == "/") && parsed.RawQuery == "" && parsed.Fragment == ""
}

func validEmailAddress(value string) bool {
	address, err := mail.ParseAddress(value)
	return err == nil && address.Address == value
}
