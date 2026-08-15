package repo

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"projectview/internal/config"
	"projectview/internal/db"
	"projectview/internal/logger"
)

// Settings stores the overrides an administrator has saved for the managed
// configuration keys.
type Settings struct {
	store *db.Store
	// key encrypts the secret values. Derived from the signing secret rather
	// than configured separately: one secret to protect is one secret an
	// operator can actually keep safe.
	key []byte
}

func NewSettings(store *db.Store, signingSecret string) *Settings {
	sum := sha256.Sum256([]byte("projectview.settings.v1:" + signingSecret))
	return &Settings{store: store, key: sum[:]}
}

// StoredSetting is one saved override.
type StoredSetting struct {
	Key       string     `json:"key"`
	Value     string     `json:"value"`
	Secret    bool       `json:"secret"`
	UpdatedBy *uuid.UUID `json:"updatedBy,omitempty"`
	UpdatedAt time.Time  `json:"updatedAt"`
}

const secretPrefix = "enc:v1:"

// encrypt returns an AES-GCM sealed value. The nonce is random per write and
// carried with the ciphertext, so writing the same password twice does not
// produce the same row - which would otherwise tell a reader of the database
// that two accounts share a password.
func (r *Settings) encrypt(plaintext string) (string, error) {
	block, err := aes.NewCipher(r.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return secretPrefix + base64.StdEncoding.EncodeToString(sealed), nil
}

// ErrUndecryptable marks a stored secret this process cannot read, which
// happens when JWT_SECRET has changed since it was written.
var ErrUndecryptable = errors.New("stored secret cannot be decrypted with the current signing secret")

func (r *Settings) decrypt(stored string) (string, error) {
	if len(stored) < len(secretPrefix) || stored[:len(secretPrefix)] != secretPrefix {
		// Written before encryption existed, or by hand. Returned as-is rather
		// than refused: refusing would lock an operator out of their own
		// configuration.
		return stored, nil
	}
	raw, err := base64.StdEncoding.DecodeString(stored[len(secretPrefix):])
	if err != nil {
		return "", ErrUndecryptable
	}
	block, err := aes.NewCipher(r.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", ErrUndecryptable
	}
	plaintext, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
	if err != nil {
		return "", ErrUndecryptable
	}
	return string(plaintext), nil
}

// Overrides returns every saved value, decrypted, ready to hand to
// config.Apply.
//
// A secret this process cannot decrypt is dropped with a warning rather than
// returned as gibberish: a garbled bind password would fail every login with
// an error nobody could trace back to a rotated signing secret.
func (r *Settings) Overrides(ctx context.Context) (map[string]string, error) {
	rows, err := r.store.Pool.Query(ctx, `SELECT key, value, is_secret FROM app_settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]string{}
	for rows.Next() {
		var key, value string
		var secret bool
		if err := rows.Scan(&key, &value, &secret); err != nil {
			return nil, err
		}
		// A key removed from the allow-list in a later release must stop being
		// applied, not linger because a row survived.
		if !config.IsManaged(key) {
			continue
		}
		if secret {
			plaintext, err := r.decrypt(value)
			if err != nil {
				logger.Warn("settings: ignoring %s, it cannot be decrypted with the current JWT_SECRET", key)
				continue
			}
			value = plaintext
		}
		out[key] = value
	}
	return out, rows.Err()
}

// SavedKeys reports which keys have an override, and when, without exposing
// any value. This is what the settings screen reads.
func (r *Settings) SavedKeys(ctx context.Context) (map[string]StoredSetting, error) {
	rows, err := r.store.Pool.Query(ctx,
		`SELECT key, is_secret, updated_by, updated_at FROM app_settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]StoredSetting{}
	for rows.Next() {
		var s StoredSetting
		if err := rows.Scan(&s.Key, &s.Secret, &s.UpdatedBy, &s.UpdatedAt); err != nil {
			return nil, err
		}
		out[s.Key] = s
	}
	return out, rows.Err()
}

// Save writes the given overrides, encrypting the secret ones, and deletes the
// keys listed in clear so they revert to the environment baseline.
//
// One transaction: a partially applied settings change - the new host saved
// but not the new password - is a configuration nobody asked for.
func (r *Settings) Save(ctx context.Context, values map[string]string, clear []string, actor uuid.UUID) error {
	return r.store.WithTx(ctx, func(tx pgx.Tx) error {
		for _, key := range clear {
			if !config.IsManaged(key) {
				continue
			}
			if _, err := tx.Exec(ctx, `DELETE FROM app_settings WHERE key = $1`, key); err != nil {
				return err
			}
		}

		for key, value := range values {
			// Belt and braces: the handler validates too, but this is the last
			// place before the row exists, and an unmanaged key here would be
			// applied to the process on the next boot.
			if !config.IsManaged(key) {
				return errors.New("refusing to store unmanaged setting: " + key)
			}
			secret := config.IsSecret(key)
			stored := value
			if secret {
				encrypted, err := r.encrypt(value)
				if err != nil {
					return err
				}
				stored = encrypted
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO app_settings (key, value, is_secret, updated_by, updated_at)
				VALUES ($1,$2,$3,$4, now())
				ON CONFLICT (key) DO UPDATE
				   SET value = EXCLUDED.value,
				       is_secret = EXCLUDED.is_secret,
				       updated_by = EXCLUDED.updated_by,
				       updated_at = now()`,
				key, stored, secret, actor); err != nil {
				return err
			}
		}
		return nil
	})
}
