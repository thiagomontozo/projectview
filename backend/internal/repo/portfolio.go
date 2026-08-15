package repo

import (
	"context"
	"time"

	"github.com/google/uuid"

	"projectview/internal/db"
)

type Portfolio struct{ store *db.Store }

func NewPortfolio(store *db.Store) *Portfolio { return &Portfolio{store: store} }

// PortfolioProject is one project's line in the cross-project view: enough to
// answer "which of these needs attention" without opening any of them.
type PortfolioProject struct {
	ID          uuid.UUID  `json:"id"`
	Name        string     `json:"name"`
	Key         string     `json:"key"`
	Color       string     `json:"color"`
	Status      string     `json:"status"`
	SpaceID     *uuid.UUID `json:"spaceId,omitempty"`
	OwnerID     *uuid.UUID `json:"ownerId,omitempty"`
	StartDate   *time.Time `json:"startDate,omitempty"`
	EndDate     *time.Time `json:"endDate,omitempty"`
	TotalTasks  int        `json:"totalTasks"`
	DoneTasks   int        `json:"doneTasks"`
	OverdueOpen int        `json:"overdueOpen"`
	People      int        `json:"people"`
	Estimated   float64    `json:"estimatedHours"`
	Tracked     float64    `json:"trackedHours"`
	Progress    float64    `json:"progress"`
	Health      string     `json:"health"`
}

// Health classifies a project without asking anyone to keep a RAG status
// up to date by hand.
//
// The order matters: a project that is late AND over budget should read as
// "off track", so the worst signal wins rather than the first one checked.
func Health(p PortfolioProject, now time.Time) string {
	if p.TotalTasks > 0 && p.DoneTasks == p.TotalTasks {
		return "done"
	}

	overdueProject := p.EndDate != nil && p.EndDate.Before(now) && p.DoneTasks < p.TotalTasks
	// A tenth of the work overdue is where a slip stops being noise. Below
	// that, a single stale task would paint a healthy project red.
	manyOverdue := p.TotalTasks > 0 && float64(p.OverdueOpen)/float64(p.TotalTasks) > 0.1
	overBudget := p.Estimated > 0 && p.Tracked > p.Estimated*1.1

	switch {
	case overdueProject, manyOverdue && overBudget:
		return "off_track"
	case manyOverdue, overBudget:
		return "at_risk"
	default:
		return "on_track"
	}
}

// Summary aggregates every project the caller may see. The counts come from
// the database in one pass rather than one query per project, which is what
// made the old dashboard slow.
func (r *Portfolio) Summary(ctx context.Context, spaceID *uuid.UUID, now time.Time) ([]PortfolioProject, error) {
	rows, err := r.store.Pool.Query(ctx, `
		SELECT p.id, p.name, p.key, p.color, p.status, p.space_id, p.owner_id,
		       p.start_date, p.end_date,
		       COALESCE(t.total, 0), COALESCE(t.done, 0), COALESCE(t.overdue, 0),
		       COALESCE(t.estimated, 0), COALESCE(h.tracked, 0),
		       COALESCE(m.people, 0)
		  FROM projects p
		  LEFT JOIN LATERAL (
		      SELECT count(*)                                             AS total,
		             count(*) FILTER (WHERE completed_at IS NOT NULL)     AS done,
		             count(*) FILTER (WHERE completed_at IS NULL
		                                AND due_date IS NOT NULL
		                                AND due_date < $2)                AS overdue,
		             COALESCE(sum(estimate_hours), 0)                     AS estimated
		        FROM tasks WHERE project_id = p.id
		  ) t ON true
		  LEFT JOIN LATERAL (
		      SELECT COALESCE(SUM(EXTRACT(EPOCH FROM (COALESCE(e.ended_at, now()) - e.started_at))) / 3600, 0) AS tracked
		        FROM time_entries e
		        JOIN tasks tk ON tk.id = e.task_id
		       WHERE tk.project_id = p.id
		  ) h ON true
		  LEFT JOIN LATERAL (
		      SELECT count(*) AS people FROM project_members WHERE project_id = p.id
		  ) m ON true
		 WHERE NOT p.archived
		   AND ($1::uuid IS NULL OR p.space_id = $1)
		 ORDER BY p.name`, spaceID, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []PortfolioProject{}
	for rows.Next() {
		var p PortfolioProject
		if err := rows.Scan(&p.ID, &p.Name, &p.Key, &p.Color, &p.Status, &p.SpaceID, &p.OwnerID,
			&p.StartDate, &p.EndDate,
			&p.TotalTasks, &p.DoneTasks, &p.OverdueOpen, &p.Estimated, &p.Tracked, &p.People); err != nil {
			return nil, err
		}
		if p.TotalTasks > 0 {
			p.Progress = float64(p.DoneTasks) / float64(p.TotalTasks)
		}
		p.Health = Health(p, now)
		out = append(out, p)
	}
	return out, rows.Err()
}

// CapacityRow is one person's committed hours against what they actually have.
type CapacityRow struct {
	UserID      uuid.UUID `json:"userId"`
	Name        string    `json:"name"`
	Email       string    `json:"email"`
	AvatarColor string    `json:"avatarColor"`
	Capacity    float64   `json:"capacityHours"`
	Committed   float64   `json:"committedHours"`
	Projects    int       `json:"projects"`
	OpenTasks   int       `json:"openTasks"`
	// Utilisation is committed/capacity, uncapped: the number people need to
	// see is how far past 100% someone is, not that they are "at 100%".
	Utilisation float64 `json:"utilisation"`
}

// Capacity reports allocation against declared capacity over a window.
//
// A task's estimate is shared equally between its assignees, because a task
// with three people on it is not three times the work. Capacity is scaled from
// the weekly figure to the length of the window, so a fortnight is compared
// against a fortnight.
func (r *Portfolio) Capacity(ctx context.Context, from, to time.Time) ([]CapacityRow, error) {
	weeks := to.Sub(from).Hours() / (24 * 7)
	if weeks <= 0 {
		weeks = 1
	}

	rows, err := r.store.Pool.Query(ctx, `
		SELECT u.id, u.name, u.email, u.avatar_color, u.weekly_capacity_hours,
		       COALESCE(a.committed, 0), COALESCE(a.open_tasks, 0), COALESCE(a.projects, 0)
		  FROM users u
		  LEFT JOIN LATERAL (
		      SELECT SUM(t.estimate_hours / GREATEST(shared.assignees, 1)) AS committed,
		             count(*)                                             AS open_tasks,
		             count(DISTINCT t.project_id)                         AS projects
		        FROM task_assignees ta
		        JOIN tasks t ON t.id = ta.task_id
		        JOIN LATERAL (
		            SELECT count(*) AS assignees FROM task_assignees WHERE task_id = t.id
		        ) shared ON true
		       WHERE ta.user_id = u.id
		         AND t.completed_at IS NULL
		         AND (t.due_date IS NULL OR t.due_date >= $1)
		         AND (t.start_date IS NULL OR t.start_date <= $2)
		  ) a ON true
		 WHERE u.active AND u.anonymized_at IS NULL
		 ORDER BY u.name`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []CapacityRow{}
	for rows.Next() {
		var row CapacityRow
		var weekly float64
		if err := rows.Scan(&row.UserID, &row.Name, &row.Email, &row.AvatarColor, &weekly,
			&row.Committed, &row.OpenTasks, &row.Projects); err != nil {
			return nil, err
		}
		row.Capacity = weekly * weeks
		if row.Capacity > 0 {
			row.Utilisation = row.Committed / row.Capacity
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
