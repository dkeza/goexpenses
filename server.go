package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"goexpenses/database"
	"goexpenses/midware"
	"goexpenses/migrations"
	"goexpenses/routes"
	"goexpenses/util"

	"github.com/labstack/echo/v4"
	_ "github.com/lib/pq"
)

// embeddedFiles contains every read-only file required at runtime.
//
//go:embed templates/*.html static db/*.sql
var embeddedFiles embed.FS

const gracefulShutdownTimeout = 10 * time.Second

type applicationServer interface {
	Start(address string) error
	Shutdown(context.Context) error
}

type Template struct {
	templates *template.Template
}

func templateFunctions() template.FuncMap {
	return template.FuncMap{
		"FormatCurrency": func(c float64) string {
			return fmt.Sprintf("%.2f", c)
		},
		"GetLangText": util.GetLangText,
		"FormatDateTime": func(dt string) string {
			return dt[8:10] + "." + dt[5:7] + "." + dt[0:4] + " " + dt[11:19]
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
}

func parseTemplates() (*template.Template, error) {
	return template.New("").Funcs(templateFunctions()).ParseFS(embeddedFiles, "templates/*.html")
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

func serveUntilShutdown(ctx context.Context, server applicationServer, address string) error {
	serverErrors := make(chan error, 1)
	go func() {
		serverErrors <- server.Start(address)
	}()

	select {
	case err := <-serverErrors:
		if err == nil || errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("start HTTP server: %w", err)
	case <-ctx.Done():
		shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), gracefulShutdownTimeout)
		defer cancelShutdown()
		if err := server.Shutdown(shutdownContext); err != nil {
			return fmt.Errorf("shut down HTTP server: %w", err)
		}

		err := <-serverErrors
		if err == nil || errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("stop HTTP server: %w", err)
	}
}

func runApplication(ctx context.Context) error {
	if err := initializeApplication(); err != nil {
		return err
	}
	defer database.Db.Close()

	stopRateRefreshWorker, err := util.StartExchangeRateRefreshWorker(ctx)
	if err != nil {
		return err
	}
	defer stopRateRefreshWorker()

	jobLocation, err := time.LoadLocation(backgroundJobsTimezone)
	if err != nil {
		return fmt.Errorf("load background job timezone: %w", err)
	}
	jobScheduler := startBackgroundScheduler(ctx, jobLocation, []backgroundJob{
		{name: "delete old sessions", hour: 5, run: util.DeleteOldSessions},
		{name: "refresh exchange rates", hour: 7, run: util.RefreshExchangeRates},
	})
	defer jobScheduler.Stop()

	e := routes.E

	t := &Template{
		templates: template.Must(parseTemplates()),
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

	e.Logger.Info("Listening on port " + util.Settings.Port)
	if err := serveUntilShutdown(ctx, e, ":"+util.Settings.Port); err != nil {
		return err
	}
	if ctx.Err() != nil {
		e.Logger.Info("Shutdown complete")
	}
	return nil
}

func main() {
	fmt.Println("Starting...")
	ctx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := runApplication(ctx)
	stopSignals()
	if err != nil {
		log.Fatalf("application stopped: %v", err)
	}
}
