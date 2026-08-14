package handlers

import (
	"context"

	"github.com/google/uuid"

	"projectview/internal/models"
)

// PublicUser strips sensitive fields when a user is embedded inside another
// response (assignee lists, comment authors, etc).
type PublicUser struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Email       string    `json:"email"`
	AvatarColor string    `json:"avatarColor"`
	Role        string    `json:"role,omitempty"`
	Title       string    `json:"title,omitempty"`
}

func toPublicUser(u models.User) PublicUser {
	return PublicUser{
		ID: u.ID, Name: u.Name, Email: u.Email,
		AvatarColor: u.AvatarColor, Role: u.Role, Title: u.Title,
	}
}

// usersByID resolves a batch of users in one query, and is the only place
// handlers reach for user records while composing a response.
func (a *API) usersByID(ctx context.Context, ids []uuid.UUID) map[uuid.UUID]PublicUser {
	out := map[uuid.UUID]PublicUser{}
	found, err := a.Users.PublicByIDs(ctx, ids)
	if err != nil {
		return out
	}
	for id, u := range found {
		out[id] = toPublicUser(u)
	}
	return out
}

// publicList maps ids to populated users, silently skipping ids that no longer
// resolve (a deleted account referenced by an old comment, say).
func publicList(users map[uuid.UUID]PublicUser, ids []uuid.UUID) []PublicUser {
	out := []PublicUser{}
	for _, id := range ids {
		if u, ok := users[id]; ok {
			out = append(out, u)
		}
	}
	return out
}

// uniqueIDs flattens and de-duplicates id groups, dropping zero values.
func uniqueIDs(groups ...[]uuid.UUID) []uuid.UUID {
	seen := make(map[uuid.UUID]bool)
	out := []uuid.UUID{}
	for _, group := range groups {
		for _, id := range group {
			if id != uuid.Nil && !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	return out
}

// derefIDs turns a slice of optional ids into a plain slice, for feeding
// usersByID.
func derefIDs(ids ...*uuid.UUID) []uuid.UUID {
	out := []uuid.UUID{}
	for _, id := range ids {
		if id != nil && *id != uuid.Nil {
			out = append(out, *id)
		}
	}
	return out
}
