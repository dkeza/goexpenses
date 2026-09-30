package routes

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"goexpenses/util"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/labstack/echo/v4"
)

func TestAdminOnlyChecksRoleOnEveryRequest(t *testing.T) {
	for _, tc := range []struct {
		name string
		user util.User
		want int
	}{
		{"anonymous", util.User{}, http.StatusSeeOther},
		{"regular user", util.User{Id: 4}, http.StatusForbidden},
		{"admin", util.User{Id: 7, IsAdmin: true}, http.StatusNoContent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := echo.New().NewContext(httptest.NewRequest(http.MethodGet, "/admin", nil), httptest.NewRecorder())
			c.Set("data", &util.Data{User: tc.user})
			err := adminOnly(func(c echo.Context) error { return c.NoContent(http.StatusNoContent) })(c)
			if tc.want == http.StatusForbidden {
				if httpError, ok := err.(*echo.HTTPError); !ok || httpError.Code != tc.want {
					t.Fatalf("adminOnly error = %v, want 403", err)
				}
				return
			}
			if err != nil || c.Response().Status != tc.want {
				t.Fatalf("adminOnly = %d, %v; want %d", c.Response().Status, err, tc.want)
			}
			if tc.name == "anonymous" && c.Response().Header().Get(echo.HeaderLocation) != "/login?next=/admin" {
				t.Fatalf("anonymous redirect = %q", c.Response().Header().Get(echo.HeaderLocation))
			}
		})
	}
}

func TestBlockUserRevokesSessionsAndRecordsReasonAtomically(t *testing.T) {
	mock, _ := useMockRouteDatabase(t)
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, username, is_admin, blocked_at FROM users WHERE id = $1 FOR UPDATE`)).
		WithArgs(12).WillReturnRows(sqlmock.NewRows([]string{"id", "username", "is_admin", "blocked_at"}).AddRow(12, "target", false, nil))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE users SET blocked_at = NOW(), blocked_reason = $1 WHERE id = $2`)).
		WithArgs("Policy violation", 12).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM sessions WHERE user_id = $1`)).
		WithArgs(12).WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO admin_events (kind, status, user_id, actor_user_id, subject, detail)
		VALUES ($1, $2, $3, $4, $5, $6)`)).
		WithArgs("user_block", "success", 12, 7, "target", "Policy violation").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	form := url.Values{"reason": {"Policy violation"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/users/12/block", strings.NewReader(form.Encode()))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	c := echo.New().NewContext(req, httptest.NewRecorder())
	c.SetParamNames("id")
	c.SetParamValues("12")
	c.Set("data", &util.Data{User: util.User{Id: 7, IsAdmin: true}})
	if err := changeUserBlock(c, true); err != nil {
		t.Fatal(err)
	}
	if c.Response().Status != http.StatusSeeOther {
		t.Fatalf("response = %d, want redirect", c.Response().Status)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func deleteUserTestContext(id, confirmedName string, actorID int) echo.Context {
	form := url.Values{"confirm_username": {confirmedName}}
	req := httptest.NewRequest(http.MethodPost, "/admin/users/"+id+"/delete", strings.NewReader(form.Encode()))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	c := echo.New().NewContext(req, httptest.NewRecorder())
	c.SetParamNames("id")
	c.SetParamValues(id)
	c.Set("data", &util.Data{User: util.User{Id: actorID, IsAdmin: true}})
	return c
}

func TestDeleteUserRemovesExclusiveAccountsAndPreservesSharedAccounts(t *testing.T) {
	mock, _ := useMockRouteDatabase(t)
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT username, email, is_admin FROM users WHERE id = $1 FOR UPDATE`)).
		WithArgs(12).WillReturnRows(sqlmock.NewRows([]string{"username", "email", "is_admin"}).AddRow("target", "target@example.com", false))
	mock.ExpectQuery(`SELECT id FROM accounts WHERE id IN`).WithArgs(12).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(21).AddRow(22))
	mock.ExpectExec(`DELETE FROM sessions WHERE user_id`).WithArgs(12).WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec(`DELETE FROM passwordresets WHERE`).WithArgs("target@example.com").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`DELETE FROM accountsusers WHERE`).WithArgs(12).WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec(`DELETE FROM users WHERE`).WithArgs(12).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`SELECT NOT EXISTS`).WithArgs(21).WillReturnRows(sqlmock.NewRows([]string{"exclusive"}).AddRow(true))
	for _, table := range []string{"posts", "expenses", "incomes"} {
		mock.ExpectExec(`DELETE FROM ` + table + ` WHERE accounts_id`).WithArgs(21).WillReturnResult(sqlmock.NewResult(0, 1))
	}
	mock.ExpectExec(`DELETE FROM accounts WHERE`).WithArgs(21).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`SELECT NOT EXISTS`).WithArgs(22).WillReturnRows(sqlmock.NewRows([]string{"exclusive"}).AddRow(false))
	mock.ExpectExec(`UPDATE admin_events SET subject`).WithArgs(12, "target@example.com").WillReturnResult(sqlmock.NewResult(0, 3))
	mock.ExpectExec(`INSERT INTO admin_events`).WithArgs(12, 7, 1).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	c := deleteUserTestContext("12", "target", 7)
	if err := deleteUser(c); err != nil {
		t.Fatal(err)
	}
	if c.Response().Status != http.StatusSeeOther || c.Response().Header().Get(echo.HeaderLocation) != "/admin?deleted=1" {
		t.Fatalf("delete response = %d, %q", c.Response().Status, c.Response().Header().Get(echo.HeaderLocation))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteUserRejectsInvalidTargetsAndConfirmation(t *testing.T) {
	for _, tt := range []struct {
		name      string
		id        string
		confirmed string
		admin     bool
		want      int
	}{
		{"invalid ID", "bad", "target", false, http.StatusNotFound},
		{"self", "7", "target", false, http.StatusForbidden},
		{"missing user", "13", "target", false, http.StatusNotFound},
		{"admin", "12", "target", true, http.StatusForbidden},
		{"wrong name", "12", "wrong", false, http.StatusBadRequest},
	} {
		t.Run(tt.name, func(t *testing.T) {
			mock, _ := useMockRouteDatabase(t)
			if tt.id == "12" || tt.id == "13" {
				mock.ExpectBegin()
				rows := sqlmock.NewRows([]string{"username", "email", "is_admin"})
				if tt.id == "12" {
					rows.AddRow("target", "target@example.com", tt.admin)
				}
				id := 12
				if tt.id == "13" {
					id = 13
				}
				mock.ExpectQuery(`SELECT username, email, is_admin FROM users`).WithArgs(id).WillReturnRows(rows)
				mock.ExpectRollback()
			}
			err := deleteUser(deleteUserTestContext(tt.id, tt.confirmed, 7))
			if httpError, ok := err.(*echo.HTTPError); !ok || httpError.Code != tt.want {
				t.Fatalf("deleteUser error = %v, want %d", err, tt.want)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDeleteUserRollsBackWhenAccountDataCannotBeDeleted(t *testing.T) {
	mock, _ := useMockRouteDatabase(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT username, email, is_admin FROM users`).WithArgs(12).
		WillReturnRows(sqlmock.NewRows([]string{"username", "email", "is_admin"}).AddRow("target", "target@example.com", false))
	mock.ExpectQuery(`SELECT id FROM accounts WHERE id IN`).WithArgs(12).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(21))
	mock.ExpectExec(`DELETE FROM sessions WHERE user_id`).WithArgs(12).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`DELETE FROM passwordresets WHERE`).WithArgs("target@example.com").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`DELETE FROM accountsusers WHERE`).WithArgs(12).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`DELETE FROM users WHERE`).WithArgs(12).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`SELECT NOT EXISTS`).WithArgs(21).WillReturnRows(sqlmock.NewRows([]string{"exclusive"}).AddRow(true))
	mock.ExpectExec(`DELETE FROM posts WHERE accounts_id`).WithArgs(21).WillReturnError(errors.New("database failure"))
	mock.ExpectRollback()

	err := deleteUser(deleteUserTestContext("12", "target", 7))
	if httpError, ok := err.(*echo.HTTPError); !ok || httpError.Code != http.StatusInternalServerError {
		t.Fatalf("deleteUser error = %v, want 500", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
