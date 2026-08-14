package services

import (
	"context"

	"github.com/google/uuid"

	"projectview/internal/models"
	"projectview/internal/repo"
	"projectview/internal/ws"
)

type Notifier struct {
	notifications *repo.Notifications
	users         *repo.Users
	hub           *ws.Hub
	mailer        *Mailer
}

func NewNotifier(notifications *repo.Notifications, users *repo.Users, hub *ws.Hub, mailer *Mailer) *Notifier {
	return &Notifier{notifications: notifications, users: users, hub: hub, mailer: mailer}
}

type NotifyInput struct {
	UserID  uuid.UUID
	Type    string
	Title   string
	Body    string
	Task    *uuid.UUID
	Project *uuid.UUID
	Email   bool
}

// NotifyUser creates a persisted notification, pushes it in real time over the
// WebSocket hub, and optionally emails the user (used both for deadline alerts
// and general task events like assignment and comments).
func (n *Notifier) NotifyUser(ctx context.Context, in NotifyInput) (*models.Notification, error) {
	notification := &models.Notification{
		ID:      uuid.New(),
		User:    in.UserID,
		Type:    in.Type,
		Title:   in.Title,
		Body:    in.Body,
		Task:    in.Task,
		Project: in.Project,
	}
	if err := n.notifications.Create(ctx, notification); err != nil {
		return nil, err
	}

	if n.hub != nil {
		n.hub.SendToUser(in.UserID.String(), ws.Message{Type: "notification", Payload: notification})
	}

	if in.Email {
		if user, err := n.users.ByID(ctx, in.UserID); err == nil && user.NotifyByEmail {
			email, title, body := user.Email, in.Title, in.Body
			go func() {
				_ = n.mailer.Send(email, title, "<p>"+body+"</p>")
			}()
		}
	}

	return notification, nil
}
