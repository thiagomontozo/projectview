package httpx

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestParseListDefaults(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/tasks", nil)
	p := ParseList(r, map[string]string{"createdAt": "created_at"}, "created_at")

	if p.Limit != DefaultPageSize {
		t.Errorf("Limit = %d, want %d", p.Limit, DefaultPageSize)
	}
	if p.Sort != "created_at" {
		t.Errorf("Sort = %q, want the default", p.Sort)
	}
	if p.Desc {
		t.Error("Desc should default to false")
	}
}

// An unbounded limit is what makes a listing endpoint a denial-of-service
// vector; the cap must hold no matter what is asked for.
func TestParseListClampsLimit(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/tasks?limit=100000", nil)
	if p := ParseList(r, nil, ""); p.Limit != MaxPageSize {
		t.Errorf("Limit = %d, want it clamped to %d", p.Limit, MaxPageSize)
	}

	// Nonsense falls back to the default rather than to zero, which would
	// return an empty page and look like "no data".
	for _, raw := range []string{"0", "-5", "abc", ""} {
		r := httptest.NewRequest("GET", "/api/tasks?limit="+raw, nil)
		if p := ParseList(r, nil, ""); p.Limit != DefaultPageSize {
			t.Errorf("limit=%q gave %d, want the default %d", raw, p.Limit, DefaultPageSize)
		}
	}
}

// Only known sort fields may reach SQL. This is the check that stops a caller
// from steering the ORDER BY clause.
func TestParseListRejectsUnknownSortFields(t *testing.T) {
	allowed := map[string]string{"createdAt": "created_at", "title": "title"}

	r := httptest.NewRequest("GET", "/api/tasks?sort=-createdAt", nil)
	p := ParseList(r, allowed, "created_at")
	if p.Sort != "created_at" || !p.Desc {
		t.Errorf("sort=-createdAt gave (%q, desc=%v)", p.Sort, p.Desc)
	}

	r = httptest.NewRequest("GET", "/api/tasks?sort=title", nil)
	p = ParseList(r, allowed, "created_at")
	if p.Sort != "title" || p.Desc {
		t.Errorf("sort=title gave (%q, desc=%v)", p.Sort, p.Desc)
	}

	// An injection attempt must be ignored, not passed through.
	r = httptest.NewRequest("GET", "/api/tasks?sort=title;DROP+TABLE+tasks", nil)
	p = ParseList(r, allowed, "created_at")
	if p.Sort != "created_at" {
		t.Errorf("unknown sort field leaked through as %q", p.Sort)
	}
}

func TestParseListReadsSearchAndFilters(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/tasks?q=%20deploy%20&status=done&priority=high", nil)
	p := ParseList(r, nil, "")

	if p.Search != "deploy" {
		t.Errorf("Search = %q, want it trimmed to %q", p.Search, "deploy")
	}
	if p.Filters["status"] != "done" || p.Filters["priority"] != "high" {
		t.Errorf("Filters = %v", p.Filters)
	}
	// Reserved keys must not leak into the filter map.
	for _, reserved := range []string{"limit", "cursor", "q", "sort"} {
		if _, present := p.Filters[reserved]; present {
			t.Errorf("reserved key %q leaked into Filters", reserved)
		}
	}
}

func TestCursorRoundTrip(t *testing.T) {
	cursor := EncodeCursor("2026-08-14T12:00:00Z", "abc-123")

	sortValue, id, ok := DecodeCursor(cursor)
	if !ok {
		t.Fatal("a cursor we produced did not decode")
	}
	if sortValue != "2026-08-14T12:00:00Z" || id != "abc-123" {
		t.Errorf("decoded (%q, %q)", sortValue, id)
	}
}

// A malformed cursor means "start from the beginning", not an error: clients
// keep stale cursors around, and a 500 for one is worse than a first page.
func TestDecodeCursorRejectsGarbageGracefully(t *testing.T) {
	for _, cursor := range []string{"", "!!!not-base64!!!", "bm8tc2VwYXJhdG9y"} {
		if _, _, ok := DecodeCursor(cursor); ok {
			t.Errorf("garbage cursor %q was accepted", cursor)
		}
	}
}

func TestTimeCursorRoundTrip(t *testing.T) {
	now := time.Date(2026, 8, 14, 12, 30, 45, 123456789, time.UTC)

	parsed, ok := ParseTimeCursor(TimeCursor(now))
	if !ok {
		t.Fatal("a timestamp we formatted did not parse back")
	}
	if !parsed.Equal(now) {
		t.Errorf("round trip lost precision: %v != %v", parsed, now)
	}
}

// The extra row fetched beyond the limit is how "is there more?" is answered
// without a second COUNT query.
func TestNewPageDetectsMoreUsingTheOverFetchedRow(t *testing.T) {
	rows := []string{"a", "b", "c", "d"}
	page := NewPage(rows, 3, func(s string) string { return "cursor-" + s })

	if len(page.Items) != 3 {
		t.Errorf("Items = %v, want the extra row trimmed", page.Items)
	}
	if !page.HasMore {
		t.Error("HasMore should be true when an extra row came back")
	}
	if page.NextCursor != "cursor-c" {
		t.Errorf("NextCursor = %q, want it derived from the last kept row", page.NextCursor)
	}
}

func TestNewPageOnTheLastPage(t *testing.T) {
	page := NewPage([]string{"a", "b"}, 3, func(s string) string { return s })

	if page.HasMore {
		t.Error("HasMore should be false when fewer rows than the limit came back")
	}
	if page.NextCursor != "" {
		t.Errorf("NextCursor = %q, want empty on the last page", page.NextCursor)
	}
}

// An empty result must serialize as [] rather than null.
func TestNewPageEmptyIsNotNull(t *testing.T) {
	page := NewPage([]string(nil), 10, func(s string) string { return s })
	if page.Items == nil {
		t.Error("Items is nil; it would marshal to null instead of []")
	}
}
