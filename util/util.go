package util

import (
	"fmt"
	"strconv"
	"time"

	"goexpenses/database"
)

func Flash(message string, data *Data, success int, description string, expense_id int) {
	sql := fmt.Sprintf(`UPDATE sessions SET message = %v, message_success = %v, last_post_description = %v, expenses_id = %v WHERE uuid = %v`, SqlParam(1), SqlParam(2), SqlParam(3), SqlParam(4), SqlParam(5))
	_ = database.Db.MustExec(sql, GetLangText(message, data.Lang), success, description, expense_id, data.CookieId)
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
	sessions := []Session{}
	sql := fmt.Sprintf(`SELECT id FROM sessions WHERE created_at < %v`, SqlParam(1))
	database.Db.Select(&sessions, sql, oneMonthBefore)
	fmt.Printf("Deleting session record older then %v \n", oneMonthBefore)
	for _, one := range sessions {
		sql := fmt.Sprintf(`DELETE FROM sessions WHERE id = %v`, SqlParam(1))
		_ = database.Db.MustExec(sql, one.Id)
		fmt.Printf("Deleting session record with id %v \n", one.Id)
	}
}
