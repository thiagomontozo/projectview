package services

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/robfig/cron/v3"

	"projectview/internal/config"
	"projectview/internal/logger"
	"projectview/internal/models"
	"projectview/internal/repo"
)

// AlertScheduler individually alerts every assigned resource about tasks that
// are coming due soon or already overdue.
//
// De-duplication now lives in the database: task_alerts_sent has
// (task, user, alert_type) as its primary key, and the query that finds work
// to do excludes rows that already exist. The previous version loaded every
// task and compared arrays in Go.
type AlertScheduler struct {
	cron *cron.Cron

	// Guard makes this run once across the installation. Set by main; nil
	// keeps the single-process behaviour every test relies on.
	Guard *SweepGuard

	tasks    *repo.Tasks
	cfg      *config.Config
	notifier *Notifier
	engine   *AutomationEngine
}

func NewAlertScheduler(tasks *repo.Tasks, cfg *config.Config, notifier *Notifier, engine *AutomationEngine) *AlertScheduler {
	return &AlertScheduler{tasks: tasks, cfg: cfg, notifier: notifier, engine: engine}
}

// Restart rebuilds the schedule from the current configuration.
//
// The cron expression is read when the cron is built, so a change saved from
// the settings screen used to do nothing until the next deploy. Calling this
// after a save is what turns that field from decoration into a setting.
func (s *AlertScheduler) Restart() {
	s.stop()
	s.Start()
}

func (s *AlertScheduler) stop() {
	if s.cron != nil {
		// Stop() lets a sweep already running finish; it only stops new ones
		// being scheduled. Cutting a half-sent batch of alerts off mid-flight
		// would be worse than letting it complete under the old schedule.
		s.cron.Stop()
		s.cron = nil
	}
}

func (s *AlertScheduler) Start() {
	s.stop()
	c := cron.New()
	s.cron = c
	_, err := c.AddFunc(s.cfg.Alerts().CronExpr, func() {
		// Guarded so two replicas do not each notify the same person about the
		// same deadline.
		s.Guard.Do(context.Background(), "Alerts", func(ctx context.Context) {
			if err := s.RunDeadlineCheck(ctx); err != nil {
				logger.Error("deadline alert sweep failed: %v", err)
			}
		})
	})
	if err != nil {
		logger.Error("invalid ALERT_CRON expression %q: %v", s.cfg.Alerts().CronExpr, err)
		return
	}
	c.Start()
	logger.Info("Deadline alert scheduler starting with cron %q", s.cfg.Alerts().CronExpr)

	// Also run once shortly after boot so alerts don't wait a full cron cycle.
	go func() {
		time.Sleep(15 * time.Second)
		if err := s.RunDeadlineCheck(context.Background()); err != nil {
			logger.Error("initial deadline alert sweep failed: %v", err)
		}
	}()
}

func (s *AlertScheduler) RunDeadlineCheck(ctx context.Context) error {
	threshold := time.Now().Add(time.Duration(s.cfg.Alerts().WarnDaysBefore) * 24 * time.Hour)

	pending, err := s.tasks.PendingDeadlineAlerts(ctx, threshold)
	if err != nil {
		return err
	}

	sent := 0
	// A task with several assignees produces several alerts; the automation
	// attached to the deadline should still run once.
	firedAutomation := map[uuid.UUID]bool{}

	for _, alert := range pending {
		alertType := models.AlertTypeDueSoon
		notifType := models.NotifTaskDueSoon
		title := fmt.Sprintf("Due soon: %q", alert.Title)
		body := fmt.Sprintf("This task is due on %s (within %d day(s)).",
			alert.DueDate.Format("Jan 2, 2006"), s.cfg.Alerts().WarnDaysBefore)

		if alert.Overdue {
			alertType = models.AlertTypeOverdue
			notifType = models.NotifTaskOverdue
			title = fmt.Sprintf("Overdue: %q", alert.Title)
			body = fmt.Sprintf("This task was due on %s and is now overdue.",
				alert.DueDate.Format("Jan 2, 2006"))
		}

		taskID, projectID := alert.TaskID, alert.ProjectID
		if _, err := s.notifier.NotifyUser(ctx, NotifyInput{
			UserID:  alert.UserID,
			Type:    notifType,
			Title:   title,
			Body:    body,
			Task:    &taskID,
			Project: &projectID,
			Email:   true,
		}); err != nil {
			logger.Error("failed to notify user %s about task %s: %v", alert.UserID, alert.TaskID, err)
			continue
		}

		if err := s.tasks.MarkAlertSent(ctx, alert.TaskID, alert.UserID, alertType); err != nil {
			logger.Error("failed to record alert for task %s: %v", alert.TaskID, err)
		}
		sent++

		// A deadline is also an automation trigger: this is what lets a rule
		// like "overdue -> raise priority and notify" exist. Fired once per
		// task rather than once per assignee, since the rule acts on the task.
		if s.engine != nil && !firedAutomation[alert.TaskID] {
			firedAutomation[alert.TaskID] = true
			trigger := TriggerTaskDueSoon
			if alert.Overdue {
				trigger = TriggerTaskOverdue
			}
			if task, err := s.tasks.ByID(ctx, alert.TaskID); err == nil {
				s.engine.Run(ctx, Event{
					Trigger:   trigger,
					Task:      task,
					ProjectID: alert.ProjectID,
				})
			}
		}
	}

	if sent > 0 {
		logger.Info("Deadline alert sweep sent %d individual notification(s).", sent)
	}
	return nil
}
