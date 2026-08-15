package repo

import (
	"context"
	"time"

	"github.com/google/uuid"

	"projectview/internal/db"
)

// Audit writes the append-only trail of who changed what.
//
// The application only ever INSERTs here: there is no update or delete path in
// this file, by design. A trail that can be edited by the same code that
// writes it is not a trail.
type Audit struct{ store *db.Store }

func NewAudit(store *db.Store) *Audit { return &Audit{store: store} }

// Entry is one recorded action.
type Entry struct {
	ID           int64          `json:"id"`
	OccurredAt   time.Time      `json:"occurredAt"`
	ActorID      *uuid.UUID     `json:"actorId,omitempty"`
	ActorLabel   string         `json:"actor"`
	Action       string         `json:"action"`
	ResourceType string         `json:"resourceType"`
	ResourceID   string         `json:"resourceId,omitempty"`
	Changes      map[string]any `json:"changes,omitempty"`
	IP           string         `json:"ip,omitempty"`
	UserAgent    string         `json:"userAgent,omitempty"`
	RequestID    string         `json:"requestId,omitempty"`
	Status       int            `json:"status"`
}

func (r *Audit) Write(ctx context.Context, e Entry) error {
	_, err := r.store.Pool.Exec(ctx, `
		INSERT INTO audit_log (actor_id, actor_label, action, resource_type, resource_id,
		                       changes, ip, user_agent, request_id, status)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		e.ActorID, e.ActorLabel, e.Action, e.ResourceType, e.ResourceID,
		e.Changes, e.IP, e.UserAgent, e.RequestID, e.Status)
	return err
}

// AuditQuery narrows a search of the trail.
type AuditQuery struct {
	ActorID      *uuid.UUID
	ResourceType string
	ResourceID   string
	Action       string
	Since        *time.Time
	Limit        int
	// Cursor is the id of the last row of the previous page; results continue
	// strictly below it, matching the DESC ordering.
	Cursor *int64
}

// List returns a page of the trail, newest first.
func (r *Audit) List(ctx context.Context, q AuditQuery) ([]Entry, error) {
	limit := q.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	rows, err := r.store.Pool.Query(ctx, `
		SELECT id, occurred_at, actor_id, actor_label, action, resource_type,
		       resource_id, changes, ip, user_agent, request_id, status
		  FROM audit_log
		 WHERE ($1::uuid   IS NULL OR actor_id = $1)
		   AND ($2::text   = ''    OR resource_type = $2)
		   AND ($3::text   = ''    OR resource_id = $3)
		   AND ($4::text   = ''    OR action = $4)
		   AND ($5::timestamptz IS NULL OR occurred_at >= $5)
		   AND ($6::bigint IS NULL OR id < $6)
		 ORDER BY id DESC
		 LIMIT $7`,
		q.ActorID, q.ResourceType, q.ResourceID, q.Action, q.Since, q.Cursor, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Entry{}
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.ID, &e.OccurredAt, &e.ActorID, &e.ActorLabel, &e.Action,
			&e.ResourceType, &e.ResourceID, &e.Changes, &e.IP, &e.UserAgent,
			&e.RequestID, &e.Status); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Count reports how many entries match, for the "total" on an audit screen.
func (r *Audit) Count(ctx context.Context, q AuditQuery) (int64, error) {
	var n int64
	err := r.store.Pool.QueryRow(ctx, `
		SELECT count(*) FROM audit_log
		 WHERE ($1::uuid IS NULL OR actor_id = $1)
		   AND ($2::text = ''    OR resource_type = $2)
		   AND ($3::text = ''    OR resource_id = $3)`,
		q.ActorID, q.ResourceType, q.ResourceID).Scan(&n)
	return n, err
}
