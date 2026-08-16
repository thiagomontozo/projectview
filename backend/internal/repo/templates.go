package repo

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"projectview/internal/db"
	"projectview/internal/models"
)

// Templates stores reusable shapes of work.
//
// The body is a snapshot rather than a relational mirror of tasks, checklists
// and custom fields. A relational template would need migrating in step with
// every column a task grows, and one captured a year ago would then describe a
// task that no longer exists; a snapshot describes what to create at the moment
// it was taken, and applying it is a translation that ignores what it no longer
// recognises rather than failing whole.
type Templates struct{ store *db.Store }

func NewTemplates(store *db.Store) *Templates { return &Templates{store: store} }

const (
	TemplateTask    = "task"
	TemplateProject = "project"
)

func ValidTemplateKind(kind string) bool {
	return kind == TemplateTask || kind == TemplateProject
}

// TemplateTaskSpec is one task a template will create. Dates are deliberately
// absent: a template captured in March must not create a task due in March.
// Offsets are carried instead, in days from the moment it is applied.
type TemplateTaskSpec struct {
	Title         string         `json:"title"`
	Description   string         `json:"description,omitempty"`
	Status        string         `json:"status,omitempty"`
	Priority      string         `json:"priority,omitempty"`
	EstimateHours float64        `json:"estimateHours,omitempty"`
	Tags          []string       `json:"tags,omitempty"`
	Checklist     []string       `json:"checklist,omitempty"`
	CustomFields  map[string]any `json:"customFields,omitempty"`
	// StartOffsetDays and DueOffsetDays are counted from the day the template
	// is applied, so a kickoff plan lands on the week it is actually used.
	StartOffsetDays *int `json:"startOffsetDays,omitempty"`
	DueOffsetDays   *int `json:"dueOffsetDays,omitempty"`
	// Sub-tasks, one level. Deeper nesting is possible in the model and left
	// out here on purpose: a template nobody can read is not reusable.
	Subtasks []TemplateTaskSpec `json:"subtasks,omitempty"`
}

// TemplatePayload is the whole body. A task template uses Task; a project
// template uses the rest.
type TemplatePayload struct {
	Task     *TemplateTaskSpec      `json:"task,omitempty"`
	Statuses []models.ProjectStatus `json:"statuses,omitempty"`
	Tasks    []TemplateTaskSpec     `json:"tasks,omitempty"`
	Color    string                 `json:"color,omitempty"`
}

type Template struct {
	ID          uuid.UUID       `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Kind        string          `json:"kind"`
	SpaceID     *uuid.UUID      `json:"spaceId,omitempty"`
	Payload     TemplatePayload `json:"payload"`
	CreatedBy   *uuid.UUID      `json:"-"`
	CreatedAt   time.Time       `json:"createdAt"`
	UpdatedAt   time.Time       `json:"updatedAt"`
}

const templateColumns = `id, name, description, kind, space_id, payload, created_by, created_at, updated_at`

func scanTemplate(row pgx.Row) (*Template, error) {
	var t Template
	err := row.Scan(&t.ID, &t.Name, &t.Description, &t.Kind, &t.SpaceID,
		&t.Payload, &t.CreatedBy, &t.CreatedAt, &t.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// List returns the templates visible in a scope: those belonging to the space,
// plus the global ones, because a global template is available everywhere by
// definition.
func (r *Templates) List(ctx context.Context, kind string, spaceID *uuid.UUID) ([]Template, error) {
	rows, err := r.store.Pool.Query(ctx, `
		SELECT `+templateColumns+`
		  FROM templates
		 WHERE ($1::text = '' OR kind = $1)
		   AND (space_id IS NULL OR $2::uuid IS NULL OR space_id = $2)
		 ORDER BY kind, name`, kind, spaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Template{}
	for rows.Next() {
		t, err := scanTemplate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

func (r *Templates) ByID(ctx context.Context, id uuid.UUID) (*Template, error) {
	return scanTemplate(r.store.Pool.QueryRow(ctx,
		`SELECT `+templateColumns+` FROM templates WHERE id = $1`, id))
}

func (r *Templates) Create(ctx context.Context, t *Template) error {
	if t.ID == uuid.Nil {
		t.ID = uuid.New()
	}
	return r.store.Pool.QueryRow(ctx, `
		INSERT INTO templates (id, name, description, kind, space_id, payload, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		RETURNING created_at, updated_at`,
		t.ID, t.Name, t.Description, t.Kind, t.SpaceID, t.Payload, t.CreatedBy).
		Scan(&t.CreatedAt, &t.UpdatedAt)
}

func (r *Templates) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.store.Pool.Exec(ctx, `DELETE FROM templates WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
