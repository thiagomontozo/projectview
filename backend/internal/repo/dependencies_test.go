package repo

import (
	"testing"

	"github.com/google/uuid"
)

// Builds a graph from a description: name -> (duration, blockers).
func graph(spec map[string]struct {
	duration float64
	blockers []string
}) (map[uuid.UUID]*ScheduleNode, []uuid.UUID, map[string]uuid.UUID) {
	ids := map[string]uuid.UUID{}
	for name := range spec {
		ids[name] = uuid.New()
	}

	nodes := map[uuid.UUID]*ScheduleNode{}
	order := []uuid.UUID{}
	// Deterministic order, so a failure is reproducible.
	for _, name := range sortedKeys(spec) {
		entry := spec[name]
		blockers := make([]uuid.UUID, 0, len(entry.blockers))
		for _, blocker := range entry.blockers {
			blockers = append(blockers, ids[blocker])
		}
		id := ids[name]
		nodes[id] = &ScheduleNode{ID: id, Title: name, Duration: entry.duration, Blockers: blockers}
		order = append(order, id)
	}
	return nodes, order, ids
}

func sortedKeys(m map[string]struct {
	duration float64
	blockers []string
}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[j] < keys[i] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	return keys
}

func names(path []uuid.UUID, nodes map[uuid.UUID]*ScheduleNode) []string {
	out := make([]string, 0, len(path))
	for _, id := range path {
		out = append(out, nodes[id].Title)
	}
	return out
}

type spec = map[string]struct {
	duration float64
	blockers []string
}

func TestCriticalPathPicksTheHeaviestChain(t *testing.T) {
	// Two routes to "ship". The longer one by duration is critical, even
	// though it has fewer steps — which is the whole point of weighting.
	//
	//   design(2) -> build(10) -> ship
	//   design(2) -> docs(1) -> review(1) -> ship
	nodes, order, _ := graph(spec{
		"design": {2, nil},
		"build":  {10, []string{"design"}},
		"docs":   {1, []string{"design"}},
		"review": {1, []string{"docs"}},
		"ship":   {1, []string{"build", "review"}},
	})

	path := names(longestPath(nodes, order), nodes)
	want := []string{"design", "build", "ship"}

	if len(path) != len(want) {
		t.Fatalf("critical path = %v, want %v", path, want)
	}
	for i := range want {
		if path[i] != want[i] {
			t.Fatalf("critical path = %v, want %v", path, want)
		}
	}
}

func TestCriticalPathIsOrderedFromStart(t *testing.T) {
	nodes, order, _ := graph(spec{
		"a": {1, nil},
		"b": {1, []string{"a"}},
		"c": {1, []string{"b"}},
	})

	path := names(longestPath(nodes, order), nodes)
	if len(path) != 3 || path[0] != "a" || path[2] != "c" {
		t.Errorf("path = %v, want it ordered a -> b -> c", path)
	}
}

// A project with no dependencies has no critical path in any useful sense.
// Returning the single longest task would light up something arbitrary.
func TestCriticalPathIsEmptyWithoutDependencies(t *testing.T) {
	nodes, order, _ := graph(spec{
		"one":   {5, nil},
		"two":   {3, nil},
		"three": {8, nil},
	})

	if path := longestPath(nodes, order); len(path) != 0 {
		t.Errorf("path = %v, want empty when nothing depends on anything", names(path, nodes))
	}
}

func TestCriticalPathHandlesAnEmptyProject(t *testing.T) {
	if path := longestPath(map[uuid.UUID]*ScheduleNode{}, nil); len(path) != 0 {
		t.Errorf("path = %v, want empty", path)
	}
}

// An edge pointing outside the loaded set (a task in another project, or one
// filtered out) must be skipped rather than treated as a zero-weight step,
// which would silently shorten the chain.
func TestCriticalPathIgnoresUnknownBlockers(t *testing.T) {
	nodes, order, _ := graph(spec{
		"a": {1, nil},
		"b": {5, []string{"a"}},
	})

	stranger := uuid.New()
	for _, node := range nodes {
		if node.Title == "b" {
			node.Blockers = append(node.Blockers, stranger)
		}
	}

	path := names(longestPath(nodes, order), nodes)
	if len(path) != 2 || path[0] != "a" || path[1] != "b" {
		t.Errorf("path = %v, want a -> b with the unknown blocker skipped", path)
	}
}

// Diamond: both branches reconverge. The memoisation must not double-count the
// shared prefix.
func TestCriticalPathHandlesDiamonds(t *testing.T) {
	nodes, order, _ := graph(spec{
		"start": {1, nil},
		"left":  {4, []string{"start"}},
		"right": {2, []string{"start"}},
		"end":   {1, []string{"left", "right"}},
	})

	path := names(longestPath(nodes, order), nodes)
	want := []string{"start", "left", "end"}
	if len(path) != 3 {
		t.Fatalf("path = %v, want %v", path, want)
	}
	for i := range want {
		if path[i] != want[i] {
			t.Fatalf("path = %v, want %v", path, want)
		}
	}
}

func TestValidFieldType(t *testing.T) {
	for _, valid := range []string{"text", "number", "date", "select", "multi_select", "checkbox", "url", "email", "user"} {
		if !ValidFieldType(valid) {
			t.Errorf("%q should be a valid field type", valid)
		}
	}
	for _, invalid := range []string{"", "richtext", "TEXT", "json"} {
		if ValidFieldType(invalid) {
			t.Errorf("%q should not be a valid field type", invalid)
		}
	}
}

func TestValidTrigger(t *testing.T) {
	for _, valid := range []string{"task.created", "task.status_changed", "task.assigned", "task.overdue", "task.due_soon"} {
		if !ValidTrigger(valid) {
			t.Errorf("%q should be a valid trigger", valid)
		}
	}
	for _, invalid := range []string{"", "task.deleted", "TASK.CREATED"} {
		if ValidTrigger(invalid) {
			t.Errorf("%q should not be a valid trigger", invalid)
		}
	}
}
