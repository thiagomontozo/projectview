package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"projectview/internal/auth"
	"projectview/internal/httpx"
	"projectview/internal/models"
	"projectview/internal/repo"
	"projectview/internal/services"
)

type commentResponse struct {
	ID        uuid.UUID   `json:"id"`
	Author    *PublicUser `json:"author"`
	Body      string      `json:"body"`
	CreatedAt time.Time   `json:"createdAt"`
}

type projectRefLite struct {
	ID    uuid.UUID `json:"id"`
	Name  string    `json:"name"`
	Key   string    `json:"key"`
	Color string    `json:"color"`
}

type taskResponse struct {
	models.Task
	Assignees    []PublicUser      `json:"assignees"`
	CreatedBy    *PublicUser       `json:"createdBy,omitempty"`
	Comments     []commentResponse `json:"comments"`
	Project      *projectRefLite   `json:"project,omitempty"`
	Subtasks     []taskResponse    `json:"subtasks,omitempty"`
	SubtaskCount int               `json:"subtaskCount"`
}

// populateTasks resolves users, sub-task counts and (optionally) project refs
// for a whole batch. This is the hot path the previous implementation got
// wrong: it issued a users query and a count query *per task*, so a 200-card
// board cost 400+ round trips. It now costs three, regardless of size.
func (a *API) populateTasks(ctx context.Context, tasks []models.Task, withProject bool) []taskResponse {
	userIDs := []uuid.UUID{}
	taskIDs := make([]uuid.UUID, 0, len(tasks))
	projectIDs := map[uuid.UUID]bool{}
	for _, t := range tasks {
		userIDs = append(userIDs, t.Assignees...)
		userIDs = append(userIDs, derefIDs(t.CreatedBy)...)
		for _, c := range t.Comments {
			userIDs = append(userIDs, derefIDs(c.Author)...)
		}
		taskIDs = append(taskIDs, t.ID)
		projectIDs[t.ProjectID] = true
	}

	users := a.usersByID(ctx, uniqueIDs(userIDs))
	counts, _ := a.Tasks.SubtaskCounts(ctx, taskIDs)

	projects := map[uuid.UUID]projectRefLite{}
	if withProject && len(projectIDs) > 0 {
		if all, err := a.Projects.List(ctx); err == nil {
			for _, p := range all {
				if projectIDs[p.ID] {
					projects[p.ID] = projectRefLite{ID: p.ID, Name: p.Name, Key: p.Key, Color: p.Color}
				}
			}
		}
	}

	out := make([]taskResponse, 0, len(tasks))
	for _, t := range tasks {
		resp := taskResponse{
			Task:         t,
			Assignees:    publicList(users, t.Assignees),
			Comments:     []commentResponse{},
			SubtaskCount: counts[t.ID],
		}
		if t.CreatedBy != nil {
			if u, ok := users[*t.CreatedBy]; ok {
				resp.CreatedBy = &u
			}
		}
		for _, c := range t.Comments {
			cr := commentResponse{ID: c.ID, Body: c.Body, CreatedAt: c.CreatedAt}
			if c.Author != nil {
				if u, ok := users[*c.Author]; ok {
					cr.Author = &u
				}
			}
			resp.Comments = append(resp.Comments, cr)
		}
		if withProject {
			if p, ok := projects[t.ProjectID]; ok {
				resp.Project = &p
			}
		}
		out = append(out, resp)
	}
	return out
}

func (a *API) populateTask(ctx context.Context, t models.Task, withProject bool) taskResponse {
	return a.populateTasks(ctx, []models.Task{t}, withProject)[0]
}

// GET /api/projects/:projectId/tasks
func (a *API) ListTasksForProject(w http.ResponseWriter, r *http.Request) {
	projectID, ok := httpx.UUIDParam(w, r, "projectId")
	if !ok {
		return
	}
	tasks, err := a.Tasks.ByProject(r.Context(), projectID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, a.populateTasks(r.Context(), tasks, false))
}

// GET /api/tasks/mine
func (a *API) MyTasks(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	tasks, err := a.Tasks.AssignedTo(r.Context(), user.ID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, a.populateTasks(r.Context(), tasks, true))
}

// GET /api/tasks/:id
func (a *API) GetTask(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.UUIDParam(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()
	t, err := a.Tasks.ByID(ctx, id)
	if err != nil {
		respondRepoError(w, err, "Task not found.")
		return
	}

	resp := a.populateTask(ctx, *t, false)
	if subs, err := a.Tasks.Subtasks(ctx, id); err == nil && len(subs) > 0 {
		resp.Subtasks = a.populateTasks(ctx, subs, false)
	}
	httpx.JSON(w, http.StatusOK, resp)
}

type createTaskRequest struct {
	Title         string     `json:"title"`
	Description   string     `json:"description"`
	Project       string     `json:"project"`
	ParentTask    string     `json:"parentTask"`
	Assignees     []string   `json:"assignees"`
	Status        string     `json:"status"`
	Priority      string     `json:"priority"`
	StartDate     *time.Time `json:"startDate"`
	DueDate       *time.Time `json:"dueDate"`
	EstimateHours float64    `json:"estimateHours"`
	Tags          []string   `json:"tags"`
}

func (a *API) notifier() *services.Notifier {
	return services.NewNotifier(a.Notifications, a.Users, a.Hub, services.NewMailer(a.Cfg))
}

// POST /api/tasks
func (a *API) CreateTask(w http.ResponseWriter, r *http.Request) {
	var req createTaskRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	a.createTask(w, r, req)
}

// POST /api/projects/:projectId/tasks - same as CreateTask but the project
// id comes from the URL, mirroring the nested REST route.
func (a *API) CreateTaskForProject(w http.ResponseWriter, r *http.Request) {
	var req createTaskRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	req.Project = chi.URLParam(r, "projectId")
	a.createTask(w, r, req)
}

func (a *API) createTask(w http.ResponseWriter, r *http.Request, req createTaskRequest) {
	if req.Title == "" || req.Project == "" {
		httpx.Error(w, http.StatusBadRequest, "Title and project are required.")
		return
	}
	projectID, err := uuid.Parse(req.Project)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "Invalid project id.")
		return
	}

	ctx := r.Context()
	project, ok := a.requireProjectWork(w, r, projectID)
	if !ok {
		return
	}

	status := req.Status
	if status == "" && len(project.Statuses) > 0 {
		status = project.Statuses[0].Key
	}
	order, err := a.Tasks.NextPosition(ctx, projectID, status)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	requester := auth.CurrentUser(r)
	task := &models.Task{
		ID:            uuid.New(),
		Title:         req.Title,
		Description:   req.Description,
		ProjectID:     projectID,
		Assignees:     httpx.UUIDs(req.Assignees),
		Status:        status,
		Priority:      defaultPriority(req.Priority),
		StartDate:     req.StartDate,
		DueDate:       req.DueDate,
		EstimateHours: req.EstimateHours,
		Order:         order,
		Tags:          req.Tags,
		CreatedBy:     &requester.ID,
	}
	if parentID, ok := httpx.OptionalUUID(req.ParentTask); ok && parentID != nil {
		task.ParentTask = parentID
	}

	if err := a.Tasks.Create(ctx, task); err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	notifier := a.notifier()
	for _, userID := range task.Assignees {
		if userID == requester.ID {
			continue
		}
		_, _ = notifier.NotifyUser(ctx, services.NotifyInput{
			UserID:  userID,
			Type:    models.NotifTaskAssigned,
			Title:   "You were assigned to \"" + task.Title + "\"",
			Body:    requester.Name + " assigned you to a task in " + project.Name + ".",
			Task:    &task.ID,
			Project: &project.ID,
			Email:   true,
		})
	}

	created, err := a.Tasks.ByID(ctx, task.ID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusCreated, a.populateTask(ctx, *created, false))
}

func defaultPriority(p string) string {
	if models.ValidPriority(p) {
		return p
	}
	return models.PriorityMedium
}

type updateTaskRequest struct {
	Title         *string                 `json:"title"`
	Description   *string                 `json:"description"`
	Assignees     *[]string               `json:"assignees"`
	Status        *string                 `json:"status"`
	Priority      *string                 `json:"priority"`
	StartDate     *time.Time              `json:"startDate"`
	DueDate       *time.Time              `json:"dueDate"`
	EstimateHours *float64                `json:"estimateHours"`
	Order         *float64                `json:"order"`
	Tags          *[]string               `json:"tags"`
	Checklist     *[]models.ChecklistItem `json:"checklist"`
}

// PUT /api/tasks/:id
func (a *API) UpdateTask(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.UUIDParam(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()

	existing, _, ok := a.requireTaskWork(w, r, id)
	if !ok {
		return
	}
	previousAssignees := map[uuid.UUID]bool{}
	for _, assigneeID := range existing.Assignees {
		previousAssignees[assigneeID] = true
	}

	var req updateTaskRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}

	patch := repo.TaskPatch{
		Title:         req.Title,
		Description:   req.Description,
		Status:        req.Status,
		EstimateHours: req.EstimateHours,
		Order:         req.Order,
		Tags:          req.Tags,
		Checklist:     req.Checklist,
	}
	if req.Priority != nil {
		if !models.ValidPriority(*req.Priority) {
			httpx.Error(w, http.StatusBadRequest, "Invalid priority.")
			return
		}
		patch.Priority = req.Priority
	}
	newAssignees := existing.Assignees
	if req.Assignees != nil {
		newAssignees = httpx.UUIDs(*req.Assignees)
		patch.Assignees = &newAssignees
	}
	if req.StartDate != nil {
		start := req.StartDate
		patch.StartDate = &start
	}
	if req.DueDate != nil {
		due := req.DueDate
		patch.DueDate = &due
	}

	if err := a.Tasks.Update(ctx, id, patch); err != nil {
		respondRepoError(w, err, "Task not found.")
		return
	}

	requester := auth.CurrentUser(r)
	if req.Assignees != nil {
		notifier := a.notifier()
		for _, userID := range newAssignees {
			if previousAssignees[userID] || userID == requester.ID {
				continue
			}
			_, _ = notifier.NotifyUser(ctx, services.NotifyInput{
				UserID:  userID,
				Type:    models.NotifTaskAssigned,
				Title:   "You were assigned to \"" + existing.Title + "\"",
				Body:    requester.Name + " assigned you to this task.",
				Task:    &existing.ID,
				Project: &existing.ProjectID,
				Email:   true,
			})
		}
	}

	updated, err := a.Tasks.ByID(ctx, id)
	if err != nil {
		respondRepoError(w, err, "Task not found.")
		return
	}
	httpx.JSON(w, http.StatusOK, a.populateTask(ctx, *updated, false))
}

type moveTaskRequest struct {
	Status *string  `json:"status"`
	Order  *float64 `json:"order"`
}

// PATCH /api/tasks/:id/move - used by the kanban drag-and-drop UI to change
// status/order cheaply.
func (a *API) MoveTask(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.UUIDParam(w, r, "id")
	if !ok {
		return
	}
	if _, _, ok := a.requireTaskWork(w, r, id); !ok {
		return
	}
	var req moveTaskRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}

	// completed_at is maintained by the repository: set on entering "done",
	// cleared on leaving it.
	if err := a.Tasks.Update(r.Context(), id, repo.TaskPatch{Status: req.Status, Order: req.Order}); err != nil {
		respondRepoError(w, err, "Task not found.")
		return
	}
	t, err := a.Tasks.ByID(r.Context(), id)
	if err != nil {
		respondRepoError(w, err, "Task not found.")
		return
	}
	httpx.JSON(w, http.StatusOK, t)
}

// DELETE /api/tasks/:id
func (a *API) DeleteTask(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.UUIDParam(w, r, "id")
	if !ok {
		return
	}
	if _, _, ok := a.requireTaskWork(w, r, id); !ok {
		return
	}
	// Sub-tasks go with it through ON DELETE CASCADE.
	if err := a.Tasks.Delete(r.Context(), id); err != nil {
		respondRepoError(w, err, "Task not found.")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type addCommentRequest struct {
	Body string `json:"body"`
}

// POST /api/tasks/:id/comments
func (a *API) AddComment(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.UUIDParam(w, r, "id")
	if !ok {
		return
	}
	task, _, ok := a.requireTaskWork(w, r, id)
	if !ok {
		return
	}
	var req addCommentRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.Body == "" {
		httpx.Error(w, http.StatusBadRequest, "Comment body is required.")
		return
	}

	ctx := r.Context()
	requester := auth.CurrentUser(r)
	if _, err := a.Tasks.AddComment(ctx, id, requester.ID, req.Body); err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	notifier := a.notifier()
	for _, userID := range task.Assignees {
		if userID == requester.ID {
			continue
		}
		_, _ = notifier.NotifyUser(ctx, services.NotifyInput{
			UserID:  userID,
			Type:    models.NotifCommentMention,
			Title:   "New comment on \"" + task.Title + "\"",
			Body:    requester.Name + ": " + req.Body,
			Task:    &task.ID,
			Project: &task.ProjectID,
		})
	}

	updated, err := a.Tasks.ByID(ctx, id)
	if err != nil {
		respondRepoError(w, err, "Task not found.")
		return
	}
	httpx.JSON(w, http.StatusOK, a.populateTask(ctx, *updated, false))
}
