package services

import (
	"context"
	"time"

	"projectview/internal/logger"
	"projectview/internal/repo"
)

// RecurrenceScheduler produces the next instance of schedule-driven series.
//
// Completion-driven series need nothing here: they are spawned by the request
// that finished the previous one. This exists for the other mode, where the
// next instance is due whether or not anybody did the last one.
//
// It deliberately does not "catch up". A weekly report six weeks neglected has
// six missed dates, and creating six tasks would bury the person who was
// already not doing one; the series moves to the next date genuinely ahead and
// the unfinished instance stays open and overdue. That is the honest record.
type RecurrenceScheduler struct {
	Guard *SweepGuard

	recurrences *repo.Recurrences
	tasks       *repo.Tasks
	factory     TaskFactory
	interval    time.Duration
	batch       int
}

func NewRecurrenceScheduler(recurrences *repo.Recurrences, tasks *repo.Tasks, factory TaskFactory) *RecurrenceScheduler {
	return &RecurrenceScheduler{
		recurrences: recurrences,
		tasks:       tasks,
		factory:     factory,
		// A recurring task is due on a day, not at a minute. Checking every
		// fifteen makes the worst case a quarter of an hour late, which nobody
		// notices, and keeps the query rare.
		interval: 15 * time.Minute,
		batch:    100,
	}
}

func (s *RecurrenceScheduler) Start() {
	if s.recurrences == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		for {
			// Guarded so two replicas do not each spawn the next instance of
			// the same series.
			s.Guard.Do(context.Background(), "Recurrence", func(ctx context.Context) { s.Run(ctx) })
			<-ticker.C
		}
	}()
	logger.Info("Recurrence: scheduled series checked every %s", s.interval)
}

// Run performs one sweep and reports how many instances it created.
//
// Like the alert sweep beside it, this runs in every process and has no lock -
// so two replicas would both spawn. That is named in the runbook rather than
// papered over; the fix is the same PostgreSQL advisory lock the other
// schedulers need.
func (s *RecurrenceScheduler) Run(ctx context.Context) int {
	now := time.Now()

	due, err := s.recurrences.DueForSpawn(ctx, now, s.batch)
	if err != nil {
		logger.Warn("Recurrence: could not read the due series: %v", err)
		return 0
	}

	created := 0
	for i := range due {
		rule := due[i]

		task, err := s.tasks.ByID(ctx, rule.TaskID)
		if err != nil {
			// The task is gone but the rule survived somehow; clearing it stops
			// the sweep from retrying the same broken row every quarter hour.
			logger.Warn("Recurrence: %s has no task, removing the rule", rule.TaskID)
			_ = s.recurrences.Delete(ctx, rule.TaskID)
			continue
		}

		if rule.Exhausted(now) {
			if err := s.recurrences.Delete(ctx, rule.TaskID); err != nil {
				logger.Warn("Recurrence: could not clear an exhausted series on %s: %v", rule.TaskID, err)
			}
			continue
		}

		next, err := s.factory.SpawnNext(ctx, task, &rule, now)
		if err != nil {
			logger.Error("Recurrence: could not spawn from %s: %v", rule.TaskID, err)
			continue
		}
		created++
		logger.Info("Recurrence: %s spawned %s on schedule", task.ID, next.ID)
	}

	if created > 0 {
		logger.Info("Recurrence: created %d scheduled instance(s)", created)
	}
	return created
}
