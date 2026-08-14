package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type ProjectStatus struct {
	Key   string `bson:"key" json:"key"`
	Label string `bson:"label" json:"label"`
	Order int    `bson:"order" json:"order"`
	Color string `bson:"color" json:"color"`
}

type Project struct {
	ID          primitive.ObjectID   `bson:"_id,omitempty" json:"id"`
	Name        string               `bson:"name" json:"name"`
	Key         string               `bson:"key" json:"key"`
	Description string               `bson:"description,omitempty" json:"description,omitempty"`
	Color       string               `bson:"color" json:"color"`
	Status      string               `bson:"status" json:"status"` // planning|active|on_hold|completed|archived
	Team        *primitive.ObjectID  `bson:"team,omitempty" json:"team,omitempty"`
	Members     []primitive.ObjectID `bson:"members" json:"members"`
	Owner       primitive.ObjectID   `bson:"owner,omitempty" json:"owner,omitempty"`
	StartDate   *time.Time           `bson:"startDate,omitempty" json:"startDate,omitempty"`
	EndDate     *time.Time           `bson:"endDate,omitempty" json:"endDate,omitempty"`
	Statuses    []ProjectStatus      `bson:"statuses" json:"statuses"`
	CreatedBy   primitive.ObjectID   `bson:"createdBy,omitempty" json:"createdBy,omitempty"`
	CreatedAt   time.Time            `bson:"createdAt" json:"createdAt"`
	UpdatedAt   time.Time            `bson:"updatedAt" json:"updatedAt"`
}

// DefaultStatuses mirrors the kanban columns seeded for every new project.
func DefaultStatuses() []ProjectStatus {
	return []ProjectStatus{
		{Key: "backlog", Label: "Backlog", Order: 0, Color: "#94a3b8"},
		{Key: "todo", Label: "To Do", Order: 1, Color: "#60a5fa"},
		{Key: "in_progress", Label: "In Progress", Order: 2, Color: "#f59e0b"},
		{Key: "review", Label: "In Review", Order: 3, Color: "#a855f7"},
		{Key: "done", Label: "Done", Order: 4, Color: "#22c55e"},
	}
}
