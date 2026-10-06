package main

import (
	"html/template"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"goexpenses/database"
	"goexpenses/internal/testdb"
	"goexpenses/midware"
	"goexpenses/routes"
	"goexpenses/util"

	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo/v4"
)

const integrationPassword = "integration-password"

// startTestApp serves the real router, middleware and templates against an
// isolated PostgreSQL schema.
func startTestApp(t *testing.T) (*httptest.Server, *sqlx.DB) {
	t.Helper()
	integration := testdb.Open(t)

	previousDB, previousSettings := database.Db, util.Settings
	previousEcho, previousAuth := routes.E, routes.Auth
	t.Cleanup(func() {
		database.Db, util.Settings = previousDB, previousSettings
		routes.E, routes.Auth = previousEcho, previousAuth
		util.InvalidateEURRateCache()
	})
	database.Db = integration.DB
	util.Settings.DatabaseType = "postgres"
	util.Settings.CookieSecure = false // httptest serves plain HTTP
	util.InvalidateEURRateCache()
	if _, err := integration.DB.Exec(`INSERT INTO currencies (code, rate, date) VALUES ('EUR', 117.2, NOW())`); err != nil {
		t.Fatalf("store exchange rate: %v", err)
	}

	routes.E = echo.New()
	routes.E.Renderer = &Template{templates: template.Must(parseTemplates())}
	midware.SetMiddleware()
	routes.DefineRoutes()

	server := httptest.NewServer(routes.E)
	t.Cleanup(server.Close)
	return server, integration.DB
}

type testUser struct {
	id        int
	accountID int
	username  string
}

func createTestUser(t *testing.T, db *sqlx.DB, username string) testUser {
	t.Helper()
	passwordHash, err := util.HashPassword(integrationPassword)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	user := testUser{username: username}
	if err := db.Get(&user.accountID, `INSERT INTO accounts (description) VALUES ($1) RETURNING id`, username+" account"); err != nil {
		t.Fatalf("create account: %v", err)
	}
	if err := db.Get(&user.id, `
		INSERT INTO users (name, email, username, password, default_accounts_id, lang, email_verified)
		VALUES ($1, $2, $3, $4, $5, 'EN', true) RETURNING id`,
		username, username+"@example.com", username, passwordHash, user.accountID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO accountsusers (accounts_id, users_id) VALUES ($1, $2)`, user.accountID, user.id); err != nil {
		t.Fatalf("add account member: %v", err)
	}
	return user
}

type testRecords struct {
	expenseID  int
	expensePID string
	incomePID  string
	postPID    string
}

// createTestRecords adds one expense, income and post with descriptions that
// start with prefix, so pages can be searched for leaked records.
func createTestRecords(t *testing.T, db *sqlx.DB, user testUser, prefix string) testRecords {
	t.Helper()
	records := testRecords{expensePID: prefix + "-expense-pid", incomePID: prefix + "-income-pid", postPID: prefix + "-post-pid"}
	if err := db.Get(&records.expenseID, `
		INSERT INTO expenses (description, accounts_id, amount, exchange, p_id) VALUES ($1, $2, 10, 117.2, $3) RETURNING id`,
		prefix+" expense", user.accountID, records.expensePID); err != nil {
		t.Fatalf("create expense: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO incomes (description, accounts_id, p_id) VALUES ($1, $2, $3)`,
		prefix+" income", user.accountID, records.incomePID); err != nil {
		t.Fatalf("create income: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO posts (description, expenses_id, amount, exchange, accounts_id, p_id) VALUES ($1, $2, 25, 117.2, $3, $4)`,
		prefix+" post", records.expenseID, user.accountID, records.postPID); err != nil {
		t.Fatalf("create post: %v", err)
	}
	return records
}

type testClient struct {
	t      *testing.T
	base   *url.URL
	client *http.Client
}

type testResponse struct {
	status   int
	location string
	body     string
}

func newTestClient(t *testing.T, server *httptest.Server) *testClient {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("create cookie jar: %v", err)
	}
	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	return &testClient{t: t, base: base, client: &http.Client{
		Jar: jar,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}}
}

func (c *testClient) do(request *http.Request) testResponse {
	c.t.Helper()
	response, err := c.client.Do(request)
	if err != nil {
		c.t.Fatalf("%s %s: %v", request.Method, request.URL.Path, err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		c.t.Fatalf("read %s response: %v", request.URL.Path, err)
	}
	return testResponse{status: response.StatusCode, location: response.Header.Get("Location"), body: string(body)}
}

func (c *testClient) get(path string) testResponse {
	c.t.Helper()
	request, err := http.NewRequest(http.MethodGet, c.base.String()+path, nil)
	if err != nil {
		c.t.Fatalf("create request: %v", err)
	}
	return c.do(request)
}

// post submits a form with the CSRF token from the client's cookie.
func (c *testClient) post(path string, form url.Values) testResponse {
	c.t.Helper()
	if form == nil {
		form = url.Values{}
	}
	for _, cookie := range c.client.Jar.Cookies(c.base) {
		if cookie.Name == "_csrf" {
			form.Set("_CSRF", cookie.Value)
		}
	}
	request, err := http.NewRequest(http.MethodPost, c.base.String()+path, strings.NewReader(form.Encode()))
	if err != nil {
		c.t.Fatalf("create request: %v", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return c.do(request)
}

func (c *testClient) login(user testUser) {
	c.t.Helper()
	c.get("/")
	response := c.post("/auth", url.Values{"username": {user.username}, "password": {integrationPassword}})
	if response.status != http.StatusSeeOther || response.location != "/posts" {
		c.t.Fatalf("login as %s = %d to %q", user.username, response.status, response.location)
	}
}

func countRows(t *testing.T, db *sqlx.DB, query string, args ...any) int {
	t.Helper()
	count := 0
	if err := db.Get(&count, query, args...); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	return count
}

func TestHTTPUserCannotAccessAnotherUsersRecords(t *testing.T) {
	server, db := startTestApp(t)
	alice := createTestUser(t, db, "alice")
	bob := createTestUser(t, db, "bob")
	aliceRecords := createTestRecords(t, db, alice, "alice")
	bobRecords := createTestRecords(t, db, bob, "bob")

	client := newTestClient(t, server)
	client.login(alice)

	// Lists show only the signed-in user's records.
	for _, path := range []string{"/posts", "/expenses", "/incomes", "/posts/newincomepost"} {
		response := client.get(path)
		if response.status != http.StatusOK {
			t.Fatalf("GET %s = %d", path, response.status)
		}
		if strings.Contains(response.body, "bob ") || strings.Contains(response.body, "bob-") {
			t.Errorf("GET %s shows another user's records", path)
		}
	}
	for _, path := range []string{"/posts", "/expenses", "/incomes"} {
		if response := client.get(path); !strings.Contains(response.body, "alice ") {
			t.Errorf("GET %s does not show the user's own records", path)
		}
	}

	// Another user's records cannot be opened, changed or deleted.
	today := time.Now().Format("2006-01-02")
	attempts := []struct {
		name   string
		send   func() testResponse
		status int
	}{
		{"show post", func() testResponse { return client.get("/posts/show?id=" + bobRecords.postPID) }, http.StatusNotFound},
		{"update post", func() testResponse {
			return client.post("/posts/update", url.Values{"id": {bobRecords.postPID}, "description": {"changed"}, "amount": {"1"}, "dateonly": {today}})
		}, http.StatusNotFound},
		{"delete post", func() testResponse { return client.post("/posts/delete", url.Values{"id": {bobRecords.postPID}}) }, http.StatusNotFound},
		{"show expense", func() testResponse { return client.get("/expenses/show?id=" + bobRecords.expensePID) }, http.StatusNotFound},
		{"update expense", func() testResponse {
			return client.post("/expenses/update", url.Values{"id": {bobRecords.expensePID}, "description": {"changed"}, "amount": {"1"}})
		}, http.StatusNotFound},
		{"delete expense", func() testResponse {
			return client.post("/expenses/delete", url.Values{"id": {bobRecords.expensePID}})
		}, http.StatusNotFound},
		{"show income", func() testResponse { return client.get("/incomes/show?id=" + bobRecords.incomePID) }, http.StatusNotFound},
		{"update income", func() testResponse {
			return client.post("/incomes/update", url.Values{"id": {bobRecords.incomePID}, "description": {"changed"}})
		}, http.StatusNotFound},
		{"delete income", func() testResponse { return client.post("/incomes/delete", url.Values{"id": {bobRecords.incomePID}}) }, http.StatusNotFound},
		{"select account", func() testResponse {
			return client.post("/accounts/select", url.Values{"accounts_id": {strconv.Itoa(bob.accountID)}})
		}, http.StatusForbidden},
		{"open admin", func() testResponse { return client.get("/admin") }, http.StatusForbidden},
		{"block user", func() testResponse {
			return client.post("/admin/users/"+strconv.Itoa(bob.id)+"/block", url.Values{"reason": {"test"}})
		}, http.StatusForbidden},
	}
	for _, attempt := range attempts {
		if response := attempt.send(); response.status != attempt.status {
			t.Errorf("%s of another user = %d, want %d", attempt.name, response.status, attempt.status)
		}
	}

	// Another user's expense or income cannot be linked to the user's records.
	alicePosts := countRows(t, db, `SELECT COUNT(*) FROM posts WHERE accounts_id = $1`, alice.accountID)
	aliceExpenses := countRows(t, db, `SELECT COUNT(*) FROM expenses WHERE accounts_id = $1`, alice.accountID)
	client.post("/posts/save", url.Values{"description": {"linked"}, "expense_id": {bobRecords.expensePID}, "amount": {"5"}, "date": {today}})
	client.post("/posts/save", url.Values{"description": {"linked"}, "income_id": {bobRecords.incomePID}, "amount": {"5"}, "date": {today}})
	client.post("/expenses/save", url.Values{"description": {"linked"}, "expense_id": {bobRecords.expensePID}, "amount": {"5"}})
	if got := countRows(t, db, `SELECT COUNT(*) FROM posts WHERE accounts_id = $1`, alice.accountID); got != alicePosts {
		t.Errorf("posts linked to another user's records were saved: %d -> %d", alicePosts, got)
	}
	if got := countRows(t, db, `SELECT COUNT(*) FROM expenses WHERE accounts_id = $1`, alice.accountID); got != aliceExpenses {
		t.Errorf("expense linked to another user's expense was saved: %d -> %d", aliceExpenses, got)
	}

	// The other user's data is unchanged.
	unchanged := countRows(t, db, `
		SELECT (SELECT COUNT(*) FROM posts WHERE p_id = $1 AND description = 'bob post' AND deleted = 0)
		     + (SELECT COUNT(*) FROM expenses WHERE p_id = $2 AND description = 'bob expense' AND deleted = 0)
		     + (SELECT COUNT(*) FROM incomes WHERE p_id = $3 AND description = 'bob income' AND deleted = 0)
		     + (SELECT COUNT(*) FROM users WHERE id = $4 AND blocked_at IS NULL)`,
		bobRecords.postPID, bobRecords.expensePID, bobRecords.incomePID, bob.id)
	if unchanged != 4 {
		t.Fatalf("another user's records changed: %d of 4 unchanged", unchanged)
	}
	if got := countRows(t, db, `SELECT COUNT(*) FROM users WHERE id = $1 AND default_accounts_id = $2`, alice.id, alice.accountID); got != 1 {
		t.Error("user's default account was switched to another user's account")
	}

	// The user's own records stay accessible.
	for _, path := range []string{
		"/posts/show?id=" + aliceRecords.postPID,
		"/expenses/show?id=" + aliceRecords.expensePID,
		"/incomes/show?id=" + aliceRecords.incomePID,
	} {
		if response := client.get(path); response.status != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", path, response.status)
		}
	}
}

func TestHTTPUserManagesOwnRecords(t *testing.T) {
	server, db := startTestApp(t)
	alice := createTestUser(t, db, "alice")
	records := createTestRecords(t, db, alice, "alice")
	client := newTestClient(t, server)
	client.login(alice)
	today := time.Now().Format("2006-01-02")

	steps := []struct {
		name string
		path string
		form url.Values
	}{
		{"create expense", "/expenses/save", url.Values{"description": {"Groceries"}, "amount": {"100"}}},
		{"update expense", "/expenses/update", url.Values{"id": {records.expensePID}, "description": {"Fuel"}, "amount": {"20"}}},
		{"create income", "/incomes/save", url.Values{"description": {"Salary"}}},
		{"update income", "/incomes/update", url.Values{"id": {records.incomePID}, "description": {"Bonus"}}},
		{"create post", "/posts/save", url.Values{"description": {"Market"}, "expense_id": {records.expensePID}, "amount": {"50"}, "date": {today}}},
		{"create income post", "/posts/save", url.Values{"description": {"Paycheck"}, "income_id": {records.incomePID}, "amount": {"1000"}, "date": {today}}},
		{"update post", "/posts/update", url.Values{"id": {records.postPID}, "description": {"Updated post"}, "amount": {"75"}, "dateonly": {today}}},
	}
	for _, step := range steps {
		if response := client.post(step.path, step.form); response.status != http.StatusSeeOther {
			t.Fatalf("%s = %d, body %q", step.name, response.status, response.body)
		}
	}

	checks := []struct {
		name  string
		query string
		args  []any
	}{
		{"created expense", `SELECT COUNT(*) FROM expenses WHERE accounts_id = $1 AND description = 'Groceries' AND amount = 100`, []any{alice.accountID}},
		{"updated expense", `SELECT COUNT(*) FROM expenses WHERE p_id = $1 AND description = 'Fuel' AND amount = 20`, []any{records.expensePID}},
		{"created income", `SELECT COUNT(*) FROM incomes WHERE accounts_id = $1 AND description = 'Salary'`, []any{alice.accountID}},
		{"updated income", `SELECT COUNT(*) FROM incomes WHERE p_id = $1 AND description = 'Bonus'`, []any{records.incomePID}},
		{"created post", `SELECT COUNT(*) FROM posts WHERE accounts_id = $1 AND description = 'Market' AND amount = 50 AND expenses_id = $2`, []any{alice.accountID, records.expenseID}},
		{"created income post", `SELECT COUNT(*) FROM posts WHERE accounts_id = $1 AND description = 'Paycheck' AND amount = -1000 AND incomes_id > 0`, []any{alice.accountID}},
		{"updated post", `SELECT COUNT(*) FROM posts WHERE p_id = $1 AND description = 'Updated post' AND amount = 75`, []any{records.postPID}},
	}
	for _, check := range checks {
		if got := countRows(t, db, check.query, check.args...); got != 1 {
			t.Errorf("%s: %d matching rows, want 1", check.name, got)
		}
	}

	for _, deletion := range []struct{ path, id, table string }{
		{"/posts/delete", records.postPID, "posts"},
		{"/expenses/delete", records.expensePID, "expenses"},
		{"/incomes/delete", records.incomePID, "incomes"},
	} {
		if response := client.post(deletion.path, url.Values{"id": {deletion.id}}); response.status != http.StatusSeeOther {
			t.Fatalf("POST %s = %d", deletion.path, response.status)
		}
		// Table names come from the fixed list above.
		if got := countRows(t, db, `SELECT COUNT(*) FROM `+deletion.table+` WHERE p_id = $1 AND deleted = 1`, deletion.id); got != 1 {
			t.Errorf("%s record was not marked deleted", deletion.table)
		}
	}
}

func TestHTTPSignedOutVisitorIsRedirectedToLogin(t *testing.T) {
	server, db := startTestApp(t)
	bob := createTestUser(t, db, "bob")
	bobRecords := createTestRecords(t, db, bob, "bob")
	client := newTestClient(t, server)
	client.get("/")

	for _, response := range []testResponse{
		client.get("/posts"),
		client.get("/expenses/show?id=" + bobRecords.expensePID),
		client.post("/posts/delete", url.Values{"id": {bobRecords.postPID}}),
	} {
		if response.status != http.StatusSeeOther || response.location != "/login" {
			t.Errorf("signed-out request = %d to %q, want redirect to /login", response.status, response.location)
		}
	}
	if got := countRows(t, db, `SELECT COUNT(*) FROM posts WHERE p_id = $1 AND deleted = 0`, bobRecords.postPID); got != 1 {
		t.Fatal("signed-out visitor deleted a post")
	}
}
