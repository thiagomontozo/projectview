package handlers

import (
	"net/http"
	"time"

	"github.com/google/uuid"

	"projectview/internal/audit"
	"projectview/internal/auth"
	"projectview/internal/httpx"
	"projectview/internal/repo"
)

// GET /api/portfolio?spaceId=
//
// Cross-project reporting is a management view: it names every project and its
// health, which is precisely the summary an ordinary member has no standing
// to read for projects they are not in.
func (a *API) PortfolioSummary(w http.ResponseWriter, r *http.Request) {
	if !canAdministerStructure(auth.CurrentUser(r)) {
		httpx.Error(w, http.StatusForbidden, forbiddenMessage)
		return
	}

	var spaceID *uuid.UUID
	if raw := r.URL.Query().Get("spaceId"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, "Invalid spaceId.")
			return
		}
		spaceID = &id
	}

	projects, err := a.Portfolio.Summary(r.Context(), spaceID, time.Now())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, projects)
}

// GET /api/portfolio/capacity?from=&to=
func (a *API) CapacityReport(w http.ResponseWriter, r *http.Request) {
	if !canAdministerStructure(auth.CurrentUser(r)) {
		httpx.Error(w, http.StatusForbidden, forbiddenMessage)
		return
	}

	from, to := parseWindow(r, 4*7*24*time.Hour)
	rows, err := a.Portfolio.Capacity(r.Context(), from, to)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"from": from, "to": to, "rows": rows})
}

// parseWindow reads from/to, defaulting to a window starting today. An
// unparseable date falls back to the default rather than failing: a report
// with a sensible range beats an error page.
func parseWindow(r *http.Request, span time.Duration) (time.Time, time.Time) {
	now := time.Now()
	from := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	to := from.Add(span)

	if raw := r.URL.Query().Get("from"); raw != "" {
		if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
			from = parsed
			to = from.Add(span)
		}
	}
	if raw := r.URL.Query().Get("to"); raw != "" {
		if parsed, err := time.Parse(time.RFC3339, raw); err == nil && parsed.After(from) {
			to = parsed
		}
	}
	return from, to
}

// GET /api/projects/:id/baselines
func (a *API) ListBaselines(w http.ResponseWriter, r *http.Request) {
	projectID, ok := httpx.UUIDParam(w, r, "id")
	if !ok {
		return
	}
	project, ok := a.requireProjectWork(w, r, projectID)
	if !ok {
		return
	}
	baselines, err := a.Baselines.List(r.Context(), project.ID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, baselines)
}

type baselineRequest struct {
	Name string `json:"name"`
}

// POST /api/projects/:id/baselines - freezes the plan as it stands.
func (a *API) CaptureBaseline(w http.ResponseWriter, r *http.Request) {
	projectID, ok := httpx.UUIDParam(w, r, "id")
	if !ok {
		return
	}
	project, ok := a.requireProjectManage(w, r, projectID)
	if !ok {
		return
	}

	var req baselineRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.Name == "" {
		req.Name = "Baseline " + time.Now().Format("2006-01-02")
	}

	requester := auth.CurrentUser(r)
	baseline, err := a.Baselines.Capture(r.Context(), project.ID, req.Name, requester.ID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	a.Audit.Record(r, requester, audit.Event{
		Action: audit.ActionBaselineCapture, ResourceType: "project", ResourceID: project.ID.String(),
		Changes: map[string]any{"baseline": baseline.Name, "tasks": len(baseline.Tasks)},
		Status:  http.StatusCreated,
	})
	httpx.JSON(w, http.StatusCreated, baseline)
}

// GET /api/projects/:id/earned-value?asOf=
//
// Answers "are we where the plan said we would be, and did it cost what the
// plan said it would" - the two questions a status meeting is actually about.
func (a *API) EarnedValue(w http.ResponseWriter, r *http.Request) {
	projectID, ok := httpx.UUIDParam(w, r, "id")
	if !ok {
		return
	}
	project, ok := a.requireProjectWork(w, r, projectID)
	if !ok {
		return
	}

	baseline, err := a.Baselines.Latest(r.Context(), project.ID)
	if err != nil {
		// Without an approved plan there is nothing to measure against, and
		// saying so is more useful than returning zeroes that look like data.
		respondRepoError(w, err, "This project has no baseline yet.")
		return
	}

	asOf := time.Now()
	if raw := r.URL.Query().Get("asOf"); raw != "" {
		if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
			asOf = parsed
		}
	}

	taskIDs := make([]uuid.UUID, 0, len(baseline.Tasks))
	for _, t := range baseline.Tasks {
		taskIDs = append(taskIDs, t.TaskID)
	}
	progress, err := a.Baselines.ProgressFor(r.Context(), taskIDs)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	result := repo.ComputeEarnedValue(baseline.Tasks, progress, asOf)
	httpx.JSON(w, http.StatusOK, map[string]any{
		"baseline": map[string]any{
			"id": baseline.ID, "name": baseline.Name,
			"capturedAt": baseline.CapturedAt, "tasks": len(baseline.Tasks),
		},
		"earnedValue": result,
	})
}

// DELETE /api/baselines/:id
func (a *API) DeleteBaseline(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.UUIDParam(w, r, "id")
	if !ok {
		return
	}
	if !canAdministerStructure(auth.CurrentUser(r)) {
		httpx.Error(w, http.StatusForbidden, forbiddenMessage)
		return
	}
	if err := a.Baselines.Delete(r.Context(), id); err != nil {
		respondRepoError(w, err, "Baseline not found.")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}
