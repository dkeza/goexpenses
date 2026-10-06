package routes

import (
	"errors"
	"testing"
	"time"
)

func TestPostFilterQueryAcceptsNativeAndLegacyDates(t *testing.T) {
	for _, filter := range []postFilter{
		{From: "2026-09-21", To: "2026-09-21"},
		{From: "21.09.2026", To: "21.09.2026"},
	} {
		query, err := filter.query()
		if err != nil {
			t.Fatalf("query(%+v): %v", filter, err)
		}
		if got := query.From.Format(time.RFC3339Nano); got != "2026-09-21T00:00:00Z" {
			t.Fatalf("from = %s", got)
		}
		if got := query.To.Format(time.RFC3339Nano); got != "2026-09-21T23:59:59.999999999Z" {
			t.Fatalf("to = %s", got)
		}
	}
}

func TestPostFilterQueryAcceptsOpenDateRange(t *testing.T) {
	query, err := postFilter{From: "2026-09-21"}.query()
	if err != nil || query.From == nil || query.To != nil {
		t.Fatalf("query = %+v, %v; want only a start date", query, err)
	}
	query, err = postFilter{To: "2026-09-21"}.query()
	if err != nil || query.From != nil || query.To == nil {
		t.Fatalf("query = %+v, %v; want only an end date", query, err)
	}
}

func TestPostFilterQueryRejectsInvalidFilters(t *testing.T) {
	for _, test := range []struct {
		filter postFilter
		want   error
	}{
		{postFilter{From: "21/09/2026"}, errPostFilterDate},
		{postFilter{From: "2026-09-22", To: "2026-09-21"}, errPostFilterDateOrder},
		{postFilter{Type: "transfer"}, errPostFilterInvalid},
		{postFilter{Type: "expense:"}, errPostFilterInvalid},
		{postFilter{Text: string(make([]byte, postFilterTextMaxLength+1))}, errPostFilterInvalid},
	} {
		if _, err := test.filter.query(); !errors.Is(err, test.want) {
			t.Fatalf("query(%+v) error = %v, want %v", test.filter, err, test.want)
		}
	}
}

func TestPostFilterQueryWhere(t *testing.T) {
	from := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)
	query := postFilterQuery{From: &from, Text: `50% off_\`, Kind: "income", CategoryID: "salary"}

	conditions, args := query.where("p", []any{7})

	want := " AND p.created_at >= $2" +
		" AND p.description ILIKE $3" +
		" AND p.incomes_id IN (SELECT c.id FROM incomes c WHERE c.accounts_id = p.accounts_id AND c.p_id = $4)"
	if conditions != want {
		t.Fatalf("conditions = %q, want %q", conditions, want)
	}
	if len(args) != 4 || args[0] != 7 || args[1] != from || args[2] != `%50\% off\_\\%` || args[3] != "salary" {
		t.Fatalf("args = %#v", args)
	}
}

func TestPostFilterQueryWhereForAllExpenses(t *testing.T) {
	conditions, args := postFilterQuery{Kind: "expense"}.where("posts", []any{7})
	if conditions != " AND posts.expenses_id > 0" || len(args) != 1 {
		t.Fatalf("conditions = %q, args = %#v", conditions, args)
	}
}
