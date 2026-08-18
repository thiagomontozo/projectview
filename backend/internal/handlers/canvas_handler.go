package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"projectview/internal/audit"
	"projectview/internal/auth"
	"projectview/internal/httpx"
	"projectview/internal/logger"
	"projectview/internal/repo"
	"projectview/internal/storage"
	"projectview/internal/ws"
)

// Whiteboards, spreadsheets, clips and saved views.
//
// All four are project documents, so all four ask the same question before
// anything else: may this person work in this project? Reading takes the same
// permission as writing here, deliberately - a whiteboard is a working surface
// rather than a published artefact, and a read-only viewer of a board being
// dragged around by somebody else is a screen nobody asked for.

/* --- Saved views ------------------------------------------------------------ */

// GET /api/projects/:projectId/views
func (a *API) ListSavedViews(w http.ResponseWriter, r *http.Request) {
	projectID, ok := httpx.UUIDParam(w, r, "projectId")
	if !ok {
		return
	}
	if _, ok := a.requireProjectWork(w, r, projectID); !ok {
		return
	}
	views, err := a.SavedViews.ForProject(r.Context(), projectID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, views)
}

// POST /api/projects/:projectId/views
func (a *API) CreateSavedView(w http.ResponseWriter, r *http.Request) {
	projectID, ok := httpx.UUIDParam(w, r, "projectId")
	if !ok {
		return
	}
	if _, ok := a.requireProjectWork(w, r, projectID); !ok {
		return
	}

	var body struct {
		Name          string         `json:"name"`
		Kind          string         `json:"kind"`
		GroupBy       string         `json:"groupBy"`
		Filters       map[string]any `json:"filters"`
		SortBy        string         `json:"sortBy"`
		SortDirection string         `json:"sortDirection"`
	}
	if !httpx.DecodeJSON(w, r, &body) {
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" {
		httpx.Error(w, http.StatusBadRequest, "A view needs a name.")
		return
	}
	if !validViewKind(body.Kind) {
		httpx.Error(w, http.StatusBadRequest, "That is not a view this application draws.")
		return
	}
	if body.GroupBy == "" {
		body.GroupBy = "status"
	}

	requester := auth.CurrentUser(r)
	view := repo.SavedView{
		ProjectID: projectID, Name: body.Name, Kind: body.Kind,
		GroupBy: body.GroupBy, Filters: body.Filters, SortBy: body.SortBy,
		SortDirection: body.SortDirection, CreatedBy: &requester.ID,
	}
	if err := a.SavedViews.Create(r.Context(), &view); err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusCreated, view)
}

// DELETE /api/views/:id
func (a *API) DeleteSavedView(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.UUIDParam(w, r, "id")
	if !ok {
		return
	}
	view, err := a.SavedViews.ByID(r.Context(), id)
	if err != nil {
		respondRepoError(w, err, "View not found.")
		return
	}
	if _, ok := a.requireProjectWork(w, r, view.ProjectID); !ok {
		return
	}
	if err := a.SavedViews.Delete(r.Context(), id); err != nil {
		respondRepoError(w, err, "View not found.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// The views the interface can actually draw. An allow-list rather than free
// text, because a saved view naming a kind nothing renders is a tab that leads
// to an empty screen.
func validViewKind(kind string) bool {
	switch kind {
	case "board", "list", "table", "calendar", "timeline", "workload":
		return true
	}
	return false
}

/* --- Whiteboards ------------------------------------------------------------ */

// GET /api/projects/:projectId/whiteboards
func (a *API) ListWhiteboards(w http.ResponseWriter, r *http.Request) {
	projectID, ok := httpx.UUIDParam(w, r, "projectId")
	if !ok {
		return
	}
	if _, ok := a.requireProjectWork(w, r, projectID); !ok {
		return
	}
	boards, err := a.Whiteboards.ForProject(r.Context(), projectID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, boards)
}

// POST /api/projects/:projectId/whiteboards
func (a *API) CreateWhiteboard(w http.ResponseWriter, r *http.Request) {
	projectID, ok := httpx.UUIDParam(w, r, "projectId")
	if !ok {
		return
	}
	if _, ok := a.requireProjectWork(w, r, projectID); !ok {
		return
	}

	var body struct {
		Title string `json:"title"`
	}
	if !httpx.DecodeJSON(w, r, &body) {
		return
	}
	title := strings.TrimSpace(body.Title)
	if title == "" {
		title = "Untitled board"
	}

	requester := auth.CurrentUser(r)
	board := repo.Whiteboard{ProjectID: projectID, Title: title, CreatedBy: &requester.ID}
	if err := a.Whiteboards.Create(r.Context(), &board); err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.Audit.Record(r, requester, audit.Event{
		Action: audit.ActionWhiteboardCreated, ResourceType: "whiteboard",
		ResourceID: board.ID.String(), Changes: map[string]any{"title": title},
		Status: http.StatusCreated,
	})
	httpx.JSON(w, http.StatusCreated, board)
}

// GET /api/whiteboards/:id
func (a *API) GetWhiteboard(w http.ResponseWriter, r *http.Request) {
	board, ok := a.requireWhiteboard(w, r)
	if !ok {
		return
	}
	httpx.JSON(w, http.StatusOK, board)
}

// PUT /api/whiteboards/:id
//
// The version travels with the scene and a stale one is refused. Two people on
// one board is the ordinary case; without this the second save would delete the
// first person's work with nothing on screen to say it happened.
func (a *API) SaveWhiteboard(w http.ResponseWriter, r *http.Request) {
	board, ok := a.requireWhiteboard(w, r)
	if !ok {
		return
	}

	var body struct {
		Title   string         `json:"title"`
		Scene   map[string]any `json:"scene"`
		Version int            `json:"version"`
	}
	if !httpx.DecodeJSON(w, r, &body) {
		return
	}
	if body.Scene == nil {
		httpx.Error(w, http.StatusBadRequest, "The board has no scene to save.")
		return
	}
	if err := boundScene(body.Scene); err != nil {
		httpx.Error(w, http.StatusRequestEntityTooLarge, err.Error())
		return
	}

	saved, err := a.Whiteboards.Save(r.Context(), board.ID, body.Scene, strings.TrimSpace(body.Title), body.Version)
	if errors.Is(err, repo.ErrStale) {
		// 409 with the current document attached, so the interface can say what
		// happened and show the newer board rather than only refusing.
		current, _ := a.Whiteboards.ByID(r.Context(), board.ID)
		httpx.JSON(w, http.StatusConflict, map[string]any{
			"message": "Somebody else saved this board while you were editing it.",
			"current": current,
		})
		return
	}
	if err != nil {
		respondRepoError(w, err, "Board not found.")
		return
	}

	// Everybody else with it open is told, so a board two people are drawing on
	// stays one board rather than two divergent copies that only disagree at
	// the next save.
	a.broadcastToProject(r, saved.ProjectID, "whiteboard:saved", map[string]any{
		"id": saved.ID, "version": saved.Version, "projectId": saved.ProjectID,
	})
	httpx.JSON(w, http.StatusOK, saved)
}

// DELETE /api/whiteboards/:id
func (a *API) DeleteWhiteboard(w http.ResponseWriter, r *http.Request) {
	board, ok := a.requireWhiteboard(w, r)
	if !ok {
		return
	}
	if err := a.Whiteboards.Delete(r.Context(), board.ID); err != nil {
		respondRepoError(w, err, "Board not found.")
		return
	}
	a.Audit.Record(r, auth.CurrentUser(r), audit.Event{
		Action: audit.ActionWhiteboardDeleted, ResourceType: "whiteboard",
		ResourceID: board.ID.String(), Status: http.StatusNoContent,
	})
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) requireWhiteboard(w http.ResponseWriter, r *http.Request) (*repo.Whiteboard, bool) {
	id, ok := httpx.UUIDParam(w, r, "id")
	if !ok {
		return nil, false
	}
	board, err := a.Whiteboards.ByID(r.Context(), id)
	if err != nil {
		respondRepoError(w, err, "Board not found.")
		return nil, false
	}
	if _, ok := a.requireProjectWork(w, r, board.ProjectID); !ok {
		return nil, false
	}
	return board, true
}

/* --- Spreadsheets ----------------------------------------------------------- */

// GET /api/projects/:projectId/sheets
func (a *API) ListSheets(w http.ResponseWriter, r *http.Request) {
	projectID, ok := httpx.UUIDParam(w, r, "projectId")
	if !ok {
		return
	}
	if _, ok := a.requireProjectWork(w, r, projectID); !ok {
		return
	}
	sheets, err := a.Sheets.ForProject(r.Context(), projectID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, sheets)
}

// POST /api/projects/:projectId/sheets
func (a *API) CreateSheet(w http.ResponseWriter, r *http.Request) {
	projectID, ok := httpx.UUIDParam(w, r, "projectId")
	if !ok {
		return
	}
	if _, ok := a.requireProjectWork(w, r, projectID); !ok {
		return
	}

	var body struct {
		Title string `json:"title"`
	}
	if !httpx.DecodeJSON(w, r, &body) {
		return
	}
	title := strings.TrimSpace(body.Title)
	if title == "" {
		title = "Untitled sheet"
	}

	requester := auth.CurrentUser(r)
	sheet := repo.Spreadsheet{ProjectID: projectID, Title: title, CreatedBy: &requester.ID}
	if err := a.Sheets.Create(r.Context(), &sheet); err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusCreated, sheet)
}

// GET /api/sheets/:id
func (a *API) GetSheet(w http.ResponseWriter, r *http.Request) {
	sheet, ok := a.requireSheet(w, r)
	if !ok {
		return
	}
	httpx.JSON(w, http.StatusOK, sheet)
}

// PUT /api/sheets/:id
func (a *API) SaveSheet(w http.ResponseWriter, r *http.Request) {
	sheet, ok := a.requireSheet(w, r)
	if !ok {
		return
	}

	var body struct {
		Title   string         `json:"title"`
		Cells   map[string]any `json:"cells"`
		Rows    int            `json:"rows"`
		Cols    int            `json:"cols"`
		Version int            `json:"version"`
	}
	if !httpx.DecodeJSON(w, r, &body) {
		return
	}
	if body.Cells == nil {
		httpx.Error(w, http.StatusBadRequest, "The sheet has no cells to save.")
		return
	}
	// Cells are sparse, so this is a count of what somebody actually typed
	// rather than of the grid they can see.
	if len(body.Cells) > 20000 {
		httpx.Error(w, http.StatusRequestEntityTooLarge,
			"That sheet is larger than this application will store. Split it across two.")
		return
	}

	saved, err := a.Sheets.Save(r.Context(), sheet.ID, body.Cells,
		strings.TrimSpace(body.Title), body.Rows, body.Cols, body.Version)
	if errors.Is(err, repo.ErrStale) {
		current, _ := a.Sheets.ByID(r.Context(), sheet.ID)
		httpx.JSON(w, http.StatusConflict, map[string]any{
			"message": "Somebody else saved this sheet while you were editing it.",
			"current": current,
		})
		return
	}
	if err != nil {
		respondRepoError(w, err, "Sheet not found.")
		return
	}

	a.broadcastToProject(r, saved.ProjectID, "sheet:saved", map[string]any{
		"id": saved.ID, "version": saved.Version, "projectId": saved.ProjectID,
	})
	httpx.JSON(w, http.StatusOK, saved)
}

// DELETE /api/sheets/:id
func (a *API) DeleteSheet(w http.ResponseWriter, r *http.Request) {
	sheet, ok := a.requireSheet(w, r)
	if !ok {
		return
	}
	if err := a.Sheets.Delete(r.Context(), sheet.ID); err != nil {
		respondRepoError(w, err, "Sheet not found.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) requireSheet(w http.ResponseWriter, r *http.Request) (*repo.Spreadsheet, bool) {
	id, ok := httpx.UUIDParam(w, r, "id")
	if !ok {
		return nil, false
	}
	sheet, err := a.Sheets.ByID(r.Context(), id)
	if err != nil {
		respondRepoError(w, err, "Sheet not found.")
		return nil, false
	}
	if _, ok := a.requireProjectWork(w, r, sheet.ProjectID); !ok {
		return nil, false
	}
	return sheet, true
}

/* --- Clips ------------------------------------------------------------------ */

// GET /api/projects/:projectId/clips
func (a *API) ListClips(w http.ResponseWriter, r *http.Request) {
	projectID, ok := httpx.UUIDParam(w, r, "projectId")
	if !ok {
		return
	}
	if _, ok := a.requireProjectWork(w, r, projectID); !ok {
		return
	}
	clips, err := a.Clips.ForProject(r.Context(), projectID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, clips)
}

// POST /api/projects/:projectId/clips
//
// The bytes arrive as multipart from the browser's own recorder, and go to the
// same object storage as attachments. Nothing is transcoded: the browser
// produced a playable container and re-encoding it server-side would mean
// shipping ffmpeg to do what nobody asked for.
func (a *API) UploadClip(w http.ResponseWriter, r *http.Request) {
	projectID, ok := httpx.UUIDParam(w, r, "projectId")
	if !ok {
		return
	}
	if _, ok := a.requireProjectWork(w, r, projectID); !ok {
		return
	}
	if !a.requireObjectStore(w) {
		return
	}

	ctx := r.Context()
	requester := auth.CurrentUser(r)
	limit := a.Cfg.Attachments.MaxBytes

	r.Body = http.MaxBytesReader(w, r.Body, limit+(1<<10))
	if err := r.ParseMultipartForm(4 << 20); err != nil {
		if errors.As(err, new(*http.MaxBytesError)) {
			httpx.Error(w, http.StatusRequestEntityTooLarge, tooLargeMessage(limit))
			return
		}
		httpx.Error(w, http.StatusBadRequest, "Send the recording as multipart/form-data with a \"file\" field.")
		return
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()

	file, header, err := r.FormFile("file")
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "No recording was included.")
		return
	}
	defer file.Close()

	content, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "The recording could not be read: "+err.Error())
		return
	}
	if int64(len(content)) > limit {
		httpx.Error(w, http.StatusRequestEntityTooLarge, tooLargeMessage(limit))
		return
	}
	if len(content) == 0 {
		httpx.Error(w, http.StatusBadRequest, "The recording is empty.")
		return
	}

	// Only video. This endpoint exists for one thing, and a general file upload
	// that happens to live under /clips would be an attachment path with none of
	// the attachment path's limits.
	contentType := detectContentType(header.Header.Get("Content-Type"),
		storage.CleanFilename(header.Filename), content)
	if !strings.HasPrefix(contentType, "video/") {
		httpx.Error(w, http.StatusUnsupportedMediaType, "A clip has to be a video recording.")
		return
	}

	title := strings.TrimSpace(r.FormValue("title"))
	if title == "" {
		title = "Clip " + time.Now().Format("2006-01-02 15:04")
	}

	var taskID *uuid.UUID
	if raw := r.FormValue("taskId"); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, "That task id is not valid.")
			return
		}
		// Checked against the same project: a clip filed under a task in
		// somebody else's project would be reachable by the wrong people.
		task, err := a.Tasks.ByID(ctx, parsed)
		if err != nil || task.ProjectID != projectID {
			httpx.Error(w, http.StatusBadRequest, "That task is not in this project.")
			return
		}
		taskID = &parsed
	}

	var duration *int
	if raw := r.FormValue("durationMs"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			duration = &parsed
		}
	}

	clip := repo.Clip{
		ProjectID: projectID, TaskID: taskID, Title: title,
		ContentType: contentType, SizeBytes: int64(len(content)),
		DurationMS: duration, CreatedBy: &requester.ID,
	}
	clip.ID = uuid.New()
	clip.StorageKey = fmt.Sprintf("clips/%s/%s.webm", projectID, clip.ID)

	if err := a.Objects.Put(ctx, clip.StorageKey, content, contentType); err != nil {
		httpx.Error(w, http.StatusBadGateway, "The recording could not be stored: "+err.Error())
		return
	}
	if err := a.Clips.Create(ctx, &clip); err != nil {
		// The object is removed rather than left behind: a stored object with no
		// row is unreachable and unbilled to nobody, which is how object stores
		// quietly fill up.
		_ = a.Objects.Delete(ctx, clip.StorageKey)
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	a.Audit.Record(r, requester, audit.Event{
		Action: audit.ActionClipRecorded, ResourceType: "clip",
		ResourceID: clip.ID.String(),
		Changes:    map[string]any{"title": title, "bytes": clip.SizeBytes},
		Status:     http.StatusCreated,
	})
	httpx.JSON(w, http.StatusCreated, clip)
}

// GET /api/clips/:id/url
//
// A signed URL rather than proxying the bytes: a recording is tens of
// megabytes and streamed with range requests, which is exactly what an object
// store does well and what this process should not be doing at all.
func (a *API) ClipURL(w http.ResponseWriter, r *http.Request) {
	clip, ok := a.requireClip(w, r)
	if !ok {
		return
	}
	if !a.requireObjectStore(w) {
		return
	}

	ttl := time.Duration(a.Cfg.Storage.URLTTLMinutes) * time.Minute
	url, expires, err := a.Objects.PresignGet(clip.StorageKey, clip.Title+".webm",
		clip.ContentType, ttl)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"url": url, "expiresAt": expires})
}

// DELETE /api/clips/:id
func (a *API) DeleteClip(w http.ResponseWriter, r *http.Request) {
	clip, ok := a.requireClip(w, r)
	if !ok {
		return
	}
	// The row goes first. An object left behind is waste; a row pointing at an
	// object that is gone is a broken player somebody has to explain.
	if err := a.Clips.Delete(r.Context(), clip.ID); err != nil {
		respondRepoError(w, err, "Clip not found.")
		return
	}
	if a.Objects.Enabled() {
		if err := a.Objects.Delete(r.Context(), clip.StorageKey); err != nil {
			logger.Warn("clip object %s could not be removed: %v", clip.StorageKey, err)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) requireClip(w http.ResponseWriter, r *http.Request) (*repo.Clip, bool) {
	id, ok := httpx.UUIDParam(w, r, "id")
	if !ok {
		return nil, false
	}
	clip, err := a.Clips.ByID(r.Context(), id)
	if err != nil {
		respondRepoError(w, err, "Clip not found.")
		return nil, false
	}
	if _, ok := a.requireProjectWork(w, r, clip.ProjectID); !ok {
		return nil, false
	}
	return clip, true
}

/* --- Shared ----------------------------------------------------------------- */

// boundScene refuses a board that has grown past what is worth storing in one
// row. Generous on purpose - a real board of a few hundred notes is nowhere
// near this - but present, because a JSONB column with no ceiling is a way to
// put a megabyte into every read of a list.
func boundScene(scene map[string]any) error {
	encoded, err := json.Marshal(scene)
	if err != nil {
		return errors.New("the board could not be read")
	}
	const maxSceneBytes = 4 << 20
	if len(encoded) > maxSceneBytes {
		return errors.New("that board is larger than this application will store. Split it across two")
	}
	return nil
}

// broadcastToProject tells the project's members something changed.
//
// To members rather than to everybody: a board title is not public inside the
// installation, and a broadcast is the easiest place to leak the existence of
// work somebody cannot see.
func (a *API) broadcastToProject(r *http.Request, projectID uuid.UUID, event string, payload map[string]any) {
	if a.Hub == nil {
		return
	}
	project, err := a.Projects.ByID(r.Context(), projectID)
	if err != nil {
		return
	}
	ids := make([]string, 0, len(project.Members))
	for _, member := range project.Members {
		ids = append(ids, member.String())
	}
	a.Hub.SendToUsers(ids, ws.Message{Type: event, Payload: payload})
}
