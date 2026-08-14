package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type User struct {
	ID            primitive.ObjectID   `bson:"_id,omitempty" json:"id"`
	Username      string               `bson:"username" json:"username"`
	Name          string               `bson:"name" json:"name"`
	Email         string               `bson:"email" json:"email"`
	PasswordHash  string               `bson:"passwordHash,omitempty" json:"-"`
	AuthSource    string               `bson:"authSource" json:"authSource"` // "local" | "ad"
	Role          string               `bson:"role" json:"role"`             // "admin" | "manager" | "member"
	Title         string               `bson:"title,omitempty" json:"title,omitempty"`
	AvatarColor   string               `bson:"avatarColor" json:"avatarColor"`
	Teams         []primitive.ObjectID `bson:"teams" json:"teams"`
	Active        bool                 `bson:"active" json:"active"`
	NotifyByEmail bool                 `bson:"notifyByEmail" json:"notifyByEmail"`
	LastLoginAt   *time.Time           `bson:"lastLoginAt,omitempty" json:"lastLoginAt,omitempty"`
	CreatedAt     time.Time            `bson:"createdAt" json:"createdAt"`
	UpdatedAt     time.Time            `bson:"updatedAt" json:"updatedAt"`
}

const (
	AuthSourceLocal = "local"
	AuthSourceAD    = "ad"

	RoleAdmin   = "admin"
	RoleManager = "manager"
	RoleMember  = "member"
)
