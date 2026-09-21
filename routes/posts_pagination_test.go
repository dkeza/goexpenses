package routes

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"goexpenses/util"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/labstack/echo/v4"
)

func TestPostCursorRoundTrip(t *testing.T) {
	createdAt := time.Date(2026, time.September, 21, 12, 34, 56, 123456000, time.UTC)
	encoded, err := encodePostCursor(util.Post{Id: 42, DateTime: createdAt})
	if err != nil {
		t.Fatalf("encodePostCursor: %v", err)
	}

	cursor, err := decodePostCursor(encoded)
	if err != nil {
		t.Fatalf("decodePostCursor: %v", err)
	}
	if cursor.ID != 42 || !cursor.CreatedAt.Equal(createdAt) {
		t.Fatalf("decoded cursor = %+v, want ID 42 and time %v", cursor, createdAt)
	}
}

func TestDecodePostCursorRejectsInvalidValues(t *testing.T) {
	for _, encoded := range []string{"not-base64!", "e30"} {
		if _, err := decodePostCursor(encoded); err == nil {
			t.Fatalf("decodePostCursor(%q) accepted an invalid cursor", encoded)
		}
	}
}

func TestParsePostPageRequestRejectsTwoDirections(t *testing.T) {
	context := echo.New().NewContext(
		httptest.NewRequest(http.MethodGet, "/posts?after=a&before=b", nil),
		httptest.NewRecorder(),
	)
	if _, err := parsePostPageRequest(context); err == nil {
		t.Fatal("parsePostPageRequest accepted both cursor directions")
	}
}

func TestParsePostFilterRangeAcceptsNativeDateInput(t *testing.T) {
	from, to, err := parsePostFilterRange("2026-09-21", "2026-09-21")
	if err != nil {
		t.Fatalf("parsePostFilterRange: %v", err)
	}
	if got := from.Format(time.RFC3339Nano); got != "2026-09-21T00:00:00Z" {
		t.Fatalf("from = %s", got)
	}
	if got := to.Format(time.RFC3339Nano); got != "2026-09-21T23:59:59.999999999Z" {
		t.Fatalf("to = %s", got)
	}
}

func TestParsePostFilterRangeAcceptsLegacyDateInput(t *testing.T) {
	from, to, err := parsePostFilterRange("21.09.2026", "22.09.2026")
	if err != nil {
		t.Fatalf("parsePostFilterRange: %v", err)
	}
	if from.Day() != 21 || to.Day() != 22 {
		t.Fatalf("range = %v to %v", from, to)
	}
}

func TestParsePostFilterRangeRejectsInvalidInput(t *testing.T) {
	if _, _, err := parsePostFilterRange("2026-09-21", ""); err == nil {
		t.Fatal("parsePostFilterRange accepted an incomplete range")
	}
}

func TestLoadPostsPageUsesStableOrderAndOneExtraRow(t *testing.T) {
	mock, _ := useMockRouteDatabase(t)
	createdAt := time.Date(2026, time.September, 21, 12, 0, 0, 0, time.UTC)
	rows := sqlmock.NewRows([]string{
		"id", "description", "expense", "income", "date", "datetime", "created_ts", "amount", "amounte", "p_id",
	})
	for id := postsPageSize + 1; id > 0; id-- {
		rows.AddRow(id, "post", "expense", "", createdAt, createdAt, createdAt, 10, 0.1, "post-id")
	}
	mock.ExpectQuery(regexp.QuoteMeta("ORDER BY p.created_at DESC, p.id DESC")+`\s+`+regexp.QuoteMeta("LIMIT $2")).
		WithArgs(7, postsPageSize+1).
		WillReturnRows(rows)

	posts, hasMore, err := loadPostsPage(7, nil, nil, postPageRequest{})
	if err != nil {
		t.Fatalf("loadPostsPage: %v", err)
	}
	if !hasMore || len(posts) != postsPageSize {
		t.Fatalf("loadPostsPage returned %d posts, hasMore %v", len(posts), hasMore)
	}
	if posts[0].Id != postsPageSize+1 || posts[len(posts)-1].Id != 2 {
		t.Fatalf("loadPostsPage returned unstable order: first ID %d, last ID %d", posts[0].Id, posts[len(posts)-1].Id)
	}
}

func TestLoadPostsPageLoadsImmediateNewerPageWithinDateFilter(t *testing.T) {
	mock, _ := useMockRouteDatabase(t)
	filterFrom := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)
	filterTo := time.Date(2026, time.September, 30, 23, 59, 59, 0, time.UTC)
	cursorTime := time.Date(2026, time.September, 15, 12, 0, 0, 0, time.UTC)
	newerTime := cursorTime.Add(time.Hour)
	rows := sqlmock.NewRows([]string{
		"id", "description", "expense", "income", "date", "datetime", "created_ts", "amount", "amounte", "p_id",
	}).
		AddRow(9, "nearer", "expense", "", newerTime, newerTime, newerTime, 10, 0.1, "post-9").
		AddRow(10, "newest", "expense", "", newerTime.Add(time.Hour), newerTime.Add(time.Hour), newerTime.Add(time.Hour), 10, 0.1, "post-10")
	mock.ExpectQuery(`(?s)p\.created_at BETWEEN \$2 AND \$3.*p\.created_at > \$4 OR \(p\.created_at = \$5 AND p\.id > \$6\).*ORDER BY p\.created_at ASC, p\.id ASC\s+LIMIT \$7`).
		WithArgs(7, filterFrom, filterTo, cursorTime, cursorTime, 8, postsPageSize+1).
		WillReturnRows(rows)

	posts, hasMore, err := loadPostsPage(7, &filterFrom, &filterTo, postPageRequest{
		Direction: "before",
		Cursor:    postPageCursor{CreatedAt: cursorTime, ID: 8},
	})
	if err != nil {
		t.Fatalf("loadPostsPage: %v", err)
	}
	if hasMore || len(posts) != 2 {
		t.Fatalf("loadPostsPage returned %d posts, hasMore %v", len(posts), hasMore)
	}
	if posts[0].Id != 10 || posts[1].Id != 9 {
		t.Fatalf("newer page IDs = %d, %d; want 10, 9", posts[0].Id, posts[1].Id)
	}
}

func TestSetPostPaginationForOlderPage(t *testing.T) {
	posts := []util.Post{
		{Id: 10, DateTime: time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC)},
		{Id: 9, DateTime: time.Date(2026, 9, 21, 9, 0, 0, 0, time.UTC)},
	}
	data := &util.Data{}
	if err := setPostPagination(data, posts, postPageRequest{Direction: "after"}, true); err != nil {
		t.Fatalf("setPostPagination: %v", err)
	}
	if !data.Pagination.HasPrev || !data.Pagination.HasNext {
		t.Fatalf("pagination links = %+v, want both directions", data.Pagination)
	}

	previous, err := decodePostCursor(data.Pagination.PrevCursor)
	if err != nil || previous.ID != 10 {
		t.Fatalf("previous cursor = %+v, %v; want ID 10", previous, err)
	}
	next, err := decodePostCursor(data.Pagination.NextCursor)
	if err != nil || next.ID != 9 {
		t.Fatalf("next cursor = %+v, %v; want ID 9", next, err)
	}
}
