package main

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"goexpenses/routes"

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

func memberRole(t *testing.T, db *sqlx.DB, accountID int, user testUser) string {
	t.Helper()
	role := ""
	if err := db.Get(&role, `SELECT COALESCE((SELECT role FROM accountsusers WHERE accounts_id = $1 AND users_id = $2), '')`, accountID, user.id); err != nil {
		t.Fatalf("load member role: %v", err)
	}
	return role
}

func TestHTTPOwnerManagesAccountMembers(t *testing.T) {
	server, db := startTestApp(t)
	alice := createTestUser(t, db, "alice")
	bob := createTestUser(t, db, "bob")
	owner := newTestClient(t, server)
	owner.login(alice)

	if page := owner.get("/posts").body; !strings.Contains(page, `href="/accounts/members"`) {
		t.Fatal("account bar does not link to the members page")
	}
	// Registered or not, the owner gets the same answer.
	for _, email := range []string{"BOB@example.com", "nobody@example.com"} {
		response := owner.post("/accounts/members/invite", url.Values{"email": {email}, "role": {"viewer"}})
		if response.status != http.StatusSeeOther || response.location != "/accounts/members" {
			t.Fatalf("invite %s = %d to %q", email, response.status, response.location)
		}
		if page := owner.get("/accounts/members").body; !strings.Contains(page, "Invitation sent.") {
			t.Errorf("invite %s does not report the invitation", email)
		}
	}
	if role := memberRole(t, db, alice.accountID, bob); role != "" {
		t.Fatal("invited user became a member before accepting")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := routes.WaitForEmails(ctx); err != nil {
		t.Fatalf("wait for emails: %v", err)
	}
	if got := countRows(t, db, `SELECT COUNT(*) FROM admin_events WHERE kind = 'email' AND detail = 'account_invitation' AND subject = 'bob@example.com'`); got != 1 {
		t.Errorf("registered user got %d invitation e-mails, want 1", got)
	}
	if got := countRows(t, db, `SELECT COUNT(*) FROM admin_events WHERE kind = 'email' AND subject = 'nobody@example.com'`); got != 0 {
		t.Error("an unregistered address was e-mailed")
	}

	// Only the invited user sees and accepts the invitation.
	viewer := newTestClient(t, server)
	viewer.login(bob)
	page := viewer.get("/posts").body
	if !strings.Contains(page, `href="/accounts/invitations"`) {
		t.Fatal("invited user is not shown the invitation")
	}
	invitationID := strconv.Itoa(countRows(t, db, `SELECT id FROM account_invitations WHERE email = 'bob@example.com'`))
	if response := owner.post("/accounts/invitations/accept", url.Values{"id": {invitationID}}); response.status != http.StatusNotFound {
		t.Errorf("accepting someone else's invitation = %d, want 404", response.status)
	}
	if page := viewer.get("/accounts/invitations").body; !strings.Contains(page, "alice account") {
		t.Fatal("invitations page does not name the account")
	}
	if response := viewer.post("/accounts/invitations/accept", url.Values{"id": {invitationID}}); response.status != http.StatusSeeOther || response.location != "/posts" {
		t.Fatalf("accept invitation = %d to %q", response.status, response.location)
	}
	if role := memberRole(t, db, alice.accountID, bob); role != "viewer" {
		t.Fatalf("bob's role = %q, want viewer", role)
	}
	if !strings.Contains(viewer.get("/posts").body, "View only") {
		t.Error("accepted account is not selected")
	}

	// A pending invitation can be cancelled by the owner.
	nobodyID := strconv.Itoa(countRows(t, db, `SELECT id FROM account_invitations WHERE email = 'nobody@example.com'`))
	if response := owner.post("/accounts/members/invitations/cancel", url.Values{"id": {nobodyID}}); response.status != http.StatusSeeOther {
		t.Fatalf("cancel invitation = %d", response.status)
	}
	if got := countRows(t, db, `SELECT COUNT(*) FROM account_invitations`); got != 0 {
		t.Fatalf("%d invitations remain, want 0", got)
	}

	// A viewer sees the members but cannot manage them.
	page = viewer.get("/accounts/members").body
	if !strings.Contains(page, "alice") || strings.Contains(page, `action="/accounts/members/invite"`) || strings.Contains(page, `action="/accounts/members/role"`) {
		t.Error("viewer's members page does not list members read-only")
	}
	forbidden := []struct {
		path string
		form url.Values
	}{
		{"/accounts/members/invite", url.Values{"email": {"carol@example.com"}, "role": {"owner"}}},
		{"/accounts/members/invitations/cancel", url.Values{"id": {"1"}}},
		{"/accounts/members/role", url.Values{"users_id": {strconv.Itoa(bob.id)}, "role": {"owner"}}},
		{"/accounts/members/remove", url.Values{"users_id": {strconv.Itoa(alice.id)}}},
	}
	for _, attempt := range forbidden {
		if response := viewer.post(attempt.path, attempt.form); response.status != http.StatusForbidden {
			t.Errorf("POST %s by a viewer = %d, want 403", attempt.path, response.status)
		}
	}

	// The only owner can neither step down nor leave.
	owner.post("/accounts/members/role", url.Values{"users_id": {strconv.Itoa(alice.id)}, "role": {"editor"}})
	owner.post("/accounts/members/remove", url.Values{"users_id": {strconv.Itoa(alice.id)}})
	if role := memberRole(t, db, alice.accountID, alice); role != "owner" {
		t.Fatalf("only owner was demoted or removed: role %q", role)
	}
	if page := owner.get("/accounts/members").body; !strings.Contains(page, "An account needs at least one owner.") {
		t.Error("last owner change is not explained")
	}

	if response := owner.post("/accounts/members/role", url.Values{"users_id": {strconv.Itoa(bob.id)}, "role": {"editor"}}); response.status != http.StatusSeeOther {
		t.Fatalf("change role = %d", response.status)
	}
	if role := memberRole(t, db, alice.accountID, bob); role != "editor" {
		t.Fatalf("bob's role = %q, want editor", role)
	}
	if response := owner.post("/accounts/members/remove", url.Values{"users_id": {strconv.Itoa(bob.id)}}); response.status != http.StatusSeeOther {
		t.Fatalf("remove member = %d", response.status)
	}
	if role := memberRole(t, db, alice.accountID, bob); role != "" {
		t.Fatal("removed member is still in the account")
	}

	// Any member can leave the account themselves.
	shareAccount(t, db, alice.accountID, bob, "viewer")
	if response := viewer.post("/accounts/members/remove", url.Values{"users_id": {strconv.Itoa(bob.id)}}); response.status != http.StatusSeeOther || response.location != "/posts" {
		t.Fatalf("leave account = %d to %q", response.status, response.location)
	}
	if role := memberRole(t, db, alice.accountID, bob); role != "" {
		t.Fatal("member who left is still in the account")
	}
}

func TestHTTPDeletingOnlyOwnerKeepsSharedAccountOwned(t *testing.T) {
	server, db := startTestApp(t)
	admin := createTestUser(t, db, "admin")
	alice := createTestUser(t, db, "alice")
	bob := createTestUser(t, db, "bob")
	if _, err := db.Exec(`UPDATE users SET is_admin = true WHERE id = $1`, admin.id); err != nil {
		t.Fatalf("make admin: %v", err)
	}
	shareAccount(t, db, alice.accountID, bob, "viewer")
	client := newTestClient(t, server)
	client.login(admin)

	path := "/admin/users/" + strconv.Itoa(alice.id) + "/delete"
	if response := client.post(path, url.Values{"confirm_username": {"alice"}}); response.status != http.StatusSeeOther {
		t.Fatalf("delete owner = %d, body %q", response.status, response.body)
	}
	if role := memberRole(t, db, alice.accountID, bob); role != "owner" {
		t.Fatalf("remaining member's role = %q, want owner", role)
	}
}

func TestHTTPRemovedOwnerCannotRejoinWithOldInvitation(t *testing.T) {
	server, db := startTestApp(t)
	alice := createTestUser(t, db, "alice")
	bob := createTestUser(t, db, "bob")
	shareAccount(t, db, alice.accountID, bob, "owner")
	if _, err := db.Exec(`UPDATE users SET default_accounts_id = $1 WHERE id = $2`, bob.accountID, bob.id); err != nil {
		t.Fatalf("select bob's own account: %v", err)
	}
	aliceClient := newTestClient(t, server)
	aliceClient.login(alice)
	aliceClient.post("/accounts/members/invite", url.Values{"email": {"alice@example.com"}, "role": {"owner"}})
	// An owner can also invite an address of their own that is not a member.
	aliceClient.post("/accounts/members/invite", url.Values{"email": {"alias@example.com"}, "role": {"owner"}})
	if got := len(invitationIDs(t, db, "alice@example.com")); got != 0 {
		t.Errorf("a member was invited to their own account: %d invitations", got)
	}

	bobClient := newTestClient(t, server)
	bobClient.login(bob)
	bobClient.post("/accounts/select", url.Values{"accounts_id": {strconv.Itoa(alice.accountID)}})
	if response := bobClient.post("/accounts/members/remove", url.Values{"users_id": {strconv.Itoa(alice.id)}}); response.status != http.StatusSeeOther {
		t.Fatalf("remove alice = %d", response.status)
	}

	if got := countRows(t, db, `SELECT COUNT(*) FROM account_invitations WHERE accounts_id = $1`, alice.accountID); got != 0 {
		t.Errorf("%d invitations sent by the removed owner remain", got)
	}
	for _, id := range invitationIDs(t, db, "alice@example.com") {
		aliceClient.post("/accounts/invitations/accept", url.Values{"id": {strconv.Itoa(id)}})
	}
	if role := memberRole(t, db, alice.accountID, alice); role != "" {
		t.Fatalf("removed owner rejoined as %q", role)
	}
}

func TestHTTPMemberWithoutAccountsIsSentToCreateOne(t *testing.T) {
	server, db := startTestApp(t)
	alice := createTestUser(t, db, "alice")
	bob := createTestUser(t, db, "bob")
	shareAccount(t, db, bob.accountID, alice, "owner")
	if _, err := db.Exec(`UPDATE users SET default_accounts_id = $1 WHERE id = $2`, alice.accountID, alice.id); err != nil {
		t.Fatalf("select alice's own account: %v", err)
	}
	// Alice shares her own account with bob as owner and leaves it.
	shareAccount(t, db, alice.accountID, bob, "owner")
	if _, err := db.Exec(`DELETE FROM accountsusers WHERE accounts_id = $1 AND users_id = $2`, bob.accountID, alice.id); err != nil {
		t.Fatalf("remove alice from bob's account: %v", err)
	}
	client := newTestClient(t, server)
	client.login(alice)
	response := client.post("/accounts/members/remove", url.Values{"users_id": {strconv.Itoa(alice.id)}})
	if response.status != http.StatusSeeOther {
		t.Fatalf("leave last account = %d", response.status)
	}
	posts := client.get("/posts")
	if posts.status != http.StatusSeeOther || posts.location != "/accounts/show" {
		t.Fatalf("GET /posts without accounts = %d to %q, want redirect to /accounts/show", posts.status, posts.location)
	}
	if page := client.get("/accounts/show"); page.status != http.StatusOK {
		t.Fatalf("GET /accounts/show without accounts = %d", page.status)
	}
}

func invitationIDs(t *testing.T, db *sqlx.DB, email string) []int {
	t.Helper()
	ids := []int{}
	if err := db.Select(&ids, `SELECT id FROM account_invitations WHERE email = $1`, email); err != nil {
		t.Fatalf("load invitations: %v", err)
	}
	return ids
}

func TestHTTPDeletingOwnerWithdrawsTheirInvitations(t *testing.T) {
	server, db := startTestApp(t)
	admin := createTestUser(t, db, "admin")
	alice := createTestUser(t, db, "alice")
	bob := createTestUser(t, db, "bob")
	if _, err := db.Exec(`UPDATE users SET is_admin = true WHERE id = $1`, admin.id); err != nil {
		t.Fatalf("make admin: %v", err)
	}
	shareAccount(t, db, alice.accountID, bob, "owner")
	aliceClient := newTestClient(t, server)
	aliceClient.login(alice)
	aliceClient.post("/accounts/members/invite", url.Values{"email": {"alias@example.com"}, "role": {"owner"}})
	if got := len(invitationIDs(t, db, "alias@example.com")); got != 1 {
		t.Fatalf("%d invitations to the alias, want 1", got)
	}

	client := newTestClient(t, server)
	client.login(admin)
	path := "/admin/users/" + strconv.Itoa(alice.id) + "/delete"
	if response := client.post(path, url.Values{"confirm_username": {"alice"}}); response.status != http.StatusSeeOther {
		t.Fatalf("delete owner = %d, body %q", response.status, response.body)
	}
	if got := len(invitationIDs(t, db, "alias@example.com")); got != 0 {
		t.Fatalf("%d invitations sent by the deleted owner remain", got)
	}
}
