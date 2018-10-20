package util

import (
	"fmt"
	"os"
	"strconv"

	"github.com/dkeza/goexpenses/database"
	"github.com/vaughan0/go-ini"
)

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
	DatabaseType             string
	DatabaseConnectionString string
}

func ReadSettings() {

	// Here set build version for every release
	Settings.Build = 1

	Settings.Host = ""
	Settings.Port = ""
	Settings.MailFrom = ""
	Settings.MailHost = ""
	Settings.MailHostPort = 0
	Settings.MailPassword = ""
	Settings.OpenExchangeRatesId = ""
	Settings.DatabaseType = ""
	Settings.DatabaseConnectionString = ""

	file, errf := ini.LoadFile("goexpenses.ini")
	if errf == nil {
		value, ok := file.Get("settings", "host")
		if ok {
			Settings.Host = value
		}
		value, ok = file.Get("settings", "port")
		if ok {
			Settings.Port = value
		}
		value, ok = file.Get("settings", "mailhost")
		if ok {
			Settings.MailHost = value
		}
		value, ok = file.Get("settings", "mailhostport")
		if ok {
			Settings.MailHostPort, _ = strconv.Atoi(value)
		}
		value, ok = file.Get("settings", "mailfrom")
		if ok {
			Settings.MailFrom = value
		}
		value, ok = file.Get("settings", "mailpassword")
		if ok {
			Settings.MailPassword = value
		}
		value, ok = file.Get("settings", "openexchangeratesid")
		if ok {
			Settings.OpenExchangeRatesId = value
		}
		value, ok = file.Get("settings", "databasetype")
		if ok {
			Settings.DatabaseType = value
		}
		value, ok = file.Get("settings", "DATABASE_URL")
		if ok {
			Settings.DatabaseConnectionString = value
		}
	} else {
		Settings.Host = os.Getenv("HOST")
		Settings.Port = os.Getenv("PORT")
		Settings.MailFrom = os.Getenv("MAIL_FROM")
		Settings.MailHost = os.Getenv("MAIL_HOST")
		Settings.MailHostPort, _ = strconv.Atoi(os.Getenv("MAIL_PORT"))
		Settings.MailPassword = os.Getenv("MAIL_PASSWORD")
		Settings.OpenExchangeRatesId = os.Getenv("EXCHANGE_ID")
		Settings.DatabaseType = os.Getenv("DATABASE_TYPE")
		Settings.DatabaseConnectionString = os.Getenv("DATABASE_URL")
	}

	if len(Settings.DatabaseType) == 0 {
		Settings.DatabaseType = "sqlite"
	}

	if len(Settings.DatabaseConnectionString) == 0 {
		Settings.DatabaseConnectionString = "./db/database.db"
	}
	fmt.Printf("Settings: %v", Settings)
	database.DatabaseType = Settings.DatabaseType
	database.DatabaseConnectionString = Settings.DatabaseConnectionString

}
