package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Notification covers in-app notifications, including individual deadline
// alerts. Delivered in real time over WebSocket when possible, and always
// persisted so the user sees it next time they log in even if offline.
type Notification struct {
	ID        primitive.ObjectID  `bson:"_id,omitempty" json:"id"`
	User      primitive.ObjectID  `bson:"user" json:"user"`
	Type      string              `bson:"type" json:"type"`
	Title     string              `bson:"title" json:"title"`
	Body      string              `bson:"body,omitempty" json:"body,omitempty"`
	Task      *primitive.ObjectID `bson:"task,omitempty" json:"task,omitempty"`
	Project   *primitive.ObjectID `bson:"project,omitempty" json:"project,omitempty"`
	Read      bool                `bson:"read" json:"read"`
	CreatedAt time.Time           `bson:"createdAt" json:"createdAt"`
	UpdatedAt time.Time           `bson:"updatedAt" json:"updatedAt"`
}

const (
	NotifTaskDueSoon    = "task_due_soon"
	NotifTaskOverdue    = "task_overdue"
	NotifTaskAssigned   = "task_assigned"
	NotifCommentMention = "comment_mention"
	NotifChatMessage    = "chat_message"
	NotifGeneral        = "general"
)
