package main

import (
	"context"
	"embed"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log"
	"time"

	"goexpenses/database"
	"goexpenses/midware"
	"goexpenses/migrations"
	"goexpenses/routes"
	"goexpenses/util"

	"github.com/jasonlvhit/gocron"
	"github.com/labstack/echo/v4"
	_ "github.com/lib/pq"
)

// embeddedFiles contains every read-only file required at runtime.
//
//go:embed templates/*.html static db/*.sql
var embeddedFiles embed.FS

type Template struct {
	templates *template.Template
}

func (t *Template) Render(w io.Writer, name string, data interface{}, c echo.Context) error {
	return t.templates.ExecuteTemplate(w, name, data)
}

func initializeApplication() error {
	if err := util.ReadSettings(); err != nil {
		return err
	}

	connectionContext, cancelConnection := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelConnection()
	if err := database.Connect(connectionContext); err != nil {
		return err
	}

	initialSchema, err := embeddedFiles.ReadFile("db/pg_structure.sql")
	if err != nil {
		database.Db.Close()
		return fmt.Errorf("read embedded database schema: %w", err)
	}
	migrationContext, cancelMigrations := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancelMigrations()
	if err := migrations.Apply(migrationContext, database.Db, initialSchema, util.Settings.Build); err != nil {
		database.Db.Close()
		return err
	}
	return nil
}

func main() {
	fmt.Println("Starting...")
	if err := initializeApplication(); err != nil {
		log.Fatalf("cannot start: %v", err)
	}
	defer database.Db.Close()

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
		templates: template.Must(template.New("").Funcs(funcMap).ParseFS(embeddedFiles, "templates/*.html")),
	}

	e.Renderer = t

	midware.SetMiddleware()

	staticFiles, err := fs.Sub(embeddedFiles, "static")
	if err != nil {
		panic(fmt.Errorf("open embedded static files: %w", err))
	}
	e.StaticFS("/static", staticFiles)
	e.FileFS("/favicon.ico", "favicon.ico", staticFiles)
	e.FileFS("/ads.txt", "ads.txt", staticFiles)

	routes.DefineRoutes()

	gocron.Start()

	e.Logger.Info("Listening on port " + util.Settings.Port)

	if err := e.Start(":" + util.Settings.Port); err != nil {
		e.Logger.Fatal(err.Error())
	}

}
