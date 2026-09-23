package routes

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"goexpenses/database"
	"goexpenses/util"

	"github.com/labstack/echo/v4"
)

const adminPageSize = 50

type adminUser struct {
	ID            int        `db:"id"`
	Name          string     `db:"name"`
	Username      string     `db:"username"`
	Email         string     `db:"email"`
	CreatedAt     time.Time  `db:"created_at"`
	EmailVerified bool       `db:"email_verified"`
	IsAdmin       bool       `db:"is_admin"`
	BlockedAt     *time.Time `db:"blocked_at"`
	BlockedReason *string    `db:"blocked_reason"`
}

type adminEventRow struct {
	ID          int64     `db:"id"`
	Kind        string    `db:"kind"`
	Status      string    `db:"status"`
	UserID      *int      `db:"user_id"`
	ActorUserID *int      `db:"actor_user_id"`
	Subject     string    `db:"subject"`
	Detail      string    `db:"detail"`
	ItemCount   *int      `db:"item_count"`
	CreatedAt   time.Time `db:"created_at"`
}

type adminPage struct {
	*util.Data
	Users        []adminUser
	Events       []adminEventRow
	Alerts       []adminEventRow
	SelectedUser adminUser
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
}

func adminOnly(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		data := c.Get("data").(*util.Data)
		if data.User.Id == 0 {
			return c.Redirect(http.StatusSeeOther, "/login?next=/admin")
		}
		if !data.User.IsAdmin {
			return echo.NewHTTPError(http.StatusForbidden, "admin access required")
		}
		return next(c)
	}
}

func adminPageNumber(c echo.Context) int {
	page, err := strconv.Atoi(c.QueryParam("page"))
	if err != nil || page < 1 || page > 100000 {
		return 1
	}
	return page
}

func adminDashboard(c echo.Context) error {
	data := c.Get("data").(*util.Data)
	data.Active = "admin"
	page := adminPageNumber(c)
	query := strings.TrimSpace(c.QueryParam("q"))
	if runes := []rune(query); len(runes) > 100 {
		query = string(runes[:100])
	}
	status := c.QueryParam("status")
	if status != "blocked" && status != "pending" && status != "active" {
		status = "all"
	}
	view := adminPage{Data: data, Query: query, Status: status, Page: page}
	if err := database.Db.Get(&view.TotalUsers, `SELECT COUNT(*) FROM users`); err != nil {
		return databaseReadError(c, "count users", err)
	}
	if err := database.Db.Get(&view.NewUsers, `SELECT COUNT(*) FROM users WHERE created_at >= NOW() - INTERVAL '7 days'`); err != nil {
		return databaseReadError(c, "count new users", err)
	}
	if err := database.Db.Get(&view.BlockedUsers, `SELECT COUNT(*) FROM users WHERE blocked_at IS NOT NULL`); err != nil {
		return databaseReadError(c, "count blocked users", err)
	}
	if err := database.Db.Get(&view.PendingUsers, `SELECT COUNT(*) FROM users WHERE email_verified = false`); err != nil {
		return databaseReadError(c, "count pending users", err)
	}
	if err := database.Db.Select(&view.Alerts, `
		SELECT id, kind, status, user_id, actor_user_id, subject, detail, item_count, created_at
		FROM admin_events WHERE status NOT IN ('success', 'smtp_accepted')
		ORDER BY created_at DESC, id DESC LIMIT 5`); err != nil {
		return databaseReadError(c, "list recent problems", err)
	}
	statusCondition := ""
	switch status {
	case "blocked":
		statusCondition = "AND blocked_at IS NOT NULL"
	case "pending":
		statusCondition = "AND email_verified = false AND blocked_at IS NULL"
	case "active":
		statusCondition = "AND email_verified = true AND blocked_at IS NULL"
	}
	// statusCondition is selected only from the fixed cases above.
	err := database.Db.Select(&view.Users, `
		SELECT id, name, username, email, created_at, email_verified, is_admin, blocked_at, blocked_reason
		FROM users WHERE ($1 = '' OR strpos(lower(username), lower($1)) > 0 OR strpos(lower(email), lower($1)) > 0)
		`+statusCondition+` ORDER BY created_at DESC, id DESC LIMIT $2 OFFSET $3`,
		query, adminPageSize+1, (page-1)*adminPageSize)
	if err != nil {
		return databaseReadError(c, "list users", err)
	}
	view.HasNext = len(view.Users) > adminPageSize
	if view.HasNext {
		view.Users = view.Users[:adminPageSize]
	}
	return c.Render(http.StatusOK, "admin", view)
}

func adminUserDetails(c echo.Context) error {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id < 1 {
		return echo.NewHTTPError(http.StatusNotFound, "user not found")
	}
	view := adminPage{Data: c.Get("data").(*util.Data)}
	view.Active = "admin"
	err = database.Db.Get(&view.SelectedUser, `
		SELECT id, name, username, email, created_at, email_verified, is_admin, blocked_at, blocked_reason
		FROM users WHERE id = $1`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return echo.NewHTTPError(http.StatusNotFound, "user not found")
	}
	if err != nil {
		return databaseReadError(c, "load admin user", err)
	}
	if err := database.Db.Select(&view.Events, `
		SELECT id, kind, status, user_id, actor_user_id, subject, detail, item_count, created_at
		FROM admin_events WHERE user_id = $1 ORDER BY created_at DESC, id DESC LIMIT 50`, id); err != nil {
		return databaseReadError(c, "load user events", err)
	}
	return c.Render(http.StatusOK, "admin-user", view)
}

func adminEvents(c echo.Context) error {
	view := adminPage{Data: c.Get("data").(*util.Data), Page: adminPageNumber(c)}
	view.Active = "admin"
	view.Kind = c.QueryParam("kind")
	switch view.Kind {
	case "exchange_rate", "session_cleanup", "email", "auth_login", "auth_logout", "user_block", "user_unblock":
	default:
		view.Kind = ""
	}
	view.EventStatus = c.QueryParam("status")
	switch view.EventStatus {
	case "success", "failed", "smtp_accepted", "smtp_unconfirmed", "smtp_pending":
	default:
		view.EventStatus = ""
	}
	var userID *int
	if value := c.QueryParam("user_id"); value != "" {
		id, err := strconv.Atoi(value)
		if err != nil || id < 1 {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid user ID")
		}
		userID = &id
		view.UserFilter = value
	}
	location, err := time.LoadLocation("Europe/Belgrade")
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "could not load time zone").SetInternal(err)
	}
	parseDate := func(value string) (*time.Time, error) {
		if value == "" {
			return nil, nil
		}
		date, err := time.ParseInLocation("2006-01-02", value, location)
		if err != nil || date.Format("2006-01-02") != value {
			return nil, echo.NewHTTPError(http.StatusBadRequest, "invalid date")
		}
		utc := date.UTC()
		return &utc, nil
	}
	view.FromDate, view.ToDate = c.QueryParam("from"), c.QueryParam("to")
	from, err := parseDate(view.FromDate)
	if err != nil {
		return err
	}
	to, err := parseDate(view.ToDate)
	if err != nil {
		return err
	}
	if to != nil {
		end, _ := time.ParseInLocation("2006-01-02", view.ToDate, location)
		nextDay := end.AddDate(0, 0, 1).UTC()
		to = &nextDay
	}
	if err := database.Db.Select(&view.Events, `
		SELECT id, kind, status, user_id, actor_user_id, subject, detail, item_count, created_at
		FROM admin_events WHERE ($1 = '' OR kind = $1)
		AND ($2 = '' OR status = $2)
		AND ($3::integer IS NULL OR user_id = $3)
		AND ($4::timestamptz IS NULL OR created_at >= $4)
		AND ($5::timestamptz IS NULL OR created_at < $5)
		ORDER BY created_at DESC, id DESC LIMIT $6 OFFSET $7`,
		view.Kind, view.EventStatus, userID, from, to, adminPageSize+1, (view.Page-1)*adminPageSize); err != nil {
		return databaseReadError(c, "list admin events", err)
	}
	view.HasNext = len(view.Events) > adminPageSize
	if view.HasNext {
		view.Events = view.Events[:adminPageSize]
	}
	return c.Render(http.StatusOK, "admin-events", view)
}

func changeUserBlock(c echo.Context, block bool) error {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id < 1 {
		return echo.NewHTTPError(http.StatusNotFound, "user not found")
	}
	actorID := c.Get("data").(*util.Data).User.Id
	if id == actorID {
		return echo.NewHTTPError(http.StatusForbidden, "cannot block your own account")
	}
	reason := strings.TrimSpace(c.FormValue("reason"))
	if block && (reason == "" || len([]rune(reason)) > 500) {
		return echo.NewHTTPError(http.StatusBadRequest, "a reason up to 500 characters is required")
	}
	tx, err := database.Db.Beginx()
	if err != nil {
		return databaseWriteError(c, "begin user block", err)
	}
	defer tx.Rollback()
	user := adminUser{}
	if err := tx.Get(&user, `SELECT id, username, is_admin, blocked_at FROM users WHERE id = $1 FOR UPDATE`, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return echo.NewHTTPError(http.StatusNotFound, "user not found")
		}
		return databaseReadError(c, "load user to block", err)
	}
	if user.IsAdmin {
		return echo.NewHTTPError(http.StatusForbidden, "admin accounts cannot be blocked here")
	}
	status := "success"
	kind := "user_unblock"
	if block {
		kind = "user_block"
		if user.BlockedAt != nil {
			return echo.NewHTTPError(http.StatusConflict, "user is already blocked")
		}
		if _, err := tx.Exec(`UPDATE users SET blocked_at = NOW(), blocked_reason = $1 WHERE id = $2`, reason, id); err != nil {
			return databaseWriteError(c, "block user", err)
		}
		if _, err := tx.Exec(`DELETE FROM sessions WHERE user_id = $1`, id); err != nil {
			return databaseWriteError(c, "revoke user sessions", err)
		}
	} else {
		if user.BlockedAt == nil {
			return echo.NewHTTPError(http.StatusConflict, "user is not blocked")
		}
		if _, err := tx.Exec(`UPDATE users SET blocked_at = NULL, blocked_reason = NULL WHERE id = $1`, id); err != nil {
			return databaseWriteError(c, "unblock user", err)
		}
	}
	if _, err := tx.Exec(`INSERT INTO admin_events (kind, status, user_id, actor_user_id, subject, detail)
		VALUES ($1, $2, $3, $4, $5, $6)`, kind, status, id, actorID, user.Username, reason); err != nil {
		return databaseWriteError(c, "record user block", err)
	}
	if err := tx.Commit(); err != nil {
		return databaseWriteError(c, "commit user block", err)
	}
	return c.Redirect(http.StatusSeeOther, "/admin/users/"+strconv.Itoa(id))
}

func DefineAdminRoutes() {
	E.GET("/admin", adminDashboard, adminOnly)
	E.GET("/admin/users/:id", adminUserDetails, adminOnly)
	E.POST("/admin/users/:id/block", func(c echo.Context) error { return changeUserBlock(c, true) }, adminOnly)
	E.POST("/admin/users/:id/unblock", func(c echo.Context) error { return changeUserBlock(c, false) }, adminOnly)
	E.GET("/admin/events", adminEvents, adminOnly)
}
