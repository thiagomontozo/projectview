package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"

	"projectview/internal/audit"
	"projectview/internal/auth"
	"projectview/internal/httpx"
	"projectview/internal/logger"
	"projectview/internal/repo"
	"projectview/internal/storage"
)

// Files and edits in chat.
//
// The object storage, the size limits and the virus-scan hook were already
// there for tasks; what was missing was the permission side. A task file is
// reached through its project, a chat file through membership of the channel -
// two different resolutions, which is why this was never a wider WHERE clause.

// POST /api/chat/messages/:id/attachments
func (a *API) UploadMessageAttachment(w http.ResponseWriter, r *http.Request) {
	messageID, ok := httpx.UUIDParam(w, r, "id")
	if !ok {
		return
	}
	// Membership of the channel, and it answers 404 rather than 403 for a
	// conversation the caller is not in - the error must not confirm it exists.
	message, _, ok := a.requireMessageAccess(w, r, messageID)
	if !ok {
		return
	}
	requester := auth.CurrentUser(r)
	// Only the author attaches to their own message. Anyone else adding a file
	// to somebody's words would be putting words in their mouth.
	if message.Author == nil || *message.Author != requester.ID {
		httpx.Error(w, http.StatusForbidden, "You can only attach files to your own messages.")
		return
	}
	if !a.requireObjectStore(w) {
		return
	}

	content, filename, contentType, ok := a.readUpload(w, r)
	if !ok {
		return
	}

	sum := sha256.Sum256(content)
	attachment := &repo.Attachment{
		ID: uuid.New(), MessageID: &messageID,
		Filename: filename, ContentType: contentType, SizeBytes: int64(len(content)),
		Checksum: hex.EncodeToString(sum[:]), UploadedBy: &requester.ID,
	}
	// Keyed by the message rather than a task, so the objects of one
	// conversation sit together in the bucket.
	attachment.StorageKey = "messages/" + messageID.String() + "/" + attachment.ID.String() +
		strings.ToLower(path.Ext(filename))

	ctx := r.Context()
	if err := a.Objects.Put(ctx, attachment.StorageKey, content, contentType); err != nil {
		logger.Error("chat attachment upload failed for %s: %v", messageID, err)
		httpx.Error(w, http.StatusBadGateway, "The file could not be stored. Try again in a moment.")
		return
	}

	status, scanErr := a.scanner().Scan(ctx, filename, content)
	if scanErr != nil {
		if cleanup := a.Objects.Delete(ctx, attachment.StorageKey); cleanup != nil {
			logger.Error("orphaned object %s after a failed scan: %v", attachment.StorageKey, cleanup)
		}
		httpx.Error(w, http.StatusServiceUnavailable,
			"The file could not be checked for viruses and was not stored. Try again in a moment.")
		return
	}
	now := time.Now()
	attachment.ScanStatus, attachment.ScannedAt = status, &now

	if err := a.Attachments.Create(ctx, attachment); err != nil {
		if cleanup := a.Objects.Delete(ctx, attachment.StorageKey); cleanup != nil {
			logger.Error("orphaned object %s after a failed insert: %v", attachment.StorageKey, cleanup)
		}
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	a.Audit.Record(r, requester, audit.Event{
		Action: audit.ActionAttachmentAdded, ResourceType: "attachment",
		ResourceID: attachment.ID.String(),
		Changes:    map[string]any{"filename": filename, "message": messageID.String()},
		Status:     http.StatusCreated,
	})

	httpx.JSON(w, http.StatusCreated, a.decorateAttachment(r, *attachment))
}

// readUpload pulls one file out of a multipart request, applying the same size,
// type and name rules as the task path.
//
// Shared rather than duplicated: two upload endpoints with two copies of the
// limits is how one of them ends up accepting a .exe six months from now.
func (a *API) readUpload(w http.ResponseWriter, r *http.Request) (content []byte, filename, contentType string, ok bool) {
	limit := a.Cfg.Attachments.MaxBytes
	r.Body = http.MaxBytesReader(w, r.Body, limit+(1<<10))
	if err := r.ParseMultipartForm(4 << 20); err != nil {
		if errors.As(err, new(*http.MaxBytesError)) {
			httpx.Error(w, http.StatusRequestEntityTooLarge, tooLargeMessage(limit))
			return nil, "", "", false
		}
		httpx.Error(w, http.StatusBadRequest, "Send the file as multipart/form-data with a \"file\" field.")
		return nil, "", "", false
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()

	file, header, err := r.FormFile("file")
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "No file was included in the upload.")
		return nil, "", "", false
	}
	defer file.Close()

	content, err = io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "The upload could not be read: "+err.Error())
		return nil, "", "", false
	}
	if int64(len(content)) > limit {
		httpx.Error(w, http.StatusRequestEntityTooLarge, tooLargeMessage(limit))
		return nil, "", "", false
	}
	if len(content) == 0 {
		httpx.Error(w, http.StatusBadRequest, "The file is empty.")
		return nil, "", "", false
	}

	filename = storage.CleanFilename(header.Filename)
	if storage.IsBlockedFilename(filename) {
		httpx.Error(w, http.StatusUnsupportedMediaType, fmt.Sprintf(
			"%s files cannot be attached, because they run when opened.",
			strings.ToUpper(strings.TrimPrefix(path.Ext(filename), "."))))
		return nil, "", "", false
	}

	contentType = detectContentType(header.Header.Get("Content-Type"), filename, content)
	if !storage.ContentTypeAllowed(contentType, a.Cfg.Attachments.AllowedTypes) {
		httpx.Error(w, http.StatusUnsupportedMediaType,
			"Files of type "+contentType+" cannot be attached here.")
		return nil, "", "", false
	}
	return content, filename, contentType, true
}

type editMessageRequest struct {
	Body string `json:"body"`
}

// PUT /api/chat/messages/:id
//
// Only the author, and the edit is stamped. The column has been waiting since
// the collaboration phase: a message that changes with no sign it changed is
// worse than one that cannot be changed at all, because a conversation people
// rely on stops being a record.
func (a *API) EditMessage(w http.ResponseWriter, r *http.Request) {
	messageID, ok := httpx.UUIDParam(w, r, "id")
	if !ok {
		return
	}
	message, channel, ok := a.requireMessageAccess(w, r, messageID)
	if !ok {
		return
	}

	requester := auth.CurrentUser(r)
	// Not even an administrator: rewriting what somebody else said is a
	// different power from moderating, and this application has no moderation
	// model to hang it on. Deleting is the honest alternative, and is separate.
	if message.Author == nil || *message.Author != requester.ID {
		httpx.Error(w, http.StatusForbidden, "You can only edit your own messages.")
		return
	}

	var req editMessageRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Body) == "" {
		httpx.Error(w, http.StatusBadRequest, "An edited message still needs a body.")
		return
	}

	updated, err := a.Chat.EditMessage(r.Context(), messageID, req.Body)
	if err != nil {
		respondRepoError(w, err, "Message not found.")
		return
	}

	// Pushed like any other change, so open tabs see the edit rather than the
	// version they happened to load.
	a.pushToChannel(channel, "chat:message", a.decorateMessage(r.Context(), *updated, requester))
	httpx.JSON(w, http.StatusOK, a.decorateMessage(r.Context(), *updated, requester))
}
