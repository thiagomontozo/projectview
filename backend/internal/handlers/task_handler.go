package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"projectview/internal/auth"
	"projectview/internal/httpx"
	"projectview/internal/models"
	"projectview/internal/services"
)

type commentResponse struct {
	ID        primitive.ObjectID `json:"id"`
	Author    *PublicUser        `json:"author"`
	Body      string             `json:"body"`
	CreatedAt time.Time          `json:"createdAt"`
}

type projectRefLite struct {
	ID    primitive.ObjectID `json:"id"`
	Name  string             `json:"name"`
	Key   string             `json:"key"`
	Color string             `json:"color"`
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

func (a *API) populateTask(ctx context.Context, t models.Task, withProject bool) taskResponse {
	commentAuthors := make([]primitive.ObjectID, 0, len(t.Comments))
	for _, c := range t.Comments {
		commentAuthors = append(commentAuthors, c.Author)
	}
	ids := uniqueIDs(t.Assignees, []primitive.ObjectID{t.CreatedBy}, commentAuthors)
	users, _ := a.usersByID(ctx, ids)

	resp := taskResponse{Task: t, Assignees: []PublicUser{}, Comments: []commentResponse{}}
	for _, id := range t.Assignees {
		if u, ok := users[id]; ok {
			resp.Assignees = append(resp.Assignees, u)
		}
	}
	if u, ok := users[t.CreatedBy]; ok {
		resp.CreatedBy = &u
	}
	for _, c := range t.Comments {
		cr := commentResponse{ID: c.ID, Body: c.Body, CreatedAt: c.CreatedAt}
		if u, ok := users[c.Author]; ok {
			cr.Author = &u
		}
		resp.Comments = append(resp.Comments, cr)
	}

	count, _ := a.Store.Tasks().CountDocuments(ctx, bson.M{"parentTask": t.ID})
	resp.SubtaskCount = int(count)

	if withProject {
		var p models.Project
		if err := a.Store.Projects().FindOne(ctx, bson.M{"_id": t.Project}).Decode(&p); err == nil {
			resp.Project = &projectRefLite{ID: p.ID, Name: p.Name, Key: p.Key, Color: p.Color}
		}
	}

	return resp
}

// GET /api/projects/:projectId/tasks
func (a *API) ListTasksForProject(w http.ResponseWriter, r *http.Request) {
	projectID, ok := httpx.ObjectIDParam(w, r, "projectId")
	if !ok {
		return
	}
	ctx := r.Context()
	cursor, err := a.Store.Tasks().Find(ctx, bson.M{"project": projectID},
		options.Find().SetSort(bson.D{{Key: "status", Value: 1}, {Key: "order", Value: 1}, {Key: "createdAt", Value: 1}}))
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer cursor.Close(ctx)

	tasks := []taskResponse{}
	for cursor.Next(ctx) {
		var t models.Task
		if err := cursor.Decode(&t); err != nil {
			continue
		}
		tasks = append(tasks, a.populateTask(ctx, t, false))
	}
	httpx.JSON(w, http.StatusOK, tasks)
}

// GET /api/tasks/mine
func (a *API) MyTasks(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	ctx := r.Context()
	cursor, err := a.Store.Tasks().Find(ctx, bson.M{"assignees": user.ID}, options.Find().SetSort(bson.D{{Key: "dueDate", Value: 1}}))
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer cursor.Close(ctx)

	tasks := []taskResponse{}
	for cursor.Next(ctx) {
		var t models.Task
		if err := cursor.Decode(&t); err != nil {
			continue
		}
		tasks = append(tasks, a.populateTask(ctx, t, true))
	}
	httpx.JSON(w, http.StatusOK, tasks)
}

// GET /api/tasks/:id
func (a *API) GetTask(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.ObjectIDParam(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()
	var t models.Task
	err := a.Store.Tasks().FindOne(ctx, bson.M{"_id": id}).Decode(&t)
	if err == mongo.ErrNoDocuments {
		httpx.Error(w, http.StatusNotFound, "Task not found.")
		return
	} else if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	resp := a.populateTask(ctx, t, false)

	subCursor, err := a.Store.Tasks().Find(ctx, bson.M{"parentTask": id})
	if err == nil {
		defer subCursor.Close(ctx)
		for subCursor.Next(ctx) {
			var st models.Task
			if err := subCursor.Decode(&st); err != nil {
				continue
			}
			resp.Subtasks = append(resp.Subtasks, a.populateTask(ctx, st, false))
		}
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
	return services.NewNotifier(a.Store, a.Hub, services.NewMailer(a.Cfg))
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
	projectID, err := primitive.ObjectIDFromHex(req.Project)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "Invalid project id.")
		return
	}

	ctx := r.Context()
	projectPtr, ok := a.requireProjectWork(w, r, projectID)
	if !ok {
		return
	}
	project := *projectPtr

	status := req.Status
	if status == "" && len(project.Statuses) > 0 {
		status = project.Statuses[0].Key
	}

	var lastInColumn models.Task
	order := 0.0
	err = a.Store.Tasks().FindOne(ctx, bson.M{"project": projectID, "status": status},
		options.FindOne().SetSort(bson.D{{Key: "order", Value: -1}})).Decode(&lastInColumn)
	if err == nil {
		order = lastInColumn.Order + 1
	}

	requester := auth.CurrentUser(r)
	now := time.Now()
	task := models.Task{
		ID:            primitive.NewObjectID(),
		Title:         req.Title,
		Description:   req.Description,
		Project:       projectID,
		Assignees:     httpx.ObjectIDs(req.Assignees),
		Status:        status,
		Priority:      defaultPriority(req.Priority),
		StartDate:     req.StartDate,
		DueDate:       req.DueDate,
		EstimateHours: req.EstimateHours,
		Order:         order,
		Tags:          req.Tags,
		Checklist:     []models.ChecklistItem{},
		Comments:      []models.Comment{},
		AlertsSent:    []models.AlertSent{},
		CreatedBy:     requester.ID,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if req.ParentTask != "" {
		if pid, err := primitive.ObjectIDFromHex(req.ParentTask); err == nil {
			task.ParentTask = &pid
		}
	}
	if task.Tags == nil {
		task.Tags = []string{}
	}

	if _, err := a.Store.Tasks().InsertOne(ctx, task); err != nil {
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

	httpx.JSON(w, http.StatusCreated, a.populateTask(ctx, task, false))
}

func defaultPriority(p string) string {
	switch p {
	case models.PriorityLow, models.PriorityMedium, models.PriorityHigh, models.PriorityUrgent:
		return p
	default:
		return models.PriorityMedium
	}
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
	id, ok := httpx.ObjectIDParam(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()

	existingPtr, _, ok := a.requireTaskWork(w, r, id)
	if !ok {
		return
	}
	existing := *existingPtr
	previousAssignees := map[primitive.ObjectID]bool{}
	for _, assigneeID := range existing.Assignees {
		previousAssignees[assigneeID] = true
	}

	var req updateTaskRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}

	set := bson.M{"updatedAt": time.Now()}
	newAssignees := existing.Assignees
	if req.Title != nil {
		set["title"] = *req.Title
	}
	if req.Description != nil {
		set["description"] = *req.Description
	}
	if req.Assignees != nil {
		newAssignees = httpx.ObjectIDs(*req.Assignees)
		set["assignees"] = newAssignees
	}
	newStatus := existing.Status
	if req.Status != nil {
		newStatus = *req.Status
		set["status"] = newStatus
	}
	if req.Priority != nil {
		set["priority"] = *req.Priority
	}
	if req.StartDate != nil {
		set["startDate"] = *req.StartDate
	}
	if req.DueDate != nil {
		set["dueDate"] = *req.DueDate
	}
	if req.EstimateHours != nil {
		set["estimateHours"] = *req.EstimateHours
	}
	if req.Order != nil {
		set["order"] = *req.Order
	}
	if req.Tags != nil {
		set["tags"] = *req.Tags
	}
	if req.Checklist != nil {
		set["checklist"] = *req.Checklist
	}

	if newStatus == "done" && existing.CompletedAt == nil {
		set["completedAt"] = time.Now()
	} else if newStatus != "done" {
		set["completedAt"] = nil
	}

	if _, err := a.Store.Tasks().UpdateByID(ctx, id, bson.M{"$set": set}); err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
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
				Project: &existing.Project,
				Email:   true,
			})
		}
	}

	var updated models.Task
	_ = a.Store.Tasks().FindOne(ctx, bson.M{"_id": id}).Decode(&updated)
	httpx.JSON(w, http.StatusOK, a.populateTask(ctx, updated, false))
}

type moveTaskRequest struct {
	Status *string  `json:"status"`
	Order  *float64 `json:"order"`
}

// PATCH /api/tasks/:id/move - used by the kanban drag-and-drop UI to change status/order cheaply.
func (a *API) MoveTask(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.ObjectIDParam(w, r, "id")
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

	set := bson.M{"updatedAt": time.Now()}
	if req.Status != nil {
		set["status"] = *req.Status
		if *req.Status == "done" {
			set["completedAt"] = time.Now()
		} else {
			// Dragging a card back out of "done" must clear the completion
			// timestamp, otherwise it keeps skewing the completion-trend chart.
			set["completedAt"] = nil
		}
	}
	if req.Order != nil {
		set["order"] = *req.Order
	}

	ctx := r.Context()
	_, err := a.Store.Tasks().UpdateByID(ctx, id, bson.M{"$set": set})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	var t models.Task
	if err := a.Store.Tasks().FindOne(ctx, bson.M{"_id": id}).Decode(&t); err != nil {
		httpx.Error(w, http.StatusNotFound, "Task not found.")
		return
	}
	httpx.JSON(w, http.StatusOK, t)
}

// DELETE /api/tasks/:id
func (a *API) DeleteTask(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.ObjectIDParam(w, r, "id")
	if !ok {
		return
	}
	if _, _, ok := a.requireTaskWork(w, r, id); !ok {
		return
	}
	ctx := r.Context()
	if _, err := a.Store.Tasks().DeleteOne(ctx, bson.M{"_id": id}); err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	_, _ = a.Store.Tasks().DeleteMany(ctx, bson.M{"parentTask": id})
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type addCommentRequest struct {
	Body string `json:"body"`
}

// POST /api/tasks/:id/comments
func (a *API) AddComment(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.ObjectIDParam(w, r, "id")
	if !ok {
		return
	}
	if _, _, ok := a.requireTaskWork(w, r, id); !ok {
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

	requester := auth.CurrentUser(r)
	comment := models.Comment{ID: primitive.NewObjectID(), Author: requester.ID, Body: req.Body, CreatedAt: time.Now()}

	ctx := r.Context()
	_, err := a.Store.Tasks().UpdateByID(ctx, id, bson.M{"$push": bson.M{"comments": comment}})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	var task models.Task
	if err := a.Store.Tasks().FindOne(ctx, bson.M{"_id": id}).Decode(&task); err != nil {
		httpx.Error(w, http.StatusNotFound, "Task not found.")
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
			Project: &task.Project,
		})
	}

	httpx.JSON(w, http.StatusOK, a.populateTask(ctx, task, false))
}
