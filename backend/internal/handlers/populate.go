package handlers

import (
	"context"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"

	"projectview/internal/models"
)

// PublicUser strips sensitive/large fields when a user is embedded inside
// another response (assignee lists, comment authors, etc.) - mirrors the
// Mongoose ".populate('field', 'name email avatarColor')" pattern.
type PublicUser struct {
	ID          primitive.ObjectID `json:"id"`
	Name        string             `json:"name"`
	Email       string             `json:"email"`
	AvatarColor string             `json:"avatarColor"`
	Role        string             `json:"role,omitempty"`
	Title       string             `json:"title,omitempty"`
}

func (a *API) usersByID(ctx context.Context, ids []primitive.ObjectID) (map[primitive.ObjectID]PublicUser, error) {
	result := make(map[primitive.ObjectID]PublicUser, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	cursor, err := a.Store.Users().Find(ctx, bson.M{"_id": bson.M{"$in": ids}},
		options.Find().SetProjection(bson.M{"name": 1, "email": 1, "avatarColor": 1, "role": 1, "title": 1}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	for cursor.Next(ctx) {
		var u models.User
		if err := cursor.Decode(&u); err != nil {
			continue
		}
		result[u.ID] = PublicUser{ID: u.ID, Name: u.Name, Email: u.Email, AvatarColor: u.AvatarColor, Role: u.Role, Title: u.Title}
	}
	return result, nil
}

func uniqueIDs(ids ...[]primitive.ObjectID) []primitive.ObjectID {
	seen := make(map[primitive.ObjectID]bool)
	out := []primitive.ObjectID{}
	for _, group := range ids {
		for _, id := range group {
			if !id.IsZero() && !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	return out
}
