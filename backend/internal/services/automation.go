package services

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"projectview/internal/logger"
	"projectview/internal/models"
	"projectview/internal/repo"
)

// Automation engine: trigger → condition → action.
//
// Rules are evaluated synchronously, right after the change that triggered
// them, but their failures never propagate: an automation that errors must not
// fail the user's request. Every evaluation is recorded — including the ones
// whose conditions did not hold — because an automation that silently does not
// fire is otherwise impossible to debug.
//
// Actions are deliberately a small closed set. A rule engine that can run
// arbitrary expressions becomes a scripting environment with none of the
// safeguards of one.
type AutomationEngine struct {
	automations *repo.Automations
	tasks       *repo.Tasks
	watchers    *repo.Watchers
	notifier    *Notifier
}

func NewAutomationEngine(
	automations *repo.Automations,
	tasks *repo.Tasks,
	watchers *repo.Watchers,
	notifier *Notifier,
) *AutomationEngine {
	return &AutomationEngine{automations: automations, tasks: tasks, watchers: watchers, notifier: notifier}
}

// Trigger names, mirroring the CHECK constraint on automations.trigger.
const (
	TriggerTaskCreated       = "task.created"
	TriggerTaskStatusChanged = "task.status_changed"
	TriggerTaskAssigned      = "task.assigned"
	TriggerTaskOverdue       = "task.overdue"
	TriggerTaskDueSoon       = "task.due_soon"
)

// Action types.
const (
	ActionSetStatus   = "set_status"
	ActionSetPriority = "set_priority"
	ActionAssign      = "assign"
	ActionNotify      = "notify"
	ActionWatch       = "watch"
)

// Event carries what a rule is evaluated against.
type Event struct {
	Trigger   string
	Task      *models.Task
	ProjectID uuid.UUID
	// ActorID is whoever caused the change; used to avoid notifying someone
	// about their own action.
	ActorID uuid.UUID
	// PreviousStatus is set for status_changed, so a rule can test what the
	// task moved *from*.
	PreviousStatus string
}

// Run evaluates every rule bound to the event's trigger.
//
// Errors are logged, never returned: the caller is in the middle of serving a
// request that has already succeeded, and failing it now would undo work the
// user can see was done.
func (e *AutomationEngine) Run(ctx context.Context, event Event) {
	if e == nil || e.automations == nil || event.Task == nil {
		return
	}

	rules, err := e.automations.MatchingTrigger(ctx, event.Trigger, event.ProjectID)
	if err != nil {
		logger.Error("automation lookup failed for %s: %v", event.Trigger, err)
		return
	}

	for _, rule := range rules {
		matched, reason := conditionsHold(rule.Conditions, event)
		if !matched {
			_ = e.automations.RecordRun(ctx, rule.ID, &event.Task.ID, "skipped", reason)
			continue
		}

		applied, err := e.applyActions(ctx, rule, event)
		switch {
		case err != nil:
			logger.Error("automation %q failed on task %s: %v", rule.Name, event.Task.ID, err)
			_ = e.automations.RecordRun(ctx, rule.ID, &event.Task.ID, "failed", err.Error())
		default:
			_ = e.automations.RecordRun(ctx, rule.ID, &event.Task.ID, "applied", strings.Join(applied, ", "))
		}
	}
}

// conditionsHold reports whether every condition matches, and why not when one
// does not — the reason is what makes the run log worth reading.
func conditionsHold(conditions []repo.Condition, event Event) (bool, string) {
	for _, condition := range conditions {
		actual := fieldValue(condition.Field, event)
		if !compare(actual, condition.Op, condition.Value) {
			return false, fmt.Sprintf("%s %s %q, got %q", condition.Field, condition.Op, condition.Value, actual)
		}
	}
	return true, "all conditions held"
}

func fieldValue(field string, event Event) string {
	task := event.Task
	switch field {
	case "status":
		return task.Status
	case "previous_status":
		return event.PreviousStatus
	case "priority":
		return task.Priority
	case "title":
		return task.Title
	case "assignee_count":
		return strconv.Itoa(len(task.Assignees))
	case "estimate_hours":
		return strconv.FormatFloat(task.EstimateHours, 'f', -1, 64)
	case "has_due_date":
		return strconv.FormatBool(task.DueDate != nil)
	case "is_overdue":
		return strconv.FormatBool(task.DueDate != nil && task.DueDate.Before(time.Now()) && task.Status != "done")
	default:
		return ""
	}
}

func compare(actual, op, expected string) bool {
	switch op {
	case "eq", "":
		return actual == expected
	case "neq":
		return actual != expected
	case "contains":
		return strings.Contains(strings.ToLower(actual), strings.ToLower(expected))
	case "gt", "lt":
		a, errA := strconv.ParseFloat(actual, 64)
		b, errB := strconv.ParseFloat(expected, 64)
		if errA != nil || errB != nil {
			return false
		}
		if op == "gt" {
			return a > b
		}
		return a < b
	case "is_empty":
		return strings.TrimSpace(actual) == ""
	case "is_not_empty":
		return strings.TrimSpace(actual) != ""
	default:
		return false
	}
}

// applyActions runs a rule's actions in order and returns what it did.
func (e *AutomationEngine) applyActions(ctx context.Context, rule repo.Automation, event Event) ([]string, error) {
	applied := []string{}
	task := event.Task

	for _, action := range rule.Actions {
		switch action.Type {
		case ActionSetStatus:
			if action.Status == "" || action.Status == task.Status {
				continue
			}
			// Guard against a rule triggered by status changes that sets the
			// status: without this the two would trigger each other forever.
			if event.Trigger == TriggerTaskStatusChanged && action.Status == event.PreviousStatus {
				applied = append(applied, "set_status skipped (would loop)")
				continue
			}
			status := action.Status
			if err := e.tasks.Update(ctx, task.ID, repo.TaskPatch{Status: &status}); err != nil {
				return applied, fmt.Errorf("set_status: %w", err)
			}
			applied = append(applied, "status="+status)

		case ActionSetPriority:
			if !models.ValidPriority(action.Priority) || action.Priority == task.Priority {
				continue
			}
			priority := action.Priority
			if err := e.tasks.Update(ctx, task.ID, repo.TaskPatch{Priority: &priority}); err != nil {
				return applied, fmt.Errorf("set_priority: %w", err)
			}
			applied = append(applied, "priority="+priority)

		case ActionAssign:
			userID, err := uuid.Parse(action.UserID)
			if err != nil {
				continue
			}
			already := false
			for _, existing := range task.Assignees {
				if existing == userID {
					already = true
					break
				}
			}
			if already {
				continue
			}
			assignees := append(append([]uuid.UUID{}, task.Assignees...), userID)
			if err := e.tasks.Update(ctx, task.ID, repo.TaskPatch{Assignees: &assignees}); err != nil {
				return applied, fmt.Errorf("assign: %w", err)
			}
			applied = append(applied, "assigned="+userID.String())

		case ActionWatch:
			userID, err := uuid.Parse(action.UserID)
			if err != nil {
				continue
			}
			if err := e.watchers.Add(ctx, task.ID, userID); err != nil {
				return applied, fmt.Errorf("watch: %w", err)
			}
			applied = append(applied, "watcher="+userID.String())

		case ActionNotify:
			recipients, err := e.watchers.Interested(ctx, task.ID)
			if err != nil {
				return applied, fmt.Errorf("notify: %w", err)
			}
			message := action.Message
			if message == "" {
				message = fmt.Sprintf("Automation %q ran on this task.", rule.Name)
			}
			projectID := event.ProjectID
			sent := 0
			for _, recipient := range recipients {
				// Nobody needs to be told about the consequence of their own
				// action.
				if recipient == event.ActorID {
					continue
				}
				if _, err := e.notifier.NotifyUser(ctx, NotifyInput{
					UserID:  recipient,
					Type:    models.NotifGeneral,
					Title:   rule.Name,
					Body:    message,
					Task:    &task.ID,
					Project: &projectID,
					Email:   false,
				}); err != nil {
					return applied, fmt.Errorf("notify: %w", err)
				}
				sent++
			}
			applied = append(applied, fmt.Sprintf("notified=%d", sent))

		default:
			applied = append(applied, "unknown action "+action.Type)
		}
	}

	return applied, nil
}
