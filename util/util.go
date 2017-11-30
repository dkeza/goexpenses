package util

import (
	"goexpenses/database"
)

func Flash(message string, data *Data, success int, description string, expense_id int) {
	sql := `UPDATE sessions SET message = ?, message_success = ?, last_post_description = ?, expenses_id = ? WHERE uuid = ?`
	_ = database.Db.MustExec(sql, GetLangText(message, data.Lang), success, description, expense_id, data.CookieId)
}
