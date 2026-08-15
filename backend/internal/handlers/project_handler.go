package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"

	"projectview/internal/audit"
	"projectview/internal/auth"
	"projectview/internal/httpx"
	"projectview/internal/models"
	"projectview/internal/repo"
)

type teamRef struct {
	ID    uuid.UUID `json:"id"`
	Name  string    `json:"name"`
	Color string    `json:"color"`
}

type projectResponse struct {
	models.Project
	Team    *teamRef     `json:"team,omitempty"`
	Members []PublicUser `json:"members"`
	Owner   *PublicUser  `json:"owner,omitempty"`
}

// populateProjects resolves users and teams for a batch of projects in two
// queries total, however many projects there are.
func (a *API) populateProjects(ctx context.Context, projects []models.Project) []projectResponse {
	userIDs := []uuid.UUID{}
	teamIDs := map[uuid.UUID]bool{}
	for _, p := range projects {
		userIDs = append(userIDs, p.Members...)
		if p.Owner != nil {
			userIDs = append(userIDs, *p.Owner)
		}
		if p.TeamID != nil {
			teamIDs[*p.TeamID] = true
		}
	}
	users := a.usersByID(ctx, uniqueIDs(userIDs))

	teams := map[uuid.UUID]teamRef{}
	if len(teamIDs) > 0 {
		all, err := a.Teams.List(ctx)
		if err == nil {
			for _, t := range all {
				if teamIDs[t.ID] {
					teams[t.ID] = teamRef{ID: t.ID, Name: t.Name, Color: t.Color}
				}
			}
		}
	}

	out := make([]projectResponse, 0, len(projects))
	for _, p := range projects {
		resp := projectResponse{Project: p, Members: publicList(users, p.Members)}
		if p.Owner != nil {
			if u, ok := users[*p.Owner]; ok {
				resp.Owner = &u
			}
		}
		if p.TeamID != nil {
			if t, ok := teams[*p.TeamID]; ok {
				resp.Team = &t
			}
		}
		out = append(out, resp)
	}
	return out
}

func (a *API) populateProject(ctx context.Context, p models.Project) projectResponse {
	return a.populateProjects(ctx, []models.Project{p})[0]
}

// GET /api/projects
func (a *API) ListProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := a.Projects.List(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, a.populateProjects(r.Context(), projects))
}

// GET /api/projects/:id
func (a *API) GetProject(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.UUIDParam(w, r, "id")
	if !ok {
		return
	}
	p, err := a.Projects.ByID(r.Context(), id)
	if err != nil {
		respondRepoError(w, err, "Project not found.")
		return
	}
	httpx.JSON(w, http.StatusOK, a.populateProject(r.Context(), *p))
}

type createProjectRequest struct {
	Name        string     `json:"name"`
	Key         string     `json:"key"`
	Description string     `json:"description"`
	Color       string     `json:"color"`
	Team        string     `json:"team"`
	Space       string     `json:"spaceId"`
	Folder      string     `json:"folderId"`
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

	project := &models.Project{
		ID:          uuid.New(),
		Name:        req.Name,
		Key:         req.Key,
		Description: req.Description,
		Color:       color,
		Status:      models.ProjectStatusPlanning,
		// The owner is mirrored into Members so membership checks, member
		// counts and the project chat channel all agree on who belongs here.
		Members:   uniqueIDs([]uuid.UUID{requester.ID}, httpx.UUIDs(req.MemberIDs)),
		Owner:     &requester.ID,
		StartDate: req.StartDate,
		EndDate:   req.EndDate,
		Statuses:  models.DefaultStatuses(),
		CreatedBy: &requester.ID,
	}
	if teamID, ok := httpx.OptionalUUID(req.Team); ok && teamID != nil {
		project.TeamID = teamID
	}

	ctx := r.Context()

	// Place the list in the hierarchy. Callers that do not know about spaces
	// yet land in the first one, so the tree never has orphans.
	if spaceID, ok := httpx.OptionalUUID(req.Space); ok && spaceID != nil {
		project.SpaceID = spaceID
	} else if fallback, err := a.Projects.DefaultSpaceID(ctx); err == nil {
		project.SpaceID = fallback
	}
	if folderID, ok := httpx.OptionalUUID(req.Folder); ok && folderID != nil {
		project.FolderID = folderID
	}

	if err := a.Projects.Create(ctx, project); err != nil {
		if repo.IsUniqueViolation(err) {
			httpx.Error(w, http.StatusConflict, "A project with that key already exists.")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Auto-create a project chat channel so internal chat is ready immediately.
	channel := &models.ChatChannel{
		ID:        uuid.New(),
		Name:      "# " + project.Name,
		Type:      models.ChannelTypeProject,
		ProjectID: &project.ID,
		TeamID:    project.TeamID,
		Members:   project.Members,
		CreatedBy: &requester.ID,
	}
	if err := a.Chat.CreateChannel(ctx, channel); err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	a.Audit.Record(r, requester, audit.Event{
		Action: audit.ActionProjectCreated, ResourceType: "project", ResourceID: project.ID.String(),
		Changes: map[string]any{"name": project.Name, "key": project.Key},
		Status:  http.StatusCreated,
	})

	created, err := a.Projects.ByID(ctx, project.ID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusCreated, a.populateProject(ctx, *created))
}

// updateProjectRequest is an explicit allow-list. The handler once decoded
// into a map and passed it straight to the update, which let a caller rewrite
// any field at all - reassigning the owner, forging timestamps, or injecting
// keys outside the schema.
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

// PUT /api/projects/:id
func (a *API) UpdateProject(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.UUIDParam(w, r, "id")
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

	patch := repo.ProjectPatch{
		Name:        req.Name,
		Description: req.Description,
		Color:       req.Color,
	}
	if req.Name != nil && *req.Name == "" {
		httpx.Error(w, http.StatusBadRequest, "Project name cannot be empty.")
		return
	}
	if req.Status != nil {
		if !models.ValidProjectStatus(*req.Status) {
			httpx.Error(w, http.StatusBadRequest, "Invalid project status.")
			return
		}
		patch.Status = req.Status
	}
	if req.Team != nil {
		teamID, valid := httpx.OptionalUUID(*req.Team)
		if !valid {
			httpx.Error(w, http.StatusBadRequest, "Invalid team id.")
			return
		}
		patch.TeamID = &teamID
	}
	if req.MemberIDs != nil {
		// The owner always stays a member; otherwise they could edit
		// themselves out of their own project and lose access to it.
		owner := []uuid.UUID{}
		if project.Owner != nil {
			owner = append(owner, *project.Owner)
		}
		members := uniqueIDs(owner, httpx.UUIDs(*req.MemberIDs))
		patch.Members = &members
	}
	if req.StartDate != nil {
		start := req.StartDate
		patch.StartDate = &start
	}
	if req.EndDate != nil {
		end := req.EndDate
		patch.EndDate = &end
	}
	if req.Statuses != nil {
		if len(*req.Statuses) == 0 {
			httpx.Error(w, http.StatusBadRequest, "A project needs at least one status column.")
			return
		}
		patch.Statuses = req.Statuses
	}

	// "key" and "owner" are intentionally absent: the key is the project's
	// stable identifier and ownership transfer deserves its own audited
	// endpoint rather than riding along in a generic update.

	if err := a.Projects.Update(r.Context(), id, patch); err != nil {
		respondRepoError(w, err, "Project not found.")
		return
	}

	a.Audit.Record(r, auth.CurrentUser(r), audit.Event{
		Action: audit.ActionProjectUpdated, ResourceType: "project", ResourceID: id.String(),
		Changes: audit.Diff(
			map[string]any{"name": project.Name, "status": project.Status},
			map[string]any{"name": deref(req.Name, project.Name), "status": deref(req.Status, project.Status)},
		),
		Status: http.StatusOK,
	})

	updated, err := a.Projects.ByID(r.Context(), id)
	if err != nil {
		respondRepoError(w, err, "Project not found.")
		return
	}
	httpx.JSON(w, http.StatusOK, a.populateProject(r.Context(), *updated))
}

// DELETE /api/projects/:id
//
// Tasks, status columns, memberships and the project's chat channel are
// removed by ON DELETE CASCADE, atomically - the document version issued three
// independent deletes and could leave orphans if one failed.
func (a *API) DeleteProject(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.UUIDParam(w, r, "id")
	if !ok {
		return
	}
	project, ok := a.requireProjectManage(w, r, id)
	if !ok {
		return
	}
	if err := a.Projects.Delete(r.Context(), id); err != nil {
		respondRepoError(w, err, "Project not found.")
		return
	}
	// Deleting a project cascades to every task in it, so the trail records
	// what was destroyed, not just that something was.
	a.Audit.Record(r, auth.CurrentUser(r), audit.Event{
		Action: audit.ActionProjectDeleted, ResourceType: "project", ResourceID: id.String(),
		Changes: map[string]any{"name": project.Name, "key": project.Key},
		Status:  http.StatusOK,
	})
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}
