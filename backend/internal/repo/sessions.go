package repo

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"projectview/internal/db"
)

// Sessions are server-side records backing every login.
//
// The JWT alone used to *be* the session: valid until it expired, with no way
// to cut it short. Deactivating an account left its live token working. Now a
// login creates a session row, the JWT is a short-lived access token carrying
// that session's id, and revoking the row ends access at the next refresh.
//
// Only a SHA-256 hash of the refresh token is stored, so a database leak
// yields nothing usable - the same reasoning that applies to passwords. The
// token is high-entropy random, so a fast hash is appropriate here; stretching
// protects low-entropy secrets, which this is not.
type Sessions struct{ store *db.Store }

func NewSessions(store *db.Store) *Sessions { return &Sessions{store: store} }

// Session is a persisted login.
type Session struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	ExpiresAt  time.Time
	RevokedAt  *time.Time
	UserAgent  string
	IP         string
	LastUsedAt *time.Time
	CreatedAt  time.Time
}

func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// NewToken returns a fresh 256-bit refresh token, URL-safe.
func NewToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// Create opens a session and returns it together with the plaintext refresh
// token, which is the only moment that value exists outside the client.
func (r *Sessions) Create(ctx context.Context, userID uuid.UUID, ttl time.Duration, userAgent, ip string) (*Session, string, error) {
	token, err := NewToken()
	if err != nil {
		return nil, "", err
	}

	s := &Session{
		ID:        uuid.New(),
		UserID:    userID,
		ExpiresAt: time.Now().Add(ttl),
		UserAgent: truncate(userAgent, 400),
		IP:        ip,
	}
	err = r.store.Pool.QueryRow(ctx, `
		INSERT INTO sessions (id, user_id, token_hash, user_agent, ip, expires_at)
		VALUES ($1,$2,$3,$4,$5,$6)
		RETURNING created_at`,
		s.ID, s.UserID, hashToken(token), s.UserAgent, s.IP, s.ExpiresAt).Scan(&s.CreatedAt)
	if err != nil {
		return nil, "", err
	}
	return s, token, nil
}

// ByToken resolves a live session from its refresh token. Expired or revoked
// sessions are treated as absent.
func (r *Sessions) ByToken(ctx context.Context, token string) (*Session, error) {
	var s Session
	err := r.store.Pool.QueryRow(ctx, `
		SELECT id, user_id, expires_at, revoked_at, user_agent, ip, last_used_at, created_at
		  FROM sessions
		 WHERE token_hash = $1 AND revoked_at IS NULL AND expires_at > now()`,
		hashToken(token)).Scan(&s.ID, &s.UserID, &s.ExpiresAt, &s.RevokedAt,
		&s.UserAgent, &s.IP, &s.LastUsedAt, &s.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// IsLive reports whether a session id still grants access. The auth middleware
// calls this on every request, which is what makes revocation immediate.
func (r *Sessions) IsLive(ctx context.Context, id uuid.UUID) (bool, error) {
	var live bool
	err := r.store.Pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM sessions
			 WHERE id = $1 AND revoked_at IS NULL AND expires_at > now()
		)`, id).Scan(&live)
	return live, err
}

func (r *Sessions) Touch(ctx context.Context, id uuid.UUID) error {
	_, err := r.store.Pool.Exec(ctx, `UPDATE sessions SET last_used_at = now() WHERE id = $1`, id)
	return err
}

// Rotate revokes the current session and opens a replacement in one
// transaction. Rotating the refresh token on every use means a stolen token
// is usable at most once before the legitimate client invalidates it.
func (r *Sessions) Rotate(ctx context.Context, old *Session, ttl time.Duration, userAgent, ip string) (*Session, string, error) {
	token, err := NewToken()
	if err != nil {
		return nil, "", err
	}
	next := &Session{
		ID:        uuid.New(),
		UserID:    old.UserID,
		ExpiresAt: time.Now().Add(ttl),
		UserAgent: truncate(userAgent, 400),
		IP:        ip,
	}

	err = r.store.WithTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`UPDATE sessions SET revoked_at = now() WHERE id = $1`, old.ID); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `
			INSERT INTO sessions (id, user_id, token_hash, user_agent, ip, expires_at)
			VALUES ($1,$2,$3,$4,$5,$6)
			RETURNING created_at`,
			next.ID, next.UserID, hashToken(token), next.UserAgent, next.IP, next.ExpiresAt).
			Scan(&next.CreatedAt)
	})
	if err != nil {
		return nil, "", err
	}
	return next, token, nil
}

func (r *Sessions) Revoke(ctx context.Context, id uuid.UUID) error {
	_, err := r.store.Pool.Exec(ctx,
		`UPDATE sessions SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL`, id)
	return err
}

// RevokeAllForUser ends every session an account holds. Called when an admin
// deactivates a user or changes someone's password, so access stops at once
// rather than whenever the token happened to expire.
func (r *Sessions) RevokeAllForUser(ctx context.Context, userID uuid.UUID) (int64, error) {
	tag, err := r.store.Pool.Exec(ctx,
		`UPDATE sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`, userID)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// RevokeAllForUserExcept ends every session but one. Used when someone changes
// their own password: every other device is signed out, while the browser that
// made the change stays where it is.
func (r *Sessions) RevokeAllForUserExcept(ctx context.Context, userID, keep uuid.UUID) (int64, error) {
	tag, err := r.store.Pool.Exec(ctx, `
		UPDATE sessions SET revoked_at = now()
		 WHERE user_id = $1 AND id <> $2 AND revoked_at IS NULL`, userID, keep)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// ListForUser returns the account's live sessions, newest first, so a user can
// see where they are signed in.
func (r *Sessions) ListForUser(ctx context.Context, userID uuid.UUID) ([]Session, error) {
	rows, err := r.store.Pool.Query(ctx, `
		SELECT id, user_id, expires_at, revoked_at, user_agent, ip, last_used_at, created_at
		  FROM sessions
		 WHERE user_id = $1 AND revoked_at IS NULL AND expires_at > now()
		 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Session{}
	for rows.Next() {
		var s Session
		if err := rows.Scan(&s.ID, &s.UserID, &s.ExpiresAt, &s.RevokedAt,
			&s.UserAgent, &s.IP, &s.LastUsedAt, &s.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// DeleteExpired prunes rows that can no longer grant access. Run periodically.
func (r *Sessions) DeleteExpired(ctx context.Context, olderThan time.Duration) (int64, error) {
	tag, err := r.store.Pool.Exec(ctx, `
		DELETE FROM sessions
		 WHERE expires_at < now() - $1::interval
		    OR (revoked_at IS NOT NULL AND revoked_at < now() - $1::interval)`,
		olderThan.String())
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func truncate(s string, max int) string {
	if len(s) > max {
		return s[:max]
	}
	return s
}
