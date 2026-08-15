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

type Projects struct{ store *db.Store }

func NewProjects(store *db.Store) *Projects { return &Projects{store: store} }

const projectColumns = `
	p.id, p.name, p.key, p.description, p.color, p.status, p.team_id, p.owner_id,
	p.start_date, p.end_date, p.created_by, p.created_at, p.updated_at,
	p.space_id, p.folder_id, p.position, p.archived`

func scanProject(row pgx.Row) (*models.Project, error) {
	var p models.Project
	err := row.Scan(&p.ID, &p.Name, &p.Key, &p.Description, &p.Color, &p.Status,
		&p.TeamID, &p.Owner, &p.StartDate, &p.EndDate, &p.CreatedBy,
		&p.CreatedAt, &p.UpdatedAt,
		&p.SpaceID, &p.FolderID, &p.Position, &p.Archived)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	p.Members = []uuid.UUID{}
	p.Statuses = []models.ProjectStatus{}
	return &p, nil
}

// List loads every project with members and status columns attached, in three
// queries total regardless of how many projects there are.
func (r *Projects) List(ctx context.Context) ([]models.Project, error) {
	rows, err := r.store.Pool.Query(ctx,
		`SELECT `+projectColumns+` FROM projects p ORDER BY p.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	projects := []models.Project{}
	index := map[uuid.UUID]int{}
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		index[p.ID] = len(projects)
		projects = append(projects, *p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(projects) == 0 {
		return projects, nil
	}

	memberRows, err := r.store.Pool.Query(ctx, `SELECT project_id, user_id FROM project_members`)
	if err != nil {
		return nil, err
	}
	defer memberRows.Close()
	for memberRows.Next() {
		var projectID, userID uuid.UUID
		if err := memberRows.Scan(&projectID, &userID); err != nil {
			return nil, err
		}
		if i, ok := index[projectID]; ok {
			projects[i].Members = append(projects[i].Members, userID)
		}
	}
	if err := memberRows.Err(); err != nil {
		return nil, err
	}

	statusRows, err := r.store.Pool.Query(ctx,
		`SELECT project_id, key, label, position, color FROM project_statuses ORDER BY project_id, position`)
	if err != nil {
		return nil, err
	}
	defer statusRows.Close()
	for statusRows.Next() {
		var projectID uuid.UUID
		var s models.ProjectStatus
		if err := statusRows.Scan(&projectID, &s.Key, &s.Label, &s.Order, &s.Color); err != nil {
			return nil, err
		}
		if i, ok := index[projectID]; ok {
			projects[i].Statuses = append(projects[i].Statuses, s)
		}
	}
	return projects, statusRows.Err()
}

func (r *Projects) ByID(ctx context.Context, id uuid.UUID) (*models.Project, error) {
	p, err := scanProject(r.store.Pool.QueryRow(ctx,
		`SELECT `+projectColumns+` FROM projects p WHERE p.id = $1`, id))
	if err != nil {
		return nil, err
	}
	if p.Members, err = r.memberIDs(ctx, id); err != nil {
		return nil, err
	}
	if p.Statuses, err = r.statuses(ctx, id); err != nil {
		return nil, err
	}
	return p, nil
}

func (r *Projects) memberIDs(ctx context.Context, projectID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := r.store.Pool.Query(ctx, `SELECT user_id FROM project_members WHERE project_id = $1`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (r *Projects) statuses(ctx context.Context, projectID uuid.UUID) ([]models.ProjectStatus, error) {
	rows, err := r.store.Pool.Query(ctx,
		`SELECT key, label, position, color FROM project_statuses WHERE project_id = $1 ORDER BY position`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.ProjectStatus{}
	for rows.Next() {
		var s models.ProjectStatus
		if err := rows.Scan(&s.Key, &s.Label, &s.Order, &s.Color); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Create writes the project, its members and its kanban columns in one
// transaction, so a failure cannot leave a project without status columns.
func (r *Projects) Create(ctx context.Context, p *models.Project) error {
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	return r.store.WithTx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO projects (id, name, key, description, color, status, team_id,
			                      owner_id, start_date, end_date, created_by,
			                      space_id, folder_id, position)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
			p.ID, p.Name, p.Key, p.Description, p.Color, p.Status, p.TeamID,
			p.Owner, p.StartDate, p.EndDate, p.CreatedBy,
			p.SpaceID, p.FolderID, p.Position)
		if err != nil {
			return err
		}
		if err := replaceMembers(ctx, tx, "project_members", "project_id", p.ID, p.Members); err != nil {
			return err
		}
		return replaceStatuses(ctx, tx, p.ID, p.Statuses)
	})
}

// ProjectPatch is the allow-list of updatable fields. Key and owner are absent
// on purpose - see the handler for why.
type ProjectPatch struct {
	Name        *string
	Description *string
	Color       *string
	Status      *string
	TeamID      **uuid.UUID
	Members     *[]uuid.UUID
	StartDate   **time.Time
	EndDate     **time.Time
	Statuses    *[]models.ProjectStatus
	SpaceID     *uuid.UUID
	FolderID    **uuid.UUID
	Position    *int
	Archived    *bool
}

// DefaultSpaceID returns the space a new project should land in when the
// caller did not name one: the first space they can see. Keeps the hierarchy
// populated without forcing every client to know about spaces yet.
func (r *Projects) DefaultSpaceID(ctx context.Context) (*uuid.UUID, error) {
	var id uuid.UUID
	err := r.store.Pool.QueryRow(ctx,
		`SELECT id FROM spaces WHERE NOT archived ORDER BY position, created_at LIMIT 1`).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &id, nil
}

// BySpace lists the projects (lists) inside a space, optionally within one
// folder.
func (r *Projects) BySpace(ctx context.Context, spaceID uuid.UUID, folderID *uuid.UUID) ([]models.Project, error) {
	rows, err := r.store.Pool.Query(ctx, `
		SELECT `+projectColumns+`
		  FROM projects p
		 WHERE p.space_id = $1
		   AND ($2::uuid IS NULL OR p.folder_id = $2)
		 ORDER BY p.position, p.created_at`, spaceID, folderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.Project{}
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

func (r *Projects) Update(ctx context.Context, id uuid.UUID, p ProjectPatch) error {
	return r.store.WithTx(ctx, func(tx pgx.Tx) error {
		sets := []string{"updated_at = now()"}
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
		if p.Color != nil {
			add("color", *p.Color)
		}
		if p.Status != nil {
			add("status", *p.Status)
		}
		if p.TeamID != nil {
			add("team_id", *p.TeamID)
		}
		if p.StartDate != nil {
			add("start_date", *p.StartDate)
		}
		if p.EndDate != nil {
			add("end_date", *p.EndDate)
		}
		if p.SpaceID != nil {
			add("space_id", *p.SpaceID)
		}
		if p.FolderID != nil {
			add("folder_id", *p.FolderID)
		}
		if p.Position != nil {
			add("position", *p.Position)
		}
		if p.Archived != nil {
			add("archived", *p.Archived)
		}

		tag, err := tx.Exec(ctx, `UPDATE projects SET `+strings.Join(sets, ", ")+` WHERE id = $1`, args...)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		if p.Members != nil {
			if err := replaceMembers(ctx, tx, "project_members", "project_id", id, *p.Members); err != nil {
				return err
			}
		}
		if p.Statuses != nil {
			return replaceStatuses(ctx, tx, id, *p.Statuses)
		}
		return nil
	})
}

// Delete removes the project. Tasks, statuses, members and the project's chat
// channel go with it through ON DELETE CASCADE - one statement instead of the
// three unordered deletes the document version issued.
func (r *Projects) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.store.Pool.Exec(ctx, `DELETE FROM projects WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func replaceStatuses(ctx context.Context, tx pgx.Tx, projectID uuid.UUID, statuses []models.ProjectStatus) error {
	if _, err := tx.Exec(ctx, `DELETE FROM project_statuses WHERE project_id = $1`, projectID); err != nil {
		return err
	}
	for i, s := range statuses {
		order := s.Order
		if order == 0 {
			order = i
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO project_statuses (project_id, key, label, position, color)
			VALUES ($1,$2,$3,$4,$5)
			ON CONFLICT (project_id, key) DO UPDATE
			   SET label = EXCLUDED.label, position = EXCLUDED.position, color = EXCLUDED.color`,
			projectID, s.Key, s.Label, order, s.Color); err != nil {
			return err
		}
	}
	return nil
}
