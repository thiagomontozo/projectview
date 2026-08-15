package models

import (
	"time"

	"github.com/google/uuid"
)

type ProjectStatus struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Order int    `json:"order"`
	Color string `json:"color"`
}

// Project is a List in the Space -> Folder -> List hierarchy that also carries
// scheduling metadata. It keeps the name "project" throughout the API.
type Project struct {
	ID          uuid.UUID       `json:"id"`
	Name        string          `json:"name"`
	Key         string          `json:"key"`
	Description string          `json:"description,omitempty"`
	Color       string          `json:"color"`
	Status      string          `json:"status"`
	SpaceID     *uuid.UUID      `json:"spaceId,omitempty"`
	FolderID    *uuid.UUID      `json:"folderId,omitempty"`
	Position    int             `json:"position"`
	Archived    bool            `json:"archived"`
	TeamID      *uuid.UUID      `json:"-"`
	Members     []uuid.UUID     `json:"-"`
	Owner       *uuid.UUID      `json:"-"`
	StartDate   *time.Time      `json:"startDate,omitempty"`
	EndDate     *time.Time      `json:"endDate,omitempty"`
	Statuses    []ProjectStatus `json:"statuses"`
	CreatedBy   *uuid.UUID      `json:"-"`
	CreatedAt   time.Time       `json:"createdAt"`
	UpdatedAt   time.Time       `json:"updatedAt"`
}

const (
	ProjectStatusPlanning  = "planning"
	ProjectStatusActive    = "active"
	ProjectStatusOnHold    = "on_hold"
	ProjectStatusCompleted = "completed"
	ProjectStatusArchived  = "archived"
)

// ValidProjectStatus mirrors the CHECK constraint on projects.status, so a bad
// value is refused with a 400 instead of surfacing as a database error.
func ValidProjectStatus(s string) bool {
	switch s {
	case ProjectStatusPlanning, ProjectStatusActive, ProjectStatusOnHold,
		ProjectStatusCompleted, ProjectStatusArchived:
		return true
	}
	return false
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
