package services

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"projectview/internal/models"
	"projectview/internal/repo"
)

func taskFixture() *models.Task {
	return &models.Task{
		ID:            uuid.New(),
		Title:         "Ship the release",
		Status:        "in_progress",
		Priority:      "medium",
		EstimateHours: 8,
		Assignees:     []uuid.UUID{uuid.New()},
	}
}

func TestCompare(t *testing.T) {
	cases := []struct {
		actual, op, expected string
		want                 bool
	}{
		{"done", "eq", "done", true},
		{"done", "eq", "todo", false},
		// An empty operator means equality: a condition written without one
		// must not silently match everything.
		{"done", "", "done", true},
		{"done", "", "todo", false},
		{"done", "neq", "todo", true},
		{"Ship the release", "contains", "ship", true},
		{"Ship the release", "contains", "SHIP", true},
		{"Ship the release", "contains", "deploy", false},
		{"5", "gt", "3", true},
		{"3", "gt", "5", false},
		{"3", "lt", "5", true},
		// Non-numeric operands must not compare true by accident.
		{"abc", "gt", "3", false},
		{"5", "gt", "abc", false},
		{"", "is_empty", "", true},
		{"   ", "is_empty", "", true},
		{"x", "is_empty", "", false},
		{"x", "is_not_empty", "", true},
		{"", "is_not_empty", "", false},
		// An operator nobody implemented must fail closed.
		{"anything", "regex", "anything", false},
	}

	for _, c := range cases {
		if got := compare(c.actual, c.op, c.expected); got != c.want {
			t.Errorf("compare(%q, %q, %q) = %v, want %v", c.actual, c.op, c.expected, got, c.want)
		}
	}
}

func TestFieldValue(t *testing.T) {
	task := taskFixture()
	past := time.Now().Add(-48 * time.Hour)
	task.DueDate = &past

	event := Event{Task: task, PreviousStatus: "todo"}

	cases := map[string]string{
		"status":          "in_progress",
		"previous_status": "todo",
		"priority":        "medium",
		"title":           "Ship the release",
		"assignee_count":  "1",
		"estimate_hours":  "8",
		"has_due_date":    "true",
		"is_overdue":      "true",
		// A field the engine does not know about resolves to empty rather
		// than matching something unrelated.
		"nonsense": "",
	}

	for field, want := range cases {
		if got := fieldValue(field, event); got != want {
			t.Errorf("fieldValue(%q) = %q, want %q", field, got, want)
		}
	}
}

func TestFieldValueOverdueIgnoresFinishedWork(t *testing.T) {
	task := taskFixture()
	past := time.Now().Add(-48 * time.Hour)
	task.DueDate = &past
	task.Status = "done"

	if got := fieldValue("is_overdue", Event{Task: task}); got != "false" {
		t.Errorf("a completed task past its date reported is_overdue=%q, want false", got)
	}
}

func TestConditionsHold(t *testing.T) {
	task := taskFixture()
	event := Event{Task: task, PreviousStatus: "todo"}

	t.Run("no conditions always match", func(t *testing.T) {
		if ok, _ := conditionsHold(nil, event); !ok {
			t.Error("a rule with no conditions should always run")
		}
	})

	t.Run("all conditions must hold", func(t *testing.T) {
		ok, _ := conditionsHold([]repo.Condition{
			{Field: "status", Op: "eq", Value: "in_progress"},
			{Field: "priority", Op: "eq", Value: "medium"},
		}, event)
		if !ok {
			t.Error("both matching conditions should hold")
		}
	})

	t.Run("one failing condition blocks the rule", func(t *testing.T) {
		ok, reason := conditionsHold([]repo.Condition{
			{Field: "status", Op: "eq", Value: "in_progress"},
			{Field: "priority", Op: "eq", Value: "urgent"},
		}, event)
		if ok {
			t.Error("a failing condition should block the rule")
		}
		// The reason is what makes a rule that did not fire debuggable.
		if reason == "" {
			t.Error("a skipped run should explain which condition failed")
		}
	})
}
