package repo

import (
	"context"

	"github.com/google/uuid"

	"projectview/internal/db"
	"projectview/internal/models"
)

type Notifications struct{ store *db.Store }

func NewNotifications(store *db.Store) *Notifications { return &Notifications{store: store} }

func (r *Notifications) Create(ctx context.Context, n *models.Notification) error {
	if n.ID == uuid.Nil {
		n.ID = uuid.New()
	}
	return r.store.Pool.QueryRow(ctx, `
		INSERT INTO notifications (id, user_id, type, title, body, task_id, project_id, read)
		VALUES ($1,$2,$3,$4,$5,$6,$7,false)
		RETURNING created_at, updated_at`,
		n.ID, n.User, n.Type, n.Title, n.Body, n.Task, n.Project).
		Scan(&n.CreatedAt, &n.UpdatedAt)
}

func (r *Notifications) ForUser(ctx context.Context, userID uuid.UUID, limit int) ([]models.Notification, error) {
	rows, err := r.store.Pool.Query(ctx, `
		SELECT id, user_id, type, title, body, task_id, project_id, read, created_at, updated_at
		  FROM notifications
		 WHERE user_id = $1
		 ORDER BY created_at DESC
		 LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.Notification{}
	for rows.Next() {
		var n models.Notification
		if err := rows.Scan(&n.ID, &n.User, &n.Type, &n.Title, &n.Body, &n.Task,
			&n.Project, &n.Read, &n.CreatedAt, &n.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// MarkRead only touches rows owned by the caller, so a guessed id cannot flip
// somebody else's notification.
func (r *Notifications) MarkRead(ctx context.Context, id, userID uuid.UUID) error {
	tag, err := r.store.Pool.Exec(ctx, `
		UPDATE notifications SET read = true, updated_at = now()
		 WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Notifications) MarkAllRead(ctx context.Context, userID uuid.UUID) error {
	_, err := r.store.Pool.Exec(ctx, `
		UPDATE notifications SET read = true, updated_at = now()
		 WHERE user_id = $1 AND NOT read`, userID)
	return err
}
