package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"

	"projectview/internal/audit"
	"projectview/internal/auth"
	"projectview/internal/httpx"
	"projectview/internal/logger"
	"projectview/internal/models"
	"projectview/internal/repo"
	"projectview/internal/services"
	"projectview/internal/storage"
)

// Attachments on tasks and comments.
//
// Two directions, deliberately asymmetric:
//
//   - Uploads pass through this process. That is what makes the size limit,
//     the type rules and the virus-scan hook enforceable; a presigned PUT
//     handed to the browser would let a client write whatever it liked,
//     whatever the form said.
//   - Downloads do not. The client gets a time-limited signed URL and fetches
//     the object from the store directly, so a hundred people opening a video
//     do not spend a hundred copies of it through the API's memory.
//
// Nothing in the bucket is readable without a signature, so "the link is
// private" is a property of the store rather than a promise this code makes.

// GET /api/attachments/config - what the client needs to validate a file
// before spending a minute uploading something the server will refuse.
func (a *API) AttachmentConfig(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, map[string]any{
		"enabled":      a.Objects.Enabled(),
		"maxBytes":     a.Cfg.Attachments.MaxBytes,
		"maxTaskBytes": a.Cfg.Attachments.MaxTaskBytes,
		"allowedTypes": a.Cfg.Attachments.AllowedTypes,
	})
}

// GET /api/tasks/:id/attachments
//
// Readable by any authenticated user, matching GET /api/tasks/:id: an
// attachment is part of the task, and a listing stricter than the thing it
// belongs to would be a boundary drawn in one place and not the other.
func (a *API) ListAttachments(w http.ResponseWriter, r *http.Request) {
	taskID, ok := httpx.UUIDParam(w, r, "id")
	if !ok {
		return
	}
	if _, err := a.Tasks.ByID(r.Context(), taskID); err != nil {
		respondRepoError(w, err, "Task not found.")
		return
	}

	items, err := a.Attachments.ForTask(r.Context(), taskID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, a.decorateAttachments(r, items))
}

// POST /api/tasks/:id/attachments
//
// multipart/form-data with a "file" part, and optionally a "commentId" field
// naming a comment on the same task.
func (a *API) UploadAttachment(w http.ResponseWriter, r *http.Request) {
	taskID, ok := httpx.UUIDParam(w, r, "id")
	if !ok {
		return
	}
	// Attaching a file is changing the task, so it takes the same permission as
	// editing one - not merely the permission to read it.
	task, _, ok := a.requireTaskWork(w, r, taskID)
	if !ok {
		return
	}
	if !a.requireObjectStore(w) {
		return
	}

	ctx := r.Context()
	requester := auth.CurrentUser(r)
	limit := a.Cfg.Attachments.MaxBytes

	// MaxBytesReader caps what can be read at all, so an oversized upload is
	// refused while it streams instead of after the whole thing has been
	// accepted into memory. The extra kilobyte covers the multipart framing,
	// which is not part of the file the person chose.
	r.Body = http.MaxBytesReader(w, r.Body, limit+(1<<10))
	if err := r.ParseMultipartForm(4 << 20); err != nil {
		if errors.As(err, new(*http.MaxBytesError)) {
			httpx.Error(w, http.StatusRequestEntityTooLarge, tooLargeMessage(limit))
			return
		}
		httpx.Error(w, http.StatusBadRequest, "Send the file as multipart/form-data with a \"file\" field.")
		return
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()

	file, header, err := r.FormFile("file")
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "No file was included in the upload.")
		return
	}
	defer file.Close()

	content, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "The upload could not be read: "+err.Error())
		return
	}
	if int64(len(content)) > limit {
		httpx.Error(w, http.StatusRequestEntityTooLarge, tooLargeMessage(limit))
		return
	}
	if len(content) == 0 {
		httpx.Error(w, http.StatusBadRequest, "The file is empty.")
		return
	}

	filename := storage.CleanFilename(header.Filename)
	if storage.IsBlockedFilename(filename) {
		httpx.Error(w, http.StatusUnsupportedMediaType, fmt.Sprintf(
			"%s files cannot be attached, because they run when opened.", strings.ToUpper(strings.TrimPrefix(path.Ext(filename), "."))))
		return
	}

	contentType := detectContentType(header.Header.Get("Content-Type"), filename, content)
	if !storage.ContentTypeAllowed(contentType, a.Cfg.Attachments.AllowedTypes) {
		httpx.Error(w, http.StatusUnsupportedMediaType,
			"Files of type "+contentType+" cannot be attached here.")
		return
	}

	// A per-task ceiling as well as a per-file one: fifty files just under the
	// single-file limit is the same problem arriving more slowly.
	used, err := a.Attachments.UsageBytes(ctx, taskID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if taskLimit := a.Cfg.Attachments.MaxTaskBytes; taskLimit > 0 && used+int64(len(content)) > taskLimit {
		httpx.Error(w, http.StatusRequestEntityTooLarge, fmt.Sprintf(
			"This task has reached its %s attachment limit. Remove a file before adding another.",
			humanBytes(taskLimit)))
		return
	}

	var commentID *uuid.UUID
	if raw := r.FormValue("commentId"); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, "Invalid commentId.")
			return
		}
		// Checked against this task rather than trusted: a comment id from
		// another task would otherwise hang a file off a conversation the
		// caller may have no permission on.
		if !a.commentBelongsToTask(ctx, parsed, taskID) {
			httpx.Error(w, http.StatusBadRequest, "That comment does not belong to this task.")
			return
		}
		commentID = &parsed
	}

	sum := sha256.Sum256(content)
	attachment := &repo.Attachment{
		ID:          uuid.New(),
		TaskID:      &taskID,
		CommentID:   commentID,
		Filename:    filename,
		ContentType: contentType,
		SizeBytes:   int64(len(content)),
		Checksum:    hex.EncodeToString(sum[:]),
		ScanStatus:  repo.ScanPending,
		UploadedBy:  &requester.ID,
	}
	attachment.StorageKey = storage.ObjectKey(taskID, attachment.ID, filename)

	// The object first, the row second. The other order would publish a row
	// pointing at nothing if the store refused - a download that fails for a
	// file the interface says is there. This way a failure between the two
	// leaves an object nobody references, which costs storage and is visible
	// to an operator rather than to a user.
	if err := a.Objects.Put(ctx, attachment.StorageKey, content, contentType); err != nil {
		logger.Error("attachment upload failed for task %s: %v", taskID, err)
		httpx.Error(w, http.StatusBadGateway, "The file could not be stored. Try again in a moment.")
		return
	}

	// Fails closed, and fails the whole upload rather than storing the file as
	// unexamined. Keeping the row in a "pending" state would be worse than it
	// sounds: the scan runs here and nowhere else, so nothing would ever
	// revisit it, and the person would be left with an attachment that is
	// visible in the list and permanently refuses to download. Refusing now
	// gives them something they can act on - try again.
	status, scanErr := a.scanner().Scan(ctx, filename, content)
	if scanErr != nil {
		logger.Error("attachment scan failed for task %s, refusing the upload: %v", taskID, scanErr)
		if cleanup := a.Objects.Delete(ctx, attachment.StorageKey); cleanup != nil {
			logger.Error("orphaned object %s after a failed scan: %v", attachment.StorageKey, cleanup)
		}
		httpx.Error(w, http.StatusServiceUnavailable,
			"The file could not be checked for viruses and was not stored. Try again in a moment.")
		return
	}
	now := time.Now()
	attachment.ScanStatus = status
	attachment.ScannedAt = &now

	if err := a.Attachments.Create(ctx, attachment); err != nil {
		// The row never landed, so nothing will ever reference the object and
		// the trigger cannot queue it. Removing it here is the one case the
		// sweeper cannot cover.
		if cleanup := a.Objects.Delete(ctx, attachment.StorageKey); cleanup != nil {
			logger.Error("orphaned object %s after a failed insert: %v", attachment.StorageKey, cleanup)
		}
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	if status == repo.ScanInfected {
		a.Audit.Record(r, requester, audit.Event{
			Action: audit.ActionAttachmentInfected, ResourceType: "attachment",
			ResourceID: attachment.ID.String(),
			Changes:    map[string]any{"filename": filename, "task": taskID.String()},
			Status:     http.StatusCreated,
		})
	}

	a.Audit.Record(r, requester, audit.Event{
		Action: audit.ActionAttachmentAdded, ResourceType: "attachment",
		ResourceID: attachment.ID.String(),
		Changes: map[string]any{
			"filename": filename, "size": attachment.SizeBytes,
			"contentType": contentType, "task": taskID.String(),
		},
		Status: http.StatusCreated,
	})

	a.notifyAttachment(ctx, task, requester, filename)

	httpx.JSON(w, http.StatusCreated, a.decorateAttachment(r, *attachment))
}

// GET /api/attachments/:id - redirects to a signed URL.
//
// A redirect rather than a JSON body so an <img src> or an ordinary link
// works without any client code: the browser follows it, fetches the object
// from the store and never learns the storage key.
func (a *API) DownloadAttachment(w http.ResponseWriter, r *http.Request) {
	attachment, ok := a.requireAttachmentRead(w, r)
	if !ok {
		return
	}
	url, _, ok := a.signAttachment(w, *attachment)
	if !ok {
		return
	}
	// Explicitly uncacheable: the URL inside expires, and a proxy holding on
	// to this redirect would hand out a dead link long after it stopped
	// working.
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, url, http.StatusFound)
}

// GET /api/attachments/:id/url - the same link as JSON, for a client that
// wants to know when it expires.
func (a *API) AttachmentURL(w http.ResponseWriter, r *http.Request) {
	attachment, ok := a.requireAttachmentRead(w, r)
	if !ok {
		return
	}
	url, expiresAt, ok := a.signAttachment(w, *attachment)
	if !ok {
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"url":       url,
		"expiresAt": expiresAt,
		"filename":  attachment.Filename,
	})
}

// DELETE /api/attachments/:id
//
// The uploader, or anybody who may manage the project. Deliberately narrower
// than the permission to attach: adding your own file and removing someone
// else's are not the same act, and a member who can edit a task should not be
// able to quietly drop the evidence attached to it.
func (a *API) DeleteAttachment(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.UUIDParam(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()
	attachment, err := a.Attachments.ByID(ctx, id)
	if err != nil {
		respondRepoError(w, err, "Attachment not found.")
		return
	}

	requester := auth.CurrentUser(r)
	uploadedByCaller := attachment.UploadedBy != nil && *attachment.UploadedBy == requester.ID

	// A chat file resolves through membership of its channel, a task file
	// through its project. Two different questions, which is why this is a
	// branch rather than one widened check.
	if attachment.MessageID != nil {
		if _, _, ok := a.requireMessageAccess(w, r, *attachment.MessageID); !ok {
			return
		}
		// In a conversation the author is the only one who may withdraw what
		// they said; there is no "manage the channel" role to fall back on.
		if !uploadedByCaller && !isAdmin(requester) {
			httpx.Error(w, http.StatusForbidden, forbiddenMessage)
			return
		}
	} else {
		task, err := a.Tasks.ByID(ctx, *attachment.TaskID)
		if err != nil {
			respondRepoError(w, err, "Attachment not found.")
			return
		}
		if !uploadedByCaller {
			if _, ok := a.requireProjectManage(w, r, task.ProjectID); !ok {
				return
			}
		} else if _, _, ok := a.requireTaskWork(w, r, *attachment.TaskID); !ok {
			return
		}
	}

	// Removes the row only. The trigger queues the object and the sweeper
	// removes it, which is what makes a cascade behave the same way as this
	// handler.
	if err := a.Attachments.Delete(ctx, id); err != nil {
		respondRepoError(w, err, "Attachment not found.")
		return
	}

	a.Audit.Record(r, requester, audit.Event{
		Action: audit.ActionAttachmentDeleted, ResourceType: "attachment",
		ResourceID: id.String(),
		Changes:    map[string]any{"filename": attachment.Filename},
		Status:     http.StatusOK,
	})

	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---------------------------------------------------------------------------
// Shared plumbing
// ---------------------------------------------------------------------------

func (a *API) scanner() services.Scanner {
	if a.Scanner != nil {
		return a.Scanner
	}
	return services.SkipScanner{}
}

func (a *API) requireObjectStore(w http.ResponseWriter) bool {
	if a.Objects.Enabled() {
		return true
	}
	// 503 rather than 500: nothing is broken, the deployment simply has no
	// object store, and the message says so instead of asking the user to
	// retry something that cannot start working on its own.
	httpx.Error(w, http.StatusServiceUnavailable,
		"File attachments are not available: this installation has no object storage configured.")
	return false
}

// requireAttachmentRead loads an attachment the caller may see. Reads follow
// the task, which any authenticated user may read.
func (a *API) requireAttachmentRead(w http.ResponseWriter, r *http.Request) (*repo.Attachment, bool) {
	id, ok := httpx.UUIDParam(w, r, "id")
	if !ok {
		return nil, false
	}
	attachment, err := a.Attachments.ByID(r.Context(), id)
	if err != nil {
		respondRepoError(w, err, "Attachment not found.")
		return nil, false
	}

	// A task attachment is as readable as its task, which any authenticated
	// user may read. A chat one is not: the conversation is private, so the
	// same check the transcript uses applies here - and it answers 404 rather
	// than 403, so the error does not confirm the file exists.
	if attachment.MessageID != nil {
		if _, _, ok := a.requireMessageAccess(w, r, *attachment.MessageID); !ok {
			return nil, false
		}
	}
	return attachment, true
}

// signAttachment issues the time-limited URL, refusing anything the scanner has
// not cleared.
func (a *API) signAttachment(w http.ResponseWriter, attachment repo.Attachment) (string, time.Time, bool) {
	if !a.requireObjectStore(w) {
		return "", time.Time{}, false
	}

	switch attachment.ScanStatus {
	case repo.ScanInfected:
		// Kept, not deleted. The row and the object are what an incident
		// response needs; what must not happen is somebody downloading it.
		httpx.Error(w, http.StatusForbidden,
			"This file was flagged by the virus scan and cannot be downloaded.")
		return "", time.Time{}, false
	case repo.ScanPending:
		// The upload path cannot produce this state - a scan that fails
		// refuses the upload outright - but the column can hold it, and an
		// asynchronous scanner would. The guard is what makes that a safe
		// change to make later: adding one must not silently start serving
		// files before they have been looked at.
		httpx.Error(w, http.StatusConflict,
			"This file has not finished being checked. Try again shortly.")
		return "", time.Time{}, false
	}

	ttl := time.Duration(a.Cfg.Storage.URLTTLMinutes) * time.Minute
	url, expiresAt, err := a.Objects.PresignGet(
		attachment.StorageKey, attachment.Filename, attachment.ContentType, ttl)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return "", time.Time{}, false
	}
	return url, expiresAt, true
}

func (a *API) commentBelongsToTask(ctx context.Context, commentID, taskID uuid.UUID) bool {
	owner, err := a.Tasks.CommentTask(ctx, commentID)
	return err == nil && owner == taskID
}

type attachmentResponse struct {
	repo.Attachment
	UploadedBy *PublicUser `json:"uploadedBy,omitempty"`
	// DownloadURL is this API's redirect, not the signed object URL. Handing
	// the signed one out in a listing would mint a live capability for every
	// file on the page, most of which nobody opens - and each would keep
	// working after the person's session ended.
	DownloadURL string `json:"downloadUrl"`
	// Inline tells the client it can render this in place rather than offer it
	// as a download, so the decision matches the Content-Disposition the store
	// will actually send.
	Inline bool `json:"inline"`
}

func (a *API) decorateAttachments(r *http.Request, items []repo.Attachment) []attachmentResponse {
	ids := []uuid.UUID{}
	for _, item := range items {
		ids = append(ids, derefIDs(item.UploadedBy)...)
	}
	users := a.usersByID(r.Context(), uniqueIDs(ids))

	out := make([]attachmentResponse, 0, len(items))
	for _, item := range items {
		response := attachmentResponse{
			Attachment:  item,
			DownloadURL: "/api/attachments/" + item.ID.String(),
			Inline:      storage.IsInlineRenderable(item.Filename),
		}
		if item.UploadedBy != nil {
			if user, ok := users[*item.UploadedBy]; ok {
				response.UploadedBy = &user
			}
		}
		out = append(out, response)
	}
	return out
}

func (a *API) decorateAttachment(r *http.Request, item repo.Attachment) attachmentResponse {
	return a.decorateAttachments(r, []repo.Attachment{item})[0]
}

// notifyAttachment tells the people responsible for the task that something
// arrived on it - the assignees, which is the same audience the comment path
// notifies. In-app only: a file appearing on a task somebody is already
// working on does not warrant an e-mail.
func (a *API) notifyAttachment(ctx context.Context, task *models.Task, actor *models.User, filename string) {
	notifier := a.notifier()
	for _, userID := range task.Assignees {
		if userID == actor.ID {
			continue
		}
		taskID, projectID := task.ID, task.ProjectID
		_, _ = notifier.NotifyUser(ctx, services.NotifyInput{
			UserID:  userID,
			Type:    models.NotifGeneral,
			Title:   actor.Name + " attached a file to " + task.Title,
			Body:    filename,
			Task:    &taskID,
			Project: &projectID,
		})
	}
}

// detectContentType decides what the file actually is.
//
// The browser's claim is a starting point, not an answer: it is trivially
// forged, and for anything unusual browsers send application/octet-stream
// regardless of the contents. So the bytes are sniffed, and the extension is
// consulted for the formats sniffing cannot tell apart - every modern Office
// document is a ZIP archive as far as content detection is concerned.
func detectContentType(declared, filename string, content []byte) string {
	declared = strings.TrimSpace(strings.ToLower(declared))
	if idx := strings.Index(declared, ";"); idx >= 0 {
		declared = strings.TrimSpace(declared[:idx])
	}

	sniffed := http.DetectContentType(content)
	if idx := strings.Index(sniffed, ";"); idx >= 0 {
		sniffed = strings.TrimSpace(sniffed[:idx])
	}

	// Sniffing recognised something specific: trust it over both the client
	// and the extension, since it read the actual bytes.
	if !inconclusiveTypes[sniffed] {
		return sniffed
	}

	// The application's own table first, so the answer is the same on every
	// machine. mime.TypeByExtension is consulted after it, and only as a
	// bonus where the host happens to have a MIME database - the runtime
	// image does not.
	if byExtension := storage.ExtensionType(filename); byExtension != "" {
		return byExtension
	}
	if byExtension := mime.TypeByExtension(path.Ext(filename)); byExtension != "" {
		if idx := strings.Index(byExtension, ";"); idx >= 0 {
			byExtension = strings.TrimSpace(byExtension[:idx])
		}
		return byExtension
	}
	if declared != "" {
		return declared
	}
	return sniffed
}

// inconclusiveTypes are the answers from sniffing that describe a container
// rather than a format. ZIP belongs here with the two obvious ones: every
// modern Office document is a ZIP archive on the wire, so trusting the sniffed
// answer would file every .docx, .xlsx and .pptx as application/zip and make
// an allow-list naming those types impossible to write.
var inconclusiveTypes = map[string]bool{
	"application/octet-stream": true,
	"text/plain":               true,
	"application/zip":          true,
}

func tooLargeMessage(limit int64) string {
	return "That file is larger than the " + humanBytes(limit) + " limit for a single attachment."
}

// humanBytes formats a limit the way the message needs to read it. Binary
// units, because that is what the limit is expressed in.
func humanBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.3g GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.3g MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.3g kB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d bytes", n)
	}
}
