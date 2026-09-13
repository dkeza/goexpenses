package util

import (
	"fmt"
	"log"
	"strconv"
	"time"

	"goexpenses/database"
)

func Flash(message string, data *Data, success int, description string, expense_id int) {
	sql := fmt.Sprintf(`UPDATE sessions SET message = %v, message_success = %v, last_post_description = %v, expenses_id = %v WHERE uuid = %v`, SqlParam(1), SqlParam(2), SqlParam(3), SqlParam(4), SqlParam(5))
	if _, err := database.Db.Exec(sql, GetLangText(message, data.Lang), success, description, expense_id, data.CookieId); err != nil {
		log.Printf("Could not store flash message: %v", err)
	}
}

func SqlParam(param int) string {
	if Settings.DatabaseType == "sqlite" {
		return "?"
	} else {
		return "$" + strconv.Itoa(param)
	}
}

func DeleteOldSessions() {
	currentTime := time.Now()
	oneMonthBefore := currentTime.AddDate(0, -1, 0)
	sql := fmt.Sprintf(`DELETE FROM sessions WHERE created_at < %v`, SqlParam(1))
	if _, err := database.Db.Exec(sql, oneMonthBefore); err != nil {
		log.Printf("Could not delete expired sessions: %v", err)
	}
}
