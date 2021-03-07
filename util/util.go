package util

import (
	"fmt"
	"strconv"

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
