package repo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"projectview/internal/db"
)

// ---------------------------------------------------------------------------
// Custom fields
// ---------------------------------------------------------------------------

type CustomFields struct{ store *db.Store }

func NewCustomFields(store *db.Store) *CustomFields { return &CustomFields{store: store} }

// FieldDefinition describes one custom field. Scope is exactly one of project,
// space, or global (both nil).
type FieldDefinition struct {
	ID        uuid.UUID  `json:"id"`
	SpaceID   *uuid.UUID `json:"spaceId,omitempty"`
	ProjectID *uuid.UUID `json:"projectId,omitempty"`
	Key       string     `json:"key"`
	Label     string     `json:"label"`
	Type      string     `json:"type"`
	Options   []string   `json:"options"`
	Required  bool       `json:"required"`
	Position  int        `json:"position"`
	CreatedAt time.Time  `json:"createdAt"`
}

var fieldTypes = map[string]bool{
	"text": true, "number": true, "date": true, "select": true,
	"multi_select": true, "checkbox": true, "url": true, "email": true, "user": true,
}

func ValidFieldType(t string) bool { return fieldTypes[t] }

// ForProject returns every definition that applies to a project: its own, its
// space's, and the global ones — narrowest scope first, which is the order the
// form should show them in.
func (r *CustomFields) ForProject(ctx context.Context, projectID uuid.UUID) ([]FieldDefinition, error) {
	rows, err := r.store.Pool.Query(ctx, `
		SELECT f.id, f.space_id, f.project_id, f.key, f.label, f.type, f.options,
		       f.required, f.position, f.created_at
		  FROM custom_field_definitions f
		 WHERE f.project_id = $1
		    OR f.space_id = (SELECT space_id FROM projects WHERE id = $1)
		    OR (f.space_id IS NULL AND f.project_id IS NULL)
		 ORDER BY (f.project_id IS NULL), (f.space_id IS NULL), f.position, f.label`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []FieldDefinition{}
	for rows.Next() {
		var f FieldDefinition
		if err := rows.Scan(&f.ID, &f.SpaceID, &f.ProjectID, &f.Key, &f.Label, &f.Type,
			&f.Options, &f.Required, &f.Position, &f.CreatedAt); err != nil {
			return nil, err
		}
		if f.Options == nil {
			f.Options = []string{}
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (r *CustomFields) Create(ctx context.Context, f *FieldDefinition, createdBy uuid.UUID) error {
	if f.ID == uuid.Nil {
		f.ID = uuid.New()
	}
	if f.Options == nil {
		f.Options = []string{}
	}
	return r.store.Pool.QueryRow(ctx, `
		INSERT INTO custom_field_definitions
		    (id, space_id, project_id, key, label, type, options, required, position, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		RETURNING created_at`,
		f.ID, f.SpaceID, f.ProjectID, f.Key, f.Label, f.Type, f.Options,
		f.Required, f.Position, createdBy).Scan(&f.CreatedAt)
}

func (r *CustomFields) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.store.Pool.Exec(ctx, `DELETE FROM custom_field_definitions WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetValues merges values into a task's custom_fields. Merging rather than
// replacing means a client that knows about three fields cannot erase a fourth
// it has never heard of.
func (r *CustomFields) SetValues(ctx context.Context, taskID uuid.UUID, values map[string]any) error {
	tag, err := r.store.Pool.Exec(ctx, `
		UPDATE tasks
		   SET custom_fields = custom_fields || $2::jsonb, updated_at = now()
		 WHERE id = $1`, taskID, values)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ---------------------------------------------------------------------------
// Time tracking
// ---------------------------------------------------------------------------

// ErrTimerRunning is returned when someone starts a second timer. Enforced by
// a partial unique index; translated here so handlers answer 409.
var ErrTimerRunning = errors.New("a timer is already running for this user")

type TimeTracking struct{ store *db.Store }

func NewTimeTracking(store *db.Store) *TimeTracking { return &TimeTracking{store: store} }

type TimeEntry struct {
	ID        uuid.UUID  `json:"id"`
	TaskID    uuid.UUID  `json:"taskId"`
	UserID    uuid.UUID  `json:"userId"`
	StartedAt time.Time  `json:"startedAt"`
	EndedAt   *time.Time `json:"endedAt,omitempty"`
	Seconds   int64      `json:"seconds"`
	Note      string     `json:"note,omitempty"`
}

func isUniqueRunningTimer(err error) bool {
	return err != nil && strings.Contains(err.Error(), "time_entries_single_running_idx")
}

// Start opens a timer. Only one may run per person at a time.
func (r *TimeTracking) Start(ctx context.Context, taskID, userID uuid.UUID, note string) (*TimeEntry, error) {
	entry := &TimeEntry{ID: uuid.New(), TaskID: taskID, UserID: userID, Note: note}
	err := r.store.Pool.QueryRow(ctx, `
		INSERT INTO time_entries (id, task_id, user_id, started_at, note)
		VALUES ($1,$2,$3,now(),$4)
		RETURNING started_at`, entry.ID, taskID, userID, note).Scan(&entry.StartedAt)
	if isUniqueRunningTimer(err) {
		return nil, ErrTimerRunning
	}
	if err != nil {
		return nil, err
	}
	return entry, nil
}

// Stop closes the caller's running timer, whichever task it is on.
func (r *TimeTracking) Stop(ctx context.Context, userID uuid.UUID) (*TimeEntry, error) {
	var e TimeEntry
	err := r.store.Pool.QueryRow(ctx, `
		UPDATE time_entries
		   SET ended_at = now()
		 WHERE user_id = $1 AND ended_at IS NULL
		RETURNING id, task_id, user_id, started_at, ended_at,
		          EXTRACT(EPOCH FROM (now() - started_at))::bigint, note`,
		userID).Scan(&e.ID, &e.TaskID, &e.UserID, &e.StartedAt, &e.EndedAt, &e.Seconds, &e.Note)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// Running returns the caller's active timer, if any.
func (r *TimeTracking) Running(ctx context.Context, userID uuid.UUID) (*TimeEntry, error) {
	var e TimeEntry
	err := r.store.Pool.QueryRow(ctx, `
		SELECT id, task_id, user_id, started_at, ended_at,
		       EXTRACT(EPOCH FROM (now() - started_at))::bigint, note
		  FROM time_entries
		 WHERE user_id = $1 AND ended_at IS NULL`, userID).
		Scan(&e.ID, &e.TaskID, &e.UserID, &e.StartedAt, &e.EndedAt, &e.Seconds, &e.Note)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// Log records time after the fact, for work done away from the timer.
func (r *TimeTracking) Log(ctx context.Context, taskID, userID uuid.UUID, minutes int, note string) (*TimeEntry, error) {
	if minutes <= 0 {
		return nil, errors.New("minutes must be positive")
	}
	entry := &TimeEntry{ID: uuid.New(), TaskID: taskID, UserID: userID, Note: note, Seconds: int64(minutes) * 60}
	err := r.store.Pool.QueryRow(ctx, `
		INSERT INTO time_entries (id, task_id, user_id, started_at, ended_at, note)
		VALUES ($1,$2,$3, now() - make_interval(mins => $4), now(), $5)
		RETURNING started_at, ended_at`,
		entry.ID, taskID, userID, minutes, note).Scan(&entry.StartedAt, &entry.EndedAt)
	if err != nil {
		return nil, err
	}
	return entry, nil
}

// ForTask lists a task's entries, newest first.
func (r *TimeTracking) ForTask(ctx context.Context, taskID uuid.UUID) ([]TimeEntry, error) {
	rows, err := r.store.Pool.Query(ctx, `
		SELECT id, task_id, user_id, started_at, ended_at,
		       EXTRACT(EPOCH FROM (COALESCE(ended_at, now()) - started_at))::bigint, note
		  FROM time_entries
		 WHERE task_id = $1
		 ORDER BY started_at DESC`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []TimeEntry{}
	for rows.Next() {
		var e TimeEntry
		if err := rows.Scan(&e.ID, &e.TaskID, &e.UserID, &e.StartedAt, &e.EndedAt, &e.Seconds, &e.Note); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// TrackedByTask totals logged seconds per task, for estimated-vs-actual.
func (r *TimeTracking) TrackedByTask(ctx context.Context, taskIDs []uuid.UUID) (map[uuid.UUID]int64, error) {
	out := map[uuid.UUID]int64{}
	if len(taskIDs) == 0 {
		return out, nil
	}
	rows, err := r.store.Pool.Query(ctx, `
		SELECT task_id,
		       SUM(EXTRACT(EPOCH FROM (COALESCE(ended_at, now()) - started_at)))::bigint
		  FROM time_entries
		 WHERE task_id = ANY($1)
		 GROUP BY task_id`, taskIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var seconds int64
		if err := rows.Scan(&id, &seconds); err != nil {
			return nil, err
		}
		out[id] = seconds
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Watchers
// ---------------------------------------------------------------------------

type Watchers struct{ store *db.Store }

func NewWatchers(store *db.Store) *Watchers { return &Watchers{store: store} }

func (r *Watchers) Add(ctx context.Context, taskID, userID uuid.UUID) error {
	_, err := r.store.Pool.Exec(ctx, `
		INSERT INTO task_watchers (task_id, user_id) VALUES ($1,$2)
		ON CONFLICT DO NOTHING`, taskID, userID)
	return err
}

func (r *Watchers) Remove(ctx context.Context, taskID, userID uuid.UUID) error {
	_, err := r.store.Pool.Exec(ctx,
		`DELETE FROM task_watchers WHERE task_id = $1 AND user_id = $2`, taskID, userID)
	return err
}

func (r *Watchers) ForTask(ctx context.Context, taskID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := r.store.Pool.Query(ctx, `SELECT user_id FROM task_watchers WHERE task_id = $1`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// Interested returns everyone who should hear about a change to a task:
// its assignees and its watchers, de-duplicated in the query.
func (r *Watchers) Interested(ctx context.Context, taskID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := r.store.Pool.Query(ctx, `
		SELECT user_id FROM task_assignees WHERE task_id = $1
		UNION
		SELECT user_id FROM task_watchers  WHERE task_id = $1`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Automations
// ---------------------------------------------------------------------------

type Automations struct{ store *db.Store }

func NewAutomations(store *db.Store) *Automations { return &Automations{store: store} }

// Condition is one test against a task. All conditions on a rule must hold.
type Condition struct {
	Field string `json:"field"`
	Op    string `json:"op"`
	Value string `json:"value"`
}

// Action is one thing to do when a rule matches.
type Action struct {
	Type     string `json:"type"`
	Status   string `json:"status,omitempty"`
	Priority string `json:"priority,omitempty"`
	UserID   string `json:"userId,omitempty"`
	Message  string `json:"message,omitempty"`
}

type Automation struct {
	ID         uuid.UUID   `json:"id"`
	ProjectID  *uuid.UUID  `json:"projectId,omitempty"`
	SpaceID    *uuid.UUID  `json:"spaceId,omitempty"`
	Name       string      `json:"name"`
	Enabled    bool        `json:"enabled"`
	Trigger    string      `json:"trigger"`
	Conditions []Condition `json:"conditions"`
	Actions    []Action    `json:"actions"`
	CreatedAt  time.Time   `json:"createdAt"`
}

var automationTriggers = map[string]bool{
	"task.created": true, "task.status_changed": true, "task.assigned": true,
	"task.overdue": true, "task.due_soon": true,
}

func ValidTrigger(t string) bool { return automationTriggers[t] }

// MatchingTrigger returns the enabled rules for a trigger that apply to a
// project — its own, its space's, and the global ones.
func (r *Automations) MatchingTrigger(ctx context.Context, trigger string, projectID uuid.UUID) ([]Automation, error) {
	rows, err := r.store.Pool.Query(ctx, `
		SELECT a.id, a.project_id, a.space_id, a.name, a.enabled, a.trigger,
		       a.conditions, a.actions, a.created_at
		  FROM automations a
		 WHERE a.enabled
		   AND a.trigger = $1
		   AND (a.project_id = $2
		        OR a.space_id = (SELECT space_id FROM projects WHERE id = $2)
		        OR (a.project_id IS NULL AND a.space_id IS NULL))
		 ORDER BY a.created_at`, trigger, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Automation{}
	for rows.Next() {
		var a Automation
		if err := rows.Scan(&a.ID, &a.ProjectID, &a.SpaceID, &a.Name, &a.Enabled,
			&a.Trigger, &a.Conditions, &a.Actions, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *Automations) ForProject(ctx context.Context, projectID uuid.UUID) ([]Automation, error) {
	rows, err := r.store.Pool.Query(ctx, `
		SELECT id, project_id, space_id, name, enabled, trigger, conditions, actions, created_at
		  FROM automations
		 WHERE project_id = $1
		    OR space_id = (SELECT space_id FROM projects WHERE id = $1)
		    OR (project_id IS NULL AND space_id IS NULL)
		 ORDER BY created_at`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Automation{}
	for rows.Next() {
		var a Automation
		if err := rows.Scan(&a.ID, &a.ProjectID, &a.SpaceID, &a.Name, &a.Enabled,
			&a.Trigger, &a.Conditions, &a.Actions, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *Automations) Create(ctx context.Context, a *Automation, createdBy uuid.UUID) error {
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	if a.Conditions == nil {
		a.Conditions = []Condition{}
	}
	if a.Actions == nil {
		a.Actions = []Action{}
	}
	return r.store.Pool.QueryRow(ctx, `
		INSERT INTO automations (id, project_id, space_id, name, enabled, trigger, conditions, actions, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		RETURNING created_at`,
		a.ID, a.ProjectID, a.SpaceID, a.Name, a.Enabled, a.Trigger,
		a.Conditions, a.Actions, createdBy).Scan(&a.CreatedAt)
}

func (r *Automations) SetEnabled(ctx context.Context, id uuid.UUID, enabled bool) error {
	tag, err := r.store.Pool.Exec(ctx,
		`UPDATE automations SET enabled = $2, updated_at = now() WHERE id = $1`, id, enabled)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Automations) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.store.Pool.Exec(ctx, `DELETE FROM automations WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// RecordRun logs an execution, including the ones that matched nothing: an
// automation that quietly does not fire is otherwise impossible to debug.
func (r *Automations) RecordRun(ctx context.Context, automationID uuid.UUID, taskID *uuid.UUID, status, detail string) error {
	_, err := r.store.Pool.Exec(ctx, `
		INSERT INTO automation_runs (automation_id, task_id, status, detail)
		VALUES ($1,$2,$3,$4)`, automationID, taskID, status, detail)
	return err
}

// AutomationRun is one recorded execution.
type AutomationRun struct {
	ID           int64      `json:"id"`
	AutomationID uuid.UUID  `json:"automationId"`
	TaskID       *uuid.UUID `json:"taskId,omitempty"`
	Status       string     `json:"status"`
	Detail       string     `json:"detail,omitempty"`
	RanAt        time.Time  `json:"ranAt"`
}

func (r *Automations) Runs(ctx context.Context, automationID uuid.UUID, limit int) ([]AutomationRun, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := r.store.Pool.Query(ctx, `
		SELECT id, automation_id, task_id, status, detail, ran_at
		  FROM automation_runs
		 WHERE automation_id = $1
		 ORDER BY id DESC LIMIT $2`, automationID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []AutomationRun{}
	for rows.Next() {
		var run AutomationRun
		if err := rows.Scan(&run.ID, &run.AutomationID, &run.TaskID, &run.Status, &run.Detail, &run.RanAt); err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, rows.Err()
}

// describeScope is used in error messages and the audit trail.
func describeScope(projectID, spaceID *uuid.UUID) string {
	switch {
	case projectID != nil:
		return fmt.Sprintf("project %s", projectID)
	case spaceID != nil:
		return fmt.Sprintf("space %s", spaceID)
	default:
		return "global"
	}
}

var _ = describeScope
