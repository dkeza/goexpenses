package routes

import (
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"goexpenses/util"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/labstack/echo/v4"
)

func TestLinkedPostWriteUsesPrimaryPostDate(t *testing.T) {
	selectedDate := time.Date(2026, time.March, 5, 9, 30, 0, 0, time.UTC)
	primary := postWrite{
		Description: "Rent",
		ExpenseID:   10,
		Amount:      500,
		Exchange:    117.2,
		AccountID:   3,
		PublicID:    "primary",
		CreatedAt:   selectedDate,
	}

	linked := linkedPostWrite(primary, 11, 25, "linked")

	want := postWrite{
		Description: "Rent",
		ExpenseID:   11,
		Amount:      25,
		Exchange:    117.2,
		AccountID:   3,
		PublicID:    "linked",
		CreatedAt:   selectedDate,
	}
	if linked != want {
		t.Fatalf("linkedPostWrite = %+v, want %+v", linked, want)
	}
}

type discardRenderer struct{}

func (discardRenderer) Render(io.Writer, string, interface{}, echo.Context) error { return nil }

// servePostsPage runs GET /posts for a signed-in user on account 7.
func servePostsPage(t *testing.T, target string) *httptest.ResponseRecorder {
	t.Helper()
	previousEcho, previousAuth := E, Auth
	E = echo.New()
	E.Renderer = discardRenderer{}
	Auth = func(next echo.HandlerFunc) echo.HandlerFunc { return next }
	t.Cleanup(func() { E, Auth = previousEcho, previousAuth })
	E.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			data := &util.Data{}
			data.User.Id = 3
			data.User.Default_accounts_id = 7
			c.Set("data", data)
			return next(c)
		}
	})
	DefinePosts()

	recorder := httptest.NewRecorder()
	E.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))
	return recorder
}

func expectPostsPageReads(mock sqlmock.Sqlmock, savedFilter string) {
	mock.ExpectQuery(regexp.QuoteMeta("SELECT post_filter FROM accounts WHERE id = $1")).WithArgs(7).
		WillReturnRows(sqlmock.NewRows([]string{"post_filter"}).AddRow(savedFilter))
	totals := sqlmock.NewRows([]string{"saldo", "saldoe", "income_saldo", "income_saldoe", "expense_saldo", "expense_saldoe"}).
		AddRow(0, 0, 0, 0, 0, 0)
	mock.ExpectQuery(`AS expense_saldoe\s+FROM posts`).WillReturnRows(totals)
	mock.ExpectQuery(`SELECT p_id, description\s+FROM expenses`).WithArgs(7).
		WillReturnRows(sqlmock.NewRows([]string{"p_id", "description"}))
	mock.ExpectQuery(`SELECT p_id, description FROM incomes`).WithArgs(7).
		WillReturnRows(sqlmock.NewRows([]string{"p_id", "description"}))
	mock.ExpectQuery(`FROM posts p\s+LEFT JOIN expenses`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
}

func TestPostsPageAppliesSavedFilterWithoutWriting(t *testing.T) {
	mock, _ := useMockRouteDatabase(t)
	expectPostsPageReads(mock, `{"from":"2026-09-01","to":"2026-09-30","text":"rent"}`)

	if recorder := servePostsPage(t, "/posts"); recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("database expectations: %v", err)
	}
}

func TestPostsPageSavesSubmittedFilter(t *testing.T) {
	mock, _ := useMockRouteDatabase(t)
	mock.ExpectExec(regexp.QuoteMeta("UPDATE accounts SET post_filter = $1, fromdate = $2, todate = $3 WHERE id = $4")).
		WithArgs(`{"from":"2026-09-01","to":"2026-09-30","text":"rent","type":"expense:fuel"}`, "2026-09-01", "2026-09-30", 7).
		WillReturnResult(sqlmock.NewResult(0, 1))

	recorder := servePostsPage(t, "/posts?from=2026-09-01&to=2026-09-30&q=+rent+&type=expense:fuel")
	if recorder.Code != http.StatusSeeOther || recorder.Header().Get("Location") != "/posts" {
		t.Fatalf("status = %d, location = %q", recorder.Code, recorder.Header().Get("Location"))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("database expectations: %v", err)
	}
}

func TestPostsPageClearsFilterFromEmptyForm(t *testing.T) {
	mock, _ := useMockRouteDatabase(t)
	mock.ExpectExec(regexp.QuoteMeta("UPDATE accounts SET post_filter = $1, fromdate = $2, todate = $3 WHERE id = $4")).
		WithArgs("", "", "", 7).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if recorder := servePostsPage(t, "/posts?from=&to=&q=&type="); recorder.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", recorder.Code)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("database expectations: %v", err)
	}
}

func TestPostsPageDoesNotSaveInvalidFilter(t *testing.T) {
	mock, _ := useMockRouteDatabase(t)
	mock.ExpectExec(regexp.QuoteMeta("UPDATE sessions SET message = $1")).
		WithArgs("The start date is after the end date!", 0, "", 0, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if recorder := servePostsPage(t, "/posts?from=2026-09-30&to=2026-09-01&q=&type="); recorder.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", recorder.Code)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("database expectations: %v", err)
	}
}
