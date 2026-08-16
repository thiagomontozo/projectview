package repo

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"projectview/internal/db"
)

// Recurrences stores the rule that makes a task come back.
//
// The rule is keyed by the task currently carrying it rather than by a series,
// so "the recurring task" is always the open one. Spawning moves the rule
// forward in one transaction; there is never a moment where two instances claim
// it or none does.
type Recurrences struct{ store *db.Store }

func NewRecurrences(store *db.Store) *Recurrences { return &Recurrences{store: store} }

type Recurrence struct {
	TaskID         uuid.UUID  `json:"taskId"`
	Frequency      string     `json:"frequency"`
	IntervalCount  int        `json:"intervalCount"`
	Mode           string     `json:"mode"`
	UntilDate      *time.Time `json:"untilDate,omitempty"`
	MaxOccurrences *int       `json:"maxOccurrences,omitempty"`
	Occurrences    int        `json:"occurrences"`
	NextRunAt      *time.Time `json:"nextRunAt,omitempty"`
	CreatedBy      *uuid.UUID `json:"-"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

const recurrenceColumns = `
	task_id, frequency, interval_count, mode, until_date, max_occurrences,
	occurrences, next_run_at, created_by, created_at, updated_at`

func scanRecurrence(row pgx.Row) (*Recurrence, error) {
	var r Recurrence
	err := row.Scan(&r.TaskID, &r.Frequency, &r.IntervalCount, &r.Mode, &r.UntilDate,
		&r.MaxOccurrences, &r.Occurrences, &r.NextRunAt, &r.CreatedBy, &r.CreatedAt, &r.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func (r *Recurrences) ByTask(ctx context.Context, taskID uuid.UUID) (*Recurrence, error) {
	return scanRecurrence(r.store.Pool.QueryRow(ctx,
		`SELECT `+recurrenceColumns+` FROM task_recurrences WHERE task_id = $1`, taskID))
}

// Set creates or replaces the rule on a task.
func (r *Recurrences) Set(ctx context.Context, rec *Recurrence) error {
	return r.store.Pool.QueryRow(ctx, `
		INSERT INTO task_recurrences
		    (task_id, frequency, interval_count, mode, until_date, max_occurrences,
		     occurrences, next_run_at, created_by)
		-- GREATEST, not COALESCE: the Go zero value is 0, not NULL, so
		-- COALESCE let a brand-new series start its count at zero and a
		-- "repeat 3 times" rule would have run four.
		VALUES ($1,$2,$3,$4,$5,$6,GREATEST(COALESCE($7,1),1),$8,$9)
		ON CONFLICT (task_id) DO UPDATE SET
		    frequency = EXCLUDED.frequency, interval_count = EXCLUDED.interval_count,
		    mode = EXCLUDED.mode, until_date = EXCLUDED.until_date,
		    max_occurrences = EXCLUDED.max_occurrences, next_run_at = EXCLUDED.next_run_at,
		    updated_at = now()
		RETURNING created_at, updated_at, occurrences`,
		rec.TaskID, rec.Frequency, rec.IntervalCount, rec.Mode, rec.UntilDate,
		rec.MaxOccurrences, rec.Occurrences, rec.NextRunAt, rec.CreatedBy).
		Scan(&rec.CreatedAt, &rec.UpdatedAt, &rec.Occurrences)
}

func (r *Recurrences) Delete(ctx context.Context, taskID uuid.UUID) error {
	_, err := r.store.Pool.Exec(ctx, `DELETE FROM task_recurrences WHERE task_id = $1`, taskID)
	return err
}

// Exhausted reports whether a rule has reached whichever end it was given. A
// rule with neither an end date nor a maximum never is.
func (r *Recurrence) Exhausted(at time.Time) bool {
	if r.UntilDate != nil && !at.Before(*r.UntilDate) {
		return true
	}
	if r.MaxOccurrences != nil && r.Occurrences >= *r.MaxOccurrences {
		return true
	}
	return false
}

// MoveTo transfers the rule from one task to its successor, in one statement.
//
// A delete-then-insert would leave a window in which the series belongs to
// nothing, and a crash inside it would lose the rule entirely - the task would
// simply stop recurring, silently, which is the failure nobody notices until
// the report does not arrive.
func (r *Recurrences) MoveTo(ctx context.Context, from, to uuid.UUID, nextRunAt *time.Time) error {
	return r.store.WithTx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO task_recurrences
			    (task_id, frequency, interval_count, mode, until_date, max_occurrences,
			     occurrences, next_run_at, created_by, created_at)
			SELECT $2, frequency, interval_count, mode, until_date, max_occurrences,
			       occurrences + 1, $3, created_by, created_at
			  FROM task_recurrences WHERE task_id = $1`, from, to, nextRunAt)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `DELETE FROM task_recurrences WHERE task_id = $1`, from)
		return err
	})
}

// DueForSpawn returns the on_schedule rules whose moment has arrived, with the
// task they sit on.
func (r *Recurrences) DueForSpawn(ctx context.Context, now time.Time, limit int) ([]Recurrence, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.store.Pool.Query(ctx, `
		SELECT `+recurrenceColumns+`
		  FROM task_recurrences
		 WHERE mode = 'on_schedule'
		   AND next_run_at IS NOT NULL
		   AND next_run_at <= $1
		 ORDER BY next_run_at
		 LIMIT $2`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Recurrence{}
	for rows.Next() {
		rec, err := scanRecurrence(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *rec)
	}
	return out, rows.Err()
}

// RecurrencesFor resolves the rules for a batch of tasks in one query, so a
// listing can mark which of its rows repeat without a query per row.
func (r *Recurrences) RecurrencesFor(ctx context.Context, taskIDs []uuid.UUID) (map[uuid.UUID]Recurrence, error) {
	out := map[uuid.UUID]Recurrence{}
	if len(taskIDs) == 0 {
		return out, nil
	}
	rows, err := r.store.Pool.Query(ctx,
		`SELECT `+recurrenceColumns+` FROM task_recurrences WHERE task_id = ANY($1)`, taskIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		rec, err := scanRecurrence(rows)
		if err != nil {
			return nil, err
		}
		out[rec.TaskID] = *rec
	}
	return out, rows.Err()
}
