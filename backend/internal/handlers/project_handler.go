package handlers

import (
	"context"
	"net/http"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"projectview/internal/auth"
	"projectview/internal/httpx"
	"projectview/internal/models"
)

type teamRef struct {
	ID    primitive.ObjectID `json:"id"`
	Name  string             `json:"name"`
	Color string             `json:"color"`
}

type projectResponse struct {
	models.Project
	Team    *teamRef     `json:"team,omitempty"`
	Members []PublicUser `json:"members"`
	Owner   *PublicUser  `json:"owner,omitempty"`
}

func (a *API) populateProject(ctx context.Context, p models.Project) projectResponse {
	ids := uniqueIDs(p.Members, []primitive.ObjectID{p.Owner})
	users, _ := a.usersByID(ctx, ids)

	resp := projectResponse{Project: p, Members: []PublicUser{}}
	for _, m := range p.Members {
		if u, ok := users[m]; ok {
			resp.Members = append(resp.Members, u)
		}
	}
	if u, ok := users[p.Owner]; ok {
		resp.Owner = &u
	}
	if p.Team != nil {
		var t models.Team
		if err := a.Store.Teams().FindOne(ctx, bson.M{"_id": *p.Team}).Decode(&t); err == nil {
			resp.Team = &teamRef{ID: t.ID, Name: t.Name, Color: t.Color}
		}
	}
	return resp
}

// GET /api/projects
func (a *API) ListProjects(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cursor, err := a.Store.Projects().Find(ctx, bson.M{}, options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}}))
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer cursor.Close(ctx)

	projects := []projectResponse{}
	for cursor.Next(ctx) {
		var p models.Project
		if err := cursor.Decode(&p); err != nil {
			continue
		}
		projects = append(projects, a.populateProject(ctx, p))
	}
	httpx.JSON(w, http.StatusOK, projects)
}

// GET /api/projects/:id
func (a *API) GetProject(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.ObjectIDParam(w, r, "id")
	if !ok {
		return
	}
	var p models.Project
	err := a.Store.Projects().FindOne(r.Context(), bson.M{"_id": id}).Decode(&p)
	if err == mongo.ErrNoDocuments {
		httpx.Error(w, http.StatusNotFound, "Project not found.")
		return
	} else if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, a.populateProject(r.Context(), p))
}

type createProjectRequest struct {
	Name        string     `json:"name"`
	Key         string     `json:"key"`
	Description string     `json:"description"`
	Color       string     `json:"color"`
	Team        string     `json:"team"`
	MemberIDs   []string   `json:"memberIds"`
	StartDate   *time.Time `json:"startDate"`
	EndDate     *time.Time `json:"endDate"`
}

// POST /api/projects
func (a *API) CreateProject(w http.ResponseWriter, r *http.Request) {
	var req createProjectRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.Name == "" || req.Key == "" {
		httpx.Error(w, http.StatusBadRequest, "Project name and key are required.")
		return
	}

	requester := auth.CurrentUser(r)
	if !canAdministerStructure(requester) {
		httpx.Error(w, http.StatusForbidden, "Only managers and administrators can create projects.")
		return
	}

	color := req.Color
	if color == "" {
		color = "#8b5cf6"
	}
	now := time.Now()

	project := models.Project{
		ID:          primitive.NewObjectID(),
		Name:        req.Name,
		Key:         req.Key,
		Description: req.Description,
		Color:       color,
		Status:      "planning",
		// The owner is mirrored into Members so membership checks, member
		// counts and the project chat channel all agree on who belongs here.
		Members:   uniqueIDs([]primitive.ObjectID{requester.ID}, httpx.ObjectIDs(req.MemberIDs)),
		Owner:     requester.ID,
		StartDate: req.StartDate,
		EndDate:   req.EndDate,
		Statuses:  models.DefaultStatuses(),
		CreatedBy: requester.ID,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if req.Team != "" {
		if teamID, err := primitive.ObjectIDFromHex(req.Team); err == nil {
			project.Team = &teamID
		}
	}

	ctx := r.Context()
	if _, err := a.Store.Projects().InsertOne(ctx, project); err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Auto-create a project chat channel so internal chat is ready immediately.
	channel := models.ChatChannel{
		ID:        primitive.NewObjectID(),
		Name:      "# " + project.Name,
		Type:      "project",
		Project:   &project.ID,
		Team:      project.Team,
		Members:   project.Members,
		CreatedBy: requester.ID,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if _, err := a.Store.ChatChannels().InsertOne(ctx, channel); err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	httpx.JSON(w, http.StatusCreated, a.populateProject(ctx, project))
}

// updateProjectRequest is an explicit allow-list. The handler previously
// decoded into a map and passed it straight to $set, which let a caller
// rewrite any field at all - reassigning "owner" to themselves, forging
// "createdAt", or injecting keys outside the schema.
type updateProjectRequest struct {
	Name        *string                 `json:"name"`
	Description *string                 `json:"description"`
	Color       *string                 `json:"color"`
	Status      *string                 `json:"status"`
	Team        *string                 `json:"team"`
	MemberIDs   *[]string               `json:"memberIds"`
	StartDate   *time.Time              `json:"startDate"`
	EndDate     *time.Time              `json:"endDate"`
	Statuses    *[]models.ProjectStatus `json:"statuses"`
}

var validProjectStatuses = map[string]bool{
	"planning": true, "active": true, "on_hold": true, "completed": true, "archived": true,
}

// PUT /api/projects/:id
func (a *API) UpdateProject(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.ObjectIDParam(w, r, "id")
	if !ok {
		return
	}
	project, ok := a.requireProjectManage(w, r, id)
	if !ok {
		return
	}

	var req updateProjectRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}

	set := bson.M{"updatedAt": time.Now()}
	if req.Name != nil {
		if *req.Name == "" {
			httpx.Error(w, http.StatusBadRequest, "Project name cannot be empty.")
			return
		}
		set["name"] = *req.Name
	}
	if req.Description != nil {
		set["description"] = *req.Description
	}
	if req.Color != nil {
		set["color"] = *req.Color
	}
	if req.Status != nil {
		if !validProjectStatuses[*req.Status] {
			httpx.Error(w, http.StatusBadRequest, "Invalid project status.")
			return
		}
		set["status"] = *req.Status
	}
	if req.Team != nil {
		if *req.Team == "" {
			set["team"] = nil
		} else if teamID, err := primitive.ObjectIDFromHex(*req.Team); err == nil {
			set["team"] = teamID
		} else {
			httpx.Error(w, http.StatusBadRequest, "Invalid team id.")
			return
		}
	}
	if req.MemberIDs != nil {
		// The owner always stays a member; otherwise they could edit
		// themselves out of their own project and lose access to it.
		set["members"] = uniqueIDs([]primitive.ObjectID{project.Owner}, httpx.ObjectIDs(*req.MemberIDs))
	}
	if req.StartDate != nil {
		set["startDate"] = *req.StartDate
	}
	if req.EndDate != nil {
		set["endDate"] = *req.EndDate
	}
	if req.Statuses != nil {
		if len(*req.Statuses) == 0 {
			httpx.Error(w, http.StatusBadRequest, "A project needs at least one status column.")
			return
		}
		set["statuses"] = *req.Statuses
	}

	// "key" and "owner" are intentionally absent: the key is the project's
	// stable identifier and ownership transfer deserves its own audited
	// endpoint rather than riding along in a generic update.

	_, err := a.Store.Projects().UpdateByID(r.Context(), id, bson.M{"$set": set})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	var p models.Project
	if err := a.Store.Projects().FindOne(r.Context(), bson.M{"_id": id}).Decode(&p); err != nil {
		httpx.Error(w, http.StatusNotFound, "Project not found.")
		return
	}
	httpx.JSON(w, http.StatusOK, a.populateProject(r.Context(), p))
}

// DELETE /api/projects/:id
func (a *API) DeleteProject(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.ObjectIDParam(w, r, "id")
	if !ok {
		return
	}
	if _, ok := a.requireProjectManage(w, r, id); !ok {
		return
	}
	ctx := r.Context()
	if _, err := a.Store.Projects().DeleteOne(ctx, bson.M{"_id": id}); err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	_, _ = a.Store.Tasks().DeleteMany(ctx, bson.M{"project": id})
	_, _ = a.Store.ChatChannels().DeleteMany(ctx, bson.M{"project": id})
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}
