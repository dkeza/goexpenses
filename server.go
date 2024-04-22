package main

import (
	"fmt"
	"html/template"
	"io"
	"io/ioutil"

	"goexpenses/database"
	"goexpenses/midware"
	"goexpenses/routes"
	"goexpenses/util"

	"github.com/jasonlvhit/gocron"
	"github.com/labstack/echo/v4"
	_ "github.com/lib/pq"
)

type Template struct {
	templates *template.Template
}

func (t *Template) Render(w io.Writer, name string, data interface{}, c echo.Context) error {
	return t.templates.ExecuteTemplate(w, name, data)
}

func init() {
	fmt.Println("Starting...")
	util.ReadSettings()

	database.Connect()

	// Check if database exists
	session := util.Session{}
	err := database.Db.Get(&session, "SELECT id, uuid, user_id, lang, message FROM sessions WHERE 1=0")
	expectedErrorMsg := ""
	fmt.Println("Check if database exists", err)
	if util.Settings.DatabaseType == "sqlite" {
		expectedErrorMsg = "no such table: sessions"
	} else {
		expectedErrorMsg = `pq: relation "sessions" does not exist`
	}
	if err.Error() == expectedErrorMsg {
		fmt.Println("Create database")
		// Create database
		sqlScript := ""
		if util.Settings.DatabaseType == "sqlite" {
			sqlScript = "./db/structure.sql"
		} else {
			sqlScript = "./db/pg_structure.sql"
		}
		sql, err := ioutil.ReadFile(sqlScript)
		fmt.Println("After readfile", err)
		s := string(sql)
		fmt.Println("SQL SCRIPT:", s)
		r := database.Db.MustExec(s)
		fmt.Println("MustExec:", r)
	}

}

func main() {

	DatabaseUpdate()

	//gocron.Every(1).Minute().Do(util.GetExchangeRates)
	// Do it on every restart
	util.GetExchangeRates()
	util.DeleteOldSessions()
	gocron.Every(1).Day().At("07:00").Do(util.GetExchangeRates)
	gocron.Every(1).Day().At("05:00").Do(util.DeleteOldSessions)

	e := routes.E

	// Example how we can use some custom function in template
	funcMap := template.FuncMap{
		"FormatCurrency": func(c float64) string {
			return fmt.Sprintf("%.2f", c)
		},
		"GetLangText": util.GetLangText,
		"FormatDateTime": func(dt string) string {
			return dt[8:10] + "." + dt[5:7] + "." + dt[0:4] + " " + dt[11:19]
			//2019-03-05T00:00:00Z
		},
		"FormatDate": func(dt string) string {
			return dt[8:10] + "." + dt[5:7] + "." + dt[0:4]
		},
		"FormatVisibleId": func(vid string) string {
			x := ""
			if len(vid) > 0 {
				x = vid[len(vid)-10:]
			}
			return x
		},
		"ShowBuildVersion": func() string {
			return fmt.Sprint(util.Settings.Build)
		},
	}

	t := &Template{
		templates: template.Must(template.New("").Funcs(funcMap).ParseGlob("templates/*.html")),
	}

	e.Renderer = t

	midware.SetMiddleware()

	e.Static("/static", "static")
	e.File("/favicon.ico", "static/favicon.ico")
	e.File("/ads.txt", "static/ads.txt")

	routes.DefineRoutes()

	gocron.Start()

	e.Logger.Info("Listening on port " + util.Settings.Port)

	if err := e.Start(":" + util.Settings.Port); err != nil {
		e.Logger.Fatal(err.Error())
	}

}

func DatabaseUpdate() {
	paramTest := util.Param{}
	err := database.Db.Get(&paramTest, "SELECT id, build FROM params WHERE id=1")
	if err != nil {
		if err.Error() == "no such table: params" {
			fmt.Println("Must create params table")
			database.Db.MustExec(`
				CREATE TABLE params (
				    build INTEGER NOT NULL
				                  DEFAULT (0),
				    id    INTEGER PRIMARY KEY
				                  NOT NULL
				);
			`)
		}
	}

	param := util.Param{}
	database.Db.Get(&param, "SELECT id, build FROM params WHERE id=1")

	if param.Id != 1 {
		database.Db.MustExec(fmt.Sprintf(`INSERT INTO params (id, build) VALUES (%v, %v)`, util.SqlParam(1), util.SqlParam(2)), 1, 0)
	}

	// BEGIN Here add build revision specific changes for database

	if param.Build < 1 {
		fmt.Println("Must update public id in post table")
		posts := []util.Post{}
		database.Db.Select(&posts, `
			SELECT id, p_id 
				FROM posts 
				WHERE p_id = '' 
				`)
		for _, post := range posts {
			fmt.Println("Updating post.p_id for id ", post.Id)
			if post.Pid == "" {
				sql := fmt.Sprintf(`UPDATE posts SET p_id = %v WHERE id = %v`, util.SqlParam(1), util.SqlParam(2))
				database.Db.MustExec(sql, util.Encrypt(util.CreateUUID()), post.Id)
			}
		}
		fmt.Println("Must update public id in expenses table")
		expenses := []util.Expense{}
		database.Db.Select(&expenses, `
			SELECT id, p_id 
				FROM expenses 
				WHERE p_id = '' 
				`)
		for _, record := range expenses {
			fmt.Println("Updating expenses.p_id for id ", record.Id)
			if record.Pid == "" {
				sql := fmt.Sprintf(`UPDATE expenses SET p_id = %v WHERE id = %v`, util.SqlParam(1), util.SqlParam(2))
				database.Db.MustExec(sql, util.Encrypt(util.CreateUUID()), record.Id)
			}
		}
		fmt.Println("Must update public id in incomes table")
		incomes := []util.Income{}
		database.Db.Select(&incomes, `
			SELECT id, p_id 
				FROM incomes 
				WHERE p_id = '' 
				`)
		for _, record := range incomes {
			fmt.Println("Updating incomes.p_id for id ", record.Id)
			if record.Pid == "" {
				sql := fmt.Sprintf(`UPDATE incomes SET p_id = %v WHERE id = %v`, util.SqlParam(1), util.SqlParam(2))
				database.Db.MustExec(sql, util.Encrypt(util.CreateUUID()), record.Id)
			}
		}
	}

	if param.Build < 8 {
		fmt.Println("Add created_at to sessions table")
		database.Db.MustExec(`DELETE FROM sessions`)
		database.Db.MustExec(`ALTER TABLE sessions ADD COLUMN created_at timestamp NOT NULL DEFAULT NOW()`)
	}

	if param.Build < 11 {
		fmt.Println("Add created_ts to posts table")
		database.Db.MustExec(`ALTER TABLE posts ADD COLUMN created_ts timestamp NOT NULL DEFAULT NOW()`)
		database.Db.MustExec(`UPDATE posts SET created_ts = created_at;`)
	}

	if param.Build != util.Settings.Build {
		// Update to leatest database version
		fmt.Println("Update build version to ", util.Settings.Build)
		sql := fmt.Sprintf(`UPDATE params SET build = %v`, util.SqlParam(1))
		database.Db.MustExec(sql, util.Settings.Build)
	}
}
