package routes

import (
	"database/sql"
	"errors"
	"html"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"goexpenses/database"
	"goexpenses/util"

	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo/v4"
)

const (
	lastOwnerMessage = "An account needs at least one owner."
	// openInvitation selects invitations that can still be accepted.
	openInvitation = `i.created_at > NOW() - INTERVAL '30 days'`

	invitationRateLimitWindow    = time.Hour
	invitationUserRateLimit      = 20
	invitationRecipientRateLimit = 3
)

var errLastOwner = errors.New("account needs at least one owner")

type accountMember struct {
	UserID    int       `db:"users_id"`
	Name      string    `db:"name"`
	Username  string    `db:"username"`
	Role      string    `db:"role"`
	CreatedAt time.Time `db:"created_at"`
}

type accountInvitation struct {
	ID        int       `db:"id"`
	Email     string    `db:"email"`
	Account   string    `db:"account"`
	InvitedBy string    `db:"invited_by"`
	Role      string    `db:"role"`
	CreatedAt time.Time `db:"created_at"`
}

type membersPage struct {
	*util.Data
	Account     string
	Members     []accountMember
	Invitations []accountInvitation
	IsOwner     bool
}

// requireAccountOwner lets only owners of the selected account manage its
// members.
func requireAccountOwner(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		data, ok := c.Get("data").(*util.Data)
		if !ok || data.AccountRole != util.RoleOwner {
			return echo.NewHTTPError(http.StatusForbidden, "only account owners can manage members")
		}
		return next(c)
	}
}

func validRole(role string) bool {
	return role == util.RoleOwner || role == util.RoleEditor || role == util.RoleViewer
}

func showMembers(c echo.Context) error {
	data := c.Get("data").(*util.Data)
	data.Active = ""
	view := membersPage{Data: data, IsOwner: data.AccountRole == util.RoleOwner}
	for _, account := range data.Accounts {
		if account.Id == data.User.Default_accounts_id {
			view.Account = account.Description
		}
	}
	query := `SELECT au.users_id, u.name, u.username, au.role, au.created_at
		FROM accountsusers au INNER JOIN users u ON u.id = au.users_id
		WHERE au.accounts_id = $1
		ORDER BY CASE au.role WHEN 'owner' THEN 0 WHEN 'editor' THEN 1 ELSE 2 END, lower(u.name), u.id`
	if err := database.Db.Select(&view.Members, query, data.User.Default_accounts_id); err != nil {
		return databaseReadError(c, "load account members", err)
	}
	if view.IsOwner {
		query = `SELECT i.id, i.email, '' AS account, '' AS invited_by, i.role, i.created_at
			FROM account_invitations i
			WHERE i.accounts_id = $1 AND ` + openInvitation + `
			ORDER BY i.created_at DESC, i.id DESC`
		if err := database.Db.Select(&view.Invitations, query, data.User.Default_accounts_id); err != nil {
			return databaseReadError(c, "load account invitations", err)
		}
	}
	return c.Render(http.StatusOK, "accountmembers", view)
}

// inviteMember records an invitation and e-mails the user with that address,
// if there is one. The owner sees the same response either way, so inviting
// does not reveal which addresses are registered.
func inviteMember(c echo.Context) error {
	data := c.Get("data").(*util.Data)
	email := strings.ToLower(strings.TrimSpace(c.FormValue("email")))
	role := c.FormValue("role")
	if !validRole(role) {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid role")
	}
	if address, err := mail.ParseAddress(email); err != nil || address.Address != email {
		util.Flash("Invalid E-Mail!", data, 0, "", 0)
		return c.Redirect(http.StatusSeeOther, "/accounts/members")
	}

	query := `INSERT INTO account_invitations (accounts_id, email, role, invited_by) VALUES ($1, $2, $3, $4)
		ON CONFLICT (accounts_id, email) DO UPDATE
		SET role = EXCLUDED.role, invited_by = EXCLUDED.invited_by, created_at = NOW()`
	if _, err := database.Db.Exec(query, data.User.Default_accounts_id, email, role, data.User.Id); err != nil {
		return databaseWriteError(c, "invite account member", err)
	}

	recipients := []string{}
	query = `SELECT u.lang FROM users u
		WHERE lower(btrim(u.email)) = $1 AND u.email_verified AND u.blocked_at IS NULL
		  AND NOT EXISTS (SELECT 1 FROM accountsusers au WHERE au.accounts_id = $2 AND au.users_id = u.id)`
	if err := database.Db.Select(&recipients, query, email, data.User.Default_accounts_id); err != nil {
		c.Logger().Errorf("find invited user: %v", err)
	}
	if len(recipients) == 1 {
		sendInvitationEmail(email, recipients[0])
	}

	util.Flash("Invitation sent.", data, 1, "", 0)
	return c.Redirect(http.StatusSeeOther, "/accounts/members")
}

// sendInvitationEmail only tells the user to sign in. The account name and
// the inviter's name stay out of the e-mail, so an owner cannot use
// invitations to send their own text to other users.
func sendInvitationEmail(recipient, lang string) {
	link := strings.TrimRight(util.Settings.Host, "/") + "/accounts/invitations"
	queueTrackedEmail("account_invitation", emailMessage{
		Recipient: recipient,
		Subject:   "Goexpenses " + util.GetLangText("Invitation to a shared account", lang),
		HTMLBody:  util.GetLangText("You have been invited to a shared account. Sign in to accept or decline the invitation:", lang) + ` <a href="` + html.EscapeString(link) + `">` + util.GetLangText("Invitations", lang) + `</a>`,
	})
}

func invitationRateLimitRules(c echo.Context) []rateLimitRule {
	userID := 0
	if data, ok := c.Get("data").(*util.Data); ok {
		userID = data.User.Id
	}
	return []rateLimitRule{
		{key: "invitation:user:" + strconv.Itoa(userID), limit: invitationUserRateLimit, window: invitationRateLimitWindow},
		{key: rateLimitIdentifierKey("invitation", "email", c.FormValue("email")), limit: invitationRecipientRateLimit, window: invitationRateLimitWindow},
	}
}

func cancelInvitation(c echo.Context) error {
	data := c.Get("data").(*util.Data)
	id, err := strconv.Atoi(c.FormValue("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid invitation")
	}
	query := `DELETE FROM account_invitations WHERE id = $1 AND accounts_id = $2`
	if err := executeExactlyOne(database.Db, query, id, data.User.Default_accounts_id); err != nil {
		return databaseWriteError(c, "cancel invitation", err)
	}
	return c.Redirect(http.StatusSeeOther, "/accounts/members")
}

func showInvitations(c echo.Context) error {
	data := c.Get("data").(*util.Data)
	data.Active = ""
	view := membersPage{Data: data}
	query := `SELECT i.id, i.email, a.description AS account, COALESCE(u.name, '') AS invited_by, i.role, i.created_at
		FROM account_invitations i
		INNER JOIN accounts a ON a.id = i.accounts_id AND a.deleted = 0
		LEFT JOIN users u ON u.id = i.invited_by
		WHERE i.email = lower(btrim($1)) AND ` + openInvitation + `
		ORDER BY i.created_at DESC, i.id DESC`
	if err := database.Db.Select(&view.Invitations, query, data.User.Email); err != nil {
		return databaseReadError(c, "load received invitations", err)
	}
	return c.Render(http.StatusOK, "accountinvitations", view)
}

// acceptInvitation makes the user a member of the invited account and selects
// it. Only the user with the invited e-mail address can accept.
func acceptInvitation(c echo.Context) error {
	data := c.Get("data").(*util.Data)
	id, err := strconv.Atoi(c.FormValue("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid invitation")
	}
	err = runTransaction(database.Db, func(tx *sqlx.Tx) error {
		invitation := struct {
			AccountID int    `db:"accounts_id"`
			Role      string `db:"role"`
		}{}
		query := `SELECT i.accounts_id, i.role FROM account_invitations i
			INNER JOIN accounts a ON a.id = i.accounts_id AND a.deleted = 0
			WHERE i.id = $1 AND i.email = lower(btrim($2)) AND ` + openInvitation + `
			FOR UPDATE OF i`
		if err := tx.Get(&invitation, query, id, data.User.Email); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return errRecordNotFound
			}
			return err
		}
		if _, err := tx.Exec(`INSERT INTO accountsusers (accounts_id, users_id, role) VALUES ($1, $2, $3)
			ON CONFLICT (accounts_id, users_id) DO NOTHING`, invitation.AccountID, data.User.Id, invitation.Role); err != nil {
			return err
		}
		if err := executeExactlyOne(tx, `DELETE FROM account_invitations WHERE id = $1`, id); err != nil {
			return err
		}
		return executeExactlyOne(tx, `UPDATE users SET default_accounts_id = $1 WHERE id = $2`, invitation.AccountID, data.User.Id)
	})
	if err != nil {
		return databaseWriteError(c, "accept invitation", err)
	}
	return c.Redirect(http.StatusSeeOther, "/posts")
}

func declineInvitation(c echo.Context) error {
	data := c.Get("data").(*util.Data)
	id, err := strconv.Atoi(c.FormValue("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid invitation")
	}
	query := `DELETE FROM account_invitations WHERE id = $1 AND email = lower(btrim($2))`
	if err := executeExactlyOne(database.Db, query, id, data.User.Email); err != nil {
		return databaseWriteError(c, "decline invitation", err)
	}
	return c.Redirect(http.StatusSeeOther, "/accounts/invitations")
}

func changeMemberRole(c echo.Context) error {
	data := c.Get("data").(*util.Data)
	userID, err := strconv.Atoi(c.FormValue("users_id"))
	role := c.FormValue("role")
	if err != nil || !validRole(role) {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid member or role")
	}
	if err := updateMember(data.User.Default_accounts_id, userID, role); err != nil {
		return memberWriteError(c, data, "change member role", err)
	}
	return c.Redirect(http.StatusSeeOther, "/accounts/members")
}

// removeMember lets an owner remove any member and any member leave the
// account themselves.
func removeMember(c echo.Context) error {
	data := c.Get("data").(*util.Data)
	userID, err := strconv.Atoi(c.FormValue("users_id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid member")
	}
	if userID != data.User.Id && data.AccountRole != util.RoleOwner {
		return echo.NewHTTPError(http.StatusForbidden, "only account owners can remove other members")
	}
	if err := updateMember(data.User.Default_accounts_id, userID, ""); err != nil {
		return memberWriteError(c, data, "remove account member", err)
	}
	if userID == data.User.Id {
		return c.Redirect(http.StatusSeeOther, "/posts")
	}
	return c.Redirect(http.StatusSeeOther, "/accounts/members")
}

// updateMember gives the member a new role, or removes them when role is
// empty, unless that would leave the account without an owner.
func updateMember(accountID, userID int, role string) error {
	return runTransaction(database.Db, func(tx *sqlx.Tx) error {
		// Lock the account's members so two owners cannot demote each other
		// at the same time.
		members := []accountMember{}
		if err := tx.Select(&members, `SELECT users_id, role FROM accountsusers WHERE accounts_id = $1 ORDER BY id FOR UPDATE`, accountID); err != nil {
			return err
		}
		owners, current := 0, ""
		for _, member := range members {
			if member.Role == util.RoleOwner {
				owners++
			}
			if member.UserID == userID {
				current = member.Role
			}
		}
		if current == "" {
			return errRecordNotFound
		}
		if current == util.RoleOwner && role != util.RoleOwner && owners == 1 {
			return errLastOwner
		}
		if role == "" {
			return executeExactlyOne(tx, `DELETE FROM accountsusers WHERE accounts_id = $1 AND users_id = $2`, accountID, userID)
		}
		return executeExactlyOne(tx, `UPDATE accountsusers SET role = $1 WHERE accounts_id = $2 AND users_id = $3`, role, accountID, userID)
	})
}

func memberWriteError(c echo.Context, data *util.Data, operation string, err error) error {
	if errors.Is(err, errLastOwner) {
		util.Flash(lastOwnerMessage, data, 0, "", 0)
		return c.Redirect(http.StatusSeeOther, "/accounts/members")
	}
	return databaseWriteError(c, operation, err)
}

func DefineAccountMemberRoutes() {
	e := E
	auth := Auth

	e.GET("/accounts/members", showMembers, auth)
	e.POST("/accounts/members/invite", inviteMember, auth, requireAccountOwner, rateLimitMiddleware(publicRequestLimiter, invitationRateLimitRules))
	e.POST("/accounts/members/invitations/cancel", cancelInvitation, auth, requireAccountOwner)
	e.POST("/accounts/members/role", changeMemberRole, auth, requireAccountOwner)
	e.POST("/accounts/members/remove", removeMember, auth)

	e.GET("/accounts/invitations", showInvitations, auth)
	e.POST("/accounts/invitations/accept", acceptInvitation, auth)
	e.POST("/accounts/invitations/decline", declineInvitation, auth)
}
