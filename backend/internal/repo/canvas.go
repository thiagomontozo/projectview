package repo

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"projectview/internal/db"
)

// Whiteboards, spreadsheets, clips and saved views.
//
// Together because they are the same shape: a document that belongs to a
// project and is edited by whoever can work in it. The two editable documents -
// the board and the sheet - also share a problem, which is that two people will
// have them open at once, so both carry a version and both refuse a write based
// on a version somebody has already replaced.

// ErrStale is returned when a document was written by somebody else since it
// was read. Its own error rather than a generic conflict, because the caller
// can do something useful with it: re-read and re-apply, rather than tell
// somebody their work is gone.
var ErrStale = errors.New("that document changed while you were editing it")

/* --- Saved views ------------------------------------------------------------ */

type SavedView struct {
	ID            uuid.UUID      `json:"id"`
	ProjectID     uuid.UUID      `json:"projectId"`
	Name          string         `json:"name"`
	Kind          string         `json:"kind"`
	GroupBy       string         `json:"groupBy"`
	Filters       map[string]any `json:"filters"`
	SortBy        string         `json:"sortBy,omitempty"`
	SortDirection string         `json:"sortDirection,omitempty"`
	CreatedBy     *uuid.UUID     `json:"createdBy,omitempty"`
	CreatedAt     time.Time      `json:"createdAt"`
}

type SavedViews struct{ store *db.Store }

func NewSavedViews(store *db.Store) *SavedViews { return &SavedViews{store: store} }

const savedViewColumns = `id, project_id, name, kind, group_by, filters,
	COALESCE(sort_by, ''), COALESCE(sort_direction, ''), created_by, created_at`

func scanSavedView(row pgx.Row) (*SavedView, error) {
	var v SavedView
	err := row.Scan(&v.ID, &v.ProjectID, &v.Name, &v.Kind, &v.GroupBy, &v.Filters,
		&v.SortBy, &v.SortDirection, &v.CreatedBy, &v.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if v.Filters == nil {
		v.Filters = map[string]any{}
	}
	return &v, nil
}

func (r *SavedViews) ForProject(ctx context.Context, projectID uuid.UUID) ([]SavedView, error) {
	rows, err := r.store.Pool.Query(ctx,
		`SELECT `+savedViewColumns+` FROM saved_views WHERE project_id = $1 ORDER BY created_at`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []SavedView{}
	for rows.Next() {
		v, err := scanSavedView(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

func (r *SavedViews) ByID(ctx context.Context, id uuid.UUID) (*SavedView, error) {
	return scanSavedView(r.store.Pool.QueryRow(ctx,
		`SELECT `+savedViewColumns+` FROM saved_views WHERE id = $1`, id))
}

// Create stores a view, or replaces the one with that name.
//
// Upsert rather than a conflict: somebody pressing save on a name they already
// used means "update it", and answering with an error would make adjusting a
// saved view a delete-then-create.
func (r *SavedViews) Create(ctx context.Context, v *SavedView) error {
	if v.ID == uuid.Nil {
		v.ID = uuid.New()
	}
	if v.Filters == nil {
		v.Filters = map[string]any{}
	}
	return r.store.Pool.QueryRow(ctx, `
		INSERT INTO saved_views
		    (id, project_id, name, kind, group_by, filters, sort_by, sort_direction, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,NULLIF($7,''),NULLIF($8,''),$9)
		ON CONFLICT (project_id, name) DO UPDATE
		    SET kind = EXCLUDED.kind, group_by = EXCLUDED.group_by,
		        filters = EXCLUDED.filters, sort_by = EXCLUDED.sort_by,
		        sort_direction = EXCLUDED.sort_direction, updated_at = now()
		RETURNING id, created_at`,
		v.ID, v.ProjectID, v.Name, v.Kind, v.GroupBy, v.Filters, v.SortBy,
		v.SortDirection, v.CreatedBy).Scan(&v.ID, &v.CreatedAt)
}

func (r *SavedViews) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.store.Pool.Exec(ctx, `DELETE FROM saved_views WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

/* --- Whiteboards ------------------------------------------------------------ */

type Whiteboard struct {
	ID        uuid.UUID      `json:"id"`
	ProjectID uuid.UUID      `json:"projectId"`
	Title     string         `json:"title"`
	Scene     map[string]any `json:"scene"`
	Version   int            `json:"version"`
	CreatedBy *uuid.UUID     `json:"createdBy,omitempty"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
}

type Whiteboards struct{ store *db.Store }

func NewWhiteboards(store *db.Store) *Whiteboards { return &Whiteboards{store: store} }

const whiteboardColumns = `id, project_id, title, scene, version, created_by, created_at, updated_at`

func scanWhiteboard(row pgx.Row) (*Whiteboard, error) {
	var b Whiteboard
	err := row.Scan(&b.ID, &b.ProjectID, &b.Title, &b.Scene, &b.Version,
		&b.CreatedBy, &b.CreatedAt, &b.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if b.Scene == nil {
		b.Scene = map[string]any{"items": []any{}}
	}
	return &b, nil
}

// ForProject lists boards without their scenes.
//
// The scene is the whole document and a busy board is not small, so listing ten
// would mean fetching ten documents to draw ten titles. Callers that need one
// open it.
func (r *Whiteboards) ForProject(ctx context.Context, projectID uuid.UUID) ([]Whiteboard, error) {
	rows, err := r.store.Pool.Query(ctx, `
		SELECT id, project_id, title, '{}'::jsonb, version, created_by, created_at, updated_at
		  FROM whiteboards WHERE project_id = $1 ORDER BY updated_at DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Whiteboard{}
	for rows.Next() {
		b, err := scanWhiteboard(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *b)
	}
	return out, rows.Err()
}

func (r *Whiteboards) ByID(ctx context.Context, id uuid.UUID) (*Whiteboard, error) {
	return scanWhiteboard(r.store.Pool.QueryRow(ctx,
		`SELECT `+whiteboardColumns+` FROM whiteboards WHERE id = $1`, id))
}

func (r *Whiteboards) Create(ctx context.Context, b *Whiteboard) error {
	if b.ID == uuid.Nil {
		b.ID = uuid.New()
	}
	if b.Scene == nil {
		b.Scene = map[string]any{"items": []any{}}
	}
	return r.store.Pool.QueryRow(ctx, `
		INSERT INTO whiteboards (id, project_id, title, scene, created_by)
		VALUES ($1,$2,$3,$4,$5)
		RETURNING version, created_at, updated_at`,
		b.ID, b.ProjectID, b.Title, b.Scene, b.CreatedBy).
		Scan(&b.Version, &b.CreatedAt, &b.UpdatedAt)
}

// Save writes a scene, refusing one based on a version that has moved on.
//
// The refusal is the feature. Two people on one board is the ordinary case, and
// last-write-wins would delete somebody's work with nothing on screen to say it
// happened.
func (r *Whiteboards) Save(ctx context.Context, id uuid.UUID, scene map[string]any, title string, expected int) (*Whiteboard, error) {
	board, err := scanWhiteboard(r.store.Pool.QueryRow(ctx, `
		UPDATE whiteboards
		   SET scene = $2,
		       title = COALESCE(NULLIF($3, ''), title),
		       version = version + 1,
		       updated_at = now()
		 WHERE id = $1 AND version = $4
		RETURNING `+whiteboardColumns, id, scene, title, expected))
	if errors.Is(err, ErrNotFound) {
		// No row matched: either it is gone, or somebody else has written it.
		// The second is far more likely and needs a different answer.
		if _, missing := r.ByID(ctx, id); missing != nil {
			return nil, missing
		}
		return nil, ErrStale
	}
	return board, err
}

func (r *Whiteboards) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.store.Pool.Exec(ctx, `DELETE FROM whiteboards WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

/* --- Spreadsheets ----------------------------------------------------------- */

type Spreadsheet struct {
	ID        uuid.UUID      `json:"id"`
	ProjectID uuid.UUID      `json:"projectId"`
	Title     string         `json:"title"`
	Cells     map[string]any `json:"cells"`
	Rows      int            `json:"rows"`
	Cols      int            `json:"cols"`
	Version   int            `json:"version"`
	CreatedBy *uuid.UUID     `json:"createdBy,omitempty"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
}

type Spreadsheets struct{ store *db.Store }

func NewSpreadsheets(store *db.Store) *Spreadsheets { return &Spreadsheets{store: store} }

const sheetColumns = `id, project_id, title, cells, row_count, col_count, version,
	created_by, created_at, updated_at`

func scanSheet(row pgx.Row) (*Spreadsheet, error) {
	var s Spreadsheet
	err := row.Scan(&s.ID, &s.ProjectID, &s.Title, &s.Cells, &s.Rows, &s.Cols,
		&s.Version, &s.CreatedBy, &s.CreatedAt, &s.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if s.Cells == nil {
		s.Cells = map[string]any{}
	}
	return &s, nil
}

func (r *Spreadsheets) ForProject(ctx context.Context, projectID uuid.UUID) ([]Spreadsheet, error) {
	rows, err := r.store.Pool.Query(ctx, `
		SELECT id, project_id, title, '{}'::jsonb, row_count, col_count, version,
		       created_by, created_at, updated_at
		  FROM spreadsheets WHERE project_id = $1 ORDER BY updated_at DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Spreadsheet{}
	for rows.Next() {
		s, err := scanSheet(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}

func (r *Spreadsheets) ByID(ctx context.Context, id uuid.UUID) (*Spreadsheet, error) {
	return scanSheet(r.store.Pool.QueryRow(ctx,
		`SELECT `+sheetColumns+` FROM spreadsheets WHERE id = $1`, id))
}

func (r *Spreadsheets) Create(ctx context.Context, s *Spreadsheet) error {
	if s.ID == uuid.Nil {
		s.ID = uuid.New()
	}
	if s.Cells == nil {
		s.Cells = map[string]any{}
	}
	return r.store.Pool.QueryRow(ctx, `
		INSERT INTO spreadsheets (id, project_id, title, cells, created_by)
		VALUES ($1,$2,$3,$4,$5)
		RETURNING row_count, col_count, version, created_at, updated_at`,
		s.ID, s.ProjectID, s.Title, s.Cells, s.CreatedBy).
		Scan(&s.Rows, &s.Cols, &s.Version, &s.CreatedAt, &s.UpdatedAt)
}

func (r *Spreadsheets) Save(ctx context.Context, id uuid.UUID, cells map[string]any, title string, rows, cols, expected int) (*Spreadsheet, error) {
	sheet, err := scanSheet(r.store.Pool.QueryRow(ctx, `
		UPDATE spreadsheets
		   SET cells = $2,
		       title = COALESCE(NULLIF($3, ''), title),
		       row_count = GREATEST($4, row_count),
		       col_count = GREATEST($5, col_count),
		       version = version + 1,
		       updated_at = now()
		 WHERE id = $1 AND version = $6
		RETURNING `+sheetColumns, id, cells, title, rows, cols, expected))
	if errors.Is(err, ErrNotFound) {
		if _, missing := r.ByID(ctx, id); missing != nil {
			return nil, missing
		}
		return nil, ErrStale
	}
	return sheet, err
}

func (r *Spreadsheets) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.store.Pool.Exec(ctx, `DELETE FROM spreadsheets WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

/* --- Clips ------------------------------------------------------------------ */

type Clip struct {
	ID          uuid.UUID  `json:"id"`
	ProjectID   uuid.UUID  `json:"projectId"`
	TaskID      *uuid.UUID `json:"taskId,omitempty"`
	Title       string     `json:"title"`
	ContentType string     `json:"contentType"`
	SizeBytes   int64      `json:"sizeBytes"`
	DurationMS  *int       `json:"durationMs,omitempty"`
	// Never sent to a client, for the reason the attachments table gives: it is
	// the address of the object, and the only legitimate way to reach one is a
	// signed URL this server issues after checking who is asking.
	StorageKey string     `json:"-"`
	CreatedBy  *uuid.UUID `json:"createdBy,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
}

type Clips struct{ store *db.Store }

func NewClips(store *db.Store) *Clips { return &Clips{store: store} }

const clipColumns = `id, project_id, task_id, title, content_type, size_bytes,
	duration_ms, storage_key, created_by, created_at`

func scanClip(row pgx.Row) (*Clip, error) {
	var c Clip
	err := row.Scan(&c.ID, &c.ProjectID, &c.TaskID, &c.Title, &c.ContentType,
		&c.SizeBytes, &c.DurationMS, &c.StorageKey, &c.CreatedBy, &c.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &c, err
}

func (r *Clips) ForProject(ctx context.Context, projectID uuid.UUID) ([]Clip, error) {
	rows, err := r.store.Pool.Query(ctx,
		`SELECT `+clipColumns+` FROM clips WHERE project_id = $1 ORDER BY created_at DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Clip{}
	for rows.Next() {
		c, err := scanClip(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

func (r *Clips) ByID(ctx context.Context, id uuid.UUID) (*Clip, error) {
	return scanClip(r.store.Pool.QueryRow(ctx,
		`SELECT `+clipColumns+` FROM clips WHERE id = $1`, id))
}

func (r *Clips) Create(ctx context.Context, c *Clip) error {
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	return r.store.Pool.QueryRow(ctx, `
		INSERT INTO clips (id, project_id, task_id, title, storage_key,
		                   content_type, size_bytes, duration_ms, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		RETURNING created_at`,
		c.ID, c.ProjectID, c.TaskID, c.Title, c.StorageKey, c.ContentType,
		c.SizeBytes, c.DurationMS, c.CreatedBy).Scan(&c.CreatedAt)
}

func (r *Clips) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.store.Pool.Exec(ctx, `DELETE FROM clips WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
