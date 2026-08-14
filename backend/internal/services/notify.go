package services

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"projectview/internal/db"
	"projectview/internal/models"
	"projectview/internal/ws"
)

type Notifier struct {
	store  *db.Store
	hub    *ws.Hub
	mailer *Mailer
}

func NewNotifier(store *db.Store, hub *ws.Hub, mailer *Mailer) *Notifier {
	return &Notifier{store: store, hub: hub, mailer: mailer}
}

type NotifyInput struct {
	UserID  primitive.ObjectID
	Type    string
	Title   string
	Body    string
	Task    *primitive.ObjectID
	Project *primitive.ObjectID
	Email   bool
}

// NotifyUser creates a persisted notification, pushes it in real time over
// the WebSocket hub, and optionally emails the user (used both for deadline
// alerts and general task events like assignment/comments).
func (n *Notifier) NotifyUser(ctx context.Context, in NotifyInput) (*models.Notification, error) {
	now := time.Now()
	notification := models.Notification{
		ID:        primitive.NewObjectID(),
		User:      in.UserID,
		Type:      in.Type,
		Title:     in.Title,
		Body:      in.Body,
		Task:      in.Task,
		Project:   in.Project,
		Read:      false,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if _, err := n.store.Notifications().InsertOne(ctx, notification); err != nil {
		return nil, err
	}

	if n.hub != nil {
		n.hub.SendToUser(in.UserID.Hex(), ws.Message{Type: "notification", Payload: notification})
	}

	if in.Email {
		var user models.User
		if err := n.store.Users().FindOne(ctx, bson.M{"_id": in.UserID}).Decode(&user); err == nil {
			if user.NotifyByEmail {
				go func() {
					_ = n.mailer.Send(user.Email, in.Title, "<p>"+in.Body+"</p>")
				}()
			}
		}
	}

	return &notification, nil
}
