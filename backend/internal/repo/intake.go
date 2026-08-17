package repo

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"projectview/internal/db"
)

// Intake forms: a way to raise work without understanding the board it lands
// on.
//
// A submission becomes an ordinary task. That is the whole design - an intake
// queue that is its own kind of record is a second inbox nobody watches, and
// the point of intake is that a request turns into work where work already
// lives.
type Intake struct{ store *db.Store }

func NewIntake(store *db.Store) *Intake { return &Intake{store: store} }

// IntakeField is one input on a form. Stored as JSONB for the same reason
// templates are: a relational mirror would need migrating in step with every
// field type, and a form built last year would describe inputs that no longer
// exist.
type IntakeField struct {
	Key      string   `json:"key"`
	Label    string   `json:"label"`
	Type     string   `json:"type"`
	Required bool     `json:"required"`
	Options  []string `json:"options,omitempty"`
	Help     string   `json:"help,omitempty"`
}

func ValidIntakeFieldType(t string) bool {
	switch t {
	case "text", "textarea", "number", "date", "select", "checkbox", "email":
		return true
	}
	return false
}

type IntakeForm struct {
	ID          uuid.UUID     `json:"id"`
	ProjectID   uuid.UUID     `json:"projectId"`
	Title       string        `json:"title"`
	Description string        `json:"description"`
	Fields      []IntakeField `json:"fields"`
	Status      string        `json:"targetStatus"`
	Priority    string        `json:"targetPriority"`
	Enabled     bool          `json:"enabled"`
	Public      bool          `json:"public"`
	Slug        string        `json:"slug"`
	CreatedBy   *uuid.UUID    `json:"-"`
	CreatedAt   time.Time     `json:"createdAt"`
	UpdatedAt   time.Time     `json:"updatedAt"`
}

const intakeColumns = `
	id, project_id, title, description, fields, target_status, target_priority,
	enabled, public, slug, created_by, created_at, updated_at`

func scanForm(row pgx.Row) (*IntakeForm, error) {
	var f IntakeForm
	err := row.Scan(&f.ID, &f.ProjectID, &f.Title, &f.Description, &f.Fields,
		&f.Status, &f.Priority, &f.Enabled, &f.Public, &f.Slug,
		&f.CreatedBy, &f.CreatedAt, &f.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if f.Fields == nil {
		f.Fields = []IntakeField{}
	}
	return &f, nil
}

// NewSlug produces the unguessable part of a public form's URL.
//
// 128 bits from crypto/rand, not a name-derived slug: a public form is
// reachable by anyone who has the address, so the address has to be the secret.
// "acme-bug-report" would be guessable in an afternoon.
func NewSlug() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func (r *Intake) ForProject(ctx context.Context, projectID uuid.UUID) ([]IntakeForm, error) {
	rows, err := r.store.Pool.Query(ctx,
		`SELECT `+intakeColumns+` FROM intake_forms WHERE project_id = $1 ORDER BY created_at DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []IntakeForm{}
	for rows.Next() {
		f, err := scanForm(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *f)
	}
	return out, rows.Err()
}

func (r *Intake) ByID(ctx context.Context, id uuid.UUID) (*IntakeForm, error) {
	return scanForm(r.store.Pool.QueryRow(ctx,
		`SELECT `+intakeColumns+` FROM intake_forms WHERE id = $1`, id))
}

// BySlug resolves a form from its public address. Only enabled forms answer:
// closing a form has to actually close it, and a disabled one that still
// accepted submissions would be a door somebody believed they had shut.
func (r *Intake) BySlug(ctx context.Context, slug string) (*IntakeForm, error) {
	return scanForm(r.store.Pool.QueryRow(ctx,
		`SELECT `+intakeColumns+` FROM intake_forms WHERE slug = $1 AND enabled`, slug))
}

func (r *Intake) Create(ctx context.Context, f *IntakeForm) error {
	if f.ID == uuid.Nil {
		f.ID = uuid.New()
	}
	return r.store.Pool.QueryRow(ctx, `
		INSERT INTO intake_forms
		    (id, project_id, title, description, fields, target_status,
		     target_priority, enabled, public, slug, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		RETURNING created_at, updated_at`,
		f.ID, f.ProjectID, f.Title, f.Description, f.Fields, f.Status,
		f.Priority, f.Enabled, f.Public, f.Slug, f.CreatedBy).
		Scan(&f.CreatedAt, &f.UpdatedAt)
}

func (r *Intake) SetEnabled(ctx context.Context, id uuid.UUID, enabled bool) error {
	tag, err := r.store.Pool.Exec(ctx,
		`UPDATE intake_forms SET enabled = $2, updated_at = now() WHERE id = $1`, id, enabled)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Intake) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.store.Pool.Exec(ctx, `DELETE FROM intake_forms WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

type IntakeSubmission struct {
	ID          uuid.UUID      `json:"id"`
	FormID      uuid.UUID      `json:"formId"`
	TaskID      *uuid.UUID     `json:"taskId,omitempty"`
	Answers     map[string]any `json:"answers"`
	SubmittedBy *uuid.UUID     `json:"-"`
	Name        string         `json:"submitterName,omitempty"`
	Email       string         `json:"submitterEmail,omitempty"`
	CreatedAt   time.Time      `json:"createdAt"`
}

// RecordSubmission stores what was asked for, beside the task it produced.
//
// The answers are kept as well as written into the task, because the task is
// edited afterwards - retitled, re-prioritised, rewritten - and the record of
// what somebody actually requested should not change with it.
func (r *Intake) RecordSubmission(ctx context.Context, s *IntakeSubmission) error {
	if s.ID == uuid.Nil {
		s.ID = uuid.New()
	}
	return r.store.Pool.QueryRow(ctx, `
		INSERT INTO intake_submissions
		    (id, form_id, task_id, answers, submitted_by, submitter_name, submitter_email)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		RETURNING created_at`,
		s.ID, s.FormID, s.TaskID, s.Answers, s.SubmittedBy, s.Name, s.Email).
		Scan(&s.CreatedAt)
}

func (r *Intake) Submissions(ctx context.Context, formID uuid.UUID, limit int) ([]IntakeSubmission, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := r.store.Pool.Query(ctx, `
		SELECT id, form_id, task_id, answers, submitted_by, submitter_name, submitter_email, created_at
		  FROM intake_submissions WHERE form_id = $1 ORDER BY created_at DESC LIMIT $2`, formID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []IntakeSubmission{}
	for rows.Next() {
		var s IntakeSubmission
		if err := rows.Scan(&s.ID, &s.FormID, &s.TaskID, &s.Answers, &s.SubmittedBy,
			&s.Name, &s.Email, &s.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
