package routes

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"goexpenses/database"
	"goexpenses/util"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo/v4"
)

func TestSelectAccount(t *testing.T) {
	const updateAccountQuery = `
		UPDATE users
		SET default_accounts_id = $1
		WHERE id = $2
		  AND EXISTS (
			SELECT 1
			FROM accountsusers au
			JOIN accounts a ON a.id = au.accounts_id
			WHERE au.accounts_id = $3
			  AND au.users_id = $4
			  AND a.deleted = 0
		)`

	tests := []struct {
		name         string
		accountID    string
		userID       int
		rowsAffected int64
		wantStatus   int
		wantRedirect string
		wantQuery    bool
	}{
		{
			name:         "member can select account",
			accountID:    "42",
			userID:       7,
			rowsAffected: 1,
			wantStatus:   http.StatusSeeOther,
			wantRedirect: "/posts",
			wantQuery:    true,
		},
		{
			name:         "non-member cannot select account",
			accountID:    "42",
			userID:       7,
			rowsAffected: 0,
			wantStatus:   http.StatusForbidden,
			wantQuery:    true,
		},
		{
			name:       "invalid account is rejected",
			accountID:  "not-an-id",
			userID:     7,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "anonymous user is rejected",
			accountID:  "42",
			wantStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("create mock database: %v", err)
			}
			defer db.Close()

			database.Db = sqlx.NewDb(db, "sqlmock")
			util.Settings.DatabaseType = "postgres"

			if tt.wantQuery {
				mock.ExpectExec(regexp.QuoteMeta(updateAccountQuery)).
					WithArgs(42, tt.userID, 42, tt.userID).
					WillReturnResult(sqlmock.NewResult(0, tt.rowsAffected))
			}

			form := url.Values{"accounts_id": {tt.accountID}}
			req := httptest.NewRequest(http.MethodPost, "/accounts/select", strings.NewReader(form.Encode()))
			req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
			recorder := httptest.NewRecorder()
			context := echo.New().NewContext(req, recorder)
			context.Set("data", &util.Data{User: util.User{Id: tt.userID}})

			err = selectAccount(context)
			if httpError, ok := err.(*echo.HTTPError); ok {
				if httpError.Code != tt.wantStatus {
					t.Fatalf("status = %d, want %d", httpError.Code, tt.wantStatus)
				}
			} else if err != nil {
				t.Fatalf("select account: %v", err)
			} else if recorder.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, tt.wantStatus)
			}

			if tt.wantRedirect != "" && recorder.Header().Get(echo.HeaderLocation) != tt.wantRedirect {
				t.Errorf("redirect = %q, want %q", recorder.Header().Get(echo.HeaderLocation), tt.wantRedirect)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("database expectations: %v", err)
			}
		})
	}
}
