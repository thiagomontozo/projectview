package repo

import (
	"context"
	"time"

	"github.com/google/uuid"

	"projectview/internal/db"
)

// Dashboard holds the reporting queries. They were MongoDB aggregation
// pipelines; as SQL they are short enough to read and the planner can use the
// same indexes the rest of the application relies on.
type Dashboard struct{ store *db.Store }

func NewDashboard(store *db.Store) *Dashboard { return &Dashboard{store: store} }

type Overview struct {
	TotalProjects  int64 `json:"totalProjects"`
	ActiveProjects int64 `json:"activeProjects"`
	TotalTasks     int64 `json:"totalTasks"`
	DoneTasks      int64 `json:"doneTasks"`
	OverdueTasks   int64 `json:"overdueTasks"`
}

// Counters returns all five headline numbers in a single round trip.
func (r *Dashboard) Counters(ctx context.Context) (Overview, error) {
	var o Overview
	err := r.store.Pool.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM projects),
		       (SELECT count(*) FROM projects WHERE status = 'active'),
		       (SELECT count(*) FROM tasks WHERE parent_task_id IS NULL),
		       (SELECT count(*) FROM tasks WHERE status = 'done'),
		       (SELECT count(*) FROM tasks WHERE status <> 'done' AND due_date < now())`).
		Scan(&o.TotalProjects, &o.ActiveProjects, &o.TotalTasks, &o.DoneTasks, &o.OverdueTasks)
	return o, err
}

type StatusCount struct {
	Status string `json:"status"`
	Count  int    `json:"count"`
}

func (r *Dashboard) StatusBreakdown(ctx context.Context, projectID *uuid.UUID) ([]StatusCount, error) {
	rows, err := r.store.Pool.Query(ctx, `
		SELECT status, count(*)
		  FROM tasks
		 WHERE parent_task_id IS NULL
		   AND ($1::uuid IS NULL OR project_id = $1)
		 GROUP BY status
		 ORDER BY status`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []StatusCount{}
	for rows.Next() {
		var s StatusCount
		if err := rows.Scan(&s.Status, &s.Count); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

type WorkloadChartRow struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func (r *Dashboard) WorkloadChart(ctx context.Context) ([]WorkloadChartRow, error) {
	rows, err := r.store.Pool.Query(ctx, `
		SELECT u.name, count(*) AS n
		  FROM task_assignees ta
		  JOIN tasks t ON t.id = ta.task_id
		  JOIN users u ON u.id = ta.user_id
		 WHERE t.status <> 'done'
		 GROUP BY u.name
		 ORDER BY n DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []WorkloadChartRow{}
	for rows.Next() {
		var w WorkloadChartRow
		if err := rows.Scan(&w.Name, &w.Count); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

type ProjectProgressRow struct {
	ProjectID uuid.UUID
	Name      string
	Key       string
	Color     string
	Total     int
	Done      int
	Percent   int
}

func (r *Dashboard) ProjectProgress(ctx context.Context) ([]ProjectProgressRow, error) {
	rows, err := r.store.Pool.Query(ctx, `
		SELECT p.id, p.name, p.key, p.color,
		       count(t.id) FILTER (WHERE t.parent_task_id IS NULL)                         AS total,
		       count(t.id) FILTER (WHERE t.parent_task_id IS NULL AND t.status = 'done')   AS done
		  FROM projects p
		  LEFT JOIN tasks t ON t.project_id = p.id
		 GROUP BY p.id, p.name, p.key, p.color
		 ORDER BY p.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []ProjectProgressRow{}
	for rows.Next() {
		var p ProjectProgressRow
		if err := rows.Scan(&p.ProjectID, &p.Name, &p.Key, &p.Color, &p.Total, &p.Done); err != nil {
			return nil, err
		}
		if p.Total > 0 {
			p.Percent = int(float64(p.Done) / float64(p.Total) * 100)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

type CompletionTrendRow struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

// CompletionTrend counts tasks completed per day over the last 30 days.
func (r *Dashboard) CompletionTrend(ctx context.Context) ([]CompletionTrendRow, error) {
	since := time.Now().AddDate(0, 0, -30)
	rows, err := r.store.Pool.Query(ctx, `
		SELECT to_char(completed_at, 'YYYY-MM-DD') AS day, count(*)
		  FROM tasks
		 WHERE completed_at >= $1
		 GROUP BY day
		 ORDER BY day`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []CompletionTrendRow{}
	for rows.Next() {
		var c CompletionTrendRow
		if err := rows.Scan(&c.Date, &c.Count); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
