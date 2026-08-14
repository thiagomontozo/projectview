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
	"projectview/internal/models"
)

type Tasks struct{ store *db.Store }

func NewTasks(store *db.Store) *Tasks { return &Tasks{store: store} }

const taskColumns = `
	t.id, t.title, t.description, t.project_id, t.parent_task_id, t.status,
	t.priority, t.start_date, t.due_date, t.completed_at, t.estimate_hours,
	t.position, t.created_by, t.created_at, t.updated_at`

func scanTask(row pgx.Row) (*models.Task, error) {
	var t models.Task
	err := row.Scan(&t.ID, &t.Title, &t.Description, &t.ProjectID, &t.ParentTask,
		&t.Status, &t.Priority, &t.StartDate, &t.DueDate, &t.CompletedAt,
		&t.EstimateHours, &t.Order, &t.CreatedBy, &t.CreatedAt, &t.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	t.Assignees = []uuid.UUID{}
	t.Tags = []string{}
	t.Checklist = []models.ChecklistItem{}
	t.Comments = []models.Comment{}
	return &t, nil
}

// hydrate attaches assignees, tags, checklist items and comments to a batch of
// tasks using four queries total, independent of how many tasks there are.
// The previous implementation issued several queries *per task*.
func (r *Tasks) hydrate(ctx context.Context, tasks []models.Task) error {
	if len(tasks) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(tasks))
	index := make(map[uuid.UUID]int, len(tasks))
	for i, t := range tasks {
		ids[i] = t.ID
		index[t.ID] = i
	}

	assignees, err := r.store.Pool.Query(ctx,
		`SELECT task_id, user_id FROM task_assignees WHERE task_id = ANY($1)`, ids)
	if err != nil {
		return err
	}
	defer assignees.Close()
	for assignees.Next() {
		var taskID, userID uuid.UUID
		if err := assignees.Scan(&taskID, &userID); err != nil {
			return err
		}
		if i, ok := index[taskID]; ok {
			tasks[i].Assignees = append(tasks[i].Assignees, userID)
		}
	}
	if err := assignees.Err(); err != nil {
		return err
	}

	tags, err := r.store.Pool.Query(ctx,
		`SELECT task_id, tag FROM task_tags WHERE task_id = ANY($1) ORDER BY tag`, ids)
	if err != nil {
		return err
	}
	defer tags.Close()
	for tags.Next() {
		var taskID uuid.UUID
		var tag string
		if err := tags.Scan(&taskID, &tag); err != nil {
			return err
		}
		if i, ok := index[taskID]; ok {
			tasks[i].Tags = append(tasks[i].Tags, tag)
		}
	}
	if err := tags.Err(); err != nil {
		return err
	}

	checklist, err := r.store.Pool.Query(ctx, `
		SELECT task_id, id, text, done FROM task_checklist_items
		 WHERE task_id = ANY($1) ORDER BY position`, ids)
	if err != nil {
		return err
	}
	defer checklist.Close()
	for checklist.Next() {
		var taskID uuid.UUID
		var item models.ChecklistItem
		if err := checklist.Scan(&taskID, &item.ID, &item.Text, &item.Done); err != nil {
			return err
		}
		if i, ok := index[taskID]; ok {
			tasks[i].Checklist = append(tasks[i].Checklist, item)
		}
	}
	if err := checklist.Err(); err != nil {
		return err
	}

	comments, err := r.store.Pool.Query(ctx, `
		SELECT task_id, id, author_id, body, created_at FROM task_comments
		 WHERE task_id = ANY($1) ORDER BY created_at`, ids)
	if err != nil {
		return err
	}
	defer comments.Close()
	for comments.Next() {
		var taskID uuid.UUID
		var c models.Comment
		if err := comments.Scan(&taskID, &c.ID, &c.Author, &c.Body, &c.CreatedAt); err != nil {
			return err
		}
		if i, ok := index[taskID]; ok {
			tasks[i].Comments = append(tasks[i].Comments, c)
		}
	}
	return comments.Err()
}

func (r *Tasks) collect(ctx context.Context, sql string, args ...any) ([]models.Task, error) {
	rows, err := r.store.Pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tasks := []models.Task{}
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, *t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := r.hydrate(ctx, tasks); err != nil {
		return nil, err
	}
	return tasks, nil
}

func (r *Tasks) ByProject(ctx context.Context, projectID uuid.UUID) ([]models.Task, error) {
	return r.collect(ctx, `SELECT `+taskColumns+`
		  FROM tasks t WHERE t.project_id = $1
		 ORDER BY t.status, t.position, t.created_at`, projectID)
}

func (r *Tasks) AssignedTo(ctx context.Context, userID uuid.UUID) ([]models.Task, error) {
	return r.collect(ctx, `SELECT `+taskColumns+`
		  FROM tasks t
		  JOIN task_assignees ta ON ta.task_id = t.id
		 WHERE ta.user_id = $1
		 ORDER BY t.due_date NULLS LAST, t.created_at`, userID)
}

func (r *Tasks) Subtasks(ctx context.Context, parentID uuid.UUID) ([]models.Task, error) {
	return r.collect(ctx, `SELECT `+taskColumns+`
		  FROM tasks t WHERE t.parent_task_id = $1 ORDER BY t.position, t.created_at`, parentID)
}

func (r *Tasks) ByID(ctx context.Context, id uuid.UUID) (*models.Task, error) {
	t, err := scanTask(r.store.Pool.QueryRow(ctx, `SELECT `+taskColumns+` FROM tasks t WHERE t.id = $1`, id))
	if err != nil {
		return nil, err
	}
	batch := []models.Task{*t}
	if err := r.hydrate(ctx, batch); err != nil {
		return nil, err
	}
	return &batch[0], nil
}

// SubtaskCounts returns the number of direct children per task in one query.
func (r *Tasks) SubtaskCounts(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]int, error) {
	out := map[uuid.UUID]int{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := r.store.Pool.Query(ctx, `
		SELECT parent_task_id, count(*) FROM tasks
		 WHERE parent_task_id = ANY($1) GROUP BY parent_task_id`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

// NextPosition returns the position a new card should take at the bottom of a
// board column.
func (r *Tasks) NextPosition(ctx context.Context, projectID uuid.UUID, status string) (float64, error) {
	var pos *float64
	err := r.store.Pool.QueryRow(ctx, `
		SELECT max(position) FROM tasks WHERE project_id = $1 AND status = $2`,
		projectID, status).Scan(&pos)
	if err != nil {
		return 0, err
	}
	if pos == nil {
		return 0, nil
	}
	return *pos + 1, nil
}

func (r *Tasks) Create(ctx context.Context, t *models.Task) error {
	if t.ID == uuid.Nil {
		t.ID = uuid.New()
	}
	return r.store.WithTx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO tasks (id, title, description, project_id, parent_task_id, status,
			                   priority, start_date, due_date, estimate_hours, position, created_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
			t.ID, t.Title, t.Description, t.ProjectID, t.ParentTask, t.Status,
			t.Priority, t.StartDate, t.DueDate, t.EstimateHours, t.Order, t.CreatedBy)
		if err != nil {
			return err
		}
		if err := replaceMembers(ctx, tx, "task_assignees", "task_id", t.ID, t.Assignees); err != nil {
			return err
		}
		return replaceTags(ctx, tx, t.ID, t.Tags)
	})
}

// TaskPatch is the allow-list of updatable fields.
type TaskPatch struct {
	Title         *string
	Description   *string
	Assignees     *[]uuid.UUID
	Status        *string
	Priority      *string
	StartDate     **time.Time
	DueDate       **time.Time
	EstimateHours *float64
	Order         *float64
	Tags          *[]string
	Checklist     *[]models.ChecklistItem
}

func (r *Tasks) Update(ctx context.Context, id uuid.UUID, p TaskPatch) error {
	return r.store.WithTx(ctx, func(tx pgx.Tx) error {
		sets := []string{"updated_at = now()"}
		args := []any{id}
		add := func(col string, val any) {
			args = append(args, val)
			sets = append(sets, fmt.Sprintf("%s = $%d", col, len(args)))
		}
		if p.Title != nil {
			add("title", *p.Title)
		}
		if p.Description != nil {
			add("description", *p.Description)
		}
		if p.Priority != nil {
			add("priority", *p.Priority)
		}
		if p.StartDate != nil {
			add("start_date", *p.StartDate)
		}
		if p.DueDate != nil {
			add("due_date", *p.DueDate)
		}
		if p.EstimateHours != nil {
			add("estimate_hours", *p.EstimateHours)
		}
		if p.Order != nil {
			add("position", *p.Order)
		}
		if p.Status != nil {
			add("status", *p.Status)
			// Entering "done" stamps the completion time; leaving it clears
			// the stamp, otherwise a reopened task keeps skewing the
			// completion-trend chart.
			if *p.Status == "done" {
				sets = append(sets, "completed_at = coalesce(completed_at, now())")
			} else {
				sets = append(sets, "completed_at = NULL")
			}
		}

		tag, err := tx.Exec(ctx, `UPDATE tasks SET `+strings.Join(sets, ", ")+` WHERE id = $1`, args...)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}

		if p.Assignees != nil {
			if err := replaceMembers(ctx, tx, "task_assignees", "task_id", id, *p.Assignees); err != nil {
				return err
			}
		}
		if p.Tags != nil {
			if err := replaceTags(ctx, tx, id, *p.Tags); err != nil {
				return err
			}
		}
		if p.Checklist != nil {
			return replaceChecklist(ctx, tx, id, *p.Checklist)
		}
		return nil
	})
}

// Delete removes the task; sub-tasks, assignees, tags, checklist, comments and
// alert records follow through ON DELETE CASCADE.
func (r *Tasks) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.store.Pool.Exec(ctx, `DELETE FROM tasks WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Tasks) AddComment(ctx context.Context, taskID, authorID uuid.UUID, body string) (*models.Comment, error) {
	c := models.Comment{ID: uuid.New(), Author: &authorID, Body: body, CreatedAt: time.Now()}
	_, err := r.store.Pool.Exec(ctx, `
		INSERT INTO task_comments (id, task_id, author_id, body, created_at)
		VALUES ($1,$2,$3,$4,$5)`, c.ID, taskID, authorID, body, c.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func replaceTags(ctx context.Context, tx pgx.Tx, taskID uuid.UUID, tags []string) error {
	if _, err := tx.Exec(ctx, `DELETE FROM task_tags WHERE task_id = $1`, taskID); err != nil {
		return err
	}
	for _, tag := range tags {
		if tag == "" {
			continue
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO task_tags (task_id, tag) VALUES ($1,$2) ON CONFLICT DO NOTHING`,
			taskID, tag); err != nil {
			return err
		}
	}
	return nil
}

func replaceChecklist(ctx context.Context, tx pgx.Tx, taskID uuid.UUID, items []models.ChecklistItem) error {
	if _, err := tx.Exec(ctx, `DELETE FROM task_checklist_items WHERE task_id = $1`, taskID); err != nil {
		return err
	}
	for i, item := range items {
		id := item.ID
		if id == uuid.Nil {
			id = uuid.New()
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO task_checklist_items (id, task_id, text, done, position)
			VALUES ($1,$2,$3,$4,$5)`, id, taskID, item.Text, item.Done, i); err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Deadline alerts
// ---------------------------------------------------------------------------

// DueAlert is one (task, assignee) pair that still needs to be told about a
// deadline. The query does the de-duplication that used to be a nested loop
// over an array field in Go.
type DueAlert struct {
	TaskID    uuid.UUID
	ProjectID uuid.UUID
	UserID    uuid.UUID
	Title     string
	DueDate   time.Time
	Overdue   bool
}

// PendingDeadlineAlerts finds every assignee of an unfinished task due within
// the warning window who has not already been alerted for that exact state.
func (r *Tasks) PendingDeadlineAlerts(ctx context.Context, threshold time.Time) ([]DueAlert, error) {
	rows, err := r.store.Pool.Query(ctx, `
		SELECT t.id, t.project_id, ta.user_id, t.title, t.due_date,
		       (t.due_date < now()) AS overdue
		  FROM tasks t
		  JOIN task_assignees ta ON ta.task_id = t.id
		 WHERE t.status <> 'done'
		   AND t.due_date IS NOT NULL
		   AND t.due_date <= $1
		   AND NOT EXISTS (
		         SELECT 1 FROM task_alerts_sent s
		          WHERE s.task_id = t.id
		            AND s.user_id = ta.user_id
		            AND s.alert_type = CASE WHEN t.due_date < now() THEN 'overdue' ELSE 'due_soon' END
		   )`, threshold)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []DueAlert{}
	for rows.Next() {
		var a DueAlert
		if err := rows.Scan(&a.TaskID, &a.ProjectID, &a.UserID, &a.Title, &a.DueDate, &a.Overdue); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// MarkAlertSent records delivery. The primary key makes a repeat a no-op.
func (r *Tasks) MarkAlertSent(ctx context.Context, taskID, userID uuid.UUID, alertType string) error {
	_, err := r.store.Pool.Exec(ctx, `
		INSERT INTO task_alerts_sent (task_id, user_id, alert_type)
		VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`, taskID, userID, alertType)
	return err
}
