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
	"goexpenses/version"

	"github.com/labstack/echo/v4"
	_ "github.com/lib/pq"
)

// embeddedFiles contains every read-only file required at runtime.
//
//go:embed templates/*.html static db/*.sql
var embeddedFiles embed.FS

const (
	gracefulShutdownTimeout = 10 * time.Second
	readHeaderTimeout       = 5 * time.Second
	readTimeout             = 15 * time.Second
	writeTimeout            = 30 * time.Second
	idleTimeout             = 2 * time.Minute
)

// configureHTTPServer bounds how long a client may hold a connection so slow
// or idle clients cannot exhaust server resources.
func configureHTTPServer(server *http.Server) {
	server.ReadHeaderTimeout = readHeaderTimeout
	server.ReadTimeout = readTimeout
	server.WriteTimeout = writeTimeout
	server.IdleTimeout = idleTimeout
}

type applicationServer interface {
	Start(address string) error
	Shutdown(context.Context) error
}

type Template struct {
	templates *template.Template
}

func templateFunctions() template.FuncMap {
	belgrade, _ := time.LoadLocation("Europe/Belgrade")
	return template.FuncMap{
		"add": func(a, b int) int { return a + b },
		"sub": func(a, b int) int { return a - b },
		"FormatAdminTime": func(dt time.Time) string {
			return dt.In(belgrade).Format("02.01.2006 15:04:05")
		},
		"FormatCurrency": func(c float64) string {
			return fmt.Sprintf("%.2f", c)
		},
		"GetLangText":     util.GetLangText,
		"FormatDateTime":  formatDateTime,
		"FormatDate":      formatDate,
		"FormatVisibleId": formatVisibleID,
		"AppVersion":      version.Label,
		"AssetVersion":    version.AssetTag,
	}
}

// formatDate turns a "2006-01-02..." value into "02.01.2006". Values too
// short to contain a date are returned unchanged instead of failing the page.
func formatDate(dt string) string {
	if len(dt) < 10 {
		return dt
	}
	return dt[8:10] + "." + dt[5:7] + "." + dt[0:4]
}

// formatDateTime turns a "2006-01-02T15:04:05..." value into
// "02.01.2006 15:04:05", or returns the date alone when no time is present.
func formatDateTime(dt string) string {
	if len(dt) < 19 {
		return formatDate(dt)
	}
	return formatDate(dt) + " " + dt[11:19]
}

// formatVisibleID shows the last ten characters of a public ID.
func formatVisibleID(vid string) string {
	if len(vid) <= 10 {
		return vid
	}
	return vid[len(vid)-10:]
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
	if err := migrations.Apply(migrationContext, database.Db, initialSchema, migrations.SchemaVersion); err != nil {
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
	configureHTTPServer(e.Server)

	midware.SetMiddleware()

	staticFiles, err := fs.Sub(embeddedFiles, "static")
	if err != nil {
		panic(fmt.Errorf("open embedded static files: %w", err))
	}
	registerStaticRoutes(e, staticFiles)

	routes.DefineRoutes()

	e.Logger.Info("goexpenses " + version.Label() + " listening on port " + util.Settings.Port)
	serveErr := serveUntilShutdown(ctx, e, ":"+util.Settings.Port)
	emailContext, cancelEmails := context.WithTimeout(context.Background(), gracefulShutdownTimeout)
	defer cancelEmails()
	if err := routes.WaitForEmails(emailContext); err != nil {
		e.Logger.Warn("shutdown before queued emails were sent")
	}
	if serveErr != nil {
		return serveErr
	}
	if ctx.Err() != nil {
		e.Logger.Info("Shutdown complete")
	}
	return nil
}

func registerStaticRoutes(e *echo.Echo, staticFiles fs.FS) {
	e.StaticFS("/static", staticFiles)
	e.FileFS("/favicon.ico", "favicon.ico", staticFiles)
	e.FileFS("/ads.txt", "ads.txt", staticFiles)
	// The service worker must be served from the root to control every page.
	e.FileFS("/sw.js", "sw.js", staticFiles, withHeader("Cache-Control", "no-cache"))
	e.FileFS("/manifest.webmanifest", "manifest.webmanifest", staticFiles, withHeader(echo.HeaderContentType, "application/manifest+json"))
}

// withHeader sets a response header before the route handler runs.
func withHeader(name, value string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Response().Header().Set(name, value)
			return next(c)
		}
	}
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
