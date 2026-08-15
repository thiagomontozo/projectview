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
)

// Spaces and Folders implement the work hierarchy:
//
//	Space ── Folder ── List(Project) ── Task
//	  └───────────────List(Project) ── Task
//
// A "project" is a List that also carries scheduling metadata; it keeps its
// name and its API. Folders never nest, matching the model people already know
// from comparable tools and keeping permission resolution to a fixed depth.
type Spaces struct{ store *db.Store }

func NewSpaces(store *db.Store) *Spaces { return &Spaces{store: store} }

// Space-level roles, strongest first. Effective access to anything inside a
// space is the strongest grant found walking up the tree.
const (
	SpaceRoleOwner  = "owner"
	SpaceRoleAdmin  = "admin"
	SpaceRoleMember = "member"
	SpaceRoleGuest  = "guest"
)

var spaceRoleRank = map[string]int{
	SpaceRoleGuest:  1,
	SpaceRoleMember: 2,
	SpaceRoleAdmin:  3,
	SpaceRoleOwner:  4,
}

// ValidSpaceRole mirrors the CHECK constraint on space_members.role.
func ValidSpaceRole(role string) bool {
	_, ok := spaceRoleRank[role]
	return ok
}

// SpaceRoleAtLeast reports whether have is at least as strong as want.
func SpaceRoleAtLeast(have, want string) bool {
	return spaceRoleRank[have] >= spaceRoleRank[want]
}

type Space struct {
	ID          uuid.UUID  `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description,omitempty"`
	Color       string     `json:"color"`
	TeamID      *uuid.UUID `json:"-"`
	IsPrivate   bool       `json:"isPrivate"`
	Position    int        `json:"position"`
	Archived    bool       `json:"archived"`
	CreatedBy   *uuid.UUID `json:"-"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

// SpaceMember is one grant on a space.
type SpaceMember struct {
	UserID uuid.UUID `json:"userId"`
	Role   string    `json:"role"`
}

const spaceColumns = `
	s.id, s.name, s.description, s.color, s.team_id, s.is_private, s.position,
	s.archived, s.created_by, s.created_at, s.updated_at`

func scanSpace(row pgx.Row) (*Space, error) {
	var s Space
	err := row.Scan(&s.ID, &s.Name, &s.Description, &s.Color, &s.TeamID,
		&s.IsPrivate, &s.Position, &s.Archived, &s.CreatedBy, &s.CreatedAt, &s.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// VisibleTo lists spaces the user may see: every public one, plus the private
// ones they hold a grant on. Admins see everything.
func (r *Spaces) VisibleTo(ctx context.Context, userID uuid.UUID, isAdmin, includeArchived bool) ([]Space, error) {
	rows, err := r.store.Pool.Query(ctx, `
		SELECT `+spaceColumns+`
		  FROM spaces s
		 WHERE ($3::boolean OR NOT s.archived)
		   AND ($2::boolean
		        OR NOT s.is_private
		        OR EXISTS (SELECT 1 FROM space_members m
		                    WHERE m.space_id = s.id AND m.user_id = $1))
		 ORDER BY s.position, s.name`, userID, isAdmin, includeArchived)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Space{}
	for rows.Next() {
		s, err := scanSpace(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}

func (r *Spaces) ByID(ctx context.Context, id uuid.UUID) (*Space, error) {
	return scanSpace(r.store.Pool.QueryRow(ctx, `SELECT `+spaceColumns+` FROM spaces s WHERE s.id = $1`, id))
}

// Members returns the grants held on a space.
func (r *Spaces) Members(ctx context.Context, spaceID uuid.UUID) ([]SpaceMember, error) {
	rows, err := r.store.Pool.Query(ctx,
		`SELECT user_id, role FROM space_members WHERE space_id = $1`, spaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []SpaceMember{}
	for rows.Next() {
		var m SpaceMember
		if err := rows.Scan(&m.UserID, &m.Role); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// RoleFor resolves the role a user holds on a space, or "" when they hold
// none. This is the leaf of permission resolution: everything under a space
// inherits from here.
func (r *Spaces) RoleFor(ctx context.Context, spaceID, userID uuid.UUID) (string, error) {
	var role string
	err := r.store.Pool.QueryRow(ctx,
		`SELECT role FROM space_members WHERE space_id = $1 AND user_id = $2`,
		spaceID, userID).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return role, err
}

// RoleForProject resolves the space role a user holds over a given project, by
// walking from the project up to its space in one query. This is the
// inheritance rule that hierarchical RBAC turns on.
func (r *Spaces) RoleForProject(ctx context.Context, projectID, userID uuid.UUID) (string, error) {
	var role *string
	err := r.store.Pool.QueryRow(ctx, `
		SELECT m.role
		  FROM projects p
		  JOIN space_members m ON m.space_id = p.space_id AND m.user_id = $2
		 WHERE p.id = $1`, projectID, userID).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if role == nil {
		return "", nil
	}
	return *role, nil
}

func (r *Spaces) Create(ctx context.Context, s *Space, members []SpaceMember) error {
	if s.ID == uuid.Nil {
		s.ID = uuid.New()
	}
	return r.store.WithTx(ctx, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			INSERT INTO spaces (id, name, description, color, team_id, is_private, position, created_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
			RETURNING created_at, updated_at`,
			s.ID, s.Name, s.Description, s.Color, s.TeamID, s.IsPrivate, s.Position, s.CreatedBy).
			Scan(&s.CreatedAt, &s.UpdatedAt)
		if err != nil {
			return err
		}
		return upsertSpaceMembers(ctx, tx, s.ID, members)
	})
}

// SpacePatch is the allow-list of updatable fields.
type SpacePatch struct {
	Name        *string
	Description *string
	Color       *string
	IsPrivate   *bool
	Position    *int
	Archived    *bool
	TeamID      **uuid.UUID
	Members     *[]SpaceMember
}

func (r *Spaces) Update(ctx context.Context, id uuid.UUID, p SpacePatch) error {
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
		if p.IsPrivate != nil {
			add("is_private", *p.IsPrivate)
		}
		if p.Position != nil {
			add("position", *p.Position)
		}
		if p.Archived != nil {
			add("archived", *p.Archived)
		}
		if p.TeamID != nil {
			add("team_id", *p.TeamID)
		}

		tag, err := tx.Exec(ctx, `UPDATE spaces SET `+strings.Join(sets, ", ")+` WHERE id = $1`, args...)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		if p.Members != nil {
			if _, err := tx.Exec(ctx, `DELETE FROM space_members WHERE space_id = $1`, id); err != nil {
				return err
			}
			return upsertSpaceMembers(ctx, tx, id, *p.Members)
		}
		return nil
	})
}

func (r *Spaces) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.store.Pool.Exec(ctx, `DELETE FROM spaces WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Spaces) SetMember(ctx context.Context, spaceID, userID uuid.UUID, role string) error {
	_, err := r.store.Pool.Exec(ctx, `
		INSERT INTO space_members (space_id, user_id, role) VALUES ($1,$2,$3)
		ON CONFLICT (space_id, user_id) DO UPDATE SET role = EXCLUDED.role`,
		spaceID, userID, role)
	return err
}

func (r *Spaces) RemoveMember(ctx context.Context, spaceID, userID uuid.UUID) error {
	_, err := r.store.Pool.Exec(ctx,
		`DELETE FROM space_members WHERE space_id = $1 AND user_id = $2`, spaceID, userID)
	return err
}

func upsertSpaceMembers(ctx context.Context, tx pgx.Tx, spaceID uuid.UUID, members []SpaceMember) error {
	for _, m := range members {
		if m.UserID == uuid.Nil {
			continue
		}
		role := m.Role
		if !ValidSpaceRole(role) {
			role = SpaceRoleMember
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO space_members (space_id, user_id, role) VALUES ($1,$2,$3)
			ON CONFLICT (space_id, user_id) DO UPDATE SET role = EXCLUDED.role`,
			spaceID, m.UserID, role); err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Folders
// ---------------------------------------------------------------------------

type Folders struct{ store *db.Store }

func NewFolders(store *db.Store) *Folders { return &Folders{store: store} }

type Folder struct {
	ID        uuid.UUID  `json:"id"`
	SpaceID   uuid.UUID  `json:"spaceId"`
	Name      string     `json:"name"`
	Color     string     `json:"color"`
	Position  int        `json:"position"`
	Archived  bool       `json:"archived"`
	CreatedBy *uuid.UUID `json:"-"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
}

const folderColumns = `f.id, f.space_id, f.name, f.color, f.position, f.archived, f.created_by, f.created_at, f.updated_at`

func scanFolder(row pgx.Row) (*Folder, error) {
	var f Folder
	err := row.Scan(&f.ID, &f.SpaceID, &f.Name, &f.Color, &f.Position,
		&f.Archived, &f.CreatedBy, &f.CreatedAt, &f.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &f, nil
}

func (r *Folders) BySpace(ctx context.Context, spaceID uuid.UUID) ([]Folder, error) {
	rows, err := r.store.Pool.Query(ctx,
		`SELECT `+folderColumns+` FROM folders f WHERE f.space_id = $1 ORDER BY f.position, f.name`, spaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Folder{}
	for rows.Next() {
		f, err := scanFolder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *f)
	}
	return out, rows.Err()
}

func (r *Folders) ByID(ctx context.Context, id uuid.UUID) (*Folder, error) {
	return scanFolder(r.store.Pool.QueryRow(ctx, `SELECT `+folderColumns+` FROM folders f WHERE f.id = $1`, id))
}

func (r *Folders) Create(ctx context.Context, f *Folder) error {
	if f.ID == uuid.Nil {
		f.ID = uuid.New()
	}
	return r.store.Pool.QueryRow(ctx, `
		INSERT INTO folders (id, space_id, name, color, position, created_by)
		VALUES ($1,$2,$3,$4,$5,$6)
		RETURNING created_at, updated_at`,
		f.ID, f.SpaceID, f.Name, f.Color, f.Position, f.CreatedBy).
		Scan(&f.CreatedAt, &f.UpdatedAt)
}

type FolderPatch struct {
	Name     *string
	Color    *string
	Position *int
	Archived *bool
}

func (r *Folders) Update(ctx context.Context, id uuid.UUID, p FolderPatch) error {
	sets := []string{"updated_at = now()"}
	args := []any{id}
	add := func(col string, val any) {
		args = append(args, val)
		sets = append(sets, fmt.Sprintf("%s = $%d", col, len(args)))
	}
	if p.Name != nil {
		add("name", *p.Name)
	}
	if p.Color != nil {
		add("color", *p.Color)
	}
	if p.Position != nil {
		add("position", *p.Position)
	}
	if p.Archived != nil {
		add("archived", *p.Archived)
	}

	tag, err := r.store.Pool.Exec(ctx, `UPDATE folders SET `+strings.Join(sets, ", ")+` WHERE id = $1`, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete removes the folder. Lists inside it are not deleted - they fall back
// to living directly under the space (ON DELETE SET NULL), because losing a
// folder must never silently destroy the work it grouped.
func (r *Folders) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.store.Pool.Exec(ctx, `DELETE FROM folders WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
