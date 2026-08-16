package repo

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"projectview/internal/db"
)

// Attachments stores the metadata for files kept in the object store. The
// bytes themselves never pass through here; see internal/storage.
type Attachments struct{ store *db.Store }

func NewAttachments(store *db.Store) *Attachments { return &Attachments{store: store} }

// Scan states. "skipped" is not "clean": it records that nothing examined the
// file, which is a different thing to tell somebody than that it was checked
// and found safe.
const (
	ScanPending  = "pending"
	ScanClean    = "clean"
	ScanInfected = "infected"
	ScanSkipped  = "skipped"
)

type Attachment struct {
	ID          uuid.UUID  `json:"id"`
	TaskID      uuid.UUID  `json:"taskId"`
	CommentID   *uuid.UUID `json:"commentId,omitempty"`
	Filename    string     `json:"filename"`
	ContentType string     `json:"contentType"`
	SizeBytes   int64      `json:"sizeBytes"`
	// StorageKey is never sent to a client: it is the address of the object,
	// and the only legitimate way to reach one is a signed URL this server
	// issues after checking who is asking.
	StorageKey string     `json:"-"`
	Checksum   string     `json:"checksum"`
	ScanStatus string     `json:"scanStatus"`
	ScannedAt  *time.Time `json:"scannedAt,omitempty"`
	UploadedBy *uuid.UUID `json:"-"`
	CreatedAt  time.Time  `json:"createdAt"`
}

const attachmentColumns = `
	id, task_id, comment_id, filename, content_type, size_bytes,
	storage_key, checksum, scan_status, scanned_at, uploaded_by, created_at`

func scanAttachment(row pgx.Row) (*Attachment, error) {
	var a Attachment
	err := row.Scan(&a.ID, &a.TaskID, &a.CommentID, &a.Filename, &a.ContentType,
		&a.SizeBytes, &a.StorageKey, &a.Checksum, &a.ScanStatus, &a.ScannedAt,
		&a.UploadedBy, &a.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// Create records an upload. Called only after the object is in the bucket, so
// a row never promises a file that is not there. The reverse - an object with
// no row - is the recoverable direction: it costs storage, and the operator can
// find it, whereas a row pointing at nothing is a broken download for a user.
func (r *Attachments) Create(ctx context.Context, a *Attachment) error {
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	if a.ScanStatus == "" {
		a.ScanStatus = ScanPending
	}
	return r.store.Pool.QueryRow(ctx, `
		INSERT INTO attachments
		    (id, task_id, comment_id, filename, content_type, size_bytes,
		     storage_key, checksum, scan_status, scanned_at, uploaded_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		RETURNING created_at`,
		a.ID, a.TaskID, a.CommentID, a.Filename, a.ContentType, a.SizeBytes,
		a.StorageKey, a.Checksum, a.ScanStatus, a.ScannedAt, a.UploadedBy).
		Scan(&a.CreatedAt)
}

func (r *Attachments) ByID(ctx context.Context, id uuid.UUID) (*Attachment, error) {
	return scanAttachment(r.store.Pool.QueryRow(ctx,
		`SELECT `+attachmentColumns+` FROM attachments WHERE id = $1`, id))
}

// ForTask lists everything attached to a task, its comments included, oldest
// first - the order they were added is the order they make sense in.
func (r *Attachments) ForTask(ctx context.Context, taskID uuid.UUID) ([]Attachment, error) {
	rows, err := r.store.Pool.Query(ctx,
		`SELECT `+attachmentColumns+` FROM attachments WHERE task_id = $1 ORDER BY created_at`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Attachment{}
	for rows.Next() {
		a, err := scanAttachment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

// Delete removes the row. The object is not deleted here: the trigger on the
// table queues its key, and the sweeper drains that queue. Doing it in this
// function instead would leave every cascade - deleting a task, a project, a
// space - silently orphaning objects.
func (r *Attachments) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.store.Pool.Exec(ctx, `DELETE FROM attachments WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// UsageBytes totals what one task already holds, so a per-task ceiling can be
// enforced before the next upload rather than discovered afterwards.
func (r *Attachments) UsageBytes(ctx context.Context, taskID uuid.UUID) (int64, error) {
	var total *int64
	err := r.store.Pool.QueryRow(ctx,
		`SELECT sum(size_bytes) FROM attachments WHERE task_id = $1`, taskID).Scan(&total)
	if err != nil {
		return 0, err
	}
	if total == nil {
		return 0, nil
	}
	return *total, nil
}

// ---------------------------------------------------------------------------
// The deferred-delete queue
// ---------------------------------------------------------------------------

// PendingDeletions returns storage keys whose objects still have to be removed,
// least-attempted first so a key the store keeps refusing cannot starve the
// rest of the queue.
func (r *Attachments) PendingDeletions(ctx context.Context, limit int) ([]string, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.store.Pool.Query(ctx, `
		SELECT storage_key FROM attachment_deletions
		 ORDER BY attempts, queued_at
		 LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []string{}
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		out = append(out, key)
	}
	return out, rows.Err()
}

// DeletionDone drops a key from the queue once its object is gone.
func (r *Attachments) DeletionDone(ctx context.Context, key string) error {
	_, err := r.store.Pool.Exec(ctx, `DELETE FROM attachment_deletions WHERE storage_key = $1`, key)
	return err
}

// DeletionFailed keeps the key queued and records why, so a bucket that has
// been unreachable for a day says so instead of presenting an empty queue and
// an equally empty explanation.
func (r *Attachments) DeletionFailed(ctx context.Context, key, reason string) error {
	if len(reason) > 500 {
		reason = reason[:500]
	}
	_, err := r.store.Pool.Exec(ctx, `
		UPDATE attachment_deletions
		   SET attempts = attempts + 1, last_error = $2
		 WHERE storage_key = $1`, key, reason)
	return err
}
