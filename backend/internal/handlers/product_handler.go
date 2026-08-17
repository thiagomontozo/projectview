package handlers

import (
	"errors"
	"net/http"

	"github.com/google/uuid"

	"projectview/internal/audit"
	"projectview/internal/auth"
	"projectview/internal/httpx"
	"projectview/internal/models"
	"projectview/internal/repo"
)

// ---------------------------------------------------------------------------
// Dependencies
// ---------------------------------------------------------------------------

type dependencyRequest struct {
	DependsOn string `json:"dependsOn"`
	Type      string `json:"type"`
	LagDays   int    `json:"lagDays"`
}

// POST /api/tasks/:id/dependencies
func (a *API) AddDependency(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.UUIDParam(w, r, "id")
	if !ok {
		return
	}
	task, _, ok := a.requireTaskWork(w, r, id)
	if !ok {
		return
	}

	var req dependencyRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	dependsOn, err := uuid.Parse(req.DependsOn)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "Invalid dependsOn id.")
		return
	}
	if dependsOn == id {
		httpx.Error(w, http.StatusBadRequest, "A task cannot depend on itself.")
		return
	}

	// The blocker has to be reachable by the caller too, or a dependency
	// becomes a way to learn that a task exists elsewhere.
	blocker, err := a.Tasks.ByID(r.Context(), dependsOn)
	if err != nil {
		respondRepoError(w, err, "Blocking task not found.")
		return
	}
	if blocker.ProjectID != task.ProjectID {
		httpx.Error(w, http.StatusBadRequest, "Dependencies must stay within one project.")
		return
	}

	requester := auth.CurrentUser(r)
	if err := a.Dependencies.Add(r.Context(), id, dependsOn, req.Type, req.LagDays, requester.ID); err != nil {
		if errors.Is(err, repo.ErrDependencyCycle) {
			// A cycle makes the schedule unsolvable; refused, not stored.
			httpx.Error(w, http.StatusConflict, "That dependency would create a cycle.")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	a.Audit.Record(r, requester, audit.Event{
		Action: audit.ActionTaskUpdated, ResourceType: "task", ResourceID: id.String(),
		Changes: map[string]any{"dependsOn": dependsOn.String()}, Status: http.StatusCreated,
	})
	httpx.JSON(w, http.StatusCreated, map[string]bool{"ok": true})
}

// DELETE /api/tasks/:id/dependencies/:dependsOn
func (a *API) RemoveDependency(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.UUIDParam(w, r, "id")
	if !ok {
		return
	}
	dependsOn, ok := httpx.UUIDParam(w, r, "dependsOn")
	if !ok {
		return
	}
	if _, _, ok := a.requireTaskWork(w, r, id); !ok {
		return
	}
	if err := a.Dependencies.Remove(r.Context(), id, dependsOn); err != nil {
		respondRepoError(w, err, "Dependency not found.")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type scheduleResponse struct {
	Dependencies []repo.Dependency  `json:"dependencies"`
	CriticalPath []uuid.UUID        `json:"criticalPath"`
	Blocked      []repo.BlockedTask `json:"blocked"`
}

// GET /api/projects/:projectId/schedule
//
// Everything the timeline needs in one request: the edges to draw, the chain
// where a slip moves the end date, and what cannot be started today.
func (a *API) ProjectSchedule(w http.ResponseWriter, r *http.Request) {
	projectID, ok := httpx.UUIDParam(w, r, "projectId")
	if !ok {
		return
	}
	ctx := r.Context()

	// The timeline sends the ids of the bars it is drawing, and gets back only
	// the arrows between them. An edge with one end off-screen has nothing to
	// connect, so shipping it is pure weight - measured at 512 KB and the
	// slowest endpoint in the load test before this.
	//
	// No ids means the whole graph, which keeps every other caller working and
	// is what an export wants.
	visible := httpx.UUIDs(r.URL.Query()["taskId"])

	dependencies, err := a.Dependencies.ForProject(ctx, projectID, visible)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Deliberately NOT scoped. The critical path is a property of the whole
	// project - the chain where a slip moves the end date runs through tasks
	// that may be off-screen - and its result is a list of ids, so it is small
	// however large the graph is. Scoping it would compute a different answer
	// rather than a smaller one.
	criticalPath, err := a.Dependencies.CriticalPath(ctx, projectID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	blocked, err := a.Dependencies.Blocked(ctx, projectID, visible)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	httpx.JSON(w, http.StatusOK, scheduleResponse{
		Dependencies: dependencies,
		CriticalPath: criticalPath,
		Blocked:      blocked,
	})
}

// ---------------------------------------------------------------------------
// Custom fields
// ---------------------------------------------------------------------------

// GET /api/projects/:projectId/fields
func (a *API) ListCustomFields(w http.ResponseWriter, r *http.Request) {
	projectID, ok := httpx.UUIDParam(w, r, "projectId")
	if !ok {
		return
	}
	fields, err := a.CustomFields.ForProject(r.Context(), projectID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, fields)
}

type createFieldRequest struct {
	Key      string   `json:"key"`
	Label    string   `json:"label"`
	Type     string   `json:"type"`
	Options  []string `json:"options"`
	Required bool     `json:"required"`
	Position int      `json:"position"`
}

// POST /api/projects/:projectId/fields
func (a *API) CreateCustomField(w http.ResponseWriter, r *http.Request) {
	projectID, ok := httpx.UUIDParam(w, r, "projectId")
	if !ok {
		return
	}
	// Defining a field changes the shape of every task in the project, so it
	// takes the same permission as reconfiguring the project itself.
	if _, ok := a.requireProjectManage(w, r, projectID); !ok {
		return
	}

	var req createFieldRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.Key == "" || req.Label == "" {
		httpx.Error(w, http.StatusBadRequest, "Field key and label are required.")
		return
	}
	if !repo.ValidFieldType(req.Type) {
		httpx.Error(w, http.StatusBadRequest, "Invalid field type.")
		return
	}
	if (req.Type == "select" || req.Type == "multi_select") && len(req.Options) == 0 {
		httpx.Error(w, http.StatusBadRequest, "A select field needs at least one option.")
		return
	}

	requester := auth.CurrentUser(r)
	field := &repo.FieldDefinition{
		ProjectID: &projectID,
		Key:       req.Key,
		Label:     req.Label,
		Type:      req.Type,
		Options:   req.Options,
		Required:  req.Required,
		Position:  req.Position,
	}
	if err := a.CustomFields.Create(r.Context(), field, requester.ID); err != nil {
		if repo.IsUniqueViolation(err) {
			httpx.Error(w, http.StatusConflict, "A field with that key already exists here.")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	a.Audit.Record(r, requester, audit.Event{
		Action: audit.ActionProjectUpdated, ResourceType: "custom_field", ResourceID: field.ID.String(),
		Changes: map[string]any{"key": field.Key, "type": field.Type}, Status: http.StatusCreated,
	})
	httpx.JSON(w, http.StatusCreated, field)
}

// DELETE /api/fields/:id
func (a *API) DeleteCustomField(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.UUIDParam(w, r, "id")
	if !ok {
		return
	}
	if !isAdmin(auth.CurrentUser(r)) {
		httpx.Error(w, http.StatusForbidden, forbiddenMessage)
		return
	}
	if err := a.CustomFields.Delete(r.Context(), id); err != nil {
		respondRepoError(w, err, "Field not found.")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// PUT /api/tasks/:id/fields
func (a *API) SetTaskCustomFields(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.UUIDParam(w, r, "id")
	if !ok {
		return
	}
	if _, _, ok := a.requireTaskWork(w, r, id); !ok {
		return
	}

	var values map[string]any
	if !httpx.DecodeJSON(w, r, &values) {
		return
	}
	if err := a.CustomFields.SetValues(r.Context(), id, values); err != nil {
		respondRepoError(w, err, "Task not found.")
		return
	}

	updated, err := a.Tasks.ByID(r.Context(), id)
	if err != nil {
		respondRepoError(w, err, "Task not found.")
		return
	}
	httpx.JSON(w, http.StatusOK, a.populateTask(r.Context(), *updated, false))
}

// ---------------------------------------------------------------------------
// Time tracking
// ---------------------------------------------------------------------------

// POST /api/tasks/:id/time/start
func (a *API) StartTimer(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.UUIDParam(w, r, "id")
	if !ok {
		return
	}
	if _, _, ok := a.requireTaskWork(w, r, id); !ok {
		return
	}

	requester := auth.CurrentUser(r)
	entry, err := a.Time.Start(r.Context(), id, requester.ID, "")
	if err != nil {
		if errors.Is(err, repo.ErrTimerRunning) {
			httpx.Error(w, http.StatusConflict, "You already have a timer running. Stop it first.")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusCreated, entry)
}

// POST /api/time/stop - stops whichever timer the caller has running.
func (a *API) StopTimer(w http.ResponseWriter, r *http.Request) {
	requester := auth.CurrentUser(r)
	entry, err := a.Time.Stop(r.Context(), requester.ID)
	if err != nil {
		respondRepoError(w, err, "No timer is running.")
		return
	}
	httpx.JSON(w, http.StatusOK, entry)
}

// GET /api/time/running
func (a *API) RunningTimer(w http.ResponseWriter, r *http.Request) {
	requester := auth.CurrentUser(r)
	entry, err := a.Time.Running(r.Context(), requester.ID)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			// Not an error: most of the time nobody is tracking.
			httpx.JSON(w, http.StatusOK, nil)
			return
		}
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, entry)
}

type logTimeRequest struct {
	Minutes int    `json:"minutes"`
	Note    string `json:"note"`
}

// POST /api/tasks/:id/time
func (a *API) LogTime(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.UUIDParam(w, r, "id")
	if !ok {
		return
	}
	if _, _, ok := a.requireTaskWork(w, r, id); !ok {
		return
	}
	var req logTimeRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.Minutes <= 0 {
		httpx.Error(w, http.StatusBadRequest, "Minutes must be a positive number.")
		return
	}

	requester := auth.CurrentUser(r)
	entry, err := a.Time.Log(r.Context(), id, requester.ID, req.Minutes, req.Note)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusCreated, entry)
}

// GET /api/tasks/:id/time
func (a *API) TaskTimeEntries(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.UUIDParam(w, r, "id")
	if !ok {
		return
	}
	entries, err := a.Time.ForTask(r.Context(), id)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Populate the people, so the client does not have to resolve ids itself.
	userIDs := make([]uuid.UUID, 0, len(entries))
	for _, entry := range entries {
		userIDs = append(userIDs, entry.UserID)
	}
	users := a.usersByID(r.Context(), uniqueIDs(userIDs))

	type entryResponse struct {
		repo.TimeEntry
		User *PublicUser `json:"user,omitempty"`
	}
	out := make([]entryResponse, 0, len(entries))
	for _, entry := range entries {
		item := entryResponse{TimeEntry: entry}
		if user, ok := users[entry.UserID]; ok {
			item.User = &user
		}
		out = append(out, item)
	}
	httpx.JSON(w, http.StatusOK, out)
}

// ---------------------------------------------------------------------------
// Watchers
// ---------------------------------------------------------------------------

// POST /api/tasks/:id/watch
func (a *API) WatchTask(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.UUIDParam(w, r, "id")
	if !ok {
		return
	}
	requester := auth.CurrentUser(r)
	if err := a.Watchers.Add(r.Context(), id, requester.ID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"watching": true})
}

// DELETE /api/tasks/:id/watch
func (a *API) UnwatchTask(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.UUIDParam(w, r, "id")
	if !ok {
		return
	}
	requester := auth.CurrentUser(r)
	if err := a.Watchers.Remove(r.Context(), id, requester.ID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"watching": false})
}

// ---------------------------------------------------------------------------
// Automations
// ---------------------------------------------------------------------------

// GET /api/projects/:projectId/automations
func (a *API) ListAutomations(w http.ResponseWriter, r *http.Request) {
	projectID, ok := httpx.UUIDParam(w, r, "projectId")
	if !ok {
		return
	}
	rules, err := a.Automations.ForProject(r.Context(), projectID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, rules)
}

type createAutomationRequest struct {
	Name       string           `json:"name"`
	Trigger    string           `json:"trigger"`
	Conditions []repo.Condition `json:"conditions"`
	Actions    []repo.Action    `json:"actions"`
	Enabled    *bool            `json:"enabled"`
}

// POST /api/projects/:projectId/automations
func (a *API) CreateAutomation(w http.ResponseWriter, r *http.Request) {
	projectID, ok := httpx.UUIDParam(w, r, "projectId")
	if !ok {
		return
	}
	// An automation acts on everyone's tasks, so creating one takes the same
	// permission as reconfiguring the project.
	if _, ok := a.requireProjectManage(w, r, projectID); !ok {
		return
	}

	var req createAutomationRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.Name == "" {
		httpx.Error(w, http.StatusBadRequest, "Automation name is required.")
		return
	}
	if !repo.ValidTrigger(req.Trigger) {
		httpx.Error(w, http.StatusBadRequest, "Invalid trigger.")
		return
	}
	if len(req.Actions) == 0 {
		httpx.Error(w, http.StatusBadRequest, "An automation needs at least one action.")
		return
	}
	for _, action := range req.Actions {
		if action.Type == "" {
			httpx.Error(w, http.StatusBadRequest, "Every action needs a type.")
			return
		}
		if action.Type == "set_priority" && !models.ValidPriority(action.Priority) {
			httpx.Error(w, http.StatusBadRequest, "Invalid priority in action.")
			return
		}
	}

	requester := auth.CurrentUser(r)
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	rule := &repo.Automation{
		ProjectID:  &projectID,
		Name:       req.Name,
		Enabled:    enabled,
		Trigger:    req.Trigger,
		Conditions: req.Conditions,
		Actions:    req.Actions,
	}
	if err := a.Automations.Create(r.Context(), rule, requester.ID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	a.Audit.Record(r, requester, audit.Event{
		Action: audit.ActionProjectUpdated, ResourceType: "automation", ResourceID: rule.ID.String(),
		Changes: map[string]any{"name": rule.Name, "trigger": rule.Trigger}, Status: http.StatusCreated,
	})
	httpx.JSON(w, http.StatusCreated, rule)
}

// DELETE /api/automations/:id
func (a *API) DeleteAutomation(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.UUIDParam(w, r, "id")
	if !ok {
		return
	}
	if !canAdministerStructure(auth.CurrentUser(r)) {
		httpx.Error(w, http.StatusForbidden, forbiddenMessage)
		return
	}
	if err := a.Automations.Delete(r.Context(), id); err != nil {
		respondRepoError(w, err, "Automation not found.")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// GET /api/automations/:id/runs - the execution log, which is what makes a
// rule that did not fire debuggable.
func (a *API) AutomationRuns(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.UUIDParam(w, r, "id")
	if !ok {
		return
	}
	runs, err := a.Automations.Runs(r.Context(), id, 50)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, runs)
}
