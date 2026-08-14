// Package db owns the PostgreSQL connection pool and applies the embedded
// migrations, which is how the schema gets created on first run.
package db

import (
	"context"
	"embed"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"projectview/internal/config"
	"projectview/internal/logger"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Store is the handle every repository takes. It exposes the pool directly so
// repositories can choose between pool-level calls and an explicit
// transaction (see WithTx).
type Store struct {
	Pool *pgxpool.Pool
}

var maskCreds = regexp.MustCompile(`//([^:/@]+):([^@]+)@`)

func maskURL(dsn string) string {
	return maskCreds.ReplaceAllString(dsn, "//$1:****@")
}

// Connect dials PostgreSQL, waits for it to accept queries, and brings the
// schema up to date.
func Connect(cfg *config.Config) (*Store, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	poolCfg, err := pgxpool.ParseConfig(cfg.Database.URL)
	if err != nil {
		return nil, fmt.Errorf("invalid DATABASE_URL: %w", err)
	}
	poolCfg.MaxConns = int32(cfg.Database.MaxConns)
	poolCfg.MaxConnLifetime = time.Hour
	poolCfg.MaxConnIdleTime = 30 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, err
	}

	// The database container may still be starting up; retry rather than
	// crash-looping the whole service.
	if err := waitForDatabase(ctx, pool); err != nil {
		pool.Close()
		return nil, err
	}

	store := &Store{Pool: pool}
	logger.Info("PostgreSQL connected -> %s", maskURL(cfg.Database.URL))

	if err := store.Migrate(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("migrations failed: %w", err)
	}

	return store, nil
}

func waitForDatabase(ctx context.Context, pool *pgxpool.Pool) error {
	var lastErr error
	for attempt := 1; attempt <= 30; attempt++ {
		if err := pool.Ping(ctx); err == nil {
			return nil
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("database not reachable: %w", lastErr)
		case <-time.After(2 * time.Second):
		}
	}
	return fmt.Errorf("database not reachable after 30 attempts: %w", lastErr)
}

// Close releases the pool.
func (s *Store) Close() {
	if s.Pool != nil {
		s.Pool.Close()
	}
}

// ---------------------------------------------------------------------------
// Migrations
//
// Deliberately hand-rolled instead of pulling in a migration framework: the
// history here is linear and forward-only, and the alternatives drag in
// drivers for databases this project will never use (ClickHouse, SQL Server,
// SQLite) just to run a few CREATE TABLEs.
//
// Each file runs inside its own transaction together with the bookkeeping
// insert, so a failure leaves no half-applied version behind.
// ---------------------------------------------------------------------------

// Migrate applies every migration that has not run yet, in filename order.
func (s *Store) Migrate(ctx context.Context) error {
	_, err := s.Pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version     text PRIMARY KEY,
			applied_at  timestamptz NOT NULL DEFAULT now()
		)`)
	if err != nil {
		return fmt.Errorf("creating schema_migrations: %w", err)
	}

	applied, err := s.appliedVersions(ctx)
	if err != nil {
		return err
	}

	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	ran := 0
	for _, name := range names {
		version := strings.TrimSuffix(name, ".sql")
		if applied[version] {
			continue
		}

		sqlBytes, err := migrationFiles.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}

		if err := s.applyMigration(ctx, version, string(sqlBytes)); err != nil {
			return fmt.Errorf("migration %s: %w", version, err)
		}
		logger.Info("Applied migration %s", version)
		ran++
	}

	if ran == 0 {
		logger.Info("Schema up to date (%d migration(s) already applied)", len(applied))
	} else {
		logger.Info("Schema ready (%d migration(s) applied)", ran)
	}
	return nil
}

func (s *Store) appliedVersions(ctx context.Context) (map[string]bool, error) {
	rows, err := s.Pool.Query(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	applied := map[string]bool{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		applied[v] = true
	}
	return applied, rows.Err()
}

func (s *Store) applyMigration(ctx context.Context, version, script string) error {
	return s.WithTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, script); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, version)
		return err
	})
}

// WithTx runs fn inside a transaction, committing on success and rolling back
// on any error or panic. This is what the Mongo version could not offer:
// deleting a project and everything under it is now one atomic statement set.
func (s *Store) WithTx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		// Rollback after a successful commit is a no-op error we ignore.
		_ = tx.Rollback(ctx)
	}()

	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// DatabaseName reports the database the DSN points at, for log lines.
func DatabaseName(dsn string) string {
	u, err := url.Parse(dsn)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(u.Path, "/")
}
