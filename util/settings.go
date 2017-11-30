package util

import (
	"strconv"

	"github.com/vaughan0/go-ini"
)

var Settings AppSettings

// Application settings
type AppSettings struct {
	Build               int
	Host                string
	Port                string
	MailHost            string
	MailHostPort        int
	MailFrom            string
	MailPassword        string
	OpenExchangeRatesId string
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
	}
}
