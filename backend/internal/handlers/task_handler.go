package handlers

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"projectview/internal/audit"
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

// GET /api/tasks - filtered, searchable, cursor-paginated task listing.
//
// Replaces the "return every row" behaviour the other listings had: with ten
// thousand tasks that was a slow query, a large response and a slow render.
//
// Query: q, projectId, assigneeId, status, priority, overdue, topLevel,
// limit, cursor.
func (a *API) SearchTasks(w http.ResponseWriter, r *http.Request) {
	query, ok := a.parseTaskQuery(w, r)
	if !ok {
		return
	}

	tasks, err := a.Tasks.Search(r.Context(), query)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	populated := a.populateTasks(r.Context(), tasks, true)
	page := httpx.NewPage(populated, query.Limit, func(t taskResponse) string {
		return httpx.EncodeCursor(httpx.TimeCursor(t.CreatedAt), t.ID.String())
	})

	if r.URL.Query().Get("total") == "true" {
		if total, err := a.Tasks.CountMatching(r.Context(), query); err == nil {
			page.Total = &total
		}
	}
	httpx.JSON(w, http.StatusOK, page)
}

// GET /api/projects/:projectId/tasks/counts
//
// How many tasks each board column holds under the filters currently applied.
//
// Separate from the listing because it answers a different question and has a
// different lifetime: the columns fetch a page each and re-fetch as somebody
// scrolls, while the totals change only when the work does. Folding them into
// the listing would mean recounting the whole project on every "load more".
func (a *API) TaskCounts(w http.ResponseWriter, r *http.Request) {
	projectID, ok := httpx.UUIDParam(w, r, "projectId")
	if !ok {
		return
	}
	query, ok := a.parseTaskQuery(w, r)
	if !ok {
		return
	}
	query.ProjectID = &projectID
	// A count per column has to count the columns, so any status filter from
	// the query string is dropped - keeping it would report zero for every
	// column the filter excludes and make the board look empty rather than
	// filtered.
	query.Statuses = nil

	counts, err := a.Tasks.CountByStatus(r.Context(), query)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	total := int64(0)
	for _, n := range counts {
		total += n
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"byStatus": counts, "total": total})
}

// parseTaskQuery reads the listing query string into a repo query.
//
// Filters are read with r.URL.Query()[key] rather than .Get(key), so
// "?priority=urgent&priority=high" means both rather than the first. The views
// offer multi-select, and collapsing that to one value server-side would have
// made a filter mean something different depending on where it was applied.
func (a *API) parseTaskQuery(w http.ResponseWriter, r *http.Request) (repo.TaskQuery, bool) {
	q := r.URL.Query()
	params := httpx.ParseList(r, repo.SortableTaskColumns, "")

	query := repo.TaskQuery{
		Search:     params.Search,
		Statuses:   cleanValues(q["status"]),
		Priorities: cleanValues(q["priority"]),
		Overdue:    q.Get("overdue") == "true",
		ParentOnly: q.Get("topLevel") == "true",
		SortColumn: params.Sort,
		SortDesc:   params.Desc,
		Limit:      params.Limit,
	}

	if raw := q.Get("projectId"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, "Invalid projectId.")
			return query, false
		}
		query.ProjectID = &id
	}

	for _, raw := range cleanValues(q["assigneeId"]) {
		id, err := uuid.Parse(raw)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, "Invalid assigneeId.")
			return query, false
		}
		query.AssigneeIDs = append(query.AssigneeIDs, id)
	}

	if raw := q.Get("offset"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			httpx.Error(w, http.StatusBadRequest, "Invalid offset.")
			return query, false
		}
		query.Offset = n
	}

	if params.Cursor != "" {
		if sortValue, idValue, ok := httpx.DecodeCursor(params.Cursor); ok {
			if t, ok := httpx.ParseTimeCursor(sortValue); ok {
				query.CursorTime = &t
			}
			if id, err := uuid.Parse(idValue); err == nil {
				query.CursorID = &id
			}
		}
	}

	return query, true
}

// cleanValues drops blanks from a repeated query parameter. A client that
// sends "?status=" means "no filter", not "match the empty status" - and
// without this it would silently match nothing.
func cleanValues(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
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

	a.Audit.Record(r, requester, audit.Event{
		Action: audit.ActionTaskCreated, ResourceType: "task", ResourceID: task.ID.String(),
		Changes: map[string]any{"title": task.Title, "projectId": projectID.String()},
		Status:  http.StatusCreated,
	})

	created, err := a.Tasks.ByID(ctx, task.ID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Automations run after the task exists and the response is decided, and
	// never fail the request - see services.AutomationEngine.
	a.Engine.Run(ctx, services.Event{
		Trigger:   services.TriggerTaskCreated,
		Task:      created,
		ProjectID: projectID,
		ActorID:   requester.ID,
	})

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
	a.Audit.Record(r, requester, audit.Event{
		Action: audit.ActionTaskUpdated, ResourceType: "task", ResourceID: id.String(),
		Changes: audit.Diff(
			map[string]any{"title": existing.Title, "status": existing.Status, "priority": existing.Priority},
			map[string]any{
				"title":    deref(req.Title, existing.Title),
				"status":   deref(req.Status, existing.Status),
				"priority": deref(req.Priority, existing.Priority),
			},
		),
		Status: http.StatusOK,
	})
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

	if req.Status != nil && *req.Status != existing.Status {
		a.Engine.Run(ctx, services.Event{
			Trigger:        services.TriggerTaskStatusChanged,
			Task:           updated,
			ProjectID:      existing.ProjectID,
			ActorID:        requester.ID,
			PreviousStatus: existing.Status,
		})
	}
	if req.Assignees != nil {
		a.Engine.Run(ctx, services.Event{
			Trigger:   services.TriggerTaskAssigned,
			Task:      updated,
			ProjectID: existing.ProjectID,
			ActorID:   requester.ID,
		})
	}

	// The same completion hook as the board's move: a task can be finished from
	// the dialog just as easily as by dragging it, and a series that only
	// recurred from one of the two would look broken to whoever used the other.
	if existing.CompletedAt == nil && updated.CompletedAt != nil {
		a.spawnOnCompletion(r, updated)
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
	existing, _, ok := a.requireTaskWork(w, r, id)
	if !ok {
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
	if req.Status != nil && *req.Status != existing.Status {
		a.Audit.Record(r, auth.CurrentUser(r), audit.Event{
			Action: audit.ActionTaskMoved, ResourceType: "task", ResourceID: id.String(),
			Changes: map[string]any{"status": map[string]any{"from": existing.Status, "to": *req.Status}},
			Status:  http.StatusOK,
		})
	}
	t, err := a.Tasks.ByID(r.Context(), id)
	if err != nil {
		respondRepoError(w, err, "Task not found.")
		return
	}

	// Moving a card is a status change like any other, so the same rules fire.
	if req.Status != nil && *req.Status != existing.Status {
		a.Engine.Run(r.Context(), services.Event{
			Trigger:        services.TriggerTaskStatusChanged,
			Task:           t,
			ProjectID:      existing.ProjectID,
			ActorID:        auth.CurrentUser(r).ID,
			PreviousStatus: existing.Status,
		})
	}

	// A completion-driven series produces its next instance here: the moment
	// the work is finished is the only thing that defines "next" in that mode.
	// Keyed on completedAt appearing rather than on the status name, because a
	// project can call its final column whatever it likes - the repository is
	// what decides a task is done.
	if existing.CompletedAt == nil && t.CompletedAt != nil {
		a.spawnOnCompletion(r, t)
	}

	httpx.JSON(w, http.StatusOK, t)
}

// DELETE /api/tasks/:id
func (a *API) DeleteTask(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.UUIDParam(w, r, "id")
	if !ok {
		return
	}
	existing, _, ok := a.requireTaskWork(w, r, id)
	if !ok {
		return
	}
	// Sub-tasks go with it through ON DELETE CASCADE.
	if err := a.Tasks.Delete(r.Context(), id); err != nil {
		respondRepoError(w, err, "Task not found.")
		return
	}
	a.Audit.Record(r, auth.CurrentUser(r), audit.Event{
		Action: audit.ActionTaskDeleted, ResourceType: "task", ResourceID: id.String(),
		Changes: map[string]any{"title": existing.Title}, Status: http.StatusOK,
	})
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
