package services

import (
	"context"
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
	"go.mongodb.org/mongo-driver/bson"

	"projectview/internal/config"
	"projectview/internal/db"
	"projectview/internal/logger"
	"projectview/internal/models"
)

// AlertScheduler individually alerts every assigned resource about tasks
// that are coming due soon or already overdue. Each (task, user, alertType)
// combination is only alerted once, tracked via Task.AlertsSent, so people
// aren't spammed on every cron tick.
type AlertScheduler struct {
	store    *db.Store
	cfg      *config.Config
	notifier *Notifier
}

func NewAlertScheduler(store *db.Store, cfg *config.Config, notifier *Notifier) *AlertScheduler {
	return &AlertScheduler{store: store, cfg: cfg, notifier: notifier}
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
	now := time.Now()
	warnThreshold := now.Add(time.Duration(s.cfg.Alerts.WarnDaysBefore) * 24 * time.Hour)

	cursor, err := s.store.Tasks().Find(ctx, bson.M{
		"status":    bson.M{"$ne": "done"},
		"dueDate":   bson.M{"$ne": nil, "$lte": warnThreshold},
		"assignees": bson.M{"$exists": true, "$ne": bson.A{}},
	})
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)

	sent := 0
	for cursor.Next(ctx) {
		var task models.Task
		if err := cursor.Decode(&task); err != nil {
			continue
		}
		if task.DueDate == nil {
			continue
		}

		isOverdue := task.DueDate.Before(now)
		alertType := models.AlertTypeDueSoon
		if isOverdue {
			alertType = models.AlertTypeOverdue
		}

		newAlerts := []models.AlertSent{}
		changed := false

		for _, userID := range task.Assignees {
			alreadySent := false
			for _, a := range task.AlertsSent {
				if a.User == userID && a.Type == alertType {
					alreadySent = true
					break
				}
			}
			if alreadySent {
				continue
			}

			title := fmt.Sprintf("Due soon: %q", task.Title)
			body := fmt.Sprintf("This task is due on %s (within %d day(s)).", task.DueDate.Format("Jan 2, 2006"), s.cfg.Alerts.WarnDaysBefore)
			notifType := models.NotifTaskDueSoon
			if isOverdue {
				title = fmt.Sprintf("Overdue: %q", task.Title)
				body = fmt.Sprintf("This task was due on %s and is now overdue.", task.DueDate.Format("Jan 2, 2006"))
				notifType = models.NotifTaskOverdue
			}

			projectID := task.Project
			if _, err := s.notifier.NotifyUser(ctx, NotifyInput{
				UserID:  userID,
				Type:    notifType,
				Title:   title,
				Body:    body,
				Task:    &task.ID,
				Project: &projectID,
				Email:   true,
			}); err != nil {
				logger.Error("failed to notify user %s about task %s: %v", userID.Hex(), task.ID.Hex(), err)
				continue
			}

			newAlerts = append(newAlerts, models.AlertSent{User: userID, Type: alertType, SentAt: now})
			changed = true
			sent++
		}

		if changed {
			_, err := s.store.Tasks().UpdateByID(ctx, task.ID, bson.M{"$push": bson.M{"alertsSent": bson.M{"$each": newAlerts}}})
			if err != nil {
				logger.Error("failed to persist alertsSent for task %s: %v", task.ID.Hex(), err)
			}
		}
	}

	if sent > 0 {
		logger.Info("Deadline alert sweep sent %d individual notification(s).", sent)
	}
	return nil
}
