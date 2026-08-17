package db

import (
	"context"
	"hash/fnv"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Advisory locks, so a scheduled sweep runs once across the whole installation
// rather than once per process.
//
// Four sweeps need this: deadline alerts, digests, retention and the recurrence
// scheduler. Each runs on a timer inside every backend process, so a second
// replica means every alert sent twice, every digest sent twice, and every
// recurring task spawned twice. That was named in the runbook as the reason not
// to scale out; this is the fix.
//
// A PostgreSQL *session* advisory lock is the right shape here and a
// transaction-scoped one is not: the sweep is a sequence of queries with work
// between them, not a single transaction, and it has to hold the lock for the
// whole run. TryLock never waits - a replica that finds the lock taken skips
// that tick entirely, because a sweep already running is a sweep already
// happening, and queueing behind it would only run it twice in a row.
//
// The lock is not a correctness guarantee against every failure: a process
// killed mid-sweep releases its lock when the connection dies, and another
// replica may then repeat the work on the next tick. That is the honest bound -
// it turns "always duplicated" into "duplicated only if a process dies
// mid-sweep", which is the difference between a design flaw and an edge case.

// LockKey identifies one sweep. Named constants rather than raw numbers so two
// sweeps cannot silently collide on the same key.
type LockKey string

const (
	LockAlerts     LockKey = "projectview.alerts"
	LockDigests    LockKey = "projectview.digests"
	LockRetention  LockKey = "projectview.retention"
	LockRecurrence LockKey = "projectview.recurrence"
)

// The attachment object sweeper deliberately has no key. Deleting an object is
// idempotent - a key already gone counts as done - so two replicas duplicate
// the work rather than corrupt it. Giving it a lock would serialise a sweep
// that does not need serialising.

// lockID hashes a name into the bigint the advisory lock functions take.
//
// FNV-1a rather than anything cryptographic: this needs to be stable across
// processes and versions, not unpredictable. A collision between two of the
// five names above would make them serialise against each other - wasteful, not
// wrong - and with five fixed strings it is checkable by eye rather than a risk
// to reason about.
func lockID(key LockKey) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(key))
	// Masked to stay positive: the argument is a signed bigint, and a negative
	// key works but reads like a bug in the logs.
	return int64(h.Sum64() & 0x7FFFFFFFFFFFFFFF)
}

// SweepLock is a held advisory lock. Release it when the sweep finishes.
type SweepLock struct {
	store *Store
	id    int64
	// conn pins the session: an advisory lock belongs to the connection that
	// took it, and a pool would otherwise hand the unlock to a different one,
	// which silently does nothing and leaves the lock held until the process
	// exits.
	conn *pgxpool.Conn
}

// TryLock takes the lock if it is free, and reports whether it got it.
//
// Never waits. A replica that loses the race skips this tick rather than
// running the sweep a moment later, which would be the duplication this exists
// to prevent.
func (s *Store) TryLock(ctx context.Context, key LockKey) (*SweepLock, bool, error) {
	conn, err := s.Pool.Acquire(ctx)
	if err != nil {
		return nil, false, err
	}

	id := lockID(key)
	var acquired bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, id).Scan(&acquired); err != nil {
		conn.Release()
		return nil, false, err
	}
	if !acquired {
		conn.Release()
		return nil, false, nil
	}
	return &SweepLock{store: s, id: id, conn: conn}, true, nil
}

// Unlock releases the lock and returns the connection to the pool. Safe to call
// on a nil lock, so callers can defer it without a guard.
func (l *SweepLock) Unlock(ctx context.Context) {
	if l == nil || l.conn == nil {
		return
	}
	_, _ = l.conn.Exec(ctx, `SELECT pg_advisory_unlock($1)`, l.id)
	l.conn.Release()
	l.conn = nil
}
