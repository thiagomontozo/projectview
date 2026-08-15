package repo

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"projectview/internal/db"
)

type Baselines struct{ store *db.Store }

func NewBaselines(store *db.Store) *Baselines { return &Baselines{store: store} }

// BaselineTask is one task as it stood when the plan was approved.
type BaselineTask struct {
	TaskID    uuid.UUID  `json:"taskId"`
	Title     string     `json:"title"`
	StartDate *time.Time `json:"startDate,omitempty"`
	DueDate   *time.Time `json:"dueDate,omitempty"`
	Estimate  float64    `json:"estimate"`
}

type Baseline struct {
	ID         uuid.UUID      `json:"id"`
	ProjectID  uuid.UUID      `json:"projectId"`
	Name       string         `json:"name"`
	CapturedBy *uuid.UUID     `json:"capturedBy,omitempty"`
	CapturedAt time.Time      `json:"capturedAt"`
	Tasks      []BaselineTask `json:"tasks,omitempty"`
}

// Capture snapshots the current plan.
//
// Sub-tasks are excluded: their estimates roll up into the parent in every
// report the product already shows, and counting both would double the budget.
func (r *Baselines) Capture(ctx context.Context, projectID uuid.UUID, name string, capturedBy uuid.UUID) (*Baseline, error) {
	rows, err := r.store.Pool.Query(ctx, `
		SELECT id, title, start_date, due_date, estimate_hours
		  FROM tasks
		 WHERE project_id = $1 AND parent_task_id IS NULL
		 ORDER BY position`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tasks := []BaselineTask{}
	for rows.Next() {
		var t BaselineTask
		if err := rows.Scan(&t.TaskID, &t.Title, &t.StartDate, &t.DueDate, &t.Estimate); err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	snapshot, err := json.Marshal(tasks)
	if err != nil {
		return nil, err
	}

	baseline := &Baseline{ID: uuid.New(), ProjectID: projectID, Name: name, Tasks: tasks}
	err = r.store.Pool.QueryRow(ctx, `
		INSERT INTO project_baselines (id, project_id, name, snapshot, captured_by)
		VALUES ($1,$2,$3,$4,$5)
		RETURNING captured_at`,
		baseline.ID, projectID, name, snapshot, capturedBy).Scan(&baseline.CapturedAt)
	if err != nil {
		return nil, err
	}
	return baseline, nil
}

// List returns a project's baselines newest first, without their snapshots.
func (r *Baselines) List(ctx context.Context, projectID uuid.UUID) ([]Baseline, error) {
	rows, err := r.store.Pool.Query(ctx, `
		SELECT id, project_id, name, captured_by, captured_at
		  FROM project_baselines
		 WHERE project_id = $1
		 ORDER BY captured_at DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Baseline{}
	for rows.Next() {
		var b Baseline
		if err := rows.Scan(&b.ID, &b.ProjectID, &b.Name, &b.CapturedBy, &b.CapturedAt); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// Latest returns the most recent baseline of a project, snapshot included.
func (r *Baselines) Latest(ctx context.Context, projectID uuid.UUID) (*Baseline, error) {
	var b Baseline
	var snapshot []byte
	err := r.store.Pool.QueryRow(ctx, `
		SELECT id, project_id, name, snapshot, captured_by, captured_at
		  FROM project_baselines
		 WHERE project_id = $1
		 ORDER BY captured_at DESC
		 LIMIT 1`, projectID).
		Scan(&b.ID, &b.ProjectID, &b.Name, &snapshot, &b.CapturedBy, &b.CapturedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(snapshot, &b.Tasks); err != nil {
		return nil, err
	}
	return &b, nil
}

func (r *Baselines) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.store.Pool.Exec(ctx, `DELETE FROM project_baselines WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// EarnedValue is the standard set, measured in hours.
//
// Hours rather than currency: the system holds estimates and tracked time but
// no rates, and inventing a rate to print a money figure would produce a
// number that looks authoritative and is not.
type EarnedValue struct {
	AsOf time.Time `json:"asOf"`
	// Budget at completion: the whole approved plan.
	BAC float64 `json:"bac"`
	// Planned value: what should have been done by now.
	PV float64 `json:"pv"`
	// Earned value: the budgeted hours of the work actually finished.
	EV float64 `json:"ev"`
	// Actual cost: hours really spent.
	AC float64 `json:"ac"`
	// Schedule and cost variance, and their indices. Indices are omitted
	// rather than reported as zero when their denominator is zero.
	SV  float64  `json:"sv"`
	CV  float64  `json:"cv"`
	SPI *float64 `json:"spi"`
	CPI *float64 `json:"cpi"`
	// Estimate at completion and variance at completion, extrapolated from the
	// cost performance seen so far.
	EAC *float64 `json:"eac"`
	VAC *float64 `json:"vac"`
}

// TaskProgress is the live state a single baselined task is in.
type TaskProgress struct {
	CompletedAt *time.Time
	ActualHours float64
}

// ComputeEarnedValue compares a baseline against reality at a point in time.
//
// Earned value uses the 0/100 rule: a task earns its budgeted hours when it is
// finished and nothing before. Percent-complete would need a number somebody
// keeps up to date by hand, and the one thing every EVM implementation agrees
// on is that self-reported percentages are the part that lies.
//
// Planned value accrues linearly between a task's baselined start and due
// dates, so a long task is not treated as owing nothing until the last day. A
// task with no start accrues all at once on its due date; a task with no due
// date has no schedule to be measured against and contributes nothing to PV,
// though it still counts towards the budget.
func ComputeEarnedValue(baseline []BaselineTask, progress map[uuid.UUID]TaskProgress, asOf time.Time) EarnedValue {
	ev := EarnedValue{AsOf: asOf}

	for _, task := range baseline {
		ev.BAC += task.Estimate
		ev.PV += plannedValueAt(task, asOf)

		state, ok := progress[task.TaskID]
		if !ok {
			// A task deleted since the baseline was taken keeps its budget -
			// dropping it would silently shrink the plan and flatter the
			// numbers - but it can never earn anything.
			continue
		}
		ev.AC += state.ActualHours
		if state.CompletedAt != nil && !state.CompletedAt.After(asOf) {
			ev.EV += task.Estimate
		}
	}

	ev.SV = ev.EV - ev.PV
	ev.CV = ev.EV - ev.AC

	if ev.PV > 0 {
		spi := ev.EV / ev.PV
		ev.SPI = &spi
	}
	if ev.AC > 0 {
		cpi := ev.EV / ev.AC
		ev.CPI = &cpi
		// EAC = BAC / CPI assumes today's efficiency holds for the rest of the
		// work. It is the standard forecast and it is an assumption, not a
		// prediction.
		if cpi > 0 {
			eac := ev.BAC / cpi
			vac := ev.BAC - eac
			ev.EAC = &eac
			ev.VAC = &vac
		}
	}
	return ev
}

func plannedValueAt(task BaselineTask, asOf time.Time) float64 {
	if task.DueDate == nil {
		return 0
	}
	if !asOf.Before(*task.DueDate) {
		return task.Estimate
	}
	if task.StartDate == nil || !task.StartDate.Before(*task.DueDate) {
		// Nothing planned is owed before the day it is due.
		return 0
	}
	if asOf.Before(*task.StartDate) {
		return 0
	}
	span := task.DueDate.Sub(*task.StartDate).Seconds()
	elapsed := asOf.Sub(*task.StartDate).Seconds()
	return task.Estimate * math.Max(0, math.Min(1, elapsed/span))
}

// ProgressFor loads completion and tracked hours for the tasks in a baseline.
func (r *Baselines) ProgressFor(ctx context.Context, taskIDs []uuid.UUID) (map[uuid.UUID]TaskProgress, error) {
	out := map[uuid.UUID]TaskProgress{}
	if len(taskIDs) == 0 {
		return out, nil
	}

	rows, err := r.store.Pool.Query(ctx, `
		SELECT t.id, t.completed_at,
		       COALESCE(SUM(EXTRACT(EPOCH FROM (COALESCE(e.ended_at, now()) - e.started_at))) / 3600, 0)
		  FROM tasks t
		  LEFT JOIN time_entries e ON e.task_id = t.id
		 WHERE t.id = ANY($1)
		 GROUP BY t.id, t.completed_at`, taskIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var id uuid.UUID
		var p TaskProgress
		if err := rows.Scan(&id, &p.CompletedAt, &p.ActualHours); err != nil {
			return nil, err
		}
		out[id] = p
	}
	return out, rows.Err()
}
