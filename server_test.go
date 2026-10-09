package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"goexpenses/util"
	"goexpenses/version"

	"github.com/labstack/echo/v4"
)

type fakeApplicationServer struct {
	startError       error
	shutdownError    error
	started          chan struct{}
	stopped          chan struct{}
	shutdownCalled   chan struct{}
	stopOnce         sync.Once
	shutdownCallOnce sync.Once
	address          string
}

func newFakeApplicationServer() *fakeApplicationServer {
	return &fakeApplicationServer{
		started:        make(chan struct{}),
		stopped:        make(chan struct{}),
		shutdownCalled: make(chan struct{}),
	}
}

func (s *fakeApplicationServer) Start(address string) error {
	s.address = address
	close(s.started)
	if s.startError != nil {
		return s.startError
	}
	<-s.stopped
	return http.ErrServerClosed
}

func (s *fakeApplicationServer) Shutdown(context.Context) error {
	s.shutdownCallOnce.Do(func() { close(s.shutdownCalled) })
	s.stopOnce.Do(func() { close(s.stopped) })
	return s.shutdownError
}

func TestEmbeddedPostgresSchemaIsCurrentAndNonDestructive(t *testing.T) {
	schema, err := embeddedFiles.ReadFile("db/pg_structure.sql")
	if err != nil {
		t.Fatalf("read embedded PostgreSQL schema: %v", err)
	}
	schemaText := string(schema)

	if strings.Contains(strings.ToUpper(schemaText), "DROP TABLE") {
		t.Fatal("initial PostgreSQL schema contains a destructive DROP TABLE statement")
	}
	for _, required := range []string{
		"CREATE TABLE public.params",
		"created_at timestamp NOT NULL DEFAULT NOW()",
		"created_ts timestamp NOT NULL DEFAULT NOW()",
		"users_username_lower_uidx",
		"posts_active_account_date_id_idx",
		"posts_account_fk",
		"posts_history_trigger",
		"post_filter character varying",
	} {
		if !strings.Contains(schemaText, required) {
			t.Fatalf("initial PostgreSQL schema does not contain %q", required)
		}
	}
}

func TestTemplatesParse(t *testing.T) {
	if _, err := parseTemplates(); err != nil {
		t.Fatalf("parse embedded templates: %v", err)
	}
}

func TestAdminTemplatesRender(t *testing.T) {
	templates, err := parseTemplates()
	if err != nil {
		t.Fatal(err)
	}
	data := struct {
		*util.Data
		Users        []struct{}
		Events       []struct{}
		Alerts       []struct{}
		SelectedUser struct {
			ID            int
			Username      string
			Name          string
			Email         string
			CreatedAt     time.Time
			EmailVerified bool
			IsAdmin       bool
			BlockedAt     *time.Time
			BlockedReason *string
		}
		UserCounts struct {
			Accounts        int
			DeletedAccounts int
			Posts           int
			DeletedPosts    int
			Expenses        int
			DeletedExpenses int
			Incomes         int
			DeletedIncomes  int
		}
		Query        string
		Status       string
		Kind         string
		EventStatus  string
		UserFilter   string
		FromDate     string
		ToDate       string
		Page         int
		HasNext      bool
		TotalUsers   int
		NewUsers     int
		BlockedUsers int
		PendingUsers int
		Deleted      bool
	}{Data: &util.Data{Lang: "RS", Active: "admin", User: util.User{Id: 1, Name: "Admin", IsAdmin: true}}, Page: 1}
	data.SelectedUser.Username = "example"
	data.SelectedUser.CreatedAt = time.Now()
	for _, name := range []string{"admin", "admin-user", "admin-events"} {
		var rendered bytes.Buffer
		if err := templates.ExecuteTemplate(&rendered, name, data); err != nil {
			t.Fatalf("render %s: %v", name, err)
		}
		if !strings.Contains(rendered.String(), "Administracija") {
			t.Fatalf("%s is missing the admin navigation link", name)
		}
		if strings.Contains(rendered.String(), "googletagmanager.com") || strings.Contains(rendered.String(), "googlesyndication.com") {
			t.Fatalf("%s loads third-party scripts", name)
		}
	}
}

func TestStaticAssetURLsIncludeBuildVersion(t *testing.T) {
	templates, err := parseTemplates()
	if err != nil {
		t.Fatalf("parse embedded templates: %v", err)
	}

	originalNumber, originalCommit := version.Number, version.Commit
	version.Number, version.Commit = "123", "abc1234"
	t.Cleanup(func() {
		version.Number, version.Commit = originalNumber, originalCommit
	})

	data := &util.Data{}
	for _, name := range []string{"header", "footer"} {
		var rendered bytes.Buffer
		if err := templates.ExecuteTemplate(&rendered, name, data); err != nil {
			t.Fatalf("render %s template: %v", name, err)
		}
		if count := strings.Count(rendered.String(), "?v=123-abc1234"); count != 2 {
			t.Fatalf("%s template has %d versioned asset URLs, want 2", name, count)
		}
	}
}

func TestTemplatesApplyCSPNonceToEveryScript(t *testing.T) {
	templates, err := parseTemplates()
	if err != nil {
		t.Fatalf("parse templates: %v", err)
	}

	const nonce = "response-specific-nonce"
	var rendered bytes.Buffer
	if err := templates.ExecuteTemplate(&rendered, "index", &util.Data{CSPNonce: nonce}); err != nil {
		t.Fatalf("render index template: %v", err)
	}

	html := rendered.String()
	scriptCount := strings.Count(html, "<script")
	if scriptCount == 0 || strings.Count(html, `nonce="`+nonce+`"`) != scriptCount {
		t.Fatalf("rendered %d scripts without applying nonce to every script", scriptCount)
	}
}

func TestSpeculationRulesSkipStateChangingLinks(t *testing.T) {
	templates, err := parseTemplates()
	if err != nil {
		t.Fatalf("parse templates: %v", err)
	}

	var rendered bytes.Buffer
	if err := templates.ExecuteTemplate(&rendered, "index", &util.Data{CSPNonce: "nonce"}); err != nil {
		t.Fatalf("render index template: %v", err)
	}

	html := rendered.String()
	const opening = `<script type="speculationrules" nonce="nonce">`
	start := strings.Index(html, opening)
	if start < 0 {
		t.Fatal("index template has no speculation rules")
	}
	body := html[start+len(opening):]
	body = body[:strings.Index(body, "</script>")]

	var rules struct {
		Prefetch []struct {
			Where struct {
				And []struct {
					HrefMatches string `json:"href_matches"`
					Not         struct {
						HrefMatches string `json:"href_matches"`
					} `json:"not"`
				} `json:"and"`
			} `json:"where"`
			Eagerness string `json:"eagerness"`
		} `json:"prefetch"`
	}
	if err := json.Unmarshal([]byte(body), &rules); err != nil {
		t.Fatalf("speculation rules are not valid JSON: %v\n%s", err, body)
	}
	if len(rules.Prefetch) != 1 {
		t.Fatalf("got %d prefetch rules, want 1", len(rules.Prefetch))
	}

	excluded := map[string]bool{}
	for _, condition := range rules.Prefetch[0].Where.And {
		excluded[condition.Not.HrefMatches] = true
	}
	for _, pattern := range []string{"/logout", "/verify-email*", "/resetpassword*", `/*\?*(^|&)lang=*`, `/*\?*(^|&)reset=*`} {
		if !excluded[pattern] {
			t.Errorf("speculation rules do not exclude %q", pattern)
		}
	}
}

func TestRateLimitTemplateRendersFriendlyResponse(t *testing.T) {
	templates, err := parseTemplates()
	if err != nil {
		t.Fatalf("parse templates: %v", err)
	}

	data := &util.Data{
		Lang:                "RS",
		RateLimitRetryAfter: 42,
		RateLimitBackURL:    "/login",
	}
	var rendered bytes.Buffer
	if err := templates.ExecuteTemplate(&rendered, "rate-limit", data); err != nil {
		t.Fatalf("render rate-limit template: %v", err)
	}

	html := rendered.String()
	for _, expected := range []string{"Previše zahteva", "Približno vreme čekanja:", ">42<", "sekundi", `href="/login"`} {
		if !strings.Contains(html, expected) {
			t.Errorf("rate-limit template does not contain %q", expected)
		}
	}
}

func TestServeUntilShutdownStopsServerAfterContextCancellation(t *testing.T) {
	server := newFakeApplicationServer()
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- serveUntilShutdown(ctx, server, ":8080")
	}()

	<-server.started
	cancel()

	if err := <-result; err != nil {
		t.Fatalf("serveUntilShutdown: %v", err)
	}
	select {
	case <-server.shutdownCalled:
	default:
		t.Fatal("server shutdown was not called")
	}
	if server.address != ":8080" {
		t.Fatalf("server address = %q, want %q", server.address, ":8080")
	}
}

func TestServeUntilShutdownReturnsServerError(t *testing.T) {
	expectedError := errors.New("listen failed")
	server := newFakeApplicationServer()
	server.startError = expectedError

	err := serveUntilShutdown(context.Background(), server, ":8080")
	if !errors.Is(err, expectedError) {
		t.Fatalf("serveUntilShutdown error = %v, want %v", err, expectedError)
	}
	select {
	case <-server.shutdownCalled:
		t.Fatal("shutdown called after server startup failure")
	default:
	}
}

func TestServeUntilShutdownReturnsShutdownError(t *testing.T) {
	expectedError := errors.New("shutdown timed out")
	server := newFakeApplicationServer()
	server.shutdownError = expectedError
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- serveUntilShutdown(ctx, server, ":8080")
	}()

	<-server.started
	cancel()

	if err := <-result; !errors.Is(err, expectedError) {
		t.Fatalf("serveUntilShutdown error = %v, want %v", err, expectedError)
	}
}

func TestConfigureHTTPServerSetsTimeouts(t *testing.T) {
	server := &http.Server{}
	configureHTTPServer(server)

	if server.ReadHeaderTimeout != readHeaderTimeout || server.ReadTimeout != readTimeout ||
		server.WriteTimeout != writeTimeout || server.IdleTimeout != idleTimeout {
		t.Fatalf("server timeouts = header %v, read %v, write %v, idle %v",
			server.ReadHeaderTimeout, server.ReadTimeout, server.WriteTimeout, server.IdleTimeout)
	}
}

func TestExpenseTemplatesOfferEmptyRelatedExpense(t *testing.T) {
	templates, err := parseTemplates()
	if err != nil {
		t.Fatalf("parse templates: %v", err)
	}
	related := []util.Expense{{Pid: "pid-a", Description: "Fuel"}, {Pid: "pid-b", Description: "Rent"}}
	const eurDate = "2026-09-30 07:00:00"

	render := func(name string, data *util.Data) string {
		t.Helper()
		var rendered bytes.Buffer
		if err := templates.ExecuteTemplate(&rendered, name, data); err != nil {
			t.Fatalf("render %s: %v", name, err)
		}
		return rendered.String()
	}

	page := render("expenses", &util.Data{Eurdate: eurDate, ExpensesAdd: related, Expenses_id: "pid-b"})
	if !strings.Contains(page, `class="form-select"><option value="" ></option>`) {
		t.Error("expenses form does not start with an unselected empty related expense")
	}
	if !strings.Contains(page, `<option value="pid-b" selected>Rent</option>`) {
		t.Error("expenses form does not keep the last related expense selected")
	}
	page = render("expenses", &util.Data{Eurdate: eurDate, ExpensesAdd: related})
	if !strings.Contains(page, `<option value="" selected></option>`) {
		t.Error("expenses form does not select the empty related expense by default")
	}

	page = render("expensesshow", &util.Data{
		Eurdate:     eurDate,
		Expenses:    []util.Expense{{Pid: "pid-c", Description: "Parking", ExpensesPid: "pid-a"}},
		ExpensesAdd: related,
	})
	if !strings.Contains(page, `class="form-select"><option value="" ></option>`) ||
		!strings.Contains(page, `<option value="pid-a" selected>Fuel</option>`) {
		t.Error("expense edit form does not offer the empty option and select the stored related expense")
	}
}

func TestTemplateFormattersHandleShortValues(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"date from timestamp", formatDate("2026-09-30T07:15:42Z"), "30.09.2026"},
		{"date only", formatDate("2026-09-30"), "30.09.2026"},
		{"empty date", formatDate(""), ""},
		{"short date", formatDate("2026-09"), "2026-09"},
		{"date time", formatDateTime("2026-09-30T07:15:42.123456Z"), "30.09.2026 07:15:42"},
		{"date time with space", formatDateTime("2026-09-30 07:15:42"), "30.09.2026 07:15:42"},
		{"date time without time", formatDateTime("2026-09-30"), "30.09.2026"},
		{"empty date time", formatDateTime(""), ""},
		{"public ID", formatVisibleID("0123456789abcdef0123456789abcdef01234567"), "ef01234567"},
		{"short public ID", formatVisibleID("abc"), "abc"},
		{"empty public ID", formatVisibleID(""), ""},
	}
	for _, test := range tests {
		if test.got != test.want {
			t.Errorf("%s = %q, want %q", test.name, test.got, test.want)
		}
	}
}

func TestHeaderOmitsThirdPartyScriptsWhenHidden(t *testing.T) {
	templates, err := parseTemplates()
	if err != nil {
		t.Fatalf("parse templates: %v", err)
	}
	for _, hide := range []bool{false, true} {
		var rendered bytes.Buffer
		if err := templates.ExecuteTemplate(&rendered, "header", &util.Data{HideThirdPartyScripts: hide}); err != nil {
			t.Fatalf("render header: %v", err)
		}
		page := rendered.String()
		hasThirdParty := strings.Contains(page, "googletagmanager.com") || strings.Contains(page, "googlesyndication.com")
		if hasThirdParty == hide {
			t.Errorf("HideThirdPartyScripts=%v: third-party scripts present = %v", hide, hasThirdParty)
		}
	}
}

func TestProgressiveWebAppFilesAreServedFromRoot(t *testing.T) {
	staticFiles, err := fs.Sub(embeddedFiles, "static")
	if err != nil {
		t.Fatalf("open embedded static files: %v", err)
	}
	e := echo.New()
	registerStaticRoutes(e, staticFiles)

	serve := func(path string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		e.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want 200", path, recorder.Code)
		}
		return recorder
	}

	worker := serve("/sw.js?v=123")
	if got := worker.Header().Get(echo.HeaderContentType); !strings.HasPrefix(got, "text/javascript") {
		t.Errorf("service worker Content-Type = %q", got)
	}
	if got := worker.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("service worker Cache-Control = %q, want no-cache", got)
	}

	manifestResponse := serve("/manifest.webmanifest")
	if got := manifestResponse.Header().Get(echo.HeaderContentType); got != "application/manifest+json" {
		t.Errorf("manifest Content-Type = %q", got)
	}
	var manifest struct {
		ID       string `json:"id"`
		StartURL string `json:"start_url"`
		Display  string `json:"display"`
		Icons    []struct {
			Src     string `json:"src"`
			Purpose string `json:"purpose"`
		} `json:"icons"`
	}
	if err := json.Unmarshal(manifestResponse.Body.Bytes(), &manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if manifest.ID != "/" || manifest.StartURL != "/" || manifest.Display != "standalone" {
		t.Errorf("manifest start_url = %q, display = %q", manifest.StartURL, manifest.Display)
	}
	purposes := map[string]bool{}
	for _, icon := range manifest.Icons {
		purposes[icon.Purpose] = true
		serve(icon.Src)
	}
	if !purposes["any"] || !purposes["maskable"] {
		t.Errorf("manifest icon purposes = %v, want any and maskable", purposes)
	}

	serve("/static/offline.html")
	serve("/static/icons/apple-touch-icon.png")
}

func TestManifestNamesAppInPageLanguage(t *testing.T) {
	e := echo.New()
	e.GET("/manifest.webmanifest", serveManifest)

	tests := map[string]struct{ lang, name, shortName string }{
		"RS":      {"sr-Latn", "Aplikacija za evidenciju troškova", "Troškovi"},
		"SR":      {"sr-Cyrl", "Апликација за евиденцију трошкова", "Трошкови"},
		"DE":      {"de", "Kosten App", "Kosten"},
		"EN":      {"en", "Expenses App", "Expenses"},
		"":        {"en", "Expenses App", "Expenses"},
		"unknown": {"en", "Expenses App", "Expenses"},
	}
	for query, want := range tests {
		recorder := httptest.NewRecorder()
		e.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/manifest.webmanifest?lang="+query, nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("lang=%q status = %d, want 200", query, recorder.Code)
		}
		if got := recorder.Header().Get("Cache-Control"); got != "no-cache" {
			t.Errorf("lang=%q Cache-Control = %q, want no-cache", query, got)
		}
		var manifest struct {
			Lang      string `json:"lang"`
			Name      string `json:"name"`
			ShortName string `json:"short_name"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &manifest); err != nil {
			t.Fatalf("lang=%q decode manifest: %v", query, err)
		}
		if manifest.Lang != want.lang || manifest.Name != want.name || manifest.ShortName != want.shortName {
			t.Errorf("lang=%q manifest = %+v, want %+v", query, manifest, want)
		}
	}
}

func TestHeaderLinksLocalizedManifestAndThemeColor(t *testing.T) {
	templates, err := parseTemplates()
	if err != nil {
		t.Fatalf("parse templates: %v", err)
	}
	var rendered bytes.Buffer
	if err := templates.ExecuteTemplate(&rendered, "header", &util.Data{Lang: "RS"}); err != nil {
		t.Fatalf("render header: %v", err)
	}
	page := rendered.String()
	for _, expected := range []string{
		`<link rel="manifest" href="/manifest.webmanifest?lang=RS">`,
		`<meta name="theme-color" content="#ffffff">`,
	} {
		if !strings.Contains(page, expected) {
			t.Errorf("header does not contain %s", expected)
		}
	}
	// The theme script reads the meta tag, so the tag must come first.
	if strings.Index(page, `name="theme-color"`) > strings.Index(page, "<script") {
		t.Error("theme-color meta tag is rendered after the theme script")
	}
}

func TestListTemplatesMarkCellsForCompactCards(t *testing.T) {
	templates, err := parseTemplates()
	if err != nil {
		t.Fatalf("parse templates: %v", err)
	}
	data := &util.Data{
		Posts:    []util.Post{{Pid: "a1b2c3d4e5f6", Description: "Post", Expense: "Type"}},
		Expenses: []util.Expense{{Pid: "b1b2c3d4e5f6", Description: "Expense"}},
		Incomes:  []util.Income{{Pid: "c1b2c3d4e5f6", Description: "Income"}},
	}
	tests := map[string][]string{
		"posts":    {"compact-list posts-list", "cell-id", "cell-title", "cell-type", "cell-date", "cell-created", "cell-rsd", "cell-eur", "cell-actions"},
		"expenses": {"compact-list expenses-list", "cell-id", "cell-title", "cell-rsd", "cell-eur", "cell-fee", "cell-actions"},
		"incomes":  {"compact-list incomes-list", "cell-id", "cell-title", "cell-actions"},
	}
	for name, classes := range tests {
		var rendered bytes.Buffer
		if err := templates.ExecuteTemplate(&rendered, name, data); err != nil {
			t.Fatalf("render %s: %v", name, err)
		}
		for _, class := range classes {
			if !strings.Contains(rendered.String(), class) {
				t.Errorf("%s template does not use class %q", name, class)
			}
		}
	}
}
