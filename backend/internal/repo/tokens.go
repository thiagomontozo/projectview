package repo

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"projectview/internal/db"
)

type ServiceTokens struct{ store *db.Store }

func NewServiceTokens(store *db.Store) *ServiceTokens { return &ServiceTokens{store: store} }

// Scopes a machine credential can hold. Deliberately coarse: a permission
// system for robots that nobody can explain is one nobody configures correctly.
const (
	ScopeSCIM    = "scim"    // provision and deprovision users
	ScopeReports = "reports" // read-only reporting endpoints
)

func ValidScope(s string) bool { return s == ScopeSCIM || s == ScopeReports }

type ServiceToken struct {
	ID         uuid.UUID  `json:"id"`
	Name       string     `json:"name"`
	Scopes     []string   `json:"scopes"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
	RevokedAt  *time.Time `json:"revokedAt,omitempty"`
	// Secret is populated exactly once, by Create. It is never read back from
	// the database, because only its hash is stored.
	Secret string `json:"secret,omitempty"`
}

// tokenPrefix makes a leaked credential recognisable in a log or a paste, so
// it can be revoked without anyone having to work out what it is.
const tokenPrefix = "pvt_"

func hashServiceToken(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// Create mints a token and returns the only copy of the secret that will ever
// exist. Losing it means issuing a new one, which is the point.
func (r *ServiceTokens) Create(ctx context.Context, name string, scopes []string, createdBy uuid.UUID) (*ServiceToken, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	secret := tokenPrefix + base64.RawURLEncoding.EncodeToString(raw)

	token := &ServiceToken{ID: uuid.New(), Name: name, Scopes: scopes, Secret: secret}
	err := r.store.Pool.QueryRow(ctx, `
		INSERT INTO service_tokens (id, name, token_hash, scopes, created_by)
		VALUES ($1,$2,$3,$4,$5)
		RETURNING created_at`,
		token.ID, name, hashServiceToken(secret), scopes, createdBy).Scan(&token.CreatedAt)
	if err != nil {
		return nil, err
	}
	return token, nil
}

func (r *ServiceTokens) List(ctx context.Context) ([]ServiceToken, error) {
	rows, err := r.store.Pool.Query(ctx, `
		SELECT id, name, scopes, created_at, last_used_at, revoked_at
		  FROM service_tokens
		 ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []ServiceToken{}
	for rows.Next() {
		var t ServiceToken
		if err := rows.Scan(&t.ID, &t.Name, &t.Scopes, &t.CreatedAt, &t.LastUsedAt, &t.RevokedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// Authenticate resolves a presented secret, refusing revoked tokens. The
// lookup is by hash, so the plaintext never has to be compared against
// anything stored.
func (r *ServiceTokens) Authenticate(ctx context.Context, secret string) (*ServiceToken, error) {
	secret = strings.TrimSpace(secret)
	if !strings.HasPrefix(secret, tokenPrefix) {
		return nil, ErrNotFound
	}

	var t ServiceToken
	err := r.store.Pool.QueryRow(ctx, `
		UPDATE service_tokens SET last_used_at = now()
		 WHERE token_hash = $1 AND revoked_at IS NULL
		 RETURNING id, name, scopes, created_at, last_used_at, revoked_at`,
		hashServiceToken(secret)).
		Scan(&t.ID, &t.Name, &t.Scopes, &t.CreatedAt, &t.LastUsedAt, &t.RevokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (t *ServiceToken) HasScope(scope string) bool {
	for _, s := range t.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}

// Revoke keeps the row: the audit trail refers to it, and a deleted token is
// indistinguishable from one that never existed.
func (r *ServiceTokens) Revoke(ctx context.Context, id uuid.UUID) error {
	tag, err := r.store.Pool.Exec(ctx,
		`UPDATE service_tokens SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
