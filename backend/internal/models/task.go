package models

import (
	"time"

	"github.com/google/uuid"
)

type ChecklistItem struct {
	ID   uuid.UUID `json:"id"`
	Text string    `json:"text"`
	Done bool      `json:"done"`
}

type Comment struct {
	ID        uuid.UUID  `json:"id"`
	Author    *uuid.UUID `json:"-"`
	Body      string     `json:"body"`
	CreatedAt time.Time  `json:"createdAt"`
}

// Task represents both top-level tasks and sub-tasks. A sub-task sets
// ParentTask to the owning task's id, which the schema enforces with a
// self-referencing foreign key that cascades on delete.
type Task struct {
	ID            uuid.UUID       `json:"id"`
	Title         string          `json:"title"`
	Description   string          `json:"description,omitempty"`
	ProjectID     uuid.UUID       `json:"-"`
	ParentTask    *uuid.UUID      `json:"parentTask,omitempty"`
	Assignees     []uuid.UUID     `json:"-"`
	Status        string          `json:"status"`
	Priority      string          `json:"priority"`
	StartDate     *time.Time      `json:"startDate,omitempty"`
	DueDate       *time.Time      `json:"dueDate,omitempty"`
	CompletedAt   *time.Time      `json:"completedAt,omitempty"`
	EstimateHours float64         `json:"estimateHours"`
	Order         float64         `json:"order"`
	Tags          []string        `json:"tags"`
	Checklist     []ChecklistItem `json:"checklist"`
	Comments      []Comment       `json:"-"`
	CreatedBy     *uuid.UUID      `json:"-"`
	CreatedAt     time.Time       `json:"createdAt"`
	UpdatedAt     time.Time       `json:"updatedAt"`
}

const (
	PriorityLow    = "low"
	PriorityMedium = "medium"
	PriorityHigh   = "high"
	PriorityUrgent = "urgent"

	AlertTypeDueSoon = "due_soon"
	AlertTypeOverdue = "overdue"
)

// ValidPriority mirrors the CHECK constraint on tasks.priority.
func ValidPriority(p string) bool {
	switch p {
	case PriorityLow, PriorityMedium, PriorityHigh, PriorityUrgent:
		return true
	}
	return false
}
