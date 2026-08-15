package handlers

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"projectview/internal/auth"
	"projectview/internal/httpx"
	"projectview/internal/logger"
	"projectview/internal/models"
	"projectview/internal/repo"
	"projectview/internal/services"
	"projectview/internal/ws"
)

// ---------------------------------------------------------------------------
// Threads and reactions
// ---------------------------------------------------------------------------

type replyRequest struct {
	Body string `json:"body"`
}

// POST /api/chat/messages/:id/replies
func (a *API) ReplyToMessage(w http.ResponseWriter, r *http.Request) {
	messageID, ok := httpx.UUIDParam(w, r, "id")
	if !ok {
		return
	}
	var req replyRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.Body == "" {
		httpx.Error(w, http.StatusBadRequest, "Message body is required.")
		return
	}

	ctx := r.Context()
	requester := auth.CurrentUser(r)

	parent, channel, ok := a.requireMessageAccess(w, r, messageID)
	if !ok {
		return
	}
	if parent.ParentID != nil {
		httpx.Error(w, http.StatusBadRequest, "Replies cannot be nested. Reply to the original message instead.")
		return
	}

	message, err := a.Chat.ReplyTo(ctx, channel.ID, messageID, requester.ID, req.Body)
	if err != nil {
		if errors.Is(err, repo.ErrNestedThread) {
			httpx.Error(w, http.StatusBadRequest, "Replies cannot be nested.")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	a.notifyMentions(ctx, message, channel, requester)
	a.pushToChannel(channel, "chat:message", a.decorateMessage(ctx, *message, requester))

	httpx.JSON(w, http.StatusCreated, a.decorateMessage(ctx, *message, requester))
}

// GET /api/chat/messages/:id/replies
func (a *API) MessageThread(w http.ResponseWriter, r *http.Request) {
	messageID, ok := httpx.UUIDParam(w, r, "id")
	if !ok {
		return
	}
	if _, _, ok := a.requireMessageAccess(w, r, messageID); !ok {
		return
	}

	ctx := r.Context()
	replies, err := a.Chat.Thread(ctx, messageID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, a.decorateMessages(ctx, replies))
}

type reactionRequest struct {
	Emoji string `json:"emoji"`
}

// POST /api/chat/messages/:id/reactions - toggles the emoji for the caller.
func (a *API) ToggleReaction(w http.ResponseWriter, r *http.Request) {
	messageID, ok := httpx.UUIDParam(w, r, "id")
	if !ok {
		return
	}
	var req reactionRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	// A short allow-list of lengths rather than an emoji validator: the point
	// is to stop someone storing a paragraph as a "reaction".
	if req.Emoji == "" || len([]rune(req.Emoji)) > 4 {
		httpx.Error(w, http.StatusBadRequest, "A reaction must be a short emoji.")
		return
	}

	_, channel, ok := a.requireMessageAccess(w, r, messageID)
	if !ok {
		return
	}

	requester := auth.CurrentUser(r)
	added, err := a.Chat.ToggleReaction(r.Context(), messageID, requester.ID, req.Emoji)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	a.pushToChannel(channel, "chat:reaction", map[string]any{
		"messageId": messageID,
		"emoji":     req.Emoji,
		"userId":    requester.ID,
		"added":     added,
	})

	httpx.JSON(w, http.StatusOK, map[string]bool{"added": added})
}

// requireMessageAccess loads a message and the channel holding it, refusing
// anyone who is not a member of that channel.
func (a *API) requireMessageAccess(w http.ResponseWriter, r *http.Request, messageID uuid.UUID) (*models.ChatMessage, *models.ChatChannel, bool) {
	ctx := r.Context()
	requester := auth.CurrentUser(r)

	message, err := a.Chat.MessageByID(ctx, messageID)
	if err != nil {
		respondRepoError(w, err, "Message not found.")
		return nil, nil, false
	}

	channel, err := a.Chat.ChannelForMember(ctx, message.ChannelID, requester.ID)
	if err != nil {
		// Indistinguishable from "no such message", so the error does not
		// confirm a conversation exists.
		httpx.Error(w, http.StatusNotFound, "Message not found.")
		return nil, nil, false
	}
	return message, channel, true
}

func (a *API) pushToChannel(channel *models.ChatChannel, eventType string, payload any) {
	if a.Hub == nil {
		return
	}
	members := make([]string, 0, len(channel.Members))
	for _, member := range channel.Members {
		members = append(members, member.String())
	}
	a.Hub.SendToUsers(members, ws.Message{Type: eventType, Payload: payload})
}

// notifyMentions records who was named in a message and tells them, unless
// they named themselves.
func (a *API) notifyMentions(ctx context.Context, message *models.ChatMessage, channel *models.ChatChannel, author *models.User) {
	usernames := repo.ExtractMentions(message.Body)
	if len(usernames) == 0 {
		return
	}

	mentioned, err := a.Chat.RecordMentions(ctx, message.ID, usernames)
	if err != nil {
		logger.Warn("could not record mentions for message %s: %v", message.ID, err)
		return
	}

	channelMembers := map[uuid.UUID]bool{}
	for _, member := range channel.Members {
		channelMembers[member] = true
	}

	notifier := a.notifier()
	for _, userID := range mentioned {
		// Naming yourself is not a notification, and naming someone who
		// cannot read the channel would leak that the conversation exists.
		if userID == author.ID || !channelMembers[userID] {
			continue
		}
		_, _ = notifier.NotifyUser(ctx, services.NotifyInput{
			UserID: userID,
			Type:   models.NotifCommentMention,
			Title:  author.Name + " mentioned you",
			Body:   message.Body,
			Email:  true,
		})
	}
}

type messageResponse struct {
	models.ChatMessage
	Author     *PublicUser     `json:"author"`
	Reactions  []repo.Reaction `json:"reactions"`
	ReplyCount int             `json:"replyCount"`
}

// decorateMessages resolves authors, reactions and reply counts for a batch in
// three queries, however many messages there are.
func (a *API) decorateMessages(ctx context.Context, messages []models.ChatMessage) []messageResponse {
	authorIDs := []uuid.UUID{}
	messageIDs := make([]uuid.UUID, 0, len(messages))
	for _, message := range messages {
		authorIDs = append(authorIDs, derefIDs(message.Author)...)
		messageIDs = append(messageIDs, message.ID)
	}

	users := a.usersByID(ctx, uniqueIDs(authorIDs))
	reactions, _ := a.Chat.ReactionsFor(ctx, messageIDs)
	replyCounts, _ := a.Chat.ReplyCounts(ctx, messageIDs)

	out := make([]messageResponse, 0, len(messages))
	for _, message := range messages {
		item := messageResponse{
			ChatMessage: message,
			Reactions:   reactions[message.ID],
			ReplyCount:  replyCounts[message.ID],
		}
		if item.Reactions == nil {
			item.Reactions = []repo.Reaction{}
		}
		if message.Author != nil {
			if user, ok := users[*message.Author]; ok {
				item.Author = &user
			}
		}
		out = append(out, item)
	}
	return out
}

// decorateMessage is the single-message form, used on the write paths where
// the author is already known.
func (a *API) decorateMessage(ctx context.Context, message models.ChatMessage, author *models.User) messageResponse {
	item := messageResponse{ChatMessage: message, Reactions: []repo.Reaction{}}
	if author != nil {
		public := PublicUser{
			ID: author.ID, Name: author.Name,
			Email: author.Email, AvatarColor: author.AvatarColor,
		}
		item.Author = &public
	}
	return item
}

// ---------------------------------------------------------------------------
// Documents
// ---------------------------------------------------------------------------

// GET /api/docs?spaceId=&projectId=
func (a *API) ListDocs(w http.ResponseWriter, r *http.Request) {
	var spaceID, projectID *uuid.UUID
	if raw := r.URL.Query().Get("spaceId"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, "Invalid spaceId.")
			return
		}
		spaceID = &id
	}
	if raw := r.URL.Query().Get("projectId"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, "Invalid projectId.")
			return
		}
		projectID = &id
	}
	if spaceID == nil && projectID == nil {
		httpx.Error(w, http.StatusBadRequest, "Provide spaceId or projectId.")
		return
	}

	// A document is exactly as visible as the thing that contains it. Listing
	// without this check would turn /api/docs into a way around private spaces.
	if spaceID != nil {
		if _, ok := a.requireSpaceReadByID(w, r, *spaceID); !ok {
			return
		}
	}
	if projectID != nil {
		if _, ok := a.requireProjectWork(w, r, *projectID); !ok {
			return
		}
	}

	docs, err := a.Docs.ForScope(r.Context(), spaceID, projectID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, docs)
}

// GET /api/docs/:id
func (a *API) GetDoc(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.UUIDParam(w, r, "id")
	if !ok {
		return
	}
	doc, err := a.Docs.ByID(r.Context(), id)
	if err != nil {
		respondRepoError(w, err, "Document not found.")
		return
	}
	if !a.authorizeDoc(w, r, doc, docRead) {
		return
	}
	httpx.JSON(w, http.StatusOK, doc)
}

// What a caller wants to do with a document, mapped onto the role its
// container demands.
const (
	docRead   = ""
	docWrite  = repo.SpaceRoleMember
	docManage = repo.SpaceRoleAdmin
)

// authorizeDoc applies the permission of whatever contains the document: one in
// a project takes the project's rules, one in a space takes the space's.
// Documents carry no access list of their own, so a permission is granted in
// exactly one place and revoked in exactly one place.
func (a *API) authorizeDoc(w http.ResponseWriter, r *http.Request, doc *repo.Doc, minimum string) bool {
	if doc.ProjectID != nil {
		if minimum == docManage {
			_, ok := a.requireProjectManage(w, r, *doc.ProjectID)
			return ok
		}
		_, ok := a.requireProjectWork(w, r, *doc.ProjectID)
		return ok
	}
	if doc.SpaceID != nil {
		if minimum == docRead {
			_, ok := a.requireSpaceReadByID(w, r, *doc.SpaceID)
			return ok
		}
		_, ok := a.requireSpaceRoleByID(w, r, *doc.SpaceID, minimum)
		return ok
	}
	// A document belonging to nothing is unreachable by design; the column
	// constraint forbids it, so this is a guard against a future regression.
	httpx.Error(w, http.StatusForbidden, forbiddenMessage)
	return false
}

type docRequest struct {
	Title     string `json:"title"`
	Content   string `json:"content"`
	SpaceID   string `json:"spaceId"`
	ProjectID string `json:"projectId"`
	ParentID  string `json:"parentId"`
	Archived  *bool  `json:"archived"`
}

// POST /api/docs
func (a *API) CreateDoc(w http.ResponseWriter, r *http.Request) {
	var req docRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.Title == "" {
		httpx.Error(w, http.StatusBadRequest, "Document title is required.")
		return
	}

	doc := &repo.Doc{Title: req.Title, Content: req.Content}
	if spaceID, ok := httpx.OptionalUUID(req.SpaceID); ok && spaceID != nil {
		doc.SpaceID = spaceID
	}
	if projectID, ok := httpx.OptionalUUID(req.ProjectID); ok && projectID != nil {
		doc.ProjectID = projectID
	}
	if doc.SpaceID == nil && doc.ProjectID == nil {
		httpx.Error(w, http.StatusBadRequest, "A document belongs to a space or a project.")
		return
	}
	if parentID, ok := httpx.OptionalUUID(req.ParentID); ok && parentID != nil {
		doc.ParentID = parentID
	}

	requester := auth.CurrentUser(r)
	if !a.authorizeDoc(w, r, doc, docWrite) {
		return
	}

	if err := a.Docs.Create(r.Context(), doc, requester.ID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusCreated, doc)
}

// PUT /api/docs/:id
func (a *API) UpdateDoc(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.UUIDParam(w, r, "id")
	if !ok {
		return
	}
	doc, err := a.Docs.ByID(r.Context(), id)
	if err != nil {
		respondRepoError(w, err, "Document not found.")
		return
	}
	if !a.authorizeDoc(w, r, doc, docWrite) {
		return
	}

	var req docRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}

	var title, content *string
	if req.Title != "" {
		title = &req.Title
	}
	// An empty body is a legitimate edit - clearing a page - so content is
	// always applied rather than treated as "not supplied".
	content = &req.Content

	requester := auth.CurrentUser(r)
	if err := a.Docs.Update(r.Context(), id, title, content, req.Archived, requester.ID); err != nil {
		respondRepoError(w, err, "Document not found.")
		return
	}

	updated, err := a.Docs.ByID(r.Context(), id)
	if err != nil {
		respondRepoError(w, err, "Document not found.")
		return
	}
	httpx.JSON(w, http.StatusOK, updated)
}

// DELETE /api/docs/:id
func (a *API) DeleteDoc(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.UUIDParam(w, r, "id")
	if !ok {
		return
	}
	doc, err := a.Docs.ByID(r.Context(), id)
	if err != nil {
		respondRepoError(w, err, "Document not found.")
		return
	}
	if !a.authorizeDoc(w, r, doc, docManage) {
		return
	}

	if err := a.Docs.Delete(r.Context(), id); err != nil {
		respondRepoError(w, err, "Document not found.")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// GET /api/docs/:id/revisions
func (a *API) DocRevisions(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.UUIDParam(w, r, "id")
	if !ok {
		return
	}
	doc, err := a.Docs.ByID(r.Context(), id)
	if err != nil {
		respondRepoError(w, err, "Document not found.")
		return
	}
	// The history is the document: whoever cannot read one cannot read the
	// other.
	if !a.authorizeDoc(w, r, doc, docRead) {
		return
	}

	revisions, err := a.Docs.Revisions(r.Context(), id, 25)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, revisions)
}

// GET /api/docs/:id/revisions/:revisionId - one past version, with its text.
// Keeping history is only worth anything if the old text can be read back.
func (a *API) DocRevision(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.UUIDParam(w, r, "id")
	if !ok {
		return
	}
	revisionID, err := strconv.ParseInt(chi.URLParam(r, "revisionId"), 10, 64)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "Invalid revision.")
		return
	}

	doc, err := a.Docs.ByID(r.Context(), id)
	if err != nil {
		respondRepoError(w, err, "Document not found.")
		return
	}
	if !a.authorizeDoc(w, r, doc, docRead) {
		return
	}

	revision, err := a.Docs.RevisionByID(r.Context(), id, revisionID)
	if err != nil {
		respondRepoError(w, err, "Revision not found.")
		return
	}
	httpx.JSON(w, http.StatusOK, revision)
}

// ---------------------------------------------------------------------------
// Notification preferences
// ---------------------------------------------------------------------------

// GET /api/notifications/preferences
func (a *API) GetNotificationPreferences(w http.ResponseWriter, r *http.Request) {
	requester := auth.CurrentUser(r)
	prefs, err := a.Preferences.Get(r.Context(), requester.ID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, prefs)
}

// PUT /api/notifications/preferences
func (a *API) SetNotificationPreferences(w http.ResponseWriter, r *http.Request) {
	var prefs repo.NotificationPreferences
	if !httpx.DecodeJSON(w, r, &prefs) {
		return
	}
	if !repo.ValidDigest(prefs.Digest) {
		httpx.Error(w, http.StatusBadRequest, "Digest must be off, daily or weekly.")
		return
	}
	if prefs.DigestHour < 0 || prefs.DigestHour > 23 {
		httpx.Error(w, http.StatusBadRequest, "Digest hour must be between 0 and 23.")
		return
	}
	for _, bound := range []*int{prefs.QuietStart, prefs.QuietEnd} {
		if bound != nil && (*bound < 0 || *bound > 23) {
			httpx.Error(w, http.StatusBadRequest, "Quiet hours must be between 0 and 23.")
			return
		}
	}

	requester := auth.CurrentUser(r)
	if err := a.Preferences.Set(r.Context(), requester.ID, prefs); err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	saved, err := a.Preferences.Get(r.Context(), requester.ID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, saved)
}

// GET /api/presence - who is online right now.
func (a *API) Presence(w http.ResponseWriter, r *http.Request) {
	if a.Hub == nil {
		httpx.JSON(w, http.StatusOK, []string{})
		return
	}
	httpx.JSON(w, http.StatusOK, a.Hub.OnlineUsers())
}
