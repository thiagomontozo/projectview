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

// GET /api/goals?spaceId=
func (a *API) ListGoals(w http.ResponseWriter, r *http.Request) {
	var spaceID *uuid.UUID
	if raw := r.URL.Query().Get("spaceId"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, "Invalid spaceId.")
			return
		}
		// A goal inherits the visibility of its space, so listing inside one
		// takes the same permission as opening it.
		if _, ok := a.requireSpaceReadByID(w, r, id); !ok {
			return
		}
		spaceID = &id
	}

	goals, err := a.Goals.List(r.Context(), spaceID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, goals)
}

// GET /api/goals/:id
func (a *API) GetGoal(w http.ResponseWriter, r *http.Request) {
	goal, ok := a.loadGoal(w, r)
	if !ok {
		return
	}
	httpx.JSON(w, http.StatusOK, goal)
}

// loadGoal fetches a goal and enforces the permission of the space it belongs
// to. A goal outside every space is organisation-wide and readable by anyone
// signed in - it is a statement of intent, not a record of work.
func (a *API) loadGoal(w http.ResponseWriter, r *http.Request) (*repo.Goal, bool) {
	id, ok := httpx.UUIDParam(w, r, "id")
	if !ok {
		return nil, false
	}
	goal, err := a.Goals.ByID(r.Context(), id)
	if err != nil {
		respondRepoError(w, err, "Goal not found.")
		return nil, false
	}
	if goal.SpaceID != nil {
		if _, ok := a.requireSpaceReadByID(w, r, *goal.SpaceID); !ok {
			return nil, false
		}
	}
	return goal, true
}

type goalRequest struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	SpaceID     string     `json:"spaceId"`
	TeamID      string     `json:"teamId"`
	OwnerID     string     `json:"ownerId"`
	StartDate   *time.Time `json:"startDate"`
	DueDate     *time.Time `json:"dueDate"`
	Status      string     `json:"status"`
	Archived    *bool      `json:"archived"`
}

// POST /api/goals
func (a *API) CreateGoal(w http.ResponseWriter, r *http.Request) {
	var req goalRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.Name == "" {
		httpx.Error(w, http.StatusBadRequest, "Goal name is required.")
		return
	}
	if req.Status != "" && !repo.ValidGoalStatus(req.Status) {
		httpx.Error(w, http.StatusBadRequest, "Unknown goal status.")
		return
	}

	requester := auth.CurrentUser(r)
	goal := &repo.Goal{
		Name: req.Name, Description: req.Description,
		StartDate: req.StartDate, DueDate: req.DueDate, Status: req.Status,
	}
	if id, ok := httpx.OptionalUUID(req.SpaceID); ok && id != nil {
		// Setting a goal inside a space is a management act within it.
		if _, ok := a.requireSpaceRoleByID(w, r, *id, repo.SpaceRoleMember); !ok {
			return
		}
		goal.SpaceID = id
	} else if !canAdministerStructure(requester) {
		// An organisation-wide goal is not something any member declares.
		httpx.Error(w, http.StatusForbidden, forbiddenMessage)
		return
	}
	if id, ok := httpx.OptionalUUID(req.TeamID); ok {
		goal.TeamID = id
	}
	if id, ok := httpx.OptionalUUID(req.OwnerID); ok {
		goal.OwnerID = id
	}
	if goal.OwnerID == nil {
		goal.OwnerID = &requester.ID
	}

	if err := a.Goals.Create(r.Context(), goal, requester.ID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	a.Audit.Record(r, requester, audit.Event{
		Action: audit.ActionGoalCreated, ResourceType: "goal", ResourceID: goal.ID.String(),
		Changes: map[string]any{"name": goal.Name}, Status: http.StatusCreated,
	})
	httpx.JSON(w, http.StatusCreated, goal)
}

// PUT /api/goals/:id
func (a *API) UpdateGoal(w http.ResponseWriter, r *http.Request) {
	goal, ok := a.loadGoal(w, r)
	if !ok {
		return
	}
	if !a.mayManageGoal(w, r, goal) {
		return
	}

	var req goalRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.Status != "" && !repo.ValidGoalStatus(req.Status) {
		httpx.Error(w, http.StatusBadRequest, "Unknown goal status.")
		return
	}

	patch := repo.GoalPatch{Archived: req.Archived}
	if req.Name != "" {
		patch.Name = &req.Name
	}
	if req.Description != "" {
		patch.Description = &req.Description
	}
	if req.Status != "" {
		patch.Status = &req.Status
	}
	if id, ok := httpx.OptionalUUID(req.OwnerID); ok && id != nil {
		patch.OwnerID = id
	}
	if req.StartDate != nil {
		patch.StartDate = &req.StartDate
	}
	if req.DueDate != nil {
		patch.DueDate = &req.DueDate
	}

	if err := a.Goals.Update(r.Context(), goal.ID, patch); err != nil {
		respondRepoError(w, err, "Goal not found.")
		return
	}

	updated, err := a.Goals.ByID(r.Context(), goal.ID)
	if err != nil {
		respondRepoError(w, err, "Goal not found.")
		return
	}
	a.Audit.Record(r, auth.CurrentUser(r), audit.Event{
		Action: audit.ActionGoalUpdated, ResourceType: "goal", ResourceID: goal.ID.String(),
		Changes: map[string]any{"status": updated.Status}, Status: http.StatusOK,
	})
	httpx.JSON(w, http.StatusOK, updated)
}

// DELETE /api/goals/:id
func (a *API) DeleteGoal(w http.ResponseWriter, r *http.Request) {
	goal, ok := a.loadGoal(w, r)
	if !ok {
		return
	}
	if !a.mayManageGoal(w, r, goal) {
		return
	}
	if err := a.Goals.Delete(r.Context(), goal.ID); err != nil {
		respondRepoError(w, err, "Goal not found.")
		return
	}
	a.Audit.Record(r, auth.CurrentUser(r), audit.Event{
		Action: audit.ActionGoalDeleted, ResourceType: "goal", ResourceID: goal.ID.String(),
		Status: http.StatusOK,
	})
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// mayManageGoal allows the goal's owner, an administrator, or anyone with a
// management grant on the space it lives in. The owner is included because a
// goal without someone who can edit it is a goal nobody maintains.
func (a *API) mayManageGoal(w http.ResponseWriter, r *http.Request, goal *repo.Goal) bool {
	requester := auth.CurrentUser(r)
	if isAdmin(requester) || (goal.OwnerID != nil && *goal.OwnerID == requester.ID) {
		return true
	}
	if goal.SpaceID != nil {
		_, ok := a.requireSpaceRoleByID(w, r, *goal.SpaceID, repo.SpaceRoleAdmin)
		return ok
	}
	httpx.Error(w, http.StatusForbidden, forbiddenMessage)
	return false
}

type keyResultRequest struct {
	Name         string   `json:"name"`
	Source       string   `json:"source"`
	Unit         string   `json:"unit"`
	StartValue   float64  `json:"startValue"`
	TargetValue  float64  `json:"targetValue"`
	CurrentValue float64  `json:"currentValue"`
	ProjectID    string   `json:"projectId"`
	Value        *float64 `json:"value"`
}

// POST /api/goals/:id/key-results
func (a *API) AddKeyResult(w http.ResponseWriter, r *http.Request) {
	goal, ok := a.loadGoal(w, r)
	if !ok {
		return
	}
	if !a.mayManageGoal(w, r, goal) {
		return
	}

	var req keyResultRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.Name == "" {
		httpx.Error(w, http.StatusBadRequest, "Key result name is required.")
		return
	}
	if req.Source == "" {
		req.Source = repo.SourceManual
	}
	if !repo.ValidKeyResultSource(req.Source) {
		httpx.Error(w, http.StatusBadRequest, "Unknown key result source.")
		return
	}

	kr := &repo.KeyResult{
		GoalID: goal.ID, Name: req.Name, Source: req.Source, Unit: req.Unit,
		StartValue: req.StartValue, TargetValue: req.TargetValue, CurrentValue: req.CurrentValue,
	}
	if id, ok := httpx.OptionalUUID(req.ProjectID); ok {
		kr.ProjectID = id
	}
	if req.Source != repo.SourceManual && kr.ProjectID == nil {
		httpx.Error(w, http.StatusBadRequest, "A measure taken from tasks needs a project to read them from.")
		return
	}
	if kr.ProjectID != nil {
		// Reading a project's completion through a goal must not become a way
		// to learn about a project the caller cannot open.
		if _, ok := a.requireProjectWork(w, r, *kr.ProjectID); !ok {
			return
		}
	}

	if err := a.Goals.AddKeyResult(r.Context(), kr); err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	updated, err := a.Goals.ByID(r.Context(), goal.ID)
	if err != nil {
		respondRepoError(w, err, "Goal not found.")
		return
	}
	httpx.JSON(w, http.StatusCreated, updated)
}

// PUT /api/goals/:id/key-results/:keyResultId - records a manual reading.
func (a *API) SetKeyResultValue(w http.ResponseWriter, r *http.Request) {
	goal, ok := a.loadGoal(w, r)
	if !ok {
		return
	}
	if !a.mayManageGoal(w, r, goal) {
		return
	}
	keyResultID, ok := httpx.UUIDParam(w, r, "keyResultId")
	if !ok {
		return
	}

	var req keyResultRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.Value == nil {
		httpx.Error(w, http.StatusBadRequest, "A value is required.")
		return
	}

	if err := a.Goals.SetKeyResultValue(r.Context(), keyResultID, *req.Value); err != nil {
		// The repository refuses derived measures, which is the same shape as
		// "no such key result" from here. Both mean this value is not yours to
		// set.
		httpx.Error(w, http.StatusBadRequest, "This measure is computed from the tasks and cannot be set by hand.")
		return
	}

	updated, err := a.Goals.ByID(r.Context(), goal.ID)
	if err != nil {
		respondRepoError(w, err, "Goal not found.")
		return
	}
	httpx.JSON(w, http.StatusOK, updated)
}

// DELETE /api/goals/:id/key-results/:keyResultId
func (a *API) DeleteKeyResult(w http.ResponseWriter, r *http.Request) {
	goal, ok := a.loadGoal(w, r)
	if !ok {
		return
	}
	if !a.mayManageGoal(w, r, goal) {
		return
	}
	keyResultID, ok := httpx.UUIDParam(w, r, "keyResultId")
	if !ok {
		return
	}
	if err := a.Goals.DeleteKeyResult(r.Context(), keyResultID); err != nil {
		respondRepoError(w, err, "Key result not found.")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}
