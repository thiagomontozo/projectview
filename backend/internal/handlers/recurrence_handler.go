package handlers

import (
	"net/http"
	"time"

	"github.com/google/uuid"

	"projectview/internal/audit"
	"projectview/internal/auth"
	"projectview/internal/httpx"
	"projectview/internal/logger"
	"projectview/internal/models"
	"projectview/internal/repo"
	"projectview/internal/services"
)

// Recurring tasks.
//
// The rule sits on the task that currently carries it, so a caller reads and
// writes it through that task. Completing a task in `on_complete` mode spawns
// the next instance; `on_schedule` leaves that to the sweeper.

type recurrenceRequest struct {
	Frequency      string     `json:"frequency"`
	IntervalCount  int        `json:"intervalCount"`
	Mode           string     `json:"mode"`
	UntilDate      *time.Time `json:"untilDate"`
	MaxOccurrences *int       `json:"maxOccurrences"`
}

// GET /api/tasks/:id/recurrence
func (a *API) GetRecurrence(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.UUIDParam(w, r, "id")
	if !ok {
		return
	}
	if _, err := a.Tasks.ByID(r.Context(), id); err != nil {
		respondRepoError(w, err, "Task not found.")
		return
	}

	rule, err := a.Recurrences.ByTask(r.Context(), id)
	if err != nil {
		// Not recurring is an ordinary answer, not a missing resource.
		httpx.JSON(w, http.StatusOK, nil)
		return
	}
	httpx.JSON(w, http.StatusOK, rule)
}

// PUT /api/tasks/:id/recurrence
func (a *API) SetRecurrence(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.UUIDParam(w, r, "id")
	if !ok {
		return
	}
	task, _, ok := a.requireTaskWork(w, r, id)
	if !ok {
		return
	}

	var req recurrenceRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if !services.ValidFrequency(req.Frequency) {
		httpx.Error(w, http.StatusBadRequest, "Frequency must be daily, weekly or monthly.")
		return
	}
	if req.Mode == "" {
		req.Mode = services.ModeOnComplete
	}
	if !services.ValidRecurrenceMode(req.Mode) {
		httpx.Error(w, http.StatusBadRequest, "Mode must be on_complete or on_schedule.")
		return
	}
	if req.IntervalCount == 0 {
		req.IntervalCount = 1
	}
	if req.IntervalCount < 1 || req.IntervalCount > 52 {
		httpx.Error(w, http.StatusBadRequest, "The interval must be between 1 and 52.")
		return
	}
	if req.MaxOccurrences != nil && *req.MaxOccurrences < 1 {
		httpx.Error(w, http.StatusBadRequest, "A maximum of zero occurrences would never run.")
		return
	}

	rule := &repo.Recurrence{
		TaskID: id, Frequency: req.Frequency, IntervalCount: req.IntervalCount,
		Mode: req.Mode, UntilDate: req.UntilDate, MaxOccurrences: req.MaxOccurrences,
		CreatedBy: &auth.CurrentUser(r).ID,
	}

	// A scheduled series needs a moment to fire at. The task's own due date is
	// the only honest anchor; without one there is nothing to be late for, so
	// the mode falls back to completion-driven rather than guessing a date.
	if rule.Mode == services.ModeOnSchedule {
		if task.DueDate == nil {
			httpx.Error(w, http.StatusBadRequest,
				"A task with no due date cannot recur on a schedule. Give it a due date, or let it repeat when completed.")
			return
		}
		rule.NextRunAt = task.DueDate
	}

	if err := a.Recurrences.Set(r.Context(), rule); err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	a.Audit.Record(r, auth.CurrentUser(r), audit.Event{
		Action: audit.ActionTaskUpdated, ResourceType: "task", ResourceID: id.String(),
		Changes: map[string]any{"recurrence": rule.Frequency, "mode": rule.Mode},
		Status:  http.StatusOK,
	})
	httpx.JSON(w, http.StatusOK, rule)
}

// DELETE /api/tasks/:id/recurrence
func (a *API) DeleteRecurrence(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.UUIDParam(w, r, "id")
	if !ok {
		return
	}
	if _, _, ok := a.requireTaskWork(w, r, id); !ok {
		return
	}
	if err := a.Recurrences.Delete(r.Context(), id); err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// spawnOnCompletion creates the next instance when a completion-driven series
// is finished.
//
// Called from the paths that can complete a task. It never fails the request
// that triggered it: somebody marking work done should not see an error because
// the *next* one could not be created, and the series is recoverable by hand
// while the completion is what they asked for.
func (a *API) spawnOnCompletion(r *http.Request, task *models.Task) {
	if a.Recurrences == nil || task == nil {
		return
	}
	ctx := r.Context()

	rule, err := a.Recurrences.ByTask(ctx, task.ID)
	if err != nil || rule.Mode != services.ModeOnComplete {
		return
	}

	now := time.Now()
	if rule.Exhausted(now) {
		// The series has reached its end. The rule is removed rather than left
		// on a finished task, so nothing suggests more is coming.
		if err := a.Recurrences.Delete(ctx, task.ID); err != nil {
			logger.Warn("could not clear an exhausted recurrence on %s: %v", task.ID, err)
		}
		return
	}

	next, err := a.taskFactory().SpawnNext(ctx, task, rule, now)
	if err != nil {
		logger.Error("could not spawn the next instance of %s: %v", task.ID, err)
		return
	}
	logger.Info("Recurrence: %s spawned %s", task.ID, next.ID)
}

func (a *API) taskFactory() services.TaskFactory {
	return services.TaskFactory{
		Tasks:        a.Tasks,
		CustomFields: a.CustomFields,
		Recurrences:  a.Recurrences,
	}
}

// decorateRecurrence attaches the rule to a task response, so a listing can
// mark which of its rows repeat.
func (a *API) recurrenceFor(r *http.Request, ids []uuid.UUID) map[uuid.UUID]repo.Recurrence {
	if a.Recurrences == nil || len(ids) == 0 {
		return map[uuid.UUID]repo.Recurrence{}
	}
	out, err := a.Recurrences.RecurrencesFor(r.Context(), ids)
	if err != nil {
		return map[uuid.UUID]repo.Recurrence{}
	}
	return out
}
