package routes

import (
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
