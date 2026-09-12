package routes

import (
	"crypto/tls"
	stdsql "database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"goexpenses/database"
	"goexpenses/util"

	"github.com/labstack/echo/v4"
	gomail "gopkg.in/gomail.v2"
)

var E *echo.Echo
var Auth echo.MiddlewareFunc

var errInvalidCredentials = errors.New("invalid credentials")

func init() {
	E = echo.New()
}

func MainRoute() {
	E.GET("/", func(c echo.Context) error {
		var data *util.Data
		data = c.Get("data").(*util.Data)
		data.Active = "home"
		l := c.QueryParam("lang")
		if l != "" {

			if data.Lang != l {
				data.Lang = l
				sql := fmt.Sprintf(`UPDATE sessions SET lang = %v WHERE uuid = %v`, util.SqlParam(1), util.SqlParam(2))
				_ = database.Db.MustExec(sql, l, data.CookieId)

				if data.Username != "" {
					sql := fmt.Sprintf(`UPDATE users SET lang = %v WHERE id = %v`, util.SqlParam(1), util.SqlParam(2))
					_ = database.Db.MustExec(sql, l, data.User.Id)
					data.User.Lang = l
				}
			}
		}
		return c.Render(http.StatusOK, "index", data)
	})
}

func authenticateUser(username, password string) (util.User, error) {
	user := util.User{}
	query := fmt.Sprintf(`SELECT id, name, username, email, password FROM users WHERE username = %v`, util.SqlParam(1))
	if err := database.Db.Get(&user, query, username); err != nil {
		if errors.Is(err, stdsql.ErrNoRows) {
			return util.User{}, errInvalidCredentials
		}
		return util.User{}, err
	}

	valid, needsRehash := util.VerifyPassword(user.Password, password)
	if !valid {
		return util.User{}, errInvalidCredentials
	}
	if !needsRehash {
		return user, nil
	}

	passwordHash, err := util.HashPassword(password)
	if err != nil {
		return util.User{}, err
	}
	query = fmt.Sprintf(`UPDATE users SET password = %v WHERE id = %v AND password = %v`, util.SqlParam(1), util.SqlParam(2), util.SqlParam(3))
	result, err := database.Db.Exec(query, passwordHash, user.Id, user.Password)
	if err != nil {
		return util.User{}, err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return util.User{}, err
	}
	if rowsAffected != 1 {
		return util.User{}, errInvalidCredentials
	}

	user.Password = passwordHash
	return user, nil
}

func rotateSession(c echo.Context, userID int) error {
	currentHash, ok := c.Get("_id").(string)
	if !ok || currentHash == "" {
		return errors.New("current session is missing")
	}

	token, tokenHash, err := util.NewSessionToken()
	if err != nil {
		return err
	}
	query := fmt.Sprintf(`UPDATE sessions SET uuid = %v, user_id = %v, created_at = %v WHERE uuid = %v`, util.SqlParam(1), util.SqlParam(2), util.SqlParam(3), util.SqlParam(4))
	result, err := database.Db.Exec(query, tokenHash, userID, time.Now(), currentHash)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected != 1 {
		return errors.New("current session no longer exists")
	}

	c.Set("_id", tokenHash)
	if data, ok := c.Get("data").(*util.Data); ok {
		data.CookieId = tokenHash
	}
	c.SetCookie(util.NewSessionCookie(token))
	return nil
}

func logout(c echo.Context) error {
	sessionHash, ok := c.Get("_id").(string)
	if !ok || sessionHash == "" {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}

	query := fmt.Sprintf(`DELETE FROM sessions WHERE uuid = %v`, util.SqlParam(1))
	if _, err := database.Db.Exec(query, sessionHash); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "could not end session").SetInternal(err)
	}
	c.SetCookie(util.ExpiredSessionCookie())
	return c.Redirect(http.StatusSeeOther, "/")
}

func selectAccount(c echo.Context) error {
	data, ok := c.Get("data").(*util.Data)
	if !ok || data.User.Id == 0 {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}

	accountID, err := strconv.Atoi(c.FormValue("accounts_id"))
	if err != nil || accountID <= 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid account")
	}

	query := fmt.Sprintf(`
		UPDATE users
		SET default_accounts_id = %v
		WHERE id = %v
		  AND EXISTS (
			SELECT 1
			FROM accountsusers au
			JOIN accounts a ON a.id = au.accounts_id
			WHERE au.accounts_id = %v
			  AND au.users_id = %v
			  AND a.deleted = 0
		)`, util.SqlParam(1), util.SqlParam(2), util.SqlParam(3), util.SqlParam(4))

	result, err := database.Db.Exec(query, accountID, data.User.Id, accountID, data.User.Id)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "could not select account").SetInternal(err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "could not verify account selection").SetInternal(err)
	}
	if rowsAffected != 1 {
		return echo.NewHTTPError(http.StatusForbidden, "account is not available to this user")
	}

	return c.Redirect(http.StatusSeeOther, "/posts")
}

func DefineRoutes() {

	// Middleware

	Auth = func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {

			uuid := c.Get("_id").(string)

			session := util.Session{}
			sql := fmt.Sprintf(`SELECT id, uuid, user_id FROM sessions WHERE uuid = %v`, util.SqlParam(1))
			database.Db.Get(&session, sql, uuid)

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

	e.POST("/logout", logout, auth)

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
			passwordHash, err := util.HashPassword(password)
			if err != nil {
				util.Flash(`Invalid password!`, data, 0, ``, 0)
				return c.Redirect(http.StatusSeeOther, "/register")
			}
			password = passwordHash
			tx, err := database.Db.Begin()
			fmt.Println(err)

			accountid := 0
			if database.DatabaseType == "sqlite" {
				sql := fmt.Sprintf(`INSERT INTO accounts (description) VALUES (%v)`, util.SqlParam(1))
				_, err = tx.Exec(sql, util.GetLangText(`My account`, data.Lang))
				row := tx.QueryRow("select last_insert_rowid()") // SQLite specific
				err = row.Scan(&accountid)
			} else {
				sql := fmt.Sprintf(`INSERT INTO accounts (description) VALUES (%v) RETURNING id`, util.SqlParam(1))
				row := tx.QueryRow(sql, util.GetLangText(`My account`, data.Lang))
				err = row.Scan(&accountid)
			}
			fmt.Println(err)

			userid := 0
			if database.DatabaseType == "sqlite" {
				sql := fmt.Sprintf(`INSERT INTO users (name, email, username, password, default_accounts_id, lang) VALUES (%v, %v, %v, %v, %v, %v)`, util.SqlParam(1), util.SqlParam(2), util.SqlParam(3), util.SqlParam(4), util.SqlParam(5), util.SqlParam(6))
				_, err = tx.Exec(sql, name, email, username, password, accountid, data.Lang)
				row := tx.QueryRow("select last_insert_rowid()") // SQLite specific
				err = row.Scan(&userid)
			} else {
				sql := fmt.Sprintf(`INSERT INTO users (name, email, username, password, default_accounts_id, lang) VALUES (%v, %v, %v, %v, %v, %v) RETURNING id`, util.SqlParam(1), util.SqlParam(2), util.SqlParam(3), util.SqlParam(4), util.SqlParam(5), util.SqlParam(6))
				row := tx.QueryRow(sql, name, email, username, password, accountid, data.Lang)
				err = row.Scan(&userid)
			}
			fmt.Println(err)

			sql := fmt.Sprintf(`INSERT INTO accountsusers (accounts_id, users_id) VALUES (%v, %v)`, util.SqlParam(1), util.SqlParam(2))
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
			passwordHash, err := util.HashPassword(password)
			if err != nil {
				util.Flash(`Invalid password!`, data, 0, ``, 0)
				return c.Redirect(http.StatusSeeOther, "/changepassword")
			}
			password = passwordHash
			userid := 0
			if token != "" && data.User.Id == 0 {

				var filterdate time.Time
				filterdate = time.Now().Add(-2 * time.Hour)

				pr := util.PasswordReset{}
				sql := fmt.Sprintf(`SELECT id, email, token, created_at FROM passwordresets WHERE token  = %v AND created_at >= %v AND done = 0`, util.SqlParam(1), util.SqlParam(2))
				database.Db.Get(&pr, sql, token, filterdate)
				if pr.Email != "" {
					user := util.User{}
					sql := fmt.Sprintf(`SELECT id FROM users WHERE email = %v`, util.SqlParam(1))
					database.Db.Get(&user, sql, pr.Email)
					if user.Id != 0 {
						userid = user.Id
					}
				}
			} else {
				userid = data.User.Id
			}
			if userid == 0 {
				util.Flash(`Invalid password!`, data, 0, ``, 0)
				return c.Redirect(http.StatusSeeOther, "/changepassword")
			}

			tx, err := database.Db.Begin()
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "could not change password").SetInternal(err)
			}
			defer tx.Rollback()

			sql := fmt.Sprintf(`UPDATE users SET password = %v WHERE id = %v`, util.SqlParam(1), util.SqlParam(2))
			if _, err = tx.Exec(sql, password, userid); err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "could not change password").SetInternal(err)
			}
			if token != "" {
				sql := fmt.Sprintf(`UPDATE passwordresets SET done = 1 WHERE token = %v`, util.SqlParam(1))
				if _, err = tx.Exec(sql, token); err != nil {
					return echo.NewHTTPError(http.StatusInternalServerError, "could not complete password reset").SetInternal(err)
				}
			}
			if err = tx.Commit(); err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "could not change password").SetInternal(err)
			}
			sessionUserID := 0
			if data.User.Id != 0 {
				sessionUserID = data.User.Id
			}
			if err := rotateSession(c, sessionUserID); err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "could not rotate session").SetInternal(err)
			}
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

		user, err := authenticateUser(username, password)
		if err == nil {
			if err := rotateSession(c, user.Id); err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "could not rotate session").SetInternal(err)
			}
			c.Set("id", user.Id)
			c.Set("name", user.Name)
			c.Set("username", user.Username)
			c.Set("email", user.Email)

		} else if errors.Is(err, errInvalidCredentials) {
			util.Flash(`Unknown user or invalid password!`, data, 0, "", 0)
			return c.Redirect(http.StatusSeeOther, "/login")
		} else {
			return echo.NewHTTPError(http.StatusInternalServerError, "could not authenticate user").SetInternal(err)
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
		sql := fmt.Sprintf(`SELECT id, email FROM users WHERE email = %v`, util.SqlParam(1))
		database.Db.Get(&user, sql, email)
		if user.Id == 0 {
			util.Flash(`Unknown E-Mail!`, data, 0, "", 0)
			return c.Redirect(http.StatusSeeOther, "/reset")
		}

		token := util.Encrypt(util.CreateUUID())
		sql = fmt.Sprintf(`INSERT INTO passwordresets (email, token) VALUES (%v,%v)`, util.SqlParam(1), util.SqlParam(2))
		_ = database.Db.MustExec(sql, user.Email, token)
		// _, errsql := sqlresult.LastInsertId()
		// if errsql != nil {
		// 	util.Flash(`Error when accesing to database!`, data, 0, "", 0)
		// 	return c.Redirect(http.StatusSeeOther, "/reset")
		// }

		m := gomail.NewMessage()
		m.SetHeader("From", util.Settings.MailFrom)
		m.SetHeader("To", user.Email)
		m.SetHeader("Subject", "Goexpenses "+util.GetLangText("reset password", data.Lang))
		m.SetBody("text/html", util.GetLangText(`Click to this link to reset password:`, data.Lang)+` <a href="`+util.Settings.Host+`/resetpassword?t=`+token+`">Reset</a>`)
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
		if token == "" {
			return c.Redirect(http.StatusSeeOther, "/")
		}

		var filterdate time.Time
		filterdate = time.Now().Add(-2 * time.Hour)
		fmt.Println("filterdate:", filterdate)
		pr := util.PasswordReset{}
		sql := fmt.Sprintf(`SELECT id, email, token, created_at FROM passwordresets WHERE token  = %v AND created_at >= %v AND done = 0`, util.SqlParam(1), util.SqlParam(2))
		database.Db.Get(&pr, sql, token, filterdate)
		if pr.Id == 0 {
			util.Flash(`Invalid token!`, data, 0, "", 0)
			return c.Redirect(http.StatusSeeOther, "/reset")
		}

		data.Token = token

		return c.Render(http.StatusOK, "changepassword", data)
	})

	e.POST("/accounts/select", selectAccount, auth)

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

		accountid := 0
		if database.DatabaseType == "sqlite" {
			sql := fmt.Sprintf(`INSERT INTO accounts (description) VALUES (%v)`, util.SqlParam(1))
			_, err = tx.Exec(sql, description)
			fmt.Println(err)
			row := tx.QueryRow("select last_insert_rowid()") // SQLite specific
			err = row.Scan(&accountid)
		} else {
			sql := fmt.Sprintf(`INSERT INTO accounts (description) VALUES (%v) RETURNING id`, util.SqlParam(1))
			row := tx.QueryRow(sql, description)
			err = row.Scan(&accountid)
		}
		fmt.Println(err)

		userid := data.User.Id
		sql := fmt.Sprintf(`INSERT INTO accountsusers (accounts_id, users_id) VALUES (%v, %v)`, util.SqlParam(1), util.SqlParam(2))
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
		sql := fmt.Sprintf(`SELECT id, description, p_id FROM incomes WHERE accounts_id = %v AND deleted = 0 ORDER BY description ASC`, util.SqlParam(1))
		database.Db.Select(&incomes, sql, data.User.Default_accounts_id)
		data.Incomes = incomes
		return c.Render(http.StatusOK, "incomes", data)
	}, auth)

	e.POST("/incomes/save", func(c echo.Context) error {
		data := c.Get("data").(*util.Data)

		description := c.FormValue("description")
		sql := fmt.Sprintf(`INSERT INTO incomes (description, accounts_id, p_id) VALUES (%v, %v, %v)`, util.SqlParam(1), util.SqlParam(2), util.SqlParam(3))
		database.Db.MustExec(sql, description, data.User.Default_accounts_id, util.Encrypt(util.CreateUUID()))

		return c.Redirect(http.StatusSeeOther, "/incomes")
	}, auth)

	e.POST("/incomes/update", func(c echo.Context) error {
		data := c.Get("data").(*util.Data)
		id := c.FormValue("id")
		description := c.FormValue("description")

		fmt.Println("incomes/update", id, description)
		sql := fmt.Sprintf(`UPDATE incomes SET description = %v WHERE p_id = %v AND accounts_id = %v AND deleted = 0`, util.SqlParam(1), util.SqlParam(2), util.SqlParam(3))
		database.Db.MustExec(sql, description, id, data.User.Default_accounts_id)

		return c.Redirect(http.StatusSeeOther, "/incomes")
	}, auth)

	e.POST("/incomes/delete", func(c echo.Context) error {
		data := c.Get("data").(*util.Data)
		id := c.FormValue("id")

		fmt.Println("incomes/delete", id)
		sql := fmt.Sprintf(`UPDATE incomes SET deleted = 1 WHERE p_id = %v AND accounts_id = %v`, util.SqlParam(1), util.SqlParam(2))
		database.Db.MustExec(sql, id, data.User.Default_accounts_id)

		return c.Redirect(http.StatusSeeOther, "/incomes")
	}, auth)

	e.GET("/incomes/show", func(c echo.Context) error {
		data := c.Get("data").(*util.Data)
		data.Active = "incomes"

		id := c.QueryParam("id")
		fmt.Println("incomes/show", id)
		incomes := []util.Income{}
		sql := fmt.Sprintf(`SELECT id, description, p_id FROM incomes WHERE p_id = %v AND accounts_id = %v AND deleted = 0`, util.SqlParam(1), util.SqlParam(2))
		database.Db.Select(&incomes, sql, id, data.User.Default_accounts_id)
		data.Incomes = incomes
		fmt.Println("incomes/show", data.Incomes)
		return c.Render(http.StatusOK, "incomesshow", data)
	}, auth)

}
