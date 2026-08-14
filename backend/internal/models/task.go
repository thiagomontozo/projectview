package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type ChecklistItem struct {
	ID   primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	Text string             `bson:"text" json:"text"`
	Done bool               `bson:"done" json:"done"`
}

type Comment struct {
	ID        primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	Author    primitive.ObjectID `bson:"author" json:"author"`
	Body      string             `bson:"body" json:"body"`
	CreatedAt time.Time          `bson:"createdAt" json:"createdAt"`
}

type AlertSent struct {
	User   primitive.ObjectID `bson:"user" json:"user"`
	Type   string             `bson:"type" json:"type"`
	SentAt time.Time          `bson:"sentAt" json:"sentAt"`
}

// Task represents both top-level tasks and sub-tasks. A sub-task simply sets
// ParentTask to the owning task's _id, allowing unlimited nesting.
type Task struct {
	ID            primitive.ObjectID   `bson:"_id,omitempty" json:"id"`
	Title         string               `bson:"title" json:"title"`
	Description   string               `bson:"description,omitempty" json:"description,omitempty"`
	Project       primitive.ObjectID   `bson:"project" json:"project"`
	ParentTask    *primitive.ObjectID  `bson:"parentTask,omitempty" json:"parentTask,omitempty"`
	Assignees     []primitive.ObjectID `bson:"assignees" json:"assignees"`
	Status        string               `bson:"status" json:"status"`
	Priority      string               `bson:"priority" json:"priority"` // low|medium|high|urgent
	StartDate     *time.Time           `bson:"startDate,omitempty" json:"startDate,omitempty"`
	DueDate       *time.Time           `bson:"dueDate,omitempty" json:"dueDate,omitempty"`
	CompletedAt   *time.Time           `bson:"completedAt,omitempty" json:"completedAt,omitempty"`
	EstimateHours float64              `bson:"estimateHours" json:"estimateHours"`
	Order         float64              `bson:"order" json:"order"`
	Tags          []string             `bson:"tags" json:"tags"`
	Checklist     []ChecklistItem      `bson:"checklist" json:"checklist"`
	Comments      []Comment            `bson:"comments" json:"comments"`
	AlertsSent    []AlertSent          `bson:"alertsSent" json:"-"`
	CreatedBy     primitive.ObjectID   `bson:"createdBy,omitempty" json:"createdBy,omitempty"`
	CreatedAt     time.Time            `bson:"createdAt" json:"createdAt"`
	UpdatedAt     time.Time            `bson:"updatedAt" json:"updatedAt"`
}

const (
	PriorityLow    = "low"
	PriorityMedium = "medium"
	PriorityHigh   = "high"
	PriorityUrgent = "urgent"

	AlertTypeDueSoon = "due_soon"
	AlertTypeOverdue = "overdue"
)
