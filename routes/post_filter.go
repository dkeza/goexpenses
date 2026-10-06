package routes

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"goexpenses/database"
	"goexpenses/util"

	"github.com/labstack/echo/v4"
)

const postFilterTextMaxLength = 100

var (
	errPostFilterDate      = errors.New("invalid post filter date")
	errPostFilterDateOrder = errors.New("post filter starts after it ends")
	errPostFilterInvalid   = errors.New("invalid post filter")
)

// postFilter is the saved search of the posts list, stored as JSON on the
// account. Empty fields do not limit the list.
type postFilter struct {
	From string `json:"from,omitempty"`
	To   string `json:"to,omitempty"`
	Text string `json:"text,omitempty"`
	// Type is "expense" or "income" for all posts of that kind, or
	// "expense:<p_id>" or "income:<p_id>" for one expense or income type.
	Type string `json:"type,omitempty"`
}

// postFilterQuery is a validated postFilter, ready to limit a posts query.
type postFilterQuery struct {
	From       *time.Time
	To         *time.Time
	Text       string
	Kind       string // "", "expense" or "income"
	CategoryID string // p_id of one expense or income type of Kind
}

func parsePostFilterDate(value string) (time.Time, error) {
	for _, layout := range []string{"2006-01-02", "02.01.2006"} {
		if date, err := time.Parse(layout, value); err == nil {
			return date, nil
		}
	}
	return time.Time{}, errPostFilterDate
}

// query validates the filter. Either date may be left out for an open range;
// the end date includes the whole day.
func (f postFilter) query() (postFilterQuery, error) {
	query := postFilterQuery{Text: f.Text}
	if f.From != "" {
		from, err := parsePostFilterDate(f.From)
		if err != nil {
			return postFilterQuery{}, err
		}
		query.From = &from
	}
	if f.To != "" {
		to, err := parsePostFilterDate(f.To)
		if err != nil {
			return postFilterQuery{}, err
		}
		to = to.AddDate(0, 0, 1).Add(-time.Nanosecond)
		query.To = &to
	}
	if query.From != nil && query.To != nil && query.From.After(*query.To) {
		return postFilterQuery{}, errPostFilterDateOrder
	}
	if utf8.RuneCountInString(f.Text) > postFilterTextMaxLength {
		return postFilterQuery{}, errPostFilterInvalid
	}

	kind, categoryID, hasCategory := strings.Cut(f.Type, ":")
	if (kind != "" && kind != "expense" && kind != "income") || (hasCategory && categoryID == "") {
		return postFilterQuery{}, errPostFilterInvalid
	}
	query.Kind, query.CategoryID = kind, categoryID
	return query, nil
}

// escapeLikePattern makes LIKE wildcards in text match literally.
func escapeLikePattern(text string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(text)
}

// where returns the filter's conditions for a posts query in which the posts
// table is called table, and args extended with their values.
func (q postFilterQuery) where(table string, args []any) (string, []any) {
	arg := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}
	var conditions strings.Builder
	if q.From != nil {
		fmt.Fprintf(&conditions, " AND %s.created_at >= %s", table, arg(*q.From))
	}
	if q.To != nil {
		fmt.Fprintf(&conditions, " AND %s.created_at <= %s", table, arg(*q.To))
	}
	if q.Text != "" {
		fmt.Fprintf(&conditions, " AND %s.description ILIKE %s", table, arg("%"+escapeLikePattern(q.Text)+"%"))
	}
	if q.Kind != "" {
		// Kind is "expense" or "income", so it names the type table and the
		// post's column that refers to it.
		categories := q.Kind + "s"
		if q.CategoryID != "" {
			fmt.Fprintf(&conditions,
				" AND %[1]s.%[2]s_id IN (SELECT c.id FROM %[2]s c WHERE c.accounts_id = %[1]s.accounts_id AND c.p_id = %[3]s)",
				table, categories, arg(q.CategoryID))
		} else {
			fmt.Fprintf(&conditions, " AND %s.%s_id > 0", table, categories)
		}
	}
	return conditions.String(), args
}

// submittedPostFilter reads a filter sent from the filter form. The form
// always sends all fields, so an empty form clears the filter.
func submittedPostFilter(c echo.Context) (postFilter, bool) {
	params := c.QueryParams()
	submitted := params.Has("from") || params.Has("to") || params.Has("q") || params.Has("type")
	return postFilter{
		From: strings.TrimSpace(params.Get("from")),
		To:   strings.TrimSpace(params.Get("to")),
		Text: strings.TrimSpace(params.Get("q")),
		Type: params.Get("type"),
	}, submitted
}

// loadPostFilter returns the account's saved filter. A filter that can no
// longer be read is ignored.
func loadPostFilter(accountID int) (postFilter, postFilterQuery, error) {
	var saved string
	if err := database.Db.Get(&saved, `SELECT post_filter FROM accounts WHERE id = $1`, accountID); err != nil {
		return postFilter{}, postFilterQuery{}, err
	}
	filter := postFilter{}
	if saved == "" || json.Unmarshal([]byte(saved), &filter) != nil {
		return postFilter{}, postFilterQuery{}, nil
	}
	query, err := filter.query()
	if err != nil {
		return postFilter{}, postFilterQuery{}, nil
	}
	return filter, query, nil
}

// savePostFilter stores the filter on the account. The date range is also
// kept in fromdate and todate for an older application version.
func savePostFilter(accountID int, filter postFilter) error {
	saved := ""
	if filter != (postFilter{}) {
		encoded, err := json.Marshal(filter)
		if err != nil {
			return fmt.Errorf("encode post filter: %w", err)
		}
		saved = string(encoded)
	}
	fromDate, toDate := "", ""
	if filter.From != "" && filter.To != "" {
		fromDate, toDate = filter.From, filter.To
	}
	sql := `UPDATE accounts SET post_filter = $1, fromdate = $2, todate = $3 WHERE id = $4`
	return executeExactlyOne(database.Db, sql, saved, fromDate, toDate, accountID)
}

func postFilterErrorMessage(err error) string {
	switch {
	case errors.Is(err, errPostFilterDate):
		return "Invalid date!"
	case errors.Is(err, errPostFilterDateOrder):
		return "The start date is after the end date!"
	default:
		return "Invalid filter!"
	}
}

// postFilterView describes the filter for the filter form and the summary
// next to the page title.
func postFilterView(filter postFilter, query postFilterQuery, expenses []util.Expense, incomes []util.Income) util.PostFilterView {
	view := util.PostFilterView{
		From:   filter.From,
		To:     filter.To,
		Text:   filter.Text,
		Type:   filter.Type,
		Kind:   query.Kind,
		Active: filter != (postFilter{}),
	}
	// The form's date inputs need ISO dates; a legacy filter may hold others.
	if query.From != nil {
		view.From = query.From.Format("2006-01-02")
		view.FromLabel = query.From.Format("02.01.2006")
	}
	if query.To != nil {
		view.To = query.To.Format("2006-01-02")
		view.ToLabel = query.To.Format("02.01.2006")
	}
	switch query.Kind {
	case "expense":
		for _, expense := range expenses {
			if expense.Pid == query.CategoryID {
				view.CategoryLabel = expense.Description
			}
		}
	case "income":
		for _, income := range incomes {
			if income.Pid == query.CategoryID {
				view.CategoryLabel = income.Description
			}
		}
	}
	return view
}
