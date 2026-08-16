package services

import (
	"context"
	"time"

	"github.com/google/uuid"

	"projectview/internal/logger"
	"projectview/internal/models"
	"projectview/internal/repo"
)

// Creating work from a description of it.
//
// Recurrence and templates arrive at the same place from different directions:
// one copies a task forward in time, the other copies a shape into a project.
// They share this file so the two cannot drift into disagreeing about what a
// copied task carries - which is the bug that would show up as "my template
// loses the checklist" long after anybody remembers why.

// TaskFactory is the slice of the repositories this needs. Narrower than the
// whole API surface so the copy logic can be read without knowing the rest.
type TaskFactory struct {
	Tasks        *repo.Tasks
	CustomFields *repo.CustomFields
	Recurrences  *repo.Recurrences
}

// SpawnNext creates the next instance of a recurring task and moves the rule on
// to it.
//
// What is carried forward is the *definition* of the work - title, description,
// assignees, priority, estimate, tags and checklist - and what is not is
// everything that belongs to one occurrence: the completion stamp, the comments
// arguing about last month's numbers, the time logged against it. A recurring
// task that inherited last week's comments would make the history unreadable
// within a quarter.
//
// The checklist comes back unticked, which is the point of a checklist on a
// recurring task.
func (f TaskFactory) SpawnNext(
	ctx context.Context,
	source *models.Task,
	rule *repo.Recurrence,
	now time.Time,
) (*models.Task, error) {
	// The next dates keep the original's shape: the gap between start and due
	// is a property of the work, not of the week it happened in.
	var nextStart, nextDue *time.Time
	anchor := source.DueDate
	if anchor == nil {
		anchor = source.StartDate
	}
	if anchor == nil {
		anchor = &now
	}

	next := NextAfter(*anchor, now, rule.Frequency, rule.IntervalCount)
	if source.DueDate != nil {
		due := next
		nextDue = &due
	}
	if source.StartDate != nil {
		if source.DueDate != nil {
			// Preserve the original duration rather than the original dates.
			start := next.Add(source.StartDate.Sub(*source.DueDate))
			nextStart = &start
		} else {
			start := next
			nextStart = &start
		}
	}

	clone := &models.Task{
		ID:            uuid.New(),
		Title:         source.Title,
		Description:   source.Description,
		ProjectID:     source.ProjectID,
		ParentTask:    source.ParentTask,
		Assignees:     append([]uuid.UUID(nil), source.Assignees...),
		Priority:      source.Priority,
		EstimateHours: source.EstimateHours,
		Tags:          append([]string(nil), source.Tags...),
		StartDate:     nextStart,
		DueDate:       nextDue,
		CreatedBy:     source.CreatedBy,
	}
	// The new instance starts wherever the series was defined to start, not
	// wherever the finished one ended up. Re-opening in "done" would be absurd.
	clone.Status = firstStatusOr(source.Status)

	if err := f.Tasks.Create(ctx, clone); err != nil {
		return nil, err
	}
	if err := f.Tasks.SetRecurrenceParent(ctx, clone.ID, source.ID); err != nil {
		logger.Warn("could not record the recurrence parent of %s: %v", clone.ID, err)
	}

	// Checklist items are copied unticked.
	if len(source.Checklist) > 0 {
		fresh := make([]models.ChecklistItem, 0, len(source.Checklist))
		for _, item := range source.Checklist {
			fresh = append(fresh, models.ChecklistItem{ID: uuid.New(), Text: item.Text, Done: false})
		}
		if err := f.Tasks.Update(ctx, clone.ID, repo.TaskPatch{Checklist: &fresh}); err != nil {
			logger.Warn("could not copy the checklist onto %s: %v", clone.ID, err)
		}
	}

	if len(source.CustomFields) > 0 && f.CustomFields != nil {
		if err := f.CustomFields.SetValues(ctx, clone.ID, source.CustomFields); err != nil {
			logger.Warn("could not copy custom fields onto %s: %v", clone.ID, err)
		}
	}

	// The rule moves last, and in its own transaction, so a failure above never
	// leaves a series pointing at a task that was not created.
	var nextRun *time.Time
	if rule.Mode == ModeOnSchedule && nextDue != nil {
		nextRun = nextDue
	} else if rule.Mode == ModeOnSchedule {
		nextRun = &next
	}
	if err := f.Recurrences.MoveTo(ctx, source.ID, clone.ID, nextRun); err != nil {
		return clone, err
	}

	return clone, nil
}

// firstStatusOr is a placeholder for "wherever new work starts". The project's
// own first column would be better and needs the project loaded; the caller
// passes the source's status and the handler overrides it when it knows more.
func firstStatusOr(status string) string {
	if status == "" {
		return "todo"
	}
	return status
}

// ApplyTaskSpec creates one task from a template's description of it, plus its
// sub-tasks. Offsets are resolved against `at`, so a plan captured in March
// creates work dated from the day it is used.
func (f TaskFactory) ApplyTaskSpec(
	ctx context.Context,
	spec repo.TemplateTaskSpec,
	projectID uuid.UUID,
	parentID *uuid.UUID,
	defaultStatus string,
	createdBy uuid.UUID,
	at time.Time,
) (*models.Task, error) {
	status := spec.Status
	if status == "" {
		status = defaultStatus
	}
	priority := spec.Priority
	if !models.ValidPriority(priority) {
		priority = models.PriorityMedium
	}

	task := &models.Task{
		ID:            uuid.New(),
		Title:         spec.Title,
		Description:   spec.Description,
		ProjectID:     projectID,
		ParentTask:    parentID,
		Status:        status,
		Priority:      priority,
		EstimateHours: spec.EstimateHours,
		Tags:          append([]string(nil), spec.Tags...),
		Assignees:     []uuid.UUID{},
		StartDate:     offsetFrom(at, spec.StartOffsetDays),
		DueDate:       offsetFrom(at, spec.DueOffsetDays),
		CreatedBy:     &createdBy,
	}
	if err := f.Tasks.Create(ctx, task); err != nil {
		return nil, err
	}

	if len(spec.Checklist) > 0 {
		items := make([]models.ChecklistItem, 0, len(spec.Checklist))
		for _, text := range spec.Checklist {
			items = append(items, models.ChecklistItem{ID: uuid.New(), Text: text})
		}
		if err := f.Tasks.Update(ctx, task.ID, repo.TaskPatch{Checklist: &items}); err != nil {
			logger.Warn("could not apply the template checklist to %s: %v", task.ID, err)
		}
	}

	if len(spec.CustomFields) > 0 && f.CustomFields != nil {
		// Merged, so a field the project does not define is stored and simply
		// not shown rather than failing the whole application of the template.
		if err := f.CustomFields.SetValues(ctx, task.ID, spec.CustomFields); err != nil {
			logger.Warn("could not apply template fields to %s: %v", task.ID, err)
		}
	}

	for _, child := range spec.Subtasks {
		// One level only, matching the template model. A sub-task's own
		// sub-tasks are ignored rather than flattened into siblings, which
		// would silently change the shape somebody captured.
		child.Subtasks = nil
		if _, err := f.ApplyTaskSpec(ctx, child, projectID, &task.ID, status, createdBy, at); err != nil {
			logger.Warn("could not create a template sub-task under %s: %v", task.ID, err)
		}
	}

	return task, nil
}

func offsetFrom(at time.Time, days *int) *time.Time {
	if days == nil {
		return nil
	}
	t := at.AddDate(0, 0, *days)
	return &t
}
