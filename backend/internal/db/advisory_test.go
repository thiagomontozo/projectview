package db

import "testing"

// The lock keys are the only thing keeping four independent sweeps from
// serialising against each other, so their distinctness is worth asserting
// rather than assuming: a collision would not fail, it would quietly make two
// unrelated sweeps take turns.
func TestLockKeysAreDistinct(t *testing.T) {
	keys := []LockKey{LockAlerts, LockDigests, LockRetention, LockRecurrence, LockTriage}

	seen := map[int64]LockKey{}
	for _, key := range keys {
		id := lockID(key)
		if other, clash := seen[id]; clash {
			t.Errorf("%q and %q hash to the same lock id %d", key, other, id)
		}
		seen[id] = key

		// A negative id works but reads like a bug in a log line, and the
		// masking that prevents it is easy to lose in a refactor.
		if id < 0 {
			t.Errorf("%q produced a negative lock id: %d", key, id)
		}
	}
}

// The id has to be stable across processes and versions: two replicas compute
// it independently and must agree, and an upgrade that changed it would let an
// old and a new process both run the same sweep.
func TestLockIDIsStable(t *testing.T) {
	first := lockID(LockAlerts)
	for i := 0; i < 100; i++ {
		if lockID(LockAlerts) != first {
			t.Fatal("lockID is not deterministic")
		}
	}
	if lockID("projectview.alerts") != first {
		t.Error("the id depends on something other than the key's text")
	}
}

// A nil guard runs the sweep. That keeps single-process behaviour and every
// test that has no database working, and it is what makes the lock something
// main wires in rather than something each scheduler must know about.
func TestNilLockIsSafeToUnlock(t *testing.T) {
	var lock *SweepLock
	lock.Unlock(t.Context()) // must not panic
}
