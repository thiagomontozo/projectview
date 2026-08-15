package repo

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"projectview/internal/db"
)

// Privacy implements the two rights an internal tool has to be able to honour
// on request: give someone everything held about them, and stop holding it.
type Privacy struct{ store *db.Store }

func NewPrivacy(store *db.Store) *Privacy { return &Privacy{store: store} }

// PersonalExport is everything the system knows about one person, in the shape
// a human can actually read - not a database dump.
type PersonalExport struct {
	GeneratedAt time.Time        `json:"generatedAt"`
	Profile     map[string]any   `json:"profile"`
	Tasks       []map[string]any `json:"assignedTasks"`
	Comments    []map[string]any `json:"comments"`
	Messages    []map[string]any `json:"chatMessages"`
	TimeEntries []map[string]any `json:"timeEntries"`
	Sessions    []map[string]any `json:"sessions"`
	AuditTrail  []map[string]any `json:"auditTrail"`
}

// Export gathers a subject's data.
//
// Deliberately scoped to what is *about* the person rather than everything
// they can see: a project they belong to is the organisation's record, and
// exporting it under one member's name would hand over other people's data
// alongside their own.
func (r *Privacy) Export(ctx context.Context, userID uuid.UUID) (*PersonalExport, error) {
	out := &PersonalExport{GeneratedAt: time.Now().UTC()}

	profile, err := r.collect(ctx, `
		SELECT id, username, name, email, role, title, auth_source, active,
		       last_login_at, created_at, updated_at
		  FROM users WHERE id = $1`, userID)
	if err != nil {
		return nil, err
	}
	if len(profile) == 0 {
		return nil, ErrNotFound
	}
	out.Profile = profile[0]

	if out.Tasks, err = r.collect(ctx, `
		SELECT t.id, t.title, t.status, t.priority, t.start_date, t.due_date,
		       t.completed_at, p.name AS project
		  FROM task_assignees ta
		  JOIN tasks t ON t.id = ta.task_id
		  JOIN projects p ON p.id = t.project_id
		 WHERE ta.user_id = $1
		 ORDER BY t.created_at`, userID); err != nil {
		return nil, err
	}

	if out.Comments, err = r.collect(ctx, `
		SELECT c.id, c.body, c.created_at, t.title AS task
		  FROM task_comments c JOIN tasks t ON t.id = c.task_id
		 WHERE c.author_id = $1 ORDER BY c.created_at`, userID); err != nil {
		return nil, err
	}

	if out.Messages, err = r.collect(ctx, `
		SELECT m.id, m.body, m.created_at, ch.name AS channel
		  FROM chat_messages m JOIN chat_channels ch ON ch.id = m.channel_id
		 WHERE m.author_id = $1 ORDER BY m.created_at`, userID); err != nil {
		return nil, err
	}

	if out.TimeEntries, err = r.collect(ctx, `
		SELECT e.id, e.started_at, e.ended_at, e.note, t.title AS task
		  FROM time_entries e JOIN tasks t ON t.id = e.task_id
		 WHERE e.user_id = $1 ORDER BY e.started_at`, userID); err != nil {
		return nil, err
	}

	if out.Sessions, err = r.collect(ctx, `
		SELECT id, ip, user_agent, created_at, last_used_at, revoked_at
		  FROM sessions WHERE user_id = $1 ORDER BY created_at`, userID); err != nil {
		return nil, err
	}

	if out.AuditTrail, err = r.collect(ctx, `
		SELECT id, action, resource_type, resource_id, occurred_at, ip
		  FROM audit_log WHERE actor_id = $1 ORDER BY occurred_at`, userID); err != nil {
		return nil, err
	}

	return out, nil
}

// collect runs a query and returns rows as maps, using the column names the
// query itself declares.
func (r *Privacy) collect(ctx context.Context, query string, args ...any) ([]map[string]any, error) {
	rows, err := r.store.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []map[string]any{}
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return nil, err
		}
		record := map[string]any{}
		for i, field := range rows.FieldDescriptions() {
			record[field.Name] = values[i]
		}
		out = append(out, record)
	}
	return out, rows.Err()
}

// Anonymize erases a person without destroying the organisation's records.
//
// Deleting the row would cascade through their tasks, comments and time
// entries - work that belongs to the company, not to the individual - and
// would punch holes in the audit trail, which is the one table that has to
// stay intact to be worth keeping. So identifiers are replaced, credentials
// destroyed, sessions revoked, and the account is deactivated. What remains is
// a tombstone: enough for referential integrity, nothing that identifies
// anyone.
//
// Free-text they wrote is left alone. It can name other people, and rewriting
// history to remove one author's words would corrupt conversations that are
// not theirs alone. Anything genuinely sensitive in a comment is a deletion
// request against that comment, not against the account.
func (r *Privacy) Anonymize(ctx context.Context, userID uuid.UUID) error {
	return r.store.WithTx(ctx, func(tx pgx.Tx) error {
		var already *time.Time
		err := tx.QueryRow(ctx, `SELECT anonymized_at FROM users WHERE id = $1`, userID).Scan(&already)
		if err == pgx.ErrNoRows {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if already != nil {
			// Idempotent: a second request must not rename the tombstone again
			// and produce a different pseudonym for the same person.
			return nil
		}

		// The pseudonym keeps the id visible so an administrator can still
		// correlate an audit entry with a deletion request, without it naming
		// anybody.
		short := userID.String()[:8]
		pseudonym := fmt.Sprintf("deleted-user-%s", short)

		_, err = tx.Exec(ctx, `
			UPDATE users
			   SET username = $2,
			       name = 'Deleted user',
			       email = $3,
			       title = '',
			       password_hash = '',
			       external_id = NULL,
			       active = false,
			       notify_by_email = false,
			       anonymized_at = now(),
			       updated_at = now()
			 WHERE id = $1`,
			userID, pseudonym, pseudonym+"@deleted.invalid")
		if err != nil {
			return err
		}

		// Revoked rather than deleted: the sessions table is also a record of
		// where an account was used from, which an investigation may need.
		if _, err := tx.Exec(ctx,
			`UPDATE sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`, userID); err != nil {
			return err
		}

		// Notifications are addressed to the person, so they go with them.
		if _, err := tx.Exec(ctx, `DELETE FROM notifications WHERE user_id = $1`, userID); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `DELETE FROM notification_preferences WHERE user_id = $1`, userID)
		return err
	})
}

// Purge enforces retention: audit entries and read notifications older than
// the configured windows are removed. Returns how many rows each pass deleted,
// because "retention ran" is not the same statement as "retention did
// something".
func (r *Privacy) Purge(ctx context.Context, auditDays, notificationDays int) (audit int64, notifications int64, err error) {
	if auditDays > 0 {
		tag, err := r.store.Pool.Exec(ctx,
			`DELETE FROM audit_log WHERE occurred_at < now() - make_interval(days => $1)`, auditDays)
		if err != nil {
			return 0, 0, err
		}
		audit = tag.RowsAffected()
	}
	if notificationDays > 0 {
		// Unread notifications are spared: expiring something the person never
		// saw destroys the only copy of a message that was meant for them.
		tag, err := r.store.Pool.Exec(ctx,
			`DELETE FROM notifications WHERE read AND created_at < now() - make_interval(days => $1)`, notificationDays)
		if err != nil {
			return audit, 0, err
		}
		notifications = tag.RowsAffected()
	}
	return audit, notifications, nil
}
