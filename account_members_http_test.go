package main

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
)

// shareAccount adds user to the account with the role and selects it.
func shareAccount(t *testing.T, db *sqlx.DB, accountID int, user testUser, role string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO accountsusers (accounts_id, users_id, role) VALUES ($1, $2, $3)`, accountID, user.id, role); err != nil {
		t.Fatalf("share account: %v", err)
	}
	if _, err := db.Exec(`UPDATE users SET default_accounts_id = $1 WHERE id = $2`, accountID, user.id); err != nil {
		t.Fatalf("select shared account: %v", err)
	}
}

func TestHTTPViewerReadsButCannotChangeSharedAccount(t *testing.T) {
	server, db := startTestApp(t)
	alice := createTestUser(t, db, "alice")
	bob := createTestUser(t, db, "bob")
	records := createTestRecords(t, db, alice, "alice")
	shareAccount(t, db, alice.accountID, bob, "viewer")
	client := newTestClient(t, server)
	client.login(bob)

	pages := []struct {
		path, record string
		forbidden    []string
	}{
		{"/posts", "alice post", []string{`action="/posts/save"`, `action="/posts/delete"`, `href="/posts/newincomepost"`}},
		{"/expenses", "alice expense", []string{`action="/expenses/save"`, `action="/expenses/show"`, `action="/expenses/delete"`}},
		{"/incomes", "alice income", []string{`action="/incomes/save"`, `action="/incomes/show"`, `action="/incomes/delete"`}},
		{"/posts/show?id=" + records.postPID, "alice post", []string{`<button type="submit" class="btn btn-primary">`}},
	}
	for _, page := range pages {
		response := client.get(page.path)
		if response.status != http.StatusOK {
			t.Fatalf("GET %s = %d", page.path, response.status)
		}
		if !strings.Contains(response.body, page.record) {
			t.Errorf("GET %s does not show the shared account's records", page.path)
		}
		for _, control := range page.forbidden {
			if strings.Contains(response.body, control) {
				t.Errorf("GET %s offers %s to a viewer", page.path, control)
			}
		}
	}
	if page := client.get("/posts").body; !strings.Contains(page, "View only") {
		t.Error("posts page does not mark the account as view only")
	}

	today := time.Now().Format("2006-01-02")
	attempts := []struct {
		name string
		send func() testResponse
	}{
		{"create post", func() testResponse {
			return client.post("/posts/save", url.Values{"description": {"x"}, "expense_id": {records.expensePID}, "amount": {"1"}, "date": {today}})
		}},
		{"update post", func() testResponse {
			return client.post("/posts/update", url.Values{"id": {records.postPID}, "description": {"changed"}, "amount": {"1"}, "dateonly": {today}})
		}},
		{"delete post", func() testResponse { return client.post("/posts/delete", url.Values{"id": {records.postPID}}) }},
		{"new income post form", func() testResponse { return client.get("/posts/newincomepost") }},
		{"create expense", func() testResponse {
			return client.post("/expenses/save", url.Values{"description": {"x"}, "amount": {"1"}})
		}},
		{"show expense", func() testResponse { return client.get("/expenses/show?id=" + records.expensePID) }},
		{"update expense", func() testResponse {
			return client.post("/expenses/update", url.Values{"id": {records.expensePID}, "description": {"changed"}, "amount": {"1"}})
		}},
		{"delete expense", func() testResponse { return client.post("/expenses/delete", url.Values{"id": {records.expensePID}}) }},
		{"create income", func() testResponse { return client.post("/incomes/save", url.Values{"description": {"x"}}) }},
		{"show income", func() testResponse { return client.get("/incomes/show?id=" + records.incomePID) }},
		{"update income", func() testResponse {
			return client.post("/incomes/update", url.Values{"id": {records.incomePID}, "description": {"changed"}})
		}},
		{"delete income", func() testResponse { return client.post("/incomes/delete", url.Values{"id": {records.incomePID}}) }},
	}
	for _, attempt := range attempts {
		if response := attempt.send(); response.status != http.StatusForbidden {
			t.Errorf("%s by a viewer = %d, want 403", attempt.name, response.status)
		}
	}

	unchanged := countRows(t, db, `
		SELECT (SELECT COUNT(*) FROM posts WHERE accounts_id = $1)
		     + (SELECT COUNT(*) FROM posts WHERE p_id = $2 AND description = 'alice post' AND deleted = 0)
		     + (SELECT COUNT(*) FROM expenses WHERE accounts_id = $1 AND description = 'alice expense' AND deleted = 0)
		     + (SELECT COUNT(*) FROM incomes WHERE accounts_id = $1 AND description = 'alice income' AND deleted = 0)
		     + (SELECT COUNT(*) FROM expenses WHERE accounts_id = $1)
		     + (SELECT COUNT(*) FROM incomes WHERE accounts_id = $1)`,
		alice.accountID, records.postPID)
	if unchanged != 6 {
		t.Fatalf("a viewer changed the shared account: %d of 6 checks hold", unchanged)
	}

	// The same member can change records once they become an editor.
	if _, err := db.Exec(`UPDATE accountsusers SET role = 'editor' WHERE accounts_id = $1 AND users_id = $2`, alice.accountID, bob.id); err != nil {
		t.Fatalf("make member an editor: %v", err)
	}
	if response := client.post("/posts/update", url.Values{"id": {records.postPID}, "description": {"changed"}, "amount": {"1"}, "dateonly": {today}}); response.status != http.StatusSeeOther {
		t.Fatalf("update post by an editor = %d, body %q", response.status, response.body)
	}
	if page := client.get("/posts").body; !strings.Contains(page, `action="/posts/save"`) || strings.Contains(page, "View only") {
		t.Error("posts page does not offer editing to an editor")
	}
}

func TestHTTPMemberLosesAccessWhenRemovedOrAccountDeleted(t *testing.T) {
	server, db := startTestApp(t)
	alice := createTestUser(t, db, "alice")
	bob := createTestUser(t, db, "bob")
	records := createTestRecords(t, db, alice, "alice")
	createTestRecords(t, db, bob, "bob")
	shareAccount(t, db, alice.accountID, bob, "editor")
	client := newTestClient(t, server)
	client.login(bob)

	if page := client.get("/posts").body; !strings.Contains(page, "alice post") {
		t.Fatal("member does not see the shared account")
	}

	if _, err := db.Exec(`DELETE FROM accountsusers WHERE accounts_id = $1 AND users_id = $2`, alice.accountID, bob.id); err != nil {
		t.Fatalf("remove member: %v", err)
	}
	page := client.get("/posts").body
	if strings.Contains(page, "alice post") || !strings.Contains(page, "bob post") {
		t.Error("removed member still sees the shared account instead of their own")
	}
	if got := countRows(t, db, `SELECT COUNT(*) FROM users WHERE id = $1 AND default_accounts_id = $2`, bob.id, bob.accountID); got != 1 {
		t.Error("removed member was not moved to their own account")
	}
	today := time.Now().Format("2006-01-02")
	if response := client.post("/posts/update", url.Values{"id": {records.postPID}, "description": {"changed"}, "amount": {"1"}, "dateonly": {today}}); response.status != http.StatusNotFound {
		t.Errorf("update of the shared account's post by a removed member = %d, want 404", response.status)
	}

	// A deleted account is left the same way and no longer listed.
	shareAccount(t, db, alice.accountID, bob, "editor")
	if _, err := db.Exec(`UPDATE accounts SET deleted = 1 WHERE id = $1`, alice.accountID); err != nil {
		t.Fatalf("delete account: %v", err)
	}
	page = client.get("/posts").body
	if strings.Contains(page, "alice account") || !strings.Contains(page, "bob post") {
		t.Error("deleted account is still selected or listed")
	}
}

func TestHTTPMembersKeepTheirOwnPostsFilter(t *testing.T) {
	server, db := startTestApp(t)
	alice := createTestUser(t, db, "alice")
	bob := createTestUser(t, db, "bob")
	createTestRecords(t, db, alice, "alice")
	shareAccount(t, db, alice.accountID, bob, "editor")

	aliceClient := newTestClient(t, server)
	aliceClient.login(alice)
	filter := url.Values{"from": {""}, "to": {""}, "q": {"nothing matches"}, "type": {""}}
	if response := aliceClient.get("/posts?" + filter.Encode()); response.status != http.StatusSeeOther {
		t.Fatalf("apply filter = %d", response.status)
	}
	if page := aliceClient.get("/posts").body; !strings.Contains(page, "filter-chip") || strings.Contains(page, "alice post") {
		t.Fatal("filter was not applied for the member who saved it")
	}

	bobClient := newTestClient(t, server)
	bobClient.login(bob)
	if page := bobClient.get("/posts").body; strings.Contains(page, "filter-chip") || !strings.Contains(page, "alice post") {
		t.Error("another member's filter was applied")
	}
}
