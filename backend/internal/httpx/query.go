package httpx

import (
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Listing parameters
// ==================
//
// Every listing endpoint used to return the entire table. With ten thousand
// tasks that is a slow query, a large response and a slow render - and the
// number only grows.
//
// Pagination is by cursor rather than OFFSET: an offset scan gets linearly
// slower the deeper you page, and rows shifting underneath you cause items to
// be skipped or repeated. A cursor anchors on the last row seen, so cost is
// constant and the sequence is stable.

// DefaultPageSize and MaxPageSize bound what a caller may request.
const (
	DefaultPageSize = 50
	MaxPageSize     = 200
)

// ListParams is the parsed form of the common listing query string.
type ListParams struct {
	Limit  int
	Cursor string
	Search string
	Sort   string
	Desc   bool
	// Filters holds the raw values of endpoint-specific filters, already
	// trimmed. Handlers validate the ones they support.
	Filters map[string]string
}

// ParseList reads limit/cursor/q/sort from the query string.
//
// sort accepts "field" or "-field", the leading minus meaning descending -
// the convention used by most JSON APIs.
func ParseList(r *http.Request, allowedSorts map[string]string, defaultSort string) ListParams {
	q := r.URL.Query()

	p := ListParams{
		Limit:   DefaultPageSize,
		Cursor:  q.Get("cursor"),
		Search:  strings.TrimSpace(q.Get("q")),
		Sort:    defaultSort,
		Filters: map[string]string{},
	}

	if raw := q.Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			p.Limit = min(n, MaxPageSize)
		}
	}

	if raw := strings.TrimSpace(q.Get("sort")); raw != "" {
		desc := strings.HasPrefix(raw, "-")
		field := strings.TrimPrefix(raw, "-")
		// Only known fields reach SQL. The map values are the column
		// expressions, so a caller can never inject one.
		if column, ok := allowedSorts[field]; ok {
			p.Sort = column
			p.Desc = desc
		}
	}

	for key, values := range q {
		switch key {
		case "limit", "cursor", "q", "sort":
			continue
		}
		if len(values) > 0 {
			p.Filters[key] = strings.TrimSpace(values[0])
		}
	}

	return p
}

// Page wraps a result set with the cursor needed to fetch the next one.
type Page[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"nextCursor,omitempty"`
	HasMore    bool   `json:"hasMore"`
	Total      *int64 `json:"total,omitempty"`
}

// NewPage trims an over-fetched slice down to limit and derives the cursor.
// Callers query limit+1 rows; the extra row is what proves more exist without
// a second COUNT query.
func NewPage[T any](rows []T, limit int, cursorOf func(T) string) Page[T] {
	page := Page[T]{Items: rows}
	if len(rows) > limit {
		page.Items = rows[:limit]
		page.HasMore = true
		page.NextCursor = cursorOf(page.Items[len(page.Items)-1])
	}
	if page.Items == nil {
		page.Items = []T{}
	}
	return page
}

// EncodeCursor packs the sort key and id of the last row into an opaque token.
// Opaque on purpose: clients that parse a cursor end up depending on the sort
// implementation, which then cannot change.
func EncodeCursor(sortValue, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(sortValue + "|" + id))
}

// DecodeCursor reverses EncodeCursor. ok is false for anything malformed,
// which callers treat as "start from the beginning" rather than an error.
func DecodeCursor(cursor string) (sortValue, id string, ok bool) {
	if cursor == "" {
		return "", "", false
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return "", "", false
	}
	parts := strings.SplitN(string(raw), "|", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// TimeCursor formats a timestamp for use inside a cursor, at a precision that
// round-trips through PostgreSQL.
func TimeCursor(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

// ParseTimeCursor reverses TimeCursor.
func ParseTimeCursor(s string) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}
