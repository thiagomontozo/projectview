package repo

import (
	"strings"
	"testing"
)

// The sort allow-list is the only thing standing between a query string and an
// ORDER BY clause, so what it does and does not contain is worth asserting
// rather than assuming.
func TestSortableTaskColumnsCoverTheViews(t *testing.T) {
	// Every sort the interface offers has to exist here, or choosing it in the
	// toolbar would silently fall back to the default ordering - a control that
	// appears to work and does nothing, which is the exact class of defect the
	// browser tests were added for.
	for _, field := range []string{"position", "title", "dueDate", "priority", "status", "created"} {
		if _, ok := SortableTaskColumns[field]; !ok {
			t.Errorf("the views can sort by %q and the server cannot", field)
		}
	}
}

func TestPriorityAndStatusSortsEncodeTheirRules(t *testing.T) {
	// Alphabetically, "urgent" sorts last of the four. The board has always
	// shown it first, so the ordering is severity rather than the text.
	priority := SortableTaskColumns["priority"]
	urgent := strings.Index(priority, "'urgent'")
	low := strings.Index(priority, "ELSE")
	if urgent < 0 || low < 0 || urgent > low {
		t.Errorf("priority sort does not order by severity: %s", priority)
	}

	// Sorting by status has to follow the project's own column order, or a
	// board sorted by status disagrees with the columns it is drawn in.
	if status := SortableTaskColumns["status"]; !strings.Contains(status, "project_statuses") {
		t.Errorf("status sort ignores the project's column order: %s", status)
	}
}

// The ordering rules moved here from the client when paging did, so they are
// asserted here now. With only a page in hand the client cannot order what it
// cannot see, and a rule kept in one place and not the other would make the
// first page of a sort disagree with the second.
func TestTaskOrderBy(t *testing.T) {
	// Unscheduled tasks sort last whichever way the arrow points. PostgreSQL
	// would put them first when descending, which is the case worth pinning.
	for _, desc := range []bool{false, true} {
		got := taskOrderBy("t.due_date", desc)
		if !strings.Contains(got, "NULLS LAST") {
			t.Errorf("desc=%v: a task with no due date must sort last, got %q", desc, got)
		}
	}

	if got := taskOrderBy("t.title", false); !strings.HasSuffix(got, ", t.id ASC") {
		t.Errorf("the order must be total or paging repeats rows, got %q", got)
	}

	if got := taskOrderBy("t.title", true); !strings.Contains(got, "t.title DESC") {
		t.Errorf("descending was not applied: %q", got)
	}

	// No sort means the ordering the cursor pagination was built around, which
	// the search endpoint still uses.
	if got := taskOrderBy("", false); got != "t.created_at DESC, t.id DESC" {
		t.Errorf("the default ordering changed, which would break cursors: %q", got)
	}
}

// The filter body is shared by the listing and both counts. If they ever stop
// sharing it, a column can report a total its pages never reach.
func TestFilterSQLIsSharedByListingAndCounts(t *testing.T) {
	for _, clause := range []string{
		"COALESCE(cardinality($2::uuid[]), 0) = 0", // no assignee filter means everyone
		"COALESCE(cardinality($3::text[]), 0) = 0", // no status filter means every column
		"COALESCE(cardinality($4::text[]), 0) = 0", // no priority filter means all four
		"t.status = ANY($3)",                       // and a filter matches any of them
		"websearch_to_tsquery",                     // search still goes through the index
	} {
		if !strings.Contains(taskFilterSQL, clause) {
			t.Errorf("the shared filter body is missing %q", clause)
		}
	}
}

// An empty filter must mean "no constraint", never "match nothing".
//
// This guards a bug that shipped in the first draft of this change and made
// every unfiltered listing return zero rows. A nil Go slice reaches PostgreSQL
// as NULL, not as an empty array, so "cardinality($3) = 0" evaluated to NULL
// rather than true, the guard did not hold, and the filter applied against an
// empty set. The COALESCE is the fix, and this test is here because the failure
// mode is silent: nothing errors, the endpoint answers 200, and every board is
// empty.
func TestEmptyFilterGuardsSurviveANullArray(t *testing.T) {
	for _, guard := range []string{
		"COALESCE(cardinality($2::uuid[]), 0) = 0",
		"COALESCE(cardinality($3::text[]), 0) = 0",
		"COALESCE(cardinality($4::text[]), 0) = 0",
	} {
		if !strings.Contains(taskFilterSQL, guard) {
			t.Errorf("missing %q - a nil slice would arrive as NULL and match nothing", guard)
		}
	}

	if q := (TaskQuery{}); len(q.Statuses) != 0 || len(q.Priorities) != 0 || len(q.AssigneeIDs) != 0 {
		t.Error("the zero query should carry no filters")
	}
}
