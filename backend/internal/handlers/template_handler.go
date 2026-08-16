package handlers

import (
	"net/http"
	"time"

	"github.com/google/uuid"

	"projectview/internal/audit"
	"projectview/internal/auth"
	"projectview/internal/httpx"
	"projectview/internal/models"
	"projectview/internal/repo"
)

// Templates: reusable shapes of work.
//
// Two kinds. A *task* template creates one task, with its checklist, tags and
// field values, into a project that already exists. A *project* template
// creates the project, its status columns and every task it carries.
//
// Creating structure is a manager's action, so is capturing a template of it;
// applying a task template only needs the right to work in the project it lands
// in. That split is the whole authorization model here.

// GET /api/templates?kind=&spaceId=
func (a *API) ListTemplates(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	if kind != "" && !repo.ValidTemplateKind(kind) {
		httpx.Error(w, http.StatusBadRequest, "Kind must be task or project.")
		return
	}

	var spaceID *uuid.UUID
	if raw := r.URL.Query().Get("spaceId"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, "Invalid spaceId.")
			return
		}
		// A template scoped to a private space must not be listed to somebody
		// who cannot see the space.
		if _, ok := a.requireSpaceReadByID(w, r, id); !ok {
			return
		}
		spaceID = &id
	}

	templates, err := a.Templates.List(r.Context(), kind, spaceID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, templates)
}

type templateRequest struct {
	Name        string               `json:"name"`
	Description string               `json:"description"`
	Kind        string               `json:"kind"`
	SpaceID     string               `json:"spaceId"`
	Payload     repo.TemplatePayload `json:"payload"`
	// FromProjectID captures an existing project instead of describing one.
	FromProjectID string `json:"fromProjectId"`
}

// POST /api/templates
func (a *API) CreateTemplate(w http.ResponseWriter, r *http.Request) {
	var req templateRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.Name == "" {
		httpx.Error(w, http.StatusBadRequest, "A template needs a name.")
		return
	}
	if !repo.ValidTemplateKind(req.Kind) {
		httpx.Error(w, http.StatusBadRequest, "Kind must be task or project.")
		return
	}

	requester := auth.CurrentUser(r)
	// Templates shape how work gets created, so making one is the same kind of
	// act as creating the structure itself.
	if !canAdministerStructure(requester) {
		httpx.Error(w, http.StatusForbidden, forbiddenMessage)
		return
	}

	template := &repo.Template{
		Name: req.Name, Description: req.Description, Kind: req.Kind,
		Payload: req.Payload, CreatedBy: &requester.ID,
	}
	if spaceID, ok := httpx.OptionalUUID(req.SpaceID); ok && spaceID != nil {
		if _, ok := a.requireSpaceReadByID(w, r, *spaceID); !ok {
			return
		}
		template.SpaceID = spaceID
	}

	// Capturing an existing project is the path people actually use: describing
	// twelve tasks by hand in JSON is not a feature anybody wants.
	if req.FromProjectID != "" {
		id, err := uuid.Parse(req.FromProjectID)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, "Invalid fromProjectId.")
			return
		}
		project, ok := a.requireProjectManage(w, r, id)
		if !ok {
			return
		}
		payload, err := a.captureProject(r, project)
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		template.Payload = payload
		template.Kind = repo.TemplateProject
	}

	if template.Kind == repo.TemplateTask && template.Payload.Task == nil {
		httpx.Error(w, http.StatusBadRequest, "A task template needs a task to create.")
		return
	}

	if err := a.Templates.Create(r.Context(), template); err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	a.Audit.Record(r, requester, audit.Event{
		Action: audit.ActionTemplateCreated, ResourceType: "template",
		ResourceID: template.ID.String(),
		Changes:    map[string]any{"name": template.Name, "kind": template.Kind},
		Status:     http.StatusCreated,
	})
	httpx.JSON(w, http.StatusCreated, template)
}

// captureProject snapshots a project's columns and its top-level tasks.
//
// Dates become offsets in days from the project's start, so a plan captured in
// March creates work dated from whenever it is next used. Assignees are
// deliberately dropped: a template that silently allocates last quarter's team
// is worse than one that allocates nobody.
func (a *API) captureProject(r *http.Request, project *models.Project) (repo.TemplatePayload, error) {
	ctx := r.Context()
	payload := repo.TemplatePayload{Statuses: project.Statuses, Color: project.Color}

	tasks, err := a.Tasks.ByProject(ctx, project.ID)
	if err != nil {
		return payload, err
	}

	anchor := project.StartDate
	byParent := map[uuid.UUID][]models.Task{}
	for _, task := range tasks {
		if task.ParentTask != nil {
			byParent[*task.ParentTask] = append(byParent[*task.ParentTask], task)
		}
	}

	for _, task := range tasks {
		if task.ParentTask != nil {
			continue
		}
		spec := taskToSpec(task, anchor)
		for _, child := range byParent[task.ID] {
			spec.Subtasks = append(spec.Subtasks, taskToSpec(child, anchor))
		}
		payload.Tasks = append(payload.Tasks, spec)
	}
	return payload, nil
}

func taskToSpec(task models.Task, anchor *time.Time) repo.TemplateTaskSpec {
	spec := repo.TemplateTaskSpec{
		Title: task.Title, Description: task.Description, Status: task.Status,
		Priority: task.Priority, EstimateHours: task.EstimateHours,
		Tags: append([]string(nil), task.Tags...), CustomFields: task.CustomFields,
	}
	for _, item := range task.Checklist {
		// Unticked: the point of a checklist in a template is that it has not
		// been done yet.
		spec.Checklist = append(spec.Checklist, item.Text)
	}
	spec.StartOffsetDays = daysBetween(anchor, task.StartDate)
	spec.DueOffsetDays = daysBetween(anchor, task.DueDate)
	return spec
}

// daysBetween returns whole days from anchor to t, or nil when either is
// missing - a task with no due date should not gain one from a template.
func daysBetween(anchor, t *time.Time) *int {
	if anchor == nil || t == nil {
		return nil
	}
	days := int(t.Sub(*anchor).Hours() / 24)
	return &days
}

type applyTemplateRequest struct {
	// For a task template: where it lands.
	ProjectID string `json:"projectId"`
	// For a project template: what the new project is called.
	Name string `json:"name"`
	Key  string `json:"key"`
}

// POST /api/templates/:id/apply
func (a *API) ApplyTemplate(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.UUIDParam(w, r, "id")
	if !ok {
		return
	}
	template, err := a.Templates.ByID(r.Context(), id)
	if err != nil {
		respondRepoError(w, err, "Template not found.")
		return
	}
	if template.SpaceID != nil {
		if _, ok := a.requireSpaceReadByID(w, r, *template.SpaceID); !ok {
			return
		}
	}

	var req applyTemplateRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}

	requester := auth.CurrentUser(r)
	now := time.Now()

	if template.Kind == repo.TemplateTask {
		projectID, err := uuid.Parse(req.ProjectID)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, "A task template needs a projectId to land in.")
			return
		}
		project, ok := a.requireProjectWork(w, r, projectID)
		if !ok {
			return
		}
		task, err := a.taskFactory().ApplyTaskSpec(r.Context(), *template.Payload.Task,
			projectID, nil, firstStatusKey(project), requester.ID, now)
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		a.recordTemplateApplied(r, requester, template, task.ID.String())
		httpx.JSON(w, http.StatusCreated, a.populateTask(r.Context(), *task, false))
		return
	}

	// A project template creates structure, so it takes the permission to
	// create a project rather than merely to work in one.
	if !canAdministerStructure(requester) {
		httpx.Error(w, http.StatusForbidden, forbiddenMessage)
		return
	}
	if req.Name == "" || req.Key == "" {
		httpx.Error(w, http.StatusBadRequest, "A new project needs a name and a key.")
		return
	}

	project := &models.Project{
		ID: uuid.New(), Name: req.Name, Key: req.Key,
		Description: template.Description, Status: "active",
		Color: template.Payload.Color, Owner: &requester.ID, CreatedBy: &requester.ID,
		Members: []uuid.UUID{requester.ID}, StartDate: &now,
	}
	project.Statuses = template.Payload.Statuses
	if len(project.Statuses) == 0 {
		project.Statuses = models.DefaultStatuses()
	}

	if err := a.Projects.Create(r.Context(), project); err != nil {
		respondRepoError(w, err, "Could not create the project.")
		return
	}

	created := 0
	for _, spec := range template.Payload.Tasks {
		if _, err := a.taskFactory().ApplyTaskSpec(r.Context(), spec, project.ID, nil,
			firstStatusKey(project), requester.ID, now); err != nil {
			// One task failing does not undo the project: a partly-created plan
			// somebody can finish beats an error and nothing at all.
			continue
		}
		created++
	}

	a.recordTemplateApplied(r, requester, template, project.ID.String())
	httpx.JSON(w, http.StatusCreated, map[string]any{"project": project, "tasksCreated": created})
}

func (a *API) recordTemplateApplied(r *http.Request, actor *models.User, template *repo.Template, resource string) {
	a.Audit.Record(r, actor, audit.Event{
		Action: audit.ActionTemplateApplied, ResourceType: "template",
		ResourceID: template.ID.String(),
		Changes:    map[string]any{"name": template.Name, "created": resource},
		Status:     http.StatusCreated,
	})
}

func firstStatusKey(project *models.Project) string {
	if len(project.Statuses) > 0 {
		return project.Statuses[0].Key
	}
	return "todo"
}

// DELETE /api/templates/:id
func (a *API) DeleteTemplate(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.UUIDParam(w, r, "id")
	if !ok {
		return
	}
	if !canAdministerStructure(auth.CurrentUser(r)) {
		httpx.Error(w, http.StatusForbidden, forbiddenMessage)
		return
	}
	if err := a.Templates.Delete(r.Context(), id); err != nil {
		respondRepoError(w, err, "Template not found.")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}
