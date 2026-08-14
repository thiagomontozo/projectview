package models

import (
	"time"

	"github.com/google/uuid"
)

// Notification covers in-app notifications, including individual deadline
// alerts. Delivered in real time over WebSocket when possible, and always
// persisted so the user sees it next time they log in even if offline.
type Notification struct {
	ID        uuid.UUID  `json:"id"`
	User      uuid.UUID  `json:"user"`
	Type      string     `json:"type"`
	Title     string     `json:"title"`
	Body      string     `json:"body,omitempty"`
	Task      *uuid.UUID `json:"task,omitempty"`
	Project   *uuid.UUID `json:"project,omitempty"`
	Read      bool       `json:"read"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
}

const (
	NotifTaskDueSoon    = "task_due_soon"
	NotifTaskOverdue    = "task_overdue"
	NotifTaskAssigned   = "task_assigned"
	NotifCommentMention = "comment_mention"
	NotifChatMessage    = "chat_message"
	NotifGeneral        = "general"
)
