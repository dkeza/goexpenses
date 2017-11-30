package midware

import (
	"fmt"
	"goexpenses/database"
	"goexpenses/routes"
	"goexpenses/util"
	"net/http"
	"time"

	"github.com/labstack/echo"
	"github.com/labstack/echo/middleware"
)

func SetMiddleware() {

	e := routes.E

	e.Use(middleware.CSRFWithConfig(middleware.CSRFConfig{
		TokenLookup:  "form:_CSRF",
		CookieMaxAge: 86400 * 15,
	}))
	//e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	e.Use(ServerHeader)
	e.Use(CheckCookie)
}

// ServerHeader middleware adds a `Server` header to the response.
func ServerHeader(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		c.Response().Header().Set(echo.HeaderServer, "Keza Server 1.0")
		return next(c)
	}
}

func CheckCookie(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		data := new(util.Data)

		cookie, err := c.Cookie("_id")
		if err != nil {
			cookie = new(http.Cookie)
			cookie.Name = "_id"
			cookie.Value, _ = util.EncryptString(util.Encrypt(util.CreateUUID()))
			cookie.Expires = time.Now().Add(10 * 365 * 24 * time.Hour)
			c.SetCookie(cookie)
		}
		uuid, _ := util.DecryptString(cookie.Value)

		session := util.Session{}
		database.Db.Get(&session, "SELECT id, uuid, user_id, lang, message, expenses_id, last_post_description, message_success FROM sessions WHERE uuid = ?", uuid)

		if session.Id == 0 {
			fmt.Println("No session in table!")
			sql := `INSERT INTO sessions (uuid) VALUES (?)`
			err := database.Db.MustExec(sql, uuid)
			fmt.Println(err)
			data.Lang = "EN"
		} else {
			if session.Message != "" {
				data.Flash = session.Message
				sql := `UPDATE sessions SET message = ? WHERE uuid = ?`
				_ = database.Db.MustExec(sql, "", uuid)
			}
			if session.Expenses_id != 0 {

				expenses := []util.Expense{}

				database.Db.Select(&expenses, `
					SELECT id, p_id, description 
						FROM expenses 
						WHERE id = ? 
						ORDER BY description ASC
						`, session.Expenses_id)
				fmt.Println("expenses:", expenses)
				if expenses[0].Pid != "" {
					data.Expenses_id = expenses[0].Pid
				}
				sql := `UPDATE sessions SET expenses_id = ? WHERE uuid = ?`
				_ = database.Db.MustExec(sql, 0, uuid)
			}
			if session.Last_post_description != "" {
				data.Last_post_description = session.Last_post_description
				sql := `UPDATE sessions SET last_post_description = ? WHERE uuid = ?`
				_ = database.Db.MustExec(sql, "", uuid)
			}
			if session.Message_success != 0 {
				data.Message_success = session.Message_success
				sql := `UPDATE sessions SET message_success = ? WHERE uuid = ?`
				_ = database.Db.MustExec(sql, 0, uuid)
			}
		}

		c.Set("_id", uuid)
		data.CookieId = uuid
		if session.User_id > 0 {
			user := util.User{}
			database.Db.Get(&user, "SELECT id, name, username, email, password, default_accounts_id, lang FROM users WHERE id = ?", session.User_id)

			c.Set("id", user.Id)
			c.Set("name", user.Name)
			c.Set("username", user.Username)
			c.Set("email", user.Email)

			data.Username = user.Name
			data.User.Id = user.Id
			data.User.Name = user.Name
			data.User.Email = user.Email
			data.User.Username = user.Username
			data.User.Password = user.Password
			data.User.Default_accounts_id = user.Default_accounts_id
			data.User.Lang = user.Lang
			data.Lang = user.Lang
			accounts := []util.Account{}
			database.Db.Select(&accounts, "SELECT a.id, a.description FROM accountsusers au INNER JOIN accounts a ON au.accounts_id = a.id WHERE au.users_id = ? ORDER BY description ASC", data.User.Id)
			data.Accounts = accounts
		} else {

			c.Set("id", 0)
			c.Set("name", "")
			c.Set("username", "")
			c.Set("email", "")
			data.Lang = session.Lang
		}

		data.Csrf = c.Get("csrf").(string)

		currency := util.Currency{}
		database.Db.Get(&currency, "SELECT id, code, rate, date FROM currencies WHERE code = ?", `EUR`)
		data.Eur = util.ToFixed(currency.Rate, 4)
		data.Eurdate = currency.Date

		if data.Eur == 0.00 {
			data.Eur, data.Eurdate = util.GetExchangeRates()
		}
		c.Set("data", data)

		return next(c)
	}
}
