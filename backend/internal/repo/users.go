// Package repo holds the data-access layer: every SQL statement in the
// application lives here, and handlers deal only in domain types.
//
// The queries are written by hand rather than generated. They are few enough
// to read end to end, and keeping them explicit makes the N+1 patterns the
// document-store version suffered from obvious at the point of use - most
// listings below resolve their related rows in a single round trip.
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

// ErrNotFound is returned when a lookup matches no row. Handlers translate it
// into a 404.
var ErrNotFound = errors.New("not found")

// IsUniqueViolation reports whether err is a Postgres unique-constraint
// failure, which handlers surface as 409 rather than 500.
func IsUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "SQLSTATE 23505")
}

type Users struct{ store *db.Store }

func NewUsers(store *db.Store) *Users { return &Users{store: store} }

const userColumns = `
	u.id, u.username, u.name, u.email, u.password_hash, u.auth_source, u.role,
	u.title, u.avatar_color, u.active, u.notify_by_email, u.last_login_at,
	u.created_at, u.updated_at, u.weekly_capacity_hours, u.external_id`

func scanUser(row pgx.Row) (*models.User, error) {
	var u models.User
	err := row.Scan(&u.ID, &u.Username, &u.Name, &u.Email, &u.PasswordHash,
		&u.AuthSource, &u.Role, &u.Title, &u.AvatarColor, &u.Active,
		&u.NotifyByEmail, &u.LastLoginAt, &u.CreatedAt, &u.UpdatedAt,
		&u.WeeklyCapacity, &u.ExternalID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	u.Teams = []uuid.UUID{}
	return &u, nil
}

func (r *Users) Count(ctx context.Context) (int64, error) {
	var n int64
	err := r.store.Pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&n)
	return n, err
}

func (r *Users) ByID(ctx context.Context, id uuid.UUID) (*models.User, error) {
	u, err := scanUser(r.store.Pool.QueryRow(ctx,
		`SELECT `+userColumns+` FROM users u WHERE u.id = $1`, id))
	if err != nil {
		return nil, err
	}
	return r.withTeams(ctx, u)
}

// ByLogin resolves an account from either its username or its e-mail, which is
// what the login form allows.
func (r *Users) ByLogin(ctx context.Context, login string) (*models.User, error) {
	u, err := scanUser(r.store.Pool.QueryRow(ctx,
		`SELECT `+userColumns+` FROM users u WHERE u.username = $1 OR u.email = $1`, login))
	if err != nil {
		return nil, err
	}
	return r.withTeams(ctx, u)
}

// ByExternalID resolves an account by the identity provider's subject. This,
// not the username, is what an account is linked by: a provider may rename a
// person, and the subject is the only thing that survives it.
func (r *Users) ByExternalID(ctx context.Context, externalID string) (*models.User, error) {
	return scanUser(r.store.Pool.QueryRow(ctx,
		`SELECT `+userColumns+` FROM users u WHERE u.external_id = $1`, externalID))
}

// ListAll includes deactivated accounts, which provisioning needs to see:
// a directory that cannot see a disabled user will try to create them again.
func (r *Users) ListAll(ctx context.Context, limit, offset int) ([]models.User, int, error) {
	var total int
	if err := r.store.Pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.store.Pool.Query(ctx,
		`SELECT `+userColumns+` FROM users u ORDER BY u.created_at LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := []models.User{}
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *u)
	}
	return out, total, rows.Err()
}

func (r *Users) ByEmail(ctx context.Context, email string) (*models.User, error) {
	u, err := scanUser(r.store.Pool.QueryRow(ctx,
		`SELECT `+userColumns+` FROM users u WHERE u.email = $1`, email))
	if err != nil {
		return nil, err
	}
	return r.withTeams(ctx, u)
}

func (r *Users) withTeams(ctx context.Context, u *models.User) (*models.User, error) {
	rows, err := r.store.Pool.Query(ctx,
		`SELECT team_id FROM team_members WHERE user_id = $1`, u.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		u.Teams = append(u.Teams, id)
	}
	return u, rows.Err()
}

// ListActive returns every enabled account, ordered by name.
func (r *Users) ListActive(ctx context.Context) ([]models.User, error) {
	rows, err := r.store.Pool.Query(ctx,
		`SELECT `+userColumns+` FROM users u WHERE u.active ORDER BY u.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	users := []models.User{}
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, *u)
	}
	return users, rows.Err()
}

// PublicByIDs resolves a batch of users in one query. This is what removes the
// per-row lookups the previous implementation issued while populating tasks.
func (r *Users) PublicByIDs(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]models.User, error) {
	out := map[uuid.UUID]models.User{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := r.store.Pool.Query(ctx,
		`SELECT `+userColumns+` FROM users u WHERE u.id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out[u.ID] = *u
	}
	return out, rows.Err()
}

func (r *Users) Create(ctx context.Context, u *models.User) error {
	if u.ID == uuid.Nil {
		u.ID = uuid.New()
	}
	now := time.Now()
	u.CreatedAt, u.UpdatedAt = now, now

	_, err := r.store.Pool.Exec(ctx, `
		INSERT INTO users (id, username, name, email, password_hash, auth_source,
		                   role, title, avatar_color, active, notify_by_email,
		                   last_login_at, created_at, updated_at, external_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
		u.ID, u.Username, u.Name, u.Email, u.PasswordHash, u.AuthSource, u.Role,
		u.Title, u.AvatarColor, u.Active, u.NotifyByEmail, u.LastLoginAt,
		u.CreatedAt, u.UpdatedAt, u.ExternalID)
	return err
}

// UserPatch carries the subset of fields an update may touch. Nil means
// "leave alone", which keeps the allow-list explicit instead of trusting the
// request body's shape.
type UserPatch struct {
	Name          *string
	Email         *string
	Username      *string
	Title         *string
	AvatarColor   *string
	NotifyByEmail *bool
	Role          *string
	Active        *bool
	// WeeklyCapacity is what capacity planning compares allocation against.
	WeeklyCapacity *float64
	// ExternalID links the account to the identity provider's stable subject.
	ExternalID *string
}

func (r *Users) Update(ctx context.Context, id uuid.UUID, p UserPatch) error {
	sets := []string{"updated_at = now()"}
	args := []any{id}
	add := func(col string, val any) {
		args = append(args, val)
		sets = append(sets, fmt.Sprintf("%s = $%d", col, len(args)))
	}
	if p.Name != nil {
		add("name", *p.Name)
	}
	if p.Title != nil {
		add("title", *p.Title)
	}
	if p.AvatarColor != nil {
		add("avatar_color", *p.AvatarColor)
	}
	if p.NotifyByEmail != nil {
		add("notify_by_email", *p.NotifyByEmail)
	}
	if p.Role != nil {
		add("role", *p.Role)
	}
	if p.Active != nil {
		add("active", *p.Active)
	}
	if p.Email != nil {
		add("email", *p.Email)
	}
	if p.Username != nil {
		add("username", *p.Username)
	}
	if p.WeeklyCapacity != nil {
		add("weekly_capacity_hours", *p.WeeklyCapacity)
	}
	if p.ExternalID != nil {
		add("external_id", *p.ExternalID)
	}

	tag, err := r.store.Pool.Exec(ctx,
		`UPDATE users SET `+strings.Join(sets, ", ")+` WHERE id = $1`, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Users) SetPassword(ctx context.Context, id uuid.UUID, hash string) error {
	tag, err := r.store.Pool.Exec(ctx, `
		UPDATE users
		   SET password_hash = $2, auth_source = 'local', updated_at = now()
		 WHERE id = $1`, id, hash)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Users) TouchLogin(ctx context.Context, id uuid.UUID) error {
	_, err := r.store.Pool.Exec(ctx,
		`UPDATE users SET last_login_at = now(), updated_at = now() WHERE id = $1`, id)
	return err
}

// UpsertFromAD provisions or refreshes an account backed by Active Directory.
// The e-mail is the identity key, matching the previous behaviour.
func (r *Users) UpsertFromAD(ctx context.Context, username, name, email, avatarColor string) (*models.User, error) {
	existing, err := r.ByEmail(ctx, email)
	if err == nil {
		_, err = r.store.Pool.Exec(ctx, `
			UPDATE users
			   SET name = $2, auth_source = 'ad', last_login_at = now(), updated_at = now()
			 WHERE id = $1`, existing.ID, name)
		if err != nil {
			return nil, err
		}
		return r.ByID(ctx, existing.ID)
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, err
	}

	now := time.Now()
	u := &models.User{
		ID: uuid.New(), Username: username, Name: name, Email: email,
		AuthSource: models.AuthSourceAD, Role: models.RoleMember,
		AvatarColor: avatarColor, Active: true, NotifyByEmail: true,
		LastLoginAt: &now,
	}
	if err := r.Create(ctx, u); err != nil {
		return nil, err
	}
	return r.ByID(ctx, u.ID)
}

// WorkloadRow is one line of the resource-allocation report.
type WorkloadRow struct {
	User          models.User
	OpenTasks     int64
	EstimateHours float64
	Overdue       int64
	ProjectCount  int
}

// Workload computes every active user's load in a single aggregate query,
// replacing the previous fetch-all-then-merge-in-Go approach.
func (r *Users) Workload(ctx context.Context) ([]WorkloadRow, error) {
	rows, err := r.store.Pool.Query(ctx, `
		SELECT `+userColumns+`,
		       coalesce(w.open_tasks, 0),
		       coalesce(w.estimate_hours, 0),
		       coalesce(w.overdue, 0),
		       coalesce(w.project_count, 0)
		  FROM users u
		  LEFT JOIN (
		        SELECT ta.user_id,
		               count(*)                                        AS open_tasks,
		               sum(t.estimate_hours)                           AS estimate_hours,
		               count(*) FILTER (WHERE t.due_date < now())      AS overdue,
		               count(DISTINCT t.project_id)                    AS project_count
		          FROM task_assignees ta
		          JOIN tasks t ON t.id = ta.task_id
		         WHERE t.status <> 'done'
		         GROUP BY ta.user_id
		  ) w ON w.user_id = u.id
		 WHERE u.active
		 ORDER BY u.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []WorkloadRow{}
	for rows.Next() {
		var (
			u   models.User
			row WorkloadRow
		)
		if err := rows.Scan(&u.ID, &u.Username, &u.Name, &u.Email, &u.PasswordHash,
			&u.AuthSource, &u.Role, &u.Title, &u.AvatarColor, &u.Active,
			&u.NotifyByEmail, &u.LastLoginAt, &u.CreatedAt, &u.UpdatedAt,
			&u.WeeklyCapacity, &u.ExternalID,
			&row.OpenTasks, &row.EstimateHours, &row.Overdue, &row.ProjectCount); err != nil {
			return nil, err
		}
		u.PasswordHash = ""
		u.Teams = []uuid.UUID{}
		row.User = u
		out = append(out, row)
	}
	return out, rows.Err()
}
