package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type Team struct {
	ID          primitive.ObjectID   `bson:"_id,omitempty" json:"id"`
	Name        string               `bson:"name" json:"name"`
	Description string               `bson:"description,omitempty" json:"description,omitempty"`
	Color       string               `bson:"color" json:"color"`
	Members     []primitive.ObjectID `bson:"members" json:"members"`
	LeadID      *primitive.ObjectID  `bson:"leadId,omitempty" json:"leadId,omitempty"`
	CreatedBy   primitive.ObjectID   `bson:"createdBy,omitempty" json:"createdBy,omitempty"`
	CreatedAt   time.Time            `bson:"createdAt" json:"createdAt"`
	UpdatedAt   time.Time            `bson:"updatedAt" json:"updatedAt"`
}
