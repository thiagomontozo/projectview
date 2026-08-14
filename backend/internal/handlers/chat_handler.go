package handlers

import (
	"context"
	"net/http"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"

	"projectview/internal/auth"
	"projectview/internal/httpx"
	"projectview/internal/models"
	"projectview/internal/ws"
)

type chatChannelResponse struct {
	models.ChatChannel
	Members []PublicUser    `json:"members"`
	Project *projectRefLite `json:"project,omitempty"`
	Team    *teamRef        `json:"team,omitempty"`
}

func (a *API) populateChannel(ctx context.Context, ch models.ChatChannel) chatChannelResponse {
	users, _ := a.usersByID(ctx, uniqueIDs(ch.Members))
	resp := chatChannelResponse{ChatChannel: ch, Members: []PublicUser{}}
	for _, m := range ch.Members {
		if u, ok := users[m]; ok {
			resp.Members = append(resp.Members, u)
		}
	}
	if ch.Project != nil {
		var p models.Project
		if err := a.Store.Projects().FindOne(ctx, bson.M{"_id": *ch.Project}).Decode(&p); err == nil {
			resp.Project = &projectRefLite{ID: p.ID, Name: p.Name, Key: p.Key, Color: p.Color}
		}
	}
	if ch.Team != nil {
		var t models.Team
		if err := a.Store.Teams().FindOne(ctx, bson.M{"_id": *ch.Team}).Decode(&t); err == nil {
			resp.Team = &teamRef{ID: t.ID, Name: t.Name, Color: t.Color}
		}
	}
	return resp
}

// GET /api/chat/channels - channels the current user is a member of.
func (a *API) ListChannels(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	ctx := r.Context()
	cursor, err := a.Store.ChatChannels().Find(ctx, bson.M{"members": user.ID}, options.Find().SetSort(bson.D{{Key: "updatedAt", Value: -1}}))
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer cursor.Close(ctx)

	channels := []chatChannelResponse{}
	for cursor.Next(ctx) {
		var ch models.ChatChannel
		if err := cursor.Decode(&ch); err != nil {
			continue
		}
		channels = append(channels, a.populateChannel(ctx, ch))
	}
	httpx.JSON(w, http.StatusOK, channels)
}

type createChannelRequest struct {
	Name      string   `json:"name"`
	Type      string   `json:"type"`
	MemberIDs []string `json:"memberIds"`
	Team      string   `json:"team"`
	Project   string   `json:"project"`
}

// POST /api/chat/channels
func (a *API) CreateChannel(w http.ResponseWriter, r *http.Request) {
	var req createChannelRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.Type == "" {
		httpx.Error(w, http.StatusBadRequest, "Channel type is required.")
		return
	}

	requester := auth.CurrentUser(r)
	ctx := r.Context()
	members := uniqueIDs(append(httpx.ObjectIDs(req.MemberIDs), requester.ID))

	if req.Type == "dm" {
		var existing models.ChatChannel
		err := a.Store.ChatChannels().FindOne(ctx, bson.M{
			"type":    "dm",
			"members": bson.M{"$all": members, "$size": len(members)},
		}).Decode(&existing)
		if err == nil {
			httpx.JSON(w, http.StatusCreated, a.populateChannel(ctx, existing))
			return
		}

		now := time.Now()
		channel := models.ChatChannel{
			ID: primitive.NewObjectID(), Type: "dm", Members: members,
			CreatedBy: requester.ID, CreatedAt: now, UpdatedAt: now,
		}
		if _, err := a.Store.ChatChannels().InsertOne(ctx, channel); err != nil {
			httpx.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		httpx.JSON(w, http.StatusCreated, a.populateChannel(ctx, channel))
		return
	}

	now := time.Now()
	channel := models.ChatChannel{
		ID: primitive.NewObjectID(), Name: req.Name, Type: req.Type, Members: members,
		CreatedBy: requester.ID, CreatedAt: now, UpdatedAt: now,
	}
	if req.Team != "" {
		if id, err := primitive.ObjectIDFromHex(req.Team); err == nil {
			channel.Team = &id
		}
	}
	if req.Project != "" {
		if id, err := primitive.ObjectIDFromHex(req.Project); err == nil {
			channel.Project = &id
		}
	}
	if _, err := a.Store.ChatChannels().InsertOne(ctx, channel); err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusCreated, a.populateChannel(ctx, channel))
}

// GET /api/chat/channels/:channelId/messages
//
// Membership is enforced with the same filter PostMessage uses. Without it any
// authenticated user could read any conversation by guessing a channel id,
// direct messages included.
func (a *API) GetMessages(w http.ResponseWriter, r *http.Request) {
	channelID, ok := httpx.ObjectIDParam(w, r, "channelId")
	if !ok {
		return
	}
	ctx := r.Context()

	requester := auth.CurrentUser(r)
	var channel models.ChatChannel
	if err := a.Store.ChatChannels().FindOne(ctx, bson.M{"_id": channelID, "members": requester.ID}).Decode(&channel); err != nil {
		httpx.Error(w, http.StatusForbidden, "Not a member of this channel.")
		return
	}

	cursor, err := a.Store.ChatMessages().Find(ctx, bson.M{"channel": channelID},
		options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}}).SetLimit(100))
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer cursor.Close(ctx)

	messages := []models.ChatMessage{}
	for cursor.Next(ctx) {
		var m models.ChatMessage
		if err := cursor.Decode(&m); err != nil {
			continue
		}
		messages = append(messages, m)
	}
	// Reverse to chronological order (oldest first), then populate authors.
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}

	authorIDs := make([]primitive.ObjectID, 0, len(messages))
	for _, m := range messages {
		authorIDs = append(authorIDs, m.Author)
	}
	users, _ := a.usersByID(ctx, uniqueIDs(authorIDs))

	type messageResponse struct {
		models.ChatMessage
		Author *PublicUser `json:"author"`
	}
	out := make([]messageResponse, 0, len(messages))
	for _, m := range messages {
		mr := messageResponse{ChatMessage: m}
		if u, ok := users[m.Author]; ok {
			mr.Author = &u
		}
		out = append(out, mr)
	}

	httpx.JSON(w, http.StatusOK, out)
}

type postMessageRequest struct {
	Body string `json:"body"`
}

// POST /api/chat/channels/:channelId/messages - persists the message and
// pushes it over the WebSocket hub to every other channel member.
func (a *API) PostMessage(w http.ResponseWriter, r *http.Request) {
	channelID, ok := httpx.ObjectIDParam(w, r, "channelId")
	if !ok {
		return
	}
	var req postMessageRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.Body == "" {
		httpx.Error(w, http.StatusBadRequest, "Message body is required.")
		return
	}

	requester := auth.CurrentUser(r)
	ctx := r.Context()

	var channel models.ChatChannel
	if err := a.Store.ChatChannels().FindOne(ctx, bson.M{"_id": channelID, "members": requester.ID}).Decode(&channel); err != nil {
		httpx.Error(w, http.StatusForbidden, "Not a member of this channel.")
		return
	}

	now := time.Now()
	message := models.ChatMessage{
		ID: primitive.NewObjectID(), Channel: channelID, Author: requester.ID, Body: req.Body,
		ReadBy: []primitive.ObjectID{requester.ID}, CreatedAt: now, UpdatedAt: now,
	}
	if _, err := a.Store.ChatMessages().InsertOne(ctx, message); err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	_, _ = a.Store.ChatChannels().UpdateByID(ctx, channelID, bson.M{"$set": bson.M{"updatedAt": now}})

	author := PublicUser{ID: requester.ID, Name: requester.Name, Email: requester.Email, AvatarColor: requester.AvatarColor}
	payload := struct {
		models.ChatMessage
		Author PublicUser `json:"author"`
	}{ChatMessage: message, Author: author}

	if a.Hub != nil {
		memberIDs := make([]string, 0, len(channel.Members))
		for _, m := range channel.Members {
			memberIDs = append(memberIDs, m.Hex())
		}
		a.Hub.SendToUsers(memberIDs, ws.Message{Type: "chat:message", Payload: payload})
	}

	httpx.JSON(w, http.StatusCreated, payload)
}
