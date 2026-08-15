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

type Goals struct{ store *db.Store }

func NewGoals(store *db.Store) *Goals { return &Goals{store: store} }

// Key result sources. The derived kinds read from the work itself, so a goal
// cannot quietly drift from what the team is doing.
const (
	SourceManual         = "manual"
	SourceTasksCompleted = "tasks_completed"
	SourceTasksCount     = "tasks_count"
)

func ValidKeyResultSource(s string) bool {
	return s == SourceManual || s == SourceTasksCompleted || s == SourceTasksCount
}

func ValidGoalStatus(s string) bool {
	switch s {
	case "active", "at_risk", "achieved", "missed":
		return true
	}
	return false
}

type KeyResult struct {
	ID           uuid.UUID  `json:"id"`
	GoalID       uuid.UUID  `json:"goalId"`
	Name         string     `json:"name"`
	Source       string     `json:"source"`
	Unit         string     `json:"unit"`
	StartValue   float64    `json:"startValue"`
	TargetValue  float64    `json:"targetValue"`
	CurrentValue float64    `json:"currentValue"`
	ProjectID    *uuid.UUID `json:"projectId,omitempty"`
	Position     int        `json:"position"`
	// Progress is computed, never stored: a stored percentage is one more
	// thing that can disagree with the numbers it came from.
	Progress float64 `json:"progress"`
}

type Goal struct {
	ID          uuid.UUID   `json:"id"`
	Name        string      `json:"name"`
	Description string      `json:"description"`
	SpaceID     *uuid.UUID  `json:"spaceId,omitempty"`
	TeamID      *uuid.UUID  `json:"teamId,omitempty"`
	OwnerID     *uuid.UUID  `json:"ownerId,omitempty"`
	StartDate   *time.Time  `json:"startDate,omitempty"`
	DueDate     *time.Time  `json:"dueDate,omitempty"`
	Status      string      `json:"status"`
	Archived    bool        `json:"archived"`
	CreatedAt   time.Time   `json:"createdAt"`
	UpdatedAt   time.Time   `json:"updatedAt"`
	KeyResults  []KeyResult `json:"keyResults"`
	Progress    float64     `json:"progress"`
}

// KeyResultProgress maps a measure onto 0..1.
//
// Progress is measured from the starting value, not from zero: a key result
// that moves a number from 80 to 90 is at 0% on the day it is written, not at
// 89%. A target equal to the start has no scale to measure against, so it is
// reported as complete only once the current value has reached it.
func KeyResultProgress(start, target, current float64) float64 {
	span := target - start
	if span == 0 {
		if current >= target {
			return 1
		}
		return 0
	}
	ratio := (current - start) / span
	if ratio < 0 {
		return 0
	}
	if ratio > 1 {
		return 1
	}
	return ratio
}

// GoalProgress averages the key results, weighting them equally.
//
// Equal weights rather than configurable ones: a weight is a number somebody
// has to justify, and in practice teams set them once and never revisit them.
// A goal with no measures has no progress to report, which is different from
// zero progress - the caller decides how to show it.
func GoalProgress(results []KeyResult) float64 {
	if len(results) == 0 {
		return 0
	}
	total := 0.0
	for _, kr := range results {
		total += KeyResultProgress(kr.StartValue, kr.TargetValue, kr.CurrentValue)
	}
	return total / float64(len(results))
}

const goalColumns = `
	g.id, g.name, g.description, g.space_id, g.team_id, g.owner_id,
	g.start_date, g.due_date, g.status, g.archived, g.created_at, g.updated_at`

func scanGoal(row pgx.Row) (*Goal, error) {
	var g Goal
	err := row.Scan(&g.ID, &g.Name, &g.Description, &g.SpaceID, &g.TeamID, &g.OwnerID,
		&g.StartDate, &g.DueDate, &g.Status, &g.Archived, &g.CreatedAt, &g.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &g, nil
}

// List returns the goals of a space, or every goal when spaceID is nil, with
// their key results already resolved.
func (r *Goals) List(ctx context.Context, spaceID *uuid.UUID) ([]Goal, error) {
	rows, err := r.store.Pool.Query(ctx, `
		SELECT `+goalColumns+`
		  FROM goals g
		 WHERE NOT g.archived
		   AND ($1::uuid IS NULL OR g.space_id = $1)
		 ORDER BY g.due_date NULLS LAST, g.name`, spaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	goals := []Goal{}
	ids := []uuid.UUID{}
	for rows.Next() {
		g, err := scanGoal(rows)
		if err != nil {
			return nil, err
		}
		goals = append(goals, *g)
		ids = append(ids, g.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// One query for every goal's measures rather than one per goal.
	byGoal, err := r.keyResultsFor(ctx, ids)
	if err != nil {
		return nil, err
	}
	for i := range goals {
		goals[i].KeyResults = byGoal[goals[i].ID]
		goals[i].Progress = GoalProgress(goals[i].KeyResults)
	}
	return goals, nil
}

func (r *Goals) ByID(ctx context.Context, id uuid.UUID) (*Goal, error) {
	goal, err := scanGoal(r.store.Pool.QueryRow(ctx, `SELECT `+goalColumns+` FROM goals g WHERE g.id = $1`, id))
	if err != nil {
		return nil, err
	}
	byGoal, err := r.keyResultsFor(ctx, []uuid.UUID{id})
	if err != nil {
		return nil, err
	}
	goal.KeyResults = byGoal[id]
	goal.Progress = GoalProgress(goal.KeyResults)
	return goal, nil
}

// keyResultsFor loads measures for several goals and resolves the derived ones
// against the tasks they point at, in a single pass over the task table.
func (r *Goals) keyResultsFor(ctx context.Context, goalIDs []uuid.UUID) (map[uuid.UUID][]KeyResult, error) {
	out := map[uuid.UUID][]KeyResult{}
	if len(goalIDs) == 0 {
		return out, nil
	}

	rows, err := r.store.Pool.Query(ctx, `
		SELECT kr.id, kr.goal_id, kr.name, kr.source, kr.unit,
		       kr.start_value, kr.target_value, kr.current_value,
		       kr.project_id, kr.position,
		       COALESCE(stats.done, 0)  AS done,
		       COALESCE(stats.total, 0) AS total
		  FROM key_results kr
		  LEFT JOIN LATERAL (
		      SELECT count(*) FILTER (WHERE t.completed_at IS NOT NULL) AS done,
		             count(*)                                          AS total
		        FROM tasks t
		       WHERE t.project_id = kr.project_id
		  ) stats ON kr.project_id IS NOT NULL
		 WHERE kr.goal_id = ANY($1)
		 ORDER BY kr.position, kr.name`, goalIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var kr KeyResult
		var done, total int
		if err := rows.Scan(&kr.ID, &kr.GoalID, &kr.Name, &kr.Source, &kr.Unit,
			&kr.StartValue, &kr.TargetValue, &kr.CurrentValue,
			&kr.ProjectID, &kr.Position, &done, &total); err != nil {
			return nil, err
		}

		switch kr.Source {
		case SourceTasksCompleted:
			// A project with no tasks is 0% done, not 100%: dividing by zero
			// would otherwise declare an empty project finished.
			if total > 0 {
				kr.CurrentValue = float64(done) / float64(total) * 100
			} else {
				kr.CurrentValue = 0
			}
		case SourceTasksCount:
			kr.CurrentValue = float64(done)
		}

		kr.Progress = KeyResultProgress(kr.StartValue, kr.TargetValue, kr.CurrentValue)
		out[kr.GoalID] = append(out[kr.GoalID], kr)
	}
	return out, rows.Err()
}

func (r *Goals) Create(ctx context.Context, g *Goal, createdBy uuid.UUID) error {
	if g.ID == uuid.Nil {
		g.ID = uuid.New()
	}
	if g.Status == "" {
		g.Status = "active"
	}
	return r.store.Pool.QueryRow(ctx, `
		INSERT INTO goals (id, name, description, space_id, team_id, owner_id,
		                   start_date, due_date, status, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		RETURNING created_at, updated_at`,
		g.ID, g.Name, g.Description, g.SpaceID, g.TeamID, g.OwnerID,
		g.StartDate, g.DueDate, g.Status, createdBy).
		Scan(&g.CreatedAt, &g.UpdatedAt)
}

type GoalPatch struct {
	Name        *string
	Description *string
	OwnerID     *uuid.UUID
	TeamID      *uuid.UUID
	StartDate   **time.Time
	DueDate     **time.Time
	Status      *string
	Archived    *bool
}

func (r *Goals) Update(ctx context.Context, id uuid.UUID, p GoalPatch) error {
	sets := []string{}
	args := []any{id}
	add := func(col string, val any) {
		args = append(args, val)
		sets = append(sets, fmt.Sprintf("%s = $%d", col, len(args)))
	}

	if p.Name != nil {
		add("name", *p.Name)
	}
	if p.Description != nil {
		add("description", *p.Description)
	}
	if p.OwnerID != nil {
		add("owner_id", *p.OwnerID)
	}
	if p.TeamID != nil {
		add("team_id", *p.TeamID)
	}
	if p.StartDate != nil {
		add("start_date", *p.StartDate)
	}
	if p.DueDate != nil {
		add("due_date", *p.DueDate)
	}
	if p.Status != nil {
		add("status", *p.Status)
	}
	if p.Archived != nil {
		add("archived", *p.Archived)
	}
	if len(sets) == 0 {
		return nil
	}

	tag, err := r.store.Pool.Exec(ctx,
		`UPDATE goals SET `+strings.Join(sets, ", ")+`, updated_at = now() WHERE id = $1`, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Goals) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.store.Pool.Exec(ctx, `DELETE FROM goals WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Goals) AddKeyResult(ctx context.Context, kr *KeyResult) error {
	if kr.ID == uuid.Nil {
		kr.ID = uuid.New()
	}
	if kr.Source == "" {
		kr.Source = SourceManual
	}
	_, err := r.store.Pool.Exec(ctx, `
		INSERT INTO key_results (id, goal_id, name, source, unit,
		                         start_value, target_value, current_value, project_id, position)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,
		        COALESCE((SELECT max(position) + 1 FROM key_results WHERE goal_id = $2), 0))`,
		kr.ID, kr.GoalID, kr.Name, kr.Source, kr.Unit,
		kr.StartValue, kr.TargetValue, kr.CurrentValue, kr.ProjectID)
	return err
}

// SetKeyResultValue records a manual reading. Derived measures refuse it: a
// number computed from the tasks would be overwritten on the next read, and
// accepting the write would be a lie about what was stored.
func (r *Goals) SetKeyResultValue(ctx context.Context, id uuid.UUID, value float64) error {
	tag, err := r.store.Pool.Exec(ctx, `
		UPDATE key_results SET current_value = $2
		 WHERE id = $1 AND source = 'manual'`, id, value)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Goals) DeleteKeyResult(ctx context.Context, id uuid.UUID) error {
	tag, err := r.store.Pool.Exec(ctx, `DELETE FROM key_results WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
