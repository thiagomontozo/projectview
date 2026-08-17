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
	t.position, t.created_by, t.created_at, t.updated_at, t.custom_fields`

func scanTask(row pgx.Row) (*models.Task, error) {
	var t models.Task
	err := row.Scan(&t.ID, &t.Title, &t.Description, &t.ProjectID, &t.ParentTask,
		&t.Status, &t.Priority, &t.StartDate, &t.DueDate, &t.CompletedAt,
		&t.EstimateHours, &t.Order, &t.CreatedBy, &t.CreatedAt, &t.UpdatedAt,
		&t.CustomFields)
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
	if t.CustomFields == nil {
		// Keep it {} rather than null, so clients can index into it without
		// a guard on every read.
		t.CustomFields = map[string]any{}
	}
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

// TaskQuery describes a filtered, searchable, paginated task listing.
//
// The filters are slices rather than single values because that is what the
// views ask for: "show me urgent and high", "show me Ana's and Bruno's". An
// empty slice means no constraint, so the zero value is still "everything".
type TaskQuery struct {
	ProjectID   *uuid.UUID
	AssigneeIDs []uuid.UUID
	Statuses    []string
	Priorities  []string
	ParentOnly  bool // exclude sub-tasks
	Overdue     bool
	Search      string

	// DueFrom and DueTo bound the window a date-shaped view is showing.
	//
	// The calendar draws a month and the timeline a span, so neither ever
	// needed the whole project - they needed the dates on screen. Without this
	// they drew from whatever page happened to be loaded, which is a chart of
	// part of the data that looks exactly like a chart of all of it.
	//
	// A task with no dates is outside every window by definition and is
	// excluded when either bound is set, which is what those views already do
	// when they lay it out.
	DueFrom *time.Time
	DueTo   *time.Time

	// SortColumn is a SQL expression, never a caller-supplied string: it comes
	// from the allow-list in SortableTaskColumns, so the ORDER BY clause cannot
	// be steered from the query string.
	SortColumn string
	SortDesc   bool

	// Cursor anchors on (created_at, id) of the last row of the previous page.
	// Only meaningful for the default ordering; see Offset.
	CursorTime *time.Time
	CursorID   *uuid.UUID
	// Offset pages a result set ordered by something other than created_at.
	//
	// A cursor anchors on the sort key, so it only works for the ordering it
	// was designed around; supporting six sort fields would mean six cursor
	// encodings. Offset is used instead, and the trade-off is deliberate rather
	// than overlooked: its cost grows with depth, which matters when scrolling
	// a whole table and does not when a board column loads another hundred
	// cards. The cursor path is untouched for the callers that use it.
	Offset int
	Limit  int
}

// SortableTaskColumns maps the sort names a client may ask for onto the SQL
// that implements them. Nothing outside this map reaches an ORDER BY.
//
// Two of these encode a rule the interface already had and the database does
// not know about, and they are here so the server and the client agree rather
// than each sorting its own way:
//
//   - priority orders by severity, not alphabetically, so "urgent" leads.
//   - status follows the project's own column order, so a board sorted by
//     status matches the columns it is drawn in.
var SortableTaskColumns = map[string]string{
	"position": "t.position",
	"title":    "t.title",
	"dueDate":  "t.due_date",
	"created":  "t.created_at",
	"priority": `CASE t.priority WHEN 'urgent' THEN 0 WHEN 'high' THEN 1
	                             WHEN 'medium' THEN 2 ELSE 3 END`,
	"status": `(SELECT ps.position FROM project_statuses ps
	             WHERE ps.project_id = t.project_id AND ps.key = t.status)`,
}

// Search returns a page of tasks matching the query, newest first.
//
// One row beyond the limit is fetched so the caller can tell whether more
// exist without a second COUNT.
func (r *Tasks) Search(ctx context.Context, q TaskQuery) ([]models.Task, error) {
	limit := q.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	// The ordering and the paging clause are built rather than parameterised,
	// because neither can be a bind variable. Both are assembled only from the
	// allow-list and from integers, so nothing a caller sends reaches the SQL
	// as text.
	order := taskOrderBy(q.SortColumn, q.SortDesc)

	// Offset applies whatever the ordering is. Tying it to a custom sort, as
	// the first draft did, meant an unsorted listing silently ignored it and
	// returned page one forever.
	paging := fmt.Sprintf("LIMIT %d", limit+1)
	if q.Offset > 0 {
		paging = fmt.Sprintf("LIMIT %d OFFSET %d", limit+1, q.Offset)
	}

	// websearch_to_tsquery accepts what a person would actually type -
	// quoted phrases, "or", leading "-" to exclude - and never errors on
	// malformed input, unlike to_tsquery.
	return r.collect(ctx, `
		SELECT `+taskColumns+`
		  FROM tasks t
		 WHERE `+taskFilterSQL+`
		   AND ($10::timestamptz IS NULL
		        OR t.created_at < $10
		        OR (t.created_at = $10 AND t.id < $11::uuid))
		 ORDER BY `+order+`
		 `+paging,
		q.ProjectID, q.AssigneeIDs, q.Statuses, q.Priorities, q.ParentOnly, q.Overdue,
		q.Search, q.DueFrom, q.DueTo, q.CursorTime, q.CursorID)
}

// taskOrderBy builds the ORDER BY clause.
//
// Pure and separate so the two rules it encodes can be asserted without a
// database. Both used to live in the client's sort function and moved here when
// paging did: with only a page in hand, the client can no longer order what it
// cannot see, and a rule enforced in one place and not the other would make the
// first page of a sort disagree with the second.
//
//   - NULLS LAST in *both* directions. PostgreSQL puts them first when
//     descending, and the rule the views have always followed is that a task
//     with no due date is unscheduled rather than infinitely early.
//   - The id breaks ties, so the order is total. Without it two tasks of equal
//     priority could swap places between requests, and "load more" would repeat
//     one row and skip another.
func taskOrderBy(column string, desc bool) string {
	if column == "" {
		// The legacy ordering, which the cursor pagination anchors on.
		return "t.created_at DESC, t.id DESC"
	}
	direction := "ASC"
	if desc {
		direction = "DESC"
	}
	return fmt.Sprintf("%s %s NULLS LAST, t.id ASC", column, direction)
}

// taskFilterSQL is the WHERE body shared by the listing and its count, so the
// two can never drift into disagreeing about what "matching" means - which
// would show a total that the pages beneath it never add up to.
//
// An empty filter means "no constraint" rather than "match nothing", so the
// zero value of TaskQuery still selects everything.
//
// COALESCE around cardinality is load-bearing, not defensive. A nil Go slice
// arrives as SQL NULL rather than as an empty array, and cardinality(NULL) is
// NULL - so a bare "cardinality(...) = 0" evaluates to NULL, which is not true,
// the guard fails open into the filter, and every unfiltered listing matches
// nothing. Coalescing to 0 is what makes "no filter" mean no filter.
const taskFilterSQL = `
		       ($1::uuid IS NULL OR t.project_id = $1)
		   AND (COALESCE(cardinality($2::uuid[]), 0) = 0 OR EXISTS (
		         SELECT 1 FROM task_assignees ta
		          WHERE ta.task_id = t.id AND ta.user_id = ANY($2)))
		   AND (COALESCE(cardinality($3::text[]), 0) = 0 OR t.status = ANY($3))
		   AND (COALESCE(cardinality($4::text[]), 0) = 0 OR t.priority = ANY($4))
		   AND (NOT $5::boolean OR t.parent_task_id IS NULL)
		   AND (NOT $6::boolean OR (t.due_date < now() AND t.status <> 'done'))
		   AND ($7::text = '' OR t.search @@ websearch_to_tsquery('simple', $7))
		   AND ($8::timestamptz IS NULL
		        OR COALESCE(t.due_date, t.start_date) >= $8)
		   AND ($9::timestamptz IS NULL
		        OR COALESCE(t.start_date, t.due_date) <= $9)`

// CountMatching reports how many tasks match, ignoring pagination.
//
// This is what makes "load more" honest: a column that shows its first hundred
// cards has to be able to say how many there are, or the interface is hiding
// work while looking complete.
func (r *Tasks) CountMatching(ctx context.Context, q TaskQuery) (int64, error) {
	var n int64
	err := r.store.Pool.QueryRow(ctx, `
		SELECT count(*) FROM tasks t WHERE `+taskFilterSQL,
		q.ProjectID, q.AssigneeIDs, q.Statuses, q.Priorities,
		q.ParentOnly, q.Overdue, q.Search, q.DueFrom, q.DueTo).Scan(&n)
	return n, err
}

// CountByStatus returns how many tasks match in each status, in one query.
//
// The board needs a total per column, and asking for them one at a time would
// be a query per column on every load - the shape this whole change exists to
// remove.
func (r *Tasks) CountByStatus(ctx context.Context, q TaskQuery) (map[string]int64, error) {
	rows, err := r.store.Pool.Query(ctx, `
		SELECT t.status, count(*) FROM tasks t WHERE `+taskFilterSQL+`
		 GROUP BY t.status`,
		q.ProjectID, q.AssigneeIDs, q.Statuses, q.Priorities,
		q.ParentOnly, q.Overdue, q.Search, q.DueFrom, q.DueTo)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]int64{}
	for rows.Next() {
		var status string
		var n int64
		if err := rows.Scan(&status, &n); err != nil {
			return nil, err
		}
		out[status] = n
	}
	return out, rows.Err()
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

// SetRecurrenceParent records which instance a task was spawned from. Kept for
// the trail rather than for the logic, so it is set after creation instead of
// widening Create's signature for a column only one caller fills.
func (r *Tasks) SetRecurrenceParent(ctx context.Context, taskID, parentID uuid.UUID) error {
	_, err := r.store.Pool.Exec(ctx,
		`UPDATE tasks SET recurrence_parent_id = $2 WHERE id = $1`, taskID, parentID)
	return err
}

// CommentTask reports which task a comment belongs to, so a caller naming a
// comment can be checked against the task it claims to be on rather than
// trusted about it.
func (r *Tasks) CommentTask(ctx context.Context, commentID uuid.UUID) (uuid.UUID, error) {
	var taskID uuid.UUID
	err := r.store.Pool.QueryRow(ctx,
		`SELECT task_id FROM task_comments WHERE id = $1`, commentID).Scan(&taskID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrNotFound
	}
	return taskID, err
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
