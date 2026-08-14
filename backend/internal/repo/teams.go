package repo

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"projectview/internal/db"
	"projectview/internal/models"
)

type Teams struct{ store *db.Store }

func NewTeams(store *db.Store) *Teams { return &Teams{store: store} }

const teamColumns = `t.id, t.name, t.description, t.color, t.lead_id, t.created_by, t.created_at, t.updated_at`

func scanTeam(row pgx.Row) (*models.Team, error) {
	var t models.Team
	err := row.Scan(&t.ID, &t.Name, &t.Description, &t.Color, &t.LeadID,
		&t.CreatedBy, &t.CreatedAt, &t.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	t.Members = []uuid.UUID{}
	return &t, nil
}

// List returns every team with its members already attached, using one query
// for the teams and one for all memberships - not one per team.
func (r *Teams) List(ctx context.Context) ([]models.Team, error) {
	rows, err := r.store.Pool.Query(ctx, `SELECT `+teamColumns+` FROM teams t ORDER BY t.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	teams := []models.Team{}
	index := map[uuid.UUID]int{}
	for rows.Next() {
		t, err := scanTeam(rows)
		if err != nil {
			return nil, err
		}
		index[t.ID] = len(teams)
		teams = append(teams, *t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	memberRows, err := r.store.Pool.Query(ctx, `SELECT team_id, user_id FROM team_members`)
	if err != nil {
		return nil, err
	}
	defer memberRows.Close()
	for memberRows.Next() {
		var teamID, userID uuid.UUID
		if err := memberRows.Scan(&teamID, &userID); err != nil {
			return nil, err
		}
		if i, ok := index[teamID]; ok {
			teams[i].Members = append(teams[i].Members, userID)
		}
	}
	return teams, memberRows.Err()
}

func (r *Teams) ByID(ctx context.Context, id uuid.UUID) (*models.Team, error) {
	t, err := scanTeam(r.store.Pool.QueryRow(ctx, `SELECT `+teamColumns+` FROM teams t WHERE t.id = $1`, id))
	if err != nil {
		return nil, err
	}
	members, err := r.memberIDs(ctx, id)
	if err != nil {
		return nil, err
	}
	t.Members = members
	return t, nil
}

func (r *Teams) memberIDs(ctx context.Context, teamID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := r.store.Pool.Query(ctx, `SELECT user_id FROM team_members WHERE team_id = $1`, teamID)
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

// Create inserts the team and its membership atomically.
func (r *Teams) Create(ctx context.Context, t *models.Team) error {
	if t.ID == uuid.Nil {
		t.ID = uuid.New()
	}
	return r.store.WithTx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO teams (id, name, description, color, lead_id, created_by)
			VALUES ($1,$2,$3,$4,$5,$6)`,
			t.ID, t.Name, t.Description, t.Color, t.LeadID, t.CreatedBy)
		if err != nil {
			return err
		}
		return replaceMembers(ctx, tx, "team_members", "team_id", t.ID, t.Members)
	})
}

// TeamPatch is the allow-list of updatable fields.
type TeamPatch struct {
	Name        *string
	Description *string
	Color       *string
	LeadID      **uuid.UUID // double pointer: outer nil = leave, inner nil = clear
	Members     *[]uuid.UUID
}

func (r *Teams) Update(ctx context.Context, id uuid.UUID, p TeamPatch) error {
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
		if p.LeadID != nil {
			add("lead_id", *p.LeadID)
		}

		tag, err := tx.Exec(ctx, `UPDATE teams SET `+strings.Join(sets, ", ")+` WHERE id = $1`, args...)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		if p.Members != nil {
			return replaceMembers(ctx, tx, "team_members", "team_id", id, *p.Members)
		}
		return nil
	})
}

func (r *Teams) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.store.Pool.Exec(ctx, `DELETE FROM teams WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Teams) AddMember(ctx context.Context, teamID, userID uuid.UUID) error {
	_, err := r.store.Pool.Exec(ctx, `
		INSERT INTO team_members (team_id, user_id) VALUES ($1,$2)
		ON CONFLICT DO NOTHING`, teamID, userID)
	return err
}

func (r *Teams) RemoveMember(ctx context.Context, teamID, userID uuid.UUID) error {
	_, err := r.store.Pool.Exec(ctx,
		`DELETE FROM team_members WHERE team_id = $1 AND user_id = $2`, teamID, userID)
	return err
}

// replaceMembers rewrites a join table's rows for one parent id. Shared by
// teams, projects and chat channels, which all model membership the same way.
func replaceMembers(ctx context.Context, tx pgx.Tx, table, parentCol string, parentID uuid.UUID, members []uuid.UUID) error {
	if _, err := tx.Exec(ctx,
		fmt.Sprintf(`DELETE FROM %s WHERE %s = $1`, table, parentCol), parentID); err != nil {
		return err
	}
	for _, m := range members {
		if m == uuid.Nil {
			continue
		}
		if _, err := tx.Exec(ctx,
			fmt.Sprintf(`INSERT INTO %s (%s, user_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, table, parentCol),
			parentID, m); err != nil {
			return err
		}
	}
	return nil
}
