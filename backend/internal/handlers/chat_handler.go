package handlers

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"projectview/internal/auth"
	"projectview/internal/httpx"
	"projectview/internal/models"
	"projectview/internal/repo"
	"projectview/internal/ws"
)

type chatChannelResponse struct {
	models.ChatChannel
	Members []PublicUser    `json:"members"`
	Project *projectRefLite `json:"project,omitempty"`
	Team    *teamRef        `json:"team,omitempty"`
}

func (a *API) populateChannels(ctx context.Context, channels []models.ChatChannel) []chatChannelResponse {
	userIDs := []uuid.UUID{}
	for _, c := range channels {
		userIDs = append(userIDs, c.Members...)
	}
	users := a.usersByID(ctx, uniqueIDs(userIDs))

	projects := map[uuid.UUID]projectRefLite{}
	if all, err := a.Projects.List(ctx); err == nil {
		for _, p := range all {
			projects[p.ID] = projectRefLite{ID: p.ID, Name: p.Name, Key: p.Key, Color: p.Color}
		}
	}
	teams := map[uuid.UUID]teamRef{}
	if all, err := a.Teams.List(ctx); err == nil {
		for _, t := range all {
			teams[t.ID] = teamRef{ID: t.ID, Name: t.Name, Color: t.Color}
		}
	}

	out := make([]chatChannelResponse, 0, len(channels))
	for _, c := range channels {
		resp := chatChannelResponse{ChatChannel: c, Members: publicList(users, c.Members)}
		if c.ProjectID != nil {
			if p, ok := projects[*c.ProjectID]; ok {
				resp.Project = &p
			}
		}
		if c.TeamID != nil {
			if t, ok := teams[*c.TeamID]; ok {
				resp.Team = &t
			}
		}
		out = append(out, resp)
	}
	return out
}

// GET /api/chat/channels - channels the current user is a member of.
func (a *API) ListChannels(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	channels, err := a.Chat.ChannelsForUser(r.Context(), user.ID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, a.populateChannels(r.Context(), channels))
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
	if !models.ValidChannelType(req.Type) {
		httpx.Error(w, http.StatusBadRequest, "Channel type must be team, project or dm.")
		return
	}

	requester := auth.CurrentUser(r)
	ctx := r.Context()
	members := uniqueIDs(httpx.UUIDs(req.MemberIDs), []uuid.UUID{requester.ID})

	// Opening the same direct message twice reuses the existing conversation.
	if req.Type == models.ChannelTypeDM {
		if existing, err := a.Chat.FindDM(ctx, members); err == nil {
			httpx.JSON(w, http.StatusCreated, a.populateChannels(ctx, []models.ChatChannel{*existing})[0])
			return
		} else if !errors.Is(err, repo.ErrNotFound) {
			httpx.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	channel := &models.ChatChannel{
		ID: uuid.New(), Name: req.Name, Type: req.Type,
		Members: members, CreatedBy: &requester.ID,
	}
	if teamID, ok := httpx.OptionalUUID(req.Team); ok && teamID != nil {
		channel.TeamID = teamID
	}
	if projectID, ok := httpx.OptionalUUID(req.Project); ok && projectID != nil {
		channel.ProjectID = projectID
	}

	if err := a.Chat.CreateChannel(ctx, channel); err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusCreated, a.populateChannels(ctx, []models.ChatChannel{*channel})[0])
}

// GET /api/chat/channels/:channelId/messages
//
// Membership is part of the query, not a separate check that could be
// forgotten: without it any authenticated user could read any conversation by
// guessing a channel id, direct messages included.
func (a *API) GetMessages(w http.ResponseWriter, r *http.Request) {
	channelID, ok := httpx.UUIDParam(w, r, "channelId")
	if !ok {
		return
	}
	ctx := r.Context()
	requester := auth.CurrentUser(r)

	if _, err := a.Chat.ChannelForMember(ctx, channelID, requester.ID); err != nil {
		httpx.Error(w, http.StatusForbidden, "Not a member of this channel.")
		return
	}

	// Only the roots: a reply belongs to its thread, and repeating it here
	// would make every threaded exchange appear twice in the transcript. The
	// reply count on each root is what tells the reader a thread is there.
	messages, err := a.Chat.RootMessages(ctx, channelID, 100)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	httpx.JSON(w, http.StatusOK, a.decorateMessages(ctx, messages))
}

type postMessageRequest struct {
	Body string `json:"body"`
}

// POST /api/chat/channels/:channelId/messages - persists the message and
// pushes it over the WebSocket hub to every channel member.
func (a *API) PostMessage(w http.ResponseWriter, r *http.Request) {
	channelID, ok := httpx.UUIDParam(w, r, "channelId")
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

	ctx := r.Context()
	requester := auth.CurrentUser(r)

	channel, err := a.Chat.ChannelForMember(ctx, channelID, requester.ID)
	if err != nil {
		httpx.Error(w, http.StatusForbidden, "Not a member of this channel.")
		return
	}

	message, err := a.Chat.PostMessage(ctx, channelID, requester.ID, req.Body)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	author := PublicUser{
		ID: requester.ID, Name: requester.Name,
		Email: requester.Email, AvatarColor: requester.AvatarColor,
	}
	payload := struct {
		models.ChatMessage
		Author PublicUser `json:"author"`
	}{ChatMessage: *message, Author: author}

	if a.Hub != nil {
		memberIDs := make([]string, 0, len(channel.Members))
		for _, m := range channel.Members {
			memberIDs = append(memberIDs, m.String())
		}
		a.Hub.SendToUsers(memberIDs, ws.Message{Type: "chat:message", Payload: payload})
	}

	httpx.JSON(w, http.StatusCreated, payload)
}
