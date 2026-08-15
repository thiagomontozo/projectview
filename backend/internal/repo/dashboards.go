package repo

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"projectview/internal/db"
)

type Dashboards struct{ store *db.Store }

func NewDashboards(store *db.Store) *Dashboards { return &Dashboards{store: store} }

// Widget is one card on a dashboard. Size is a column span the frontend
// interprets; the server stores the layout without opinions about pixels.
type Widget struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Size int    `json:"size,omitempty"`
	// Hidden keeps a removed card's position, so putting it back does not mean
	// rebuilding the layout around it.
	Hidden bool `json:"hidden,omitempty"`
}

type SavedDashboard struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Layout    []Widget  `json:"layout"`
	IsDefault bool      `json:"isDefault"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// DefaultFor returns the caller's dashboard, or ErrNotFound when they have
// never saved one. The caller decides what an absent layout means - the server
// does not invent one, because the set of available cards is a frontend
// concern that would go stale here.
func (r *Dashboards) DefaultFor(ctx context.Context, userID uuid.UUID) (*SavedDashboard, error) {
	var d SavedDashboard
	var layout []byte
	err := r.store.Pool.QueryRow(ctx, `
		SELECT id, name, layout, is_default, updated_at
		  FROM dashboards
		 WHERE user_id = $1 AND is_default
		 LIMIT 1`, userID).
		Scan(&d.ID, &d.Name, &layout, &d.IsDefault, &d.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(layout, &d.Layout); err != nil {
		return nil, err
	}
	return &d, nil
}

// Save writes the caller's default layout, creating it on first use.
func (r *Dashboards) Save(ctx context.Context, userID uuid.UUID, name string, layout []Widget) (*SavedDashboard, error) {
	if name == "" {
		name = "My dashboard"
	}
	encoded, err := json.Marshal(layout)
	if err != nil {
		return nil, err
	}

	var d SavedDashboard
	err = r.store.Pool.QueryRow(ctx, `
		INSERT INTO dashboards (id, user_id, name, layout, is_default)
		VALUES ($1,$2,$3,$4,true)
		ON CONFLICT (user_id) WHERE is_default
		DO UPDATE SET name = EXCLUDED.name, layout = EXCLUDED.layout, updated_at = now()
		RETURNING id, name, is_default, updated_at`,
		uuid.New(), userID, name, encoded).
		Scan(&d.ID, &d.Name, &d.IsDefault, &d.UpdatedAt)
	if err != nil {
		return nil, err
	}
	d.Layout = layout
	return &d, nil
}

func (r *Dashboards) Delete(ctx context.Context, userID uuid.UUID) error {
	_, err := r.store.Pool.Exec(ctx, `DELETE FROM dashboards WHERE user_id = $1`, userID)
	return err
}
