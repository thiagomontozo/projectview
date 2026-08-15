package services

import (
	"context"

	"github.com/robfig/cron/v3"

	"projectview/internal/config"
	"projectview/internal/logger"
	"projectview/internal/repo"
)

// RetentionSweeper enforces how long two tables nobody prunes by hand are kept.
//
// Retention is off unless somebody configures it. Deleting records because a
// configuration file happened to be empty is exactly the behaviour a retention
// policy exists to prevent, so the default windows are zero and the sweep does
// nothing until an organisation states what it wants.
type RetentionSweeper struct {
	privacy *repo.Privacy
	cfg     *config.Config
}

func NewRetentionSweeper(privacy *repo.Privacy, cfg *config.Config) *RetentionSweeper {
	return &RetentionSweeper{privacy: privacy, cfg: cfg}
}

func (s *RetentionSweeper) Start() {
	if s.cfg.Retention().AuditDays <= 0 && s.cfg.Retention().NotificationDays <= 0 {
		logger.Info("Retention: no policy configured, nothing will be purged")
		return
	}

	c := cron.New()
	if _, err := c.AddFunc(s.cfg.Retention().CronExpr, func() {
		s.Run(context.Background())
	}); err != nil {
		logger.Error("Retention: invalid cron expression %q: %v", s.cfg.Retention().CronExpr, err)
		return
	}
	c.Start()
	logger.Info("Retention: sweeping on %q (audit %dd, notifications %dd)",
		s.cfg.Retention().CronExpr, s.cfg.Retention().AuditDays, s.cfg.Retention().NotificationDays)
}

// Run performs one sweep. Logged with counts rather than a bare "completed",
// because a purge that silently deletes nothing and a purge that silently
// deletes everything otherwise look identical in the log.
func (s *RetentionSweeper) Run(ctx context.Context) {
	audit, notifications, err := s.privacy.Purge(ctx, s.cfg.Retention().AuditDays, s.cfg.Retention().NotificationDays)
	if err != nil {
		logger.Error("Retention sweep failed: %v", err)
		return
	}
	if audit > 0 || notifications > 0 {
		logger.Info("Retention: purged %d audit entries and %d read notifications", audit, notifications)
	}
}
