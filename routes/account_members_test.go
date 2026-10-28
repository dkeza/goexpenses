package routes

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"goexpenses/database"
	"goexpenses/internal/testdb"
	"goexpenses/util"

	"github.com/labstack/echo/v4"
)

// TestChangeMemberRoleRechecksActorRole replays a role change whose request
// was authorized while the actor was still an owner, after another owner has
// already demoted them.
func TestChangeMemberRoleRechecksActorRole(t *testing.T) {
	integration := testdb.Open(t)
	previous := database.Db
	database.Db = integration.DB
	t.Cleanup(func() { database.Db = previous })
	db := integration.DB

	accountID := 0
	if err := db.Get(&accountID, `INSERT INTO accounts (description) VALUES ('Shared') RETURNING id`); err != nil {
		t.Fatal(err)
	}
	users := map[string]int{}
	for _, name := range []string{"alice", "bob"} {
		id := 0
		if err := db.Get(&id, `INSERT INTO users (name, email, username, default_accounts_id) VALUES ($1, $2, $1, $3) RETURNING id`, name, name+"@example.com", accountID); err != nil {
			t.Fatal(err)
		}
		users[name] = id
	}
	if _, err := db.Exec(`INSERT INTO accountsusers (accounts_id, users_id, role) VALUES ($1, $2, 'owner'), ($1, $3, 'viewer')`, accountID, users["alice"], users["bob"]); err != nil {
		t.Fatal(err)
	}

	form := url.Values{"users_id": {strconv.Itoa(users["bob"])}, "role": {"owner"}}
	req := httptest.NewRequest(http.MethodPost, "/accounts/members/role", strings.NewReader(form.Encode()))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	c := echo.New().NewContext(req, httptest.NewRecorder())
	stale := &util.Data{User: util.User{Id: users["bob"], Default_accounts_id: accountID}, AccountRole: util.RoleOwner}
	c.Set("data", stale)

	err := changeMemberRole(c)
	if httpError, ok := err.(*echo.HTTPError); !ok || httpError.Code != http.StatusForbidden {
		t.Errorf("role change by a demoted owner = %v, want 403", err)
	}
	role := ""
	if err := db.Get(&role, `SELECT role FROM accountsusers WHERE accounts_id = $1 AND users_id = $2`, accountID, users["bob"]); err != nil {
		t.Fatal(err)
	}
	if role != util.RoleViewer {
		t.Fatalf("demoted owner made themselves %q", role)
	}
}
