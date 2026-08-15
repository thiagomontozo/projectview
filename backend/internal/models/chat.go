package models

import (
	"time"

	"github.com/google/uuid"
)

// ChatChannel can be a team channel, a project channel, or a direct message
// between two or more users ("dm").
type ChatChannel struct {
	ID        uuid.UUID   `json:"id"`
	Name      string      `json:"name,omitempty"`
	Type      string      `json:"type"` // team|project|dm
	TeamID    *uuid.UUID  `json:"-"`
	ProjectID *uuid.UUID  `json:"-"`
	Members   []uuid.UUID `json:"-"`
	CreatedBy *uuid.UUID  `json:"-"`
	CreatedAt time.Time   `json:"createdAt"`
	UpdatedAt time.Time   `json:"updatedAt"`
}

const (
	ChannelTypeTeam    = "team"
	ChannelTypeProject = "project"
	ChannelTypeDM      = "dm"
)

// ValidChannelType mirrors the CHECK constraint on chat_channels.type.
func ValidChannelType(t string) bool {
	return t == ChannelTypeTeam || t == ChannelTypeProject || t == ChannelTypeDM
}

// ChatMessage is one message. A reply carries ParentID; threads are one level
// deep, enforced by a database trigger.
type ChatMessage struct {
	ID        uuid.UUID   `json:"id"`
	ChannelID uuid.UUID   `json:"channel"`
	Author    *uuid.UUID  `json:"-"`
	Body      string      `json:"body"`
	ParentID  *uuid.UUID  `json:"parentId,omitempty"`
	EditedAt  *time.Time  `json:"editedAt,omitempty"`
	ReadBy    []uuid.UUID `json:"readBy"`
	CreatedAt time.Time   `json:"createdAt"`
	UpdatedAt time.Time   `json:"updatedAt"`
}
