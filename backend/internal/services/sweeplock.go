package services

import (
	"context"

	"projectview/internal/db"
	"projectview/internal/logger"
)

// SweepGuard makes a scheduled sweep run once across the installation instead
// of once per process.
//
// Every timer-driven sweep in this package - deadline alerts, digests,
// retention, recurrence - ticks inside every backend process. With one replica
// that is invisible; with two it means every alert sent twice, every digest
// sent twice and every recurring task spawned twice, which is why the runbook
// named these as the reason not to scale out.
//
// A nil guard runs the sweep unconditionally. That is deliberate rather than
// defensive: it keeps single-process behaviour and every existing test working
// without a database, and makes the lock something main wires in rather than
// something each scheduler has to know about.
type SweepGuard struct {
	store *db.Store
	key   db.LockKey
}

func NewSweepGuard(store *db.Store, key db.LockKey) *SweepGuard {
	if store == nil {
		return nil
	}
	return &SweepGuard{store: store, key: key}
}

// Do runs the sweep if this process can take the lock.
//
// A replica that loses the race skips the tick entirely rather than waiting:
// a sweep already running is a sweep already happening, and queueing behind it
// would only run it twice in a row - the exact duplication being prevented.
func (g *SweepGuard) Do(ctx context.Context, name string, run func(context.Context)) {
	if g == nil {
		run(ctx)
		return
	}

	lock, acquired, err := g.store.TryLock(ctx, g.key)
	if err != nil {
		// Failing to reach the lock is not a reason to skip the work in a
		// single-replica installation, which is the overwhelmingly common case.
		// Running is the safer default here: a missed alert is worse than a
		// duplicated one, and duplication requires a second replica to even be
		// possible.
		logger.Warn("%s: could not take the sweep lock, running anyway: %v", name, err)
		run(ctx)
		return
	}
	if !acquired {
		logger.Info("%s: another replica is already running this sweep, skipping", name)
		return
	}
	defer lock.Unlock(ctx)

	run(ctx)
}
