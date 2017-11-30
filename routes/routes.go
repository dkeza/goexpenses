package routes

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/dkeza/goexpenses/database"
	"github.com/dkeza/goexpenses/util"

	"github.com/labstack/echo"
	"gopkg.in/gomail.v2"
)

var E *echo.Echo
var Auth echo.MiddlewareFunc

func init() {
	E = echo.New()
}

func MainRoute() {
	E.GET("/", func(c echo.Context) error {
		var data *util.Data
		data = c.Get("data").(*util.Data)
		data.Active = "home"
		fmt.Println(data)
		l := c.QueryParam("lang")
		if l != "" {

			if data.Lang != l {
				data.Lang = l
				sql := `UPDATE sessions SET lang = ? WHERE uuid = ?`
				_ = database.Db.MustExec(sql, l, data.CookieId)

				if data.Username != "" {
					sql := `UPDATE users SET lang = ? WHERE id = ?`
					_ = database.Db.MustExec(sql, l, data.User.Id)
					data.User.Lang = l
				}
			}
		}
		return c.Render(http.StatusOK, "index", data)
	})
}

func DefineRoutes() {

	// Middleware

	Auth = func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {

			uuid := c.Get("_id").(string)

			session := util.Session{}
			database.Db.Get(&session, "SELECT id, uuid, user_id FROM sessions WHERE uuid = ?", uuid)

			if session.User_id == 0 {
				return c.Redirect(http.StatusSeeOther, "/login")
			}

			return next(c)
		}
	}

	auth := Auth

	MainRoute()

	DefinePosts()

	DefineExpenses()

	e := E

	e.GET("/login", func(c echo.Context) error {
		//x := c.Get("csrf")
		//fmt.Println(x)
		data := c.Get("data").(*util.Data)
		data.Active = "login"
		return c.Render(http.StatusOK, "login", data)
	})

	e.GET("/logout", func(c echo.Context) error {
		uuid := c.Get("_id").(string)
		sql := `DELETE FROM sessions WHERE uuid = ?`
		err := database.Db.MustExec(sql, uuid)
		fmt.Println("logout", err)
		return c.Redirect(http.StatusSeeOther, "/")
	}, auth)

	e.GET("/register", func(c echo.Context) error {
		data := c.Get("data").(*util.Data)
		data.Active = "login"

		return c.Render(http.StatusOK, "register", data)
	})

	e.POST("/register", func(c echo.Context) error {
		data := c.Get("data").(*util.Data)
		name := c.FormValue("name")
		email := c.FormValue("email")
		username := c.FormValue("username")
		password := c.FormValue("password")

		if name != "" && password != "" {
			password = util.Encrypt(password)
			tx, err := database.Db.Begin()
			fmt.Println(err)

			sql := `INSERT INTO accounts (description) VALUES (?)`
			_, err = tx.Exec(sql, util.GetLangText(`My account`, data.Lang))
			fmt.Println(err)

			accountid := 0

			row := tx.QueryRow("select last_insert_rowid()") // SQLite specific
			err = row.Scan(&accountid)

			sql = `INSERT INTO users (name, email, username, password, default_accounts_id, lang) VALUES (?, ?, ?, ?, ?, ?)`
			_, err = tx.Exec(sql, name, email, username, password, accountid, data.Lang)
			fmt.Println(err)

			userid := 0

			row = tx.QueryRow("select last_insert_rowid()") // SQLite specific
			err = row.Scan(&userid)

			sql = `INSERT INTO accountsusers (accounts_id, users_id) VALUES (?, ?)`
			_, err = tx.Exec(sql, accountid, userid)
			fmt.Println(err)

			err = tx.Commit()
			fmt.Println(err)
		}
		return c.Redirect(http.StatusSeeOther, "/login")
	})

	e.GET("/changepassword", func(c echo.Context) error {
		data := c.Get("data").(*util.Data)
		data.Active = "login"

		return c.Render(http.StatusOK, "changepassword", data)
	})

	e.POST("/changepassword", func(c echo.Context) error {
		data := c.Get("data").(*util.Data)
		password := c.FormValue("password")
		repeatpassword := c.FormValue("repeatpassword")
		token := c.FormValue("_token")

		if password != "" && repeatpassword != "" && password == repeatpassword {
			password = util.Encrypt(password)
			userid := 0
			if token != "" && data.User.Id == 0 {

				var filterdate time.Time
				filterdate = time.Now().Add(-2 * time.Hour)

				pr := util.PasswordReset{}
				database.Db.Get(&pr, "SELECT id, email, token, created_at FROM passwordresets WHERE token  = ? AND created_at >= ? AND done = 0", token, filterdate)
				if pr.Email != "" {
					user := util.User{}
					database.Db.Get(&user, "SELECT id, name, username, email, password, default_accounts_id, lang FROM users WHERE email = ?", pr.Email)
					if user.Id != 0 {
						userid = user.Id
					}
				}
			} else {
				userid = data.User.Id
			}

			tx, err := database.Db.Begin()
			fmt.Println(err)
			sql := `UPDATE users SET password = ? WHERE id = ?`
			_, err = tx.Exec(sql, password, userid)
			fmt.Println(err)
			if token != "" {
				_, err = tx.Exec(`UPDATE passwordresets SET done = 1 WHERE token = ?`, token)
				fmt.Println(err)
			}
			err = tx.Commit()
			fmt.Println(err)
			util.Flash(`Saved`, data, 1, ``, 0)
		} else {
			util.Flash(`Invalid password!`, data, 0, ``, 0)
			return c.Redirect(http.StatusSeeOther, "/changepassword")
		}
		return c.Redirect(http.StatusSeeOther, "/posts")
	})

	e.POST("/auth", func(c echo.Context) error {
		data := c.Get("data").(*util.Data)

		username := c.FormValue("username")
		password := c.FormValue("password")
		cpassword := util.Encrypt(password)

		uuid := c.Get("_id").(string)

		session := util.Session{}
		database.Db.Get(&session, "SELECT id, uuid, user_id FROM sessions WHERE uuid = ?", uuid)

		user := util.User{}
		database.Db.Get(&user, "SELECT id, name, username, email, password FROM users WHERE username = ? AND password = ?", username, cpassword)

		fmt.Println("Entered password: " + cpassword + " | Stored password: " + user.Password)
		if user.Username == username && user.Password == cpassword {

			fmt.Println("User OK")

			sql := `UPDATE sessions SET user_id = ? WHERE uuid = ?`
			err := database.Db.MustExec(sql, user.Id, uuid)

			if err == nil {
				c.Set("id", user.Id)
				c.Set("name", user.Name)
				c.Set("username", user.Username)
				c.Set("email", user.Email)
			} else {
				fmt.Println(err)
			}

		} else {
			util.Flash(`Unknown user or invalid password!`, data, 0, "", 0)
			return c.Redirect(http.StatusSeeOther, "/login")
		}

		return c.Redirect(http.StatusSeeOther, "/posts")
	})

	e.GET("/reset", func(c echo.Context) error {
		data := c.Get("data").(*util.Data)
		data.Active = "login"

		return c.Render(http.StatusOK, "reset", data)
	})

	e.POST("/reset", func(c echo.Context) error {
		data := c.Get("data").(*util.Data)
		email := c.FormValue("email")
		if email == "" {
			util.Flash(`Not allowed to reset password!`, data, 0, "", 0)
			return c.Redirect(http.StatusSeeOther, "/reset")
		}

		user := util.User{}
		database.Db.Get(&user, "SELECT id, name, username, email, password FROM users WHERE email  = ?", email)
		if user.Id == 0 {
			util.Flash(`Unknown E-Mail!`, data, 0, "", 0)
			return c.Redirect(http.StatusSeeOther, "/reset")
		}

		token := util.Encrypt(util.CreateUUID())
		sqlresult := database.Db.MustExec(`INSERT INTO passwordresets (email, token) VALUES (?,?)`, user.Email, token)
		_, errsql := sqlresult.LastInsertId()
		if errsql != nil {
			util.Flash(`Error when accesing to database!`, data, 0, "", 0)
			return c.Redirect(http.StatusSeeOther, "/reset")
		}

		m := gomail.NewMessage()
		m.SetHeader("From", util.Settings.MailFrom)
		m.SetHeader("To", user.Email)
		m.SetHeader("Subject", "Goexpenses "+util.GetLangText("reset password", data.Lang))
		m.SetBody("text/html", util.GetLangText(`Click to this link to reset password:`, data.Lang)+` <a href="http://`+util.Settings.Host+`/resetpassword?t=`+token+`">Reset</a>`)
		d := gomail.NewDialer(util.Settings.MailHost, util.Settings.MailHostPort, util.Settings.MailFrom, util.Settings.MailPassword)
		d.TLSConfig = &tls.Config{InsecureSkipVerify: true}

		if errm := d.DialAndSend(m); errm != nil {
			fmt.Println(errm)
			util.Flash(`E-Mail not sent!`, data, 1, "", 0)
		} else {
			util.Flash(`E-Mail sent!`, data, 1, "", 0)
		}

		return c.Redirect(http.StatusSeeOther, "/login")
	})

	e.GET("/resetpassword", func(c echo.Context) error {
		data := c.Get("data").(*util.Data)
		data.Active = "login"
		token := c.FormValue("t")
		fmt.Println("token:", token)
		if token == "" {
			return c.Redirect(http.StatusSeeOther, "/")
		}

		var filterdate time.Time
		filterdate = time.Now().Add(-2 * time.Hour)
		fmt.Println("filterdate:", filterdate)
		pr := util.PasswordReset{}
		database.Db.Get(&pr, "SELECT id, email, token, created_at FROM passwordresets WHERE token  = ? AND created_at >= ? AND done = 0", token, filterdate)
		if pr.Id == 0 {
			util.Flash(`Invalid token!`, data, 0, "", 0)
			return c.Redirect(http.StatusSeeOther, "/reset")
		}

		data.Token = token

		return c.Render(http.StatusOK, "changepassword", data)
	})

	e.GET("/accounts", func(c echo.Context) error {
		accounts_id, err := strconv.Atoi(c.QueryParam("accounts_id"))
		if err != nil {
			return c.Redirect(http.StatusSeeOther, "/login")
		}

		data := c.Get("data").(*util.Data)

		if !(data.CookieId != "" && data.Username != "") {
			return c.Redirect(http.StatusSeeOther, "/login")
		}

		if data.User.Default_accounts_id == accounts_id {
			return c.Redirect(http.StatusSeeOther, "/posts")
		}

		sql := `UPDATE users SET default_accounts_id = ? WHERE id = ?`
		err1 := database.Db.MustExec(sql, accounts_id, data.User.Id)
		fmt.Println(err1)
		return c.Redirect(http.StatusSeeOther, "/posts")
	})

	e.POST("/accounts/show", func(c echo.Context) error {
		data := c.Get("data").(*util.Data)
		data.Active = ""
		return c.Render(http.StatusOK, "accountsshow", data)
	}, auth)

	e.GET("/accounts/show", func(c echo.Context) error {
		data := c.Get("data").(*util.Data)
		data.Active = ""
		return c.Render(http.StatusOK, "accountsshow", data)
	}, auth)

	e.POST("/accounts/save", func(c echo.Context) error {
		data := c.Get("data").(*util.Data)

		description := c.FormValue("description")

		if description == "" {
			util.Flash(`Invalid description!`, data, 0, ``, 0)
			return c.Redirect(http.StatusSeeOther, "/accounts/show")
		}

		tx, err := database.Db.Begin()
		fmt.Println(err)

		sql := `INSERT INTO accounts (description) VALUES (?)`
		_, err = tx.Exec(sql, description)
		fmt.Println(err)

		accountid := 0

		row := tx.QueryRow("select last_insert_rowid()") // SQLite specific
		err = row.Scan(&accountid)

		userid := data.User.Id

		sql = `INSERT INTO accountsusers (accounts_id, users_id) VALUES (?, ?)`
		_, err = tx.Exec(sql, accountid, userid)
		fmt.Println(err)

		err = tx.Commit()
		fmt.Println(err)

		return c.Redirect(http.StatusSeeOther, "/posts")
	}, auth)

	e.GET("/incomes", func(c echo.Context) error {
		data := c.Get("data").(*util.Data)
		data.Active = "incomes"

		incomes := []util.Income{}
		database.Db.Select(&incomes, "SELECT id, description, p_id FROM incomes WHERE accounts_id = ? AND deleted = 0 ORDER BY description ASC", data.User.Default_accounts_id)
		data.Incomes = incomes
		return c.Render(http.StatusOK, "incomes", data)
	}, auth)

	e.POST("/incomes/save", func(c echo.Context) error {
		data := c.Get("data").(*util.Data)

		description := c.FormValue("description")

		sql := `INSERT INTO incomes (description, accounts_id, p_id) VALUES (?, ?, ?)`
		database.Db.MustExec(sql, description, data.User.Default_accounts_id, util.Encrypt(util.CreateUUID()))

		return c.Redirect(http.StatusSeeOther, "/incomes")
	}, auth)

	e.POST("/incomes/update", func(c echo.Context) error {
		data := c.Get("data").(*util.Data)
		id := c.FormValue("id")
		description := c.FormValue("description")

		fmt.Println("incomes/update", id, description)

		sql := `UPDATE incomes SET description = ? WHERE p_id = ? AND accounts_id = ? AND deleted = 0`
		database.Db.MustExec(sql, description, id, data.User.Default_accounts_id)

		return c.Redirect(http.StatusSeeOther, "/incomes")
	}, auth)

	e.POST("/incomes/delete", func(c echo.Context) error {
		data := c.Get("data").(*util.Data)
		id := c.FormValue("id")

		fmt.Println("incomes/delete", id)

		sql := `UPDATE incomes SET deleted = 1 WHERE p_id = ? AND accounts_id = ?`
		database.Db.MustExec(sql, id, data.User.Default_accounts_id)

		return c.Redirect(http.StatusSeeOther, "/incomes")
	}, auth)

	e.GET("/incomes/show", func(c echo.Context) error {
		data := c.Get("data").(*util.Data)
		data.Active = "incomes"

		id := c.QueryParam("id")
		fmt.Println("incomes/show", id)
		incomes := []util.Income{}
		database.Db.Select(&incomes, "SELECT id, description, p_id FROM incomes WHERE p_id = ? AND accounts_id = ? AND deleted = 0", id, data.User.Default_accounts_id)
		data.Incomes = incomes
		fmt.Println("incomes/show", data.Incomes)
		return c.Render(http.StatusOK, "incomesshow", data)
	}, auth)

}
