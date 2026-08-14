package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// ChatChannel can be a team channel, a project channel, or a direct message
// between two or more users ("dm").
type ChatChannel struct {
	ID        primitive.ObjectID   `bson:"_id,omitempty" json:"id"`
	Name      string               `bson:"name,omitempty" json:"name,omitempty"`
	Type      string               `bson:"type" json:"type"` // team|project|dm
	Team      *primitive.ObjectID  `bson:"team,omitempty" json:"team,omitempty"`
	Project   *primitive.ObjectID  `bson:"project,omitempty" json:"project,omitempty"`
	Members   []primitive.ObjectID `bson:"members" json:"members"`
	CreatedBy primitive.ObjectID   `bson:"createdBy,omitempty" json:"createdBy,omitempty"`
	CreatedAt time.Time            `bson:"createdAt" json:"createdAt"`
	UpdatedAt time.Time            `bson:"updatedAt" json:"updatedAt"`
}

type ChatMessage struct {
	ID        primitive.ObjectID   `bson:"_id,omitempty" json:"id"`
	Channel   primitive.ObjectID   `bson:"channel" json:"channel"`
	Author    primitive.ObjectID   `bson:"author" json:"author"`
	Body      string               `bson:"body" json:"body"`
	ReadBy    []primitive.ObjectID `bson:"readBy" json:"readBy"`
	CreatedAt time.Time            `bson:"createdAt" json:"createdAt"`
	UpdatedAt time.Time            `bson:"updatedAt" json:"updatedAt"`
}
