package repo

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"projectview/internal/db"
)

// ErrDependencyCycle is returned when an edge would close a loop. The check
// lives in a database trigger; this translates it into something handlers can
// turn into a 409 rather than a 500.
var ErrDependencyCycle = errors.New("dependency would create a cycle")

type Dependencies struct{ store *db.Store }

func NewDependencies(store *db.Store) *Dependencies { return &Dependencies{store: store} }

// Dependency is an edge: TaskID cannot proceed until DependsOnID is done.
type Dependency struct {
	TaskID      uuid.UUID `json:"taskId"`
	DependsOnID uuid.UUID `json:"dependsOn"`
	Type        string    `json:"type"`
	LagDays     int       `json:"lagDays"`
	CreatedAt   time.Time `json:"createdAt"`
}

func isCycleError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "dependency would create a cycle")
}

func (r *Dependencies) Add(ctx context.Context, taskID, dependsOnID uuid.UUID, depType string, lagDays int, createdBy uuid.UUID) error {
	if depType == "" {
		depType = "finish_to_start"
	}
	_, err := r.store.Pool.Exec(ctx, `
		INSERT INTO task_dependencies (task_id, depends_on_id, type, lag_days, created_by)
		VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (task_id, depends_on_id) DO UPDATE
		   SET type = EXCLUDED.type, lag_days = EXCLUDED.lag_days`,
		taskID, dependsOnID, depType, lagDays, createdBy)
	if isCycleError(err) {
		return ErrDependencyCycle
	}
	return err
}

func (r *Dependencies) Remove(ctx context.Context, taskID, dependsOnID uuid.UUID) error {
	tag, err := r.store.Pool.Exec(ctx,
		`DELETE FROM task_dependencies WHERE task_id = $1 AND depends_on_id = $2`, taskID, dependsOnID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ForProject returns every edge inside a project, which is what the timeline
// needs to draw the arrows in one request.
func (r *Dependencies) ForProject(ctx context.Context, projectID uuid.UUID) ([]Dependency, error) {
	rows, err := r.store.Pool.Query(ctx, `
		SELECT d.task_id, d.depends_on_id, d.type, d.lag_days, d.created_at
		  FROM task_dependencies d
		  JOIN tasks t ON t.id = d.task_id
		 WHERE t.project_id = $1`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Dependency{}
	for rows.Next() {
		var d Dependency
		if err := rows.Scan(&d.TaskID, &d.DependsOnID, &d.Type, &d.LagDays, &d.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ForTask returns what a task waits on and what waits on it.
func (r *Dependencies) ForTask(ctx context.Context, taskID uuid.UUID) (blockedBy []Dependency, blocking []Dependency, err error) {
	rows, err := r.store.Pool.Query(ctx, `
		SELECT task_id, depends_on_id, type, lag_days, created_at
		  FROM task_dependencies
		 WHERE task_id = $1 OR depends_on_id = $1`, taskID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	blockedBy, blocking = []Dependency{}, []Dependency{}
	for rows.Next() {
		var d Dependency
		if err := rows.Scan(&d.TaskID, &d.DependsOnID, &d.Type, &d.LagDays, &d.CreatedAt); err != nil {
			return nil, nil, err
		}
		if d.TaskID == taskID {
			blockedBy = append(blockedBy, d)
		} else {
			blocking = append(blocking, d)
		}
	}
	return blockedBy, blocking, rows.Err()
}

// BlockedTask is a task that cannot start because something ahead of it is
// unfinished.
type BlockedTask struct {
	TaskID    uuid.UUID `json:"taskId"`
	BlockerID uuid.UUID `json:"blockerId"`
	Blocker   string    `json:"blockerTitle"`
}

// Blocked lists tasks in a project whose predecessors are not done yet. This
// is the practical half of dependencies: knowing what cannot be started today.
func (r *Dependencies) Blocked(ctx context.Context, projectID uuid.UUID) ([]BlockedTask, error) {
	rows, err := r.store.Pool.Query(ctx, `
		SELECT d.task_id, blocker.id, blocker.title
		  FROM task_dependencies d
		  JOIN tasks t       ON t.id = d.task_id
		  JOIN tasks blocker ON blocker.id = d.depends_on_id
		 WHERE t.project_id = $1
		   AND t.status <> 'done'
		   AND blocker.status <> 'done'`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []BlockedTask{}
	for rows.Next() {
		var b BlockedTask
		if err := rows.Scan(&b.TaskID, &b.BlockerID, &b.Blocker); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Critical path
// ---------------------------------------------------------------------------

// ScheduleNode is one task as the scheduler sees it.
type ScheduleNode struct {
	ID       uuid.UUID
	Title    string
	Duration float64 // days
	Blockers []uuid.UUID
}

// CriticalPath returns the ids of the tasks on the longest dependency chain
// through the project, weighted by duration — the sequence where any slip
// pushes the whole project's end date.
//
// The walk is done in Go rather than SQL: the recursive CTE that finds the
// longest path has to carry the accumulated duration and re-visit nodes
// reachable by several routes, which Postgres will not do in a single
// UNION-based recursion without either a cycle guard or an array of visited
// ids. The graph is guaranteed acyclic by the trigger, and a project's task
// count is small, so a topological pass here is both simpler and faster than
// forcing it into one query.
func (r *Dependencies) CriticalPath(ctx context.Context, projectID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := r.store.Pool.Query(ctx, `
		SELECT t.id,
		       t.title,
		       -- Duration in days, from the schedule when it exists and
		       -- falling back to the estimate. A task with neither counts as
		       -- one day: zero would let it vanish from the longest path.
		       GREATEST(
		           COALESCE(
		               EXTRACT(EPOCH FROM (t.due_date - t.start_date)) / 86400,
		               NULLIF(t.estimate_hours, 0) / 8,
		               1
		           ), 1)::double precision,
		       COALESCE(array_agg(d.depends_on_id) FILTER (WHERE d.depends_on_id IS NOT NULL), '{}')
		  FROM tasks t
		  LEFT JOIN task_dependencies d ON d.task_id = t.id
		 WHERE t.project_id = $1 AND t.parent_task_id IS NULL
		 GROUP BY t.id, t.title, t.due_date, t.start_date, t.estimate_hours`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	nodes := map[uuid.UUID]*ScheduleNode{}
	order := []uuid.UUID{}
	for rows.Next() {
		var n ScheduleNode
		if err := rows.Scan(&n.ID, &n.Title, &n.Duration, &n.Blockers); err != nil {
			return nil, err
		}
		nodes[n.ID] = &n
		order = append(order, n.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return longestPath(nodes, order), nil
}

// longestPath finds the heaviest chain through a DAG by memoised depth-first
// search. Exported behaviour is covered by unit tests; kept separate from the
// query so it can be tested without a database.
func longestPath(nodes map[uuid.UUID]*ScheduleNode, order []uuid.UUID) []uuid.UUID {
	type result struct {
		weight float64
		path   []uuid.UUID
	}
	memo := map[uuid.UUID]result{}

	// The longest chain ending at id, following blockers backwards.
	var walk func(id uuid.UUID) result
	walk = func(id uuid.UUID) result {
		if cached, ok := memo[id]; ok {
			return cached
		}
		node, ok := nodes[id]
		if !ok {
			return result{}
		}

		best := result{}
		for _, blockerID := range node.Blockers {
			// A blocker outside this project (a cross-project edge) is not
			// part of this schedule and is skipped rather than treated as
			// weightless.
			if _, known := nodes[blockerID]; !known {
				continue
			}
			candidate := walk(blockerID)
			if candidate.weight > best.weight {
				best = candidate
			}
		}

		combined := result{
			weight: best.weight + node.Duration,
			path:   append(append([]uuid.UUID{}, best.path...), id),
		}
		memo[id] = combined
		return combined
	}

	longest := result{}
	for _, id := range order {
		if candidate := walk(id); candidate.weight > longest.weight {
			longest = candidate
		}
	}

	// A single task is not a "critical path" in any useful sense; reporting
	// one would light up an arbitrary task on a project with no dependencies
	// at all.
	if len(longest.path) < 2 {
		return []uuid.UUID{}
	}
	return longest.path
}
