// Package models holds the domain types shared by repositories and handlers.
//
// Identifiers are UUIDs. The JSON contract is unchanged from the document-store
// era - ids were opaque strings to every client then, and still are - but the
// database now enforces the relationships that used to live in arrays.
package models

import (
	"time"

	"github.com/google/uuid"
)

type User struct {
	ID            uuid.UUID   `json:"id"`
	Username      string      `json:"username"`
	Name          string      `json:"name"`
	Email         string      `json:"email"`
	PasswordHash  string      `json:"-"`
	AuthSource    string      `json:"authSource"` // "local" | "ad"
	Role          string      `json:"role"`       // "admin" | "manager" | "member"
	Title         string      `json:"title,omitempty"`
	AvatarColor   string      `json:"avatarColor"`
	Teams         []uuid.UUID `json:"teams"`
	Active        bool        `json:"active"`
	NotifyByEmail bool        `json:"notifyByEmail"`
	LastLoginAt   *time.Time  `json:"lastLoginAt,omitempty"`
	// WeeklyCapacity is the hours a week this person is available for, which
	// capacity planning compares committed work against.
	WeeklyCapacity float64 `json:"weeklyCapacityHours"`
	// ExternalID is the identity provider's stable subject. Never shown: it
	// identifies the account to a system, not to a person.
	ExternalID *string   `json:"-"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

const (
	AuthSourceLocal = "local"
	AuthSourceAD    = "ad"
	AuthSourceOIDC  = "oidc"
	AuthSourceSCIM  = "scim"

	RoleAdmin   = "admin"
	RoleManager = "manager"
	RoleMember  = "member"
)

// ValidRole reports whether r is one of the three roles the schema accepts.
func ValidRole(r string) bool {
	return r == RoleAdmin || r == RoleManager || r == RoleMember
}
