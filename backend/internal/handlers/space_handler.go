package handlers

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"projectview/internal/audit"
	"projectview/internal/auth"
	"projectview/internal/httpx"
	"projectview/internal/repo"
)

type spaceMemberResponse struct {
	User PublicUser `json:"user"`
	Role string     `json:"role"`
}

type spaceResponse struct {
	repo.Space
	Members []spaceMemberResponse `json:"members"`
	// YourRole is the caller's effective role, so a client can hide actions it
	// would only be refused for.
	YourRole string `json:"yourRole,omitempty"`
}

func (a *API) populateSpaces(ctx context.Context, spaces []repo.Space, viewer uuid.UUID) []spaceResponse {
	out := make([]spaceResponse, 0, len(spaces))
	for _, s := range spaces {
		resp := spaceResponse{Space: s, Members: []spaceMemberResponse{}}

		members, err := a.Spaces.Members(ctx, s.ID)
		if err == nil {
			ids := make([]uuid.UUID, 0, len(members))
			for _, m := range members {
				ids = append(ids, m.UserID)
			}
			users := a.usersByID(ctx, ids)
			for _, m := range members {
				if u, ok := users[m.UserID]; ok {
					resp.Members = append(resp.Members, spaceMemberResponse{User: u, Role: m.Role})
				}
				if m.UserID == viewer {
					resp.YourRole = m.Role
				}
			}
		}
		out = append(out, resp)
	}
	return out
}

// GET /api/spaces
func (a *API) ListSpaces(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	includeArchived := r.URL.Query().Get("archived") == "true"

	spaces, err := a.Spaces.VisibleTo(r.Context(), user.ID, isAdmin(user), includeArchived)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, a.populateSpaces(r.Context(), spaces, user.ID))
}

// GET /api/spaces/:id
func (a *API) GetSpace(w http.ResponseWriter, r *http.Request) {
	space, ok := a.requireSpaceRead(w, r)
	if !ok {
		return
	}
	user := auth.CurrentUser(r)
	httpx.JSON(w, http.StatusOK, a.populateSpaces(r.Context(), []repo.Space{*space}, user.ID)[0])
}

type createSpaceRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Color       string `json:"color"`
	IsPrivate   bool   `json:"isPrivate"`
	Team        string `json:"team"`
	Members     []struct {
		UserID string `json:"userId"`
		Role   string `json:"role"`
	} `json:"members"`
}

// POST /api/spaces
func (a *API) CreateSpace(w http.ResponseWriter, r *http.Request) {
	requester := auth.CurrentUser(r)
	if !canAdministerStructure(requester) {
		httpx.Error(w, http.StatusForbidden, "Only managers and administrators can create spaces.")
		return
	}

	var req createSpaceRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.Name == "" {
		httpx.Error(w, http.StatusBadRequest, "Space name is required.")
		return
	}

	color := req.Color
	if color == "" {
		color = "#2a78d6"
	}
	space := &repo.Space{
		ID: uuid.New(), Name: req.Name, Description: req.Description,
		Color: color, IsPrivate: req.IsPrivate, CreatedBy: &requester.ID,
	}
	if teamID, ok := httpx.OptionalUUID(req.Team); ok && teamID != nil {
		space.TeamID = teamID
	}

	// The creator always owns the space; otherwise a private space could be
	// created that nobody can administer.
	members := []repo.SpaceMember{{UserID: requester.ID, Role: repo.SpaceRoleOwner}}
	for _, m := range req.Members {
		id, err := uuid.Parse(m.UserID)
		if err != nil || id == requester.ID {
			continue
		}
		role := m.Role
		if !repo.ValidSpaceRole(role) {
			role = repo.SpaceRoleMember
		}
		members = append(members, repo.SpaceMember{UserID: id, Role: role})
	}

	if err := a.Spaces.Create(r.Context(), space, members); err != nil {
		if repo.IsUniqueViolation(err) {
			httpx.Error(w, http.StatusConflict, "A space with that name already exists.")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	a.Audit.Record(r, requester, audit.Event{
		Action: audit.ActionSpaceCreated, ResourceType: "space", ResourceID: space.ID.String(),
		Changes: map[string]any{"name": space.Name, "isPrivate": space.IsPrivate},
		Status:  http.StatusCreated,
	})

	httpx.JSON(w, http.StatusCreated, a.populateSpaces(r.Context(), []repo.Space{*space}, requester.ID)[0])
}

type updateSpaceRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Color       *string `json:"color"`
	IsPrivate   *bool   `json:"isPrivate"`
	Position    *int    `json:"position"`
	Archived    *bool   `json:"archived"`
}

// PUT /api/spaces/:id
func (a *API) UpdateSpace(w http.ResponseWriter, r *http.Request) {
	space, ok := a.requireSpaceRole(w, r, repo.SpaceRoleAdmin)
	if !ok {
		return
	}
	var req updateSpaceRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.Name != nil && *req.Name == "" {
		httpx.Error(w, http.StatusBadRequest, "Space name cannot be empty.")
		return
	}

	patch := repo.SpacePatch{
		Name: req.Name, Description: req.Description, Color: req.Color,
		IsPrivate: req.IsPrivate, Position: req.Position, Archived: req.Archived,
	}
	if err := a.Spaces.Update(r.Context(), space.ID, patch); err != nil {
		respondRepoError(w, err, "Space not found.")
		return
	}

	a.Audit.Record(r, auth.CurrentUser(r), audit.Event{
		Action: audit.ActionSpaceUpdated, ResourceType: "space", ResourceID: space.ID.String(),
		Changes: audit.Diff(
			map[string]any{"name": space.Name, "isPrivate": space.IsPrivate, "archived": space.Archived},
			map[string]any{"name": deref(req.Name, space.Name),
				"isPrivate": derefBool(req.IsPrivate, space.IsPrivate),
				"archived":  derefBool(req.Archived, space.Archived)},
		),
		Status: http.StatusOK,
	})

	updated, err := a.Spaces.ByID(r.Context(), space.ID)
	if err != nil {
		respondRepoError(w, err, "Space not found.")
		return
	}
	httpx.JSON(w, http.StatusOK, a.populateSpaces(r.Context(), []repo.Space{*updated}, auth.CurrentUser(r).ID)[0])
}

// DELETE /api/spaces/:id
//
// Deleting a space cascades to its folders, lists and every task inside them,
// so it is restricted to owners and administrators.
func (a *API) DeleteSpace(w http.ResponseWriter, r *http.Request) {
	space, ok := a.requireSpaceRole(w, r, repo.SpaceRoleOwner)
	if !ok {
		return
	}
	if err := a.Spaces.Delete(r.Context(), space.ID); err != nil {
		respondRepoError(w, err, "Space not found.")
		return
	}
	a.Audit.Record(r, auth.CurrentUser(r), audit.Event{
		Action: audit.ActionSpaceDeleted, ResourceType: "space", ResourceID: space.ID.String(),
		Changes: map[string]any{"name": space.Name}, Status: http.StatusOK,
	})
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type spaceMemberRequest struct {
	UserID string `json:"userId"`
	Role   string `json:"role"`
}

// POST /api/spaces/:id/members
func (a *API) SetSpaceMember(w http.ResponseWriter, r *http.Request) {
	space, ok := a.requireSpaceRole(w, r, repo.SpaceRoleAdmin)
	if !ok {
		return
	}
	var req spaceMemberRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	userID, err := uuid.Parse(req.UserID)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "Invalid userId.")
		return
	}
	role := req.Role
	if !repo.ValidSpaceRole(role) {
		httpx.Error(w, http.StatusBadRequest, "Invalid role. Expected owner, admin, member or guest.")
		return
	}

	if err := a.Spaces.SetMember(r.Context(), space.ID, userID, role); err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.Audit.Record(r, auth.CurrentUser(r), audit.Event{
		Action: audit.ActionSpaceUpdated, ResourceType: "space", ResourceID: space.ID.String(),
		Changes: map[string]any{"member": userID.String(), "role": role}, Status: http.StatusOK,
	})
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// DELETE /api/spaces/:id/members/:userId
func (a *API) RemoveSpaceMember(w http.ResponseWriter, r *http.Request) {
	space, ok := a.requireSpaceRole(w, r, repo.SpaceRoleAdmin)
	if !ok {
		return
	}
	userID, ok := httpx.UUIDParam(w, r, "userId")
	if !ok {
		return
	}
	if err := a.Spaces.RemoveMember(r.Context(), space.ID, userID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.Audit.Record(r, auth.CurrentUser(r), audit.Event{
		Action: audit.ActionSpaceUpdated, ResourceType: "space", ResourceID: space.ID.String(),
		Changes: map[string]any{"removedMember": userID.String()}, Status: http.StatusOK,
	})
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---------------------------------------------------------------------------
// Folders
// ---------------------------------------------------------------------------

// GET /api/spaces/:id/folders
func (a *API) ListFolders(w http.ResponseWriter, r *http.Request) {
	space, ok := a.requireSpaceRead(w, r)
	if !ok {
		return
	}
	folders, err := a.Folders.BySpace(r.Context(), space.ID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, folders)
}

type createFolderRequest struct {
	Name     string `json:"name"`
	Color    string `json:"color"`
	Position int    `json:"position"`
}

// POST /api/spaces/:id/folders
func (a *API) CreateFolder(w http.ResponseWriter, r *http.Request) {
	space, ok := a.requireSpaceRole(w, r, repo.SpaceRoleMember)
	if !ok {
		return
	}
	var req createFolderRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.Name == "" {
		httpx.Error(w, http.StatusBadRequest, "Folder name is required.")
		return
	}

	requester := auth.CurrentUser(r)
	color := req.Color
	if color == "" {
		color = "#94a3b8"
	}
	folder := &repo.Folder{
		ID: uuid.New(), SpaceID: space.ID, Name: req.Name,
		Color: color, Position: req.Position, CreatedBy: &requester.ID,
	}
	if err := a.Folders.Create(r.Context(), folder); err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.Audit.Record(r, requester, audit.Event{
		Action: audit.ActionFolderCreated, ResourceType: "folder", ResourceID: folder.ID.String(),
		Changes: map[string]any{"name": folder.Name, "spaceId": space.ID.String()},
		Status:  http.StatusCreated,
	})
	httpx.JSON(w, http.StatusCreated, folder)
}

type updateFolderRequest struct {
	Name     *string `json:"name"`
	Color    *string `json:"color"`
	Position *int    `json:"position"`
	Archived *bool   `json:"archived"`
}

// PUT /api/folders/:id
func (a *API) UpdateFolder(w http.ResponseWriter, r *http.Request) {
	folder, ok := a.requireFolderRole(w, r, repo.SpaceRoleMember)
	if !ok {
		return
	}
	var req updateFolderRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.Name != nil && *req.Name == "" {
		httpx.Error(w, http.StatusBadRequest, "Folder name cannot be empty.")
		return
	}

	if err := a.Folders.Update(r.Context(), folder.ID, repo.FolderPatch{
		Name: req.Name, Color: req.Color, Position: req.Position, Archived: req.Archived,
	}); err != nil {
		respondRepoError(w, err, "Folder not found.")
		return
	}
	a.Audit.Record(r, auth.CurrentUser(r), audit.Event{
		Action: audit.ActionFolderUpdated, ResourceType: "folder", ResourceID: folder.ID.String(),
		Status: http.StatusOK,
	})

	updated, err := a.Folders.ByID(r.Context(), folder.ID)
	if err != nil {
		respondRepoError(w, err, "Folder not found.")
		return
	}
	httpx.JSON(w, http.StatusOK, updated)
}

// DELETE /api/folders/:id
//
// The lists inside are kept and fall back to living directly under the space:
// losing a folder must never silently destroy the work it grouped.
func (a *API) DeleteFolder(w http.ResponseWriter, r *http.Request) {
	folder, ok := a.requireFolderRole(w, r, repo.SpaceRoleAdmin)
	if !ok {
		return
	}
	if err := a.Folders.Delete(r.Context(), folder.ID); err != nil {
		respondRepoError(w, err, "Folder not found.")
		return
	}
	a.Audit.Record(r, auth.CurrentUser(r), audit.Event{
		Action: audit.ActionFolderDeleted, ResourceType: "folder", ResourceID: folder.ID.String(),
		Changes: map[string]any{"name": folder.Name}, Status: http.StatusOK,
	})
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func deref(p *string, fallback string) string {
	if p != nil {
		return *p
	}
	return fallback
}

func derefBool(p *bool, fallback bool) bool {
	if p != nil {
		return *p
	}
	return fallback
}
