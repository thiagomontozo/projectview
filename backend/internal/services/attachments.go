package services

import (
	"context"
	"time"

	"projectview/internal/logger"
	"projectview/internal/repo"
	"projectview/internal/storage"
)

// ---------------------------------------------------------------------------
// The virus-scan seam
// ---------------------------------------------------------------------------

// Scanner examines an uploaded file before anybody can download it.
//
// Left as a seam rather than an implementation on purpose. Wiring ClamAV or a
// vendor API means an extra daemon, its signature updates and its own failure
// modes, which is an operational commitment an installation should take
// deliberately - and pretending to make it, by shipping something that returns
// "clean" without looking, would be worse than not having it.
//
// An implementation returns one of repo.ScanClean or repo.ScanInfected. It is
// called with the bytes already in hand, so it never has to fetch the object
// back out of the store.
//
// Returning an error refuses the whole upload and removes the object again.
// That is deliberate: the scan happens on this path and nowhere else, so a
// file stored while the scanner was unavailable would never be revisited, and
// an outage in the scanner would quietly turn the check off for its duration.
type Scanner interface {
	Scan(ctx context.Context, filename string, content []byte) (string, error)
}

// SkipScanner is the default: it records that nothing examined the file.
//
// It reports ScanSkipped, never ScanClean. The distinction is the whole point
// of the seam - an administrator looking at an attachment must be able to tell
// "checked and safe" from "nobody checked", and a status that collapses the two
// makes the scanner's absence invisible exactly where it matters.
type SkipScanner struct{}

func (SkipScanner) Scan(context.Context, string, []byte) (string, error) {
	return repo.ScanSkipped, nil
}

// ---------------------------------------------------------------------------
// Draining the deferred-delete queue
// ---------------------------------------------------------------------------

// ObjectSweeper removes objects whose rows are gone.
//
// The queue it drains is filled by a database trigger rather than by the
// delete handler, so it covers the cascades too: dropping a task, a project or
// a whole space removes attachment rows in one statement the application never
// observes, and those files have to go as well.
//
// Deleting an object is idempotent - a key that is already absent counts as
// done - which is what makes this safe to run in more than one replica, unlike
// the schedulers named in the runbook. It is still worth knowing that two
// replicas will duplicate the work rather than share it.
type ObjectSweeper struct {
	attachments *repo.Attachments
	objects     *storage.S3
	interval    time.Duration
	// batch bounds one pass. A queue that grew during an outage drains over
	// several passes instead of holding a connection open while thousands of
	// requests go out.
	batch int
}

func NewObjectSweeper(attachments *repo.Attachments, objects *storage.S3) *ObjectSweeper {
	return &ObjectSweeper{
		attachments: attachments,
		objects:     objects,
		interval:    5 * time.Minute,
		batch:       100,
	}
}

func (s *ObjectSweeper) Start() {
	if !s.objects.Enabled() {
		return
	}
	go func() {
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		for {
			// Runs immediately, then on the interval: a process that restarted
			// after a failed delete should not wait five minutes to retry.
			s.Run(context.Background())
			<-ticker.C
		}
	}()
	logger.Info("Attachments: object sweeper running every %s", s.interval)
}

// Run performs one pass and reports how many objects it removed.
func (s *ObjectSweeper) Run(ctx context.Context) (int, error) {
	keys, err := s.attachments.PendingDeletions(ctx, s.batch)
	if err != nil {
		logger.Warn("Attachments: could not read the deletion queue: %v", err)
		return 0, err
	}

	removed := 0
	for _, key := range keys {
		callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		err := s.objects.Delete(callCtx, key)
		cancel()

		if err != nil {
			// Left queued deliberately. A store that is briefly unreachable
			// must not be able to make this application forget that a file
			// somebody deleted is still sitting in a bucket.
			if failed := s.attachments.DeletionFailed(ctx, key, err.Error()); failed != nil {
				logger.Warn("Attachments: could not record a failed delete for %s: %v", key, failed)
			}
			continue
		}

		if err := s.attachments.DeletionDone(ctx, key); err != nil {
			// The object is gone but the queue entry survived; the next pass
			// deletes an absent key, which succeeds, and the entry clears then.
			logger.Warn("Attachments: could not clear %s from the deletion queue: %v", key, err)
			continue
		}
		removed++
	}

	if removed > 0 {
		logger.Info("Attachments: removed %d object(s) from the store", removed)
	}
	return removed, nil
}
