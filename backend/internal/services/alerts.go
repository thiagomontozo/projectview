package services

import (
	"context"
	"fmt"
	"time"

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
	tasks    *repo.Tasks
	cfg      *config.Config
	notifier *Notifier
}

func NewAlertScheduler(tasks *repo.Tasks, cfg *config.Config, notifier *Notifier) *AlertScheduler {
	return &AlertScheduler{tasks: tasks, cfg: cfg, notifier: notifier}
}

func (s *AlertScheduler) Start() {
	c := cron.New()
	_, err := c.AddFunc(s.cfg.Alerts.CronExpr, func() {
		if err := s.RunDeadlineCheck(context.Background()); err != nil {
			logger.Error("deadline alert sweep failed: %v", err)
		}
	})
	if err != nil {
		logger.Error("invalid ALERT_CRON expression %q: %v", s.cfg.Alerts.CronExpr, err)
		return
	}
	c.Start()
	logger.Info("Deadline alert scheduler starting with cron %q", s.cfg.Alerts.CronExpr)

	// Also run once shortly after boot so alerts don't wait a full cron cycle.
	go func() {
		time.Sleep(15 * time.Second)
		if err := s.RunDeadlineCheck(context.Background()); err != nil {
			logger.Error("initial deadline alert sweep failed: %v", err)
		}
	}()
}

func (s *AlertScheduler) RunDeadlineCheck(ctx context.Context) error {
	threshold := time.Now().Add(time.Duration(s.cfg.Alerts.WarnDaysBefore) * 24 * time.Hour)

	pending, err := s.tasks.PendingDeadlineAlerts(ctx, threshold)
	if err != nil {
		return err
	}

	sent := 0
	for _, alert := range pending {
		alertType := models.AlertTypeDueSoon
		notifType := models.NotifTaskDueSoon
		title := fmt.Sprintf("Due soon: %q", alert.Title)
		body := fmt.Sprintf("This task is due on %s (within %d day(s)).",
			alert.DueDate.Format("Jan 2, 2006"), s.cfg.Alerts.WarnDaysBefore)

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
	}

	if sent > 0 {
		logger.Info("Deadline alert sweep sent %d individual notification(s).", sent)
	}
	return nil
}
