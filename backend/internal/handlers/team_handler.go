package handlers

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"projectview/internal/auth"
	"projectview/internal/httpx"
	"projectview/internal/models"
	"projectview/internal/repo"
)

type teamResponse struct {
	models.Team
	MembersPopulated []PublicUser `json:"members"`
	Lead             *PublicUser  `json:"leadId,omitempty"`
}

// populateTeams resolves every member of every team with one users query.
func (a *API) populateTeams(ctx context.Context, teams []models.Team) []teamResponse {
	ids := []uuid.UUID{}
	for _, t := range teams {
		ids = append(ids, t.Members...)
		if t.LeadID != nil {
			ids = append(ids, *t.LeadID)
		}
	}
	users := a.usersByID(ctx, uniqueIDs(ids))

	out := make([]teamResponse, 0, len(teams))
	for _, t := range teams {
		resp := teamResponse{Team: t, MembersPopulated: publicList(users, t.Members)}
		if t.LeadID != nil {
			if u, ok := users[*t.LeadID]; ok {
				resp.Lead = &u
			}
		}
		out = append(out, resp)
	}
	return out
}

func (a *API) populateTeam(ctx context.Context, t models.Team) teamResponse {
	return a.populateTeams(ctx, []models.Team{t})[0]
}

// GET /api/teams
func (a *API) ListTeams(w http.ResponseWriter, r *http.Request) {
	teams, err := a.Teams.List(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, a.populateTeams(r.Context(), teams))
}

// GET /api/teams/:id
func (a *API) GetTeam(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.UUIDParam(w, r, "id")
	if !ok {
		return
	}
	t, err := a.Teams.ByID(r.Context(), id)
	if err != nil {
		respondRepoError(w, err, "Team not found.")
		return
	}
	httpx.JSON(w, http.StatusOK, a.populateTeam(r.Context(), *t))
}

type createTeamRequest struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Color       string   `json:"color"`
	LeadID      string   `json:"leadId"`
	MemberIDs   []string `json:"memberIds"`
}

// POST /api/teams
func (a *API) CreateTeam(w http.ResponseWriter, r *http.Request) {
	var req createTeamRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.Name == "" {
		httpx.Error(w, http.StatusBadRequest, "Team name is required.")
		return
	}

	requester := auth.CurrentUser(r)
	if !canAdministerStructure(requester) {
		httpx.Error(w, http.StatusForbidden, "Only managers and administrators can create teams.")
		return
	}

	color := req.Color
	if color == "" {
		color = "#0ea5e9"
	}
	team := &models.Team{
		ID:          uuid.New(),
		Name:        req.Name,
		Description: req.Description,
		Color:       color,
		Members:     httpx.UUIDs(req.MemberIDs),
		CreatedBy:   &requester.ID,
	}
	if leadID, ok := httpx.OptionalUUID(req.LeadID); ok && leadID != nil {
		team.LeadID = leadID
	}

	ctx := r.Context()
	if err := a.Teams.Create(ctx, team); err != nil {
		if repo.IsUniqueViolation(err) {
			httpx.Error(w, http.StatusConflict, "A team with that name already exists.")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	created, err := a.Teams.ByID(ctx, team.ID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusCreated, a.populateTeam(ctx, *created))
}

// updateTeamRequest is an explicit allow-list, for the same reason as
// updateProjectRequest: the previous version accepted any field.
type updateTeamRequest struct {
	Name        *string   `json:"name"`
	Description *string   `json:"description"`
	Color       *string   `json:"color"`
	LeadID      *string   `json:"leadId"`
	MemberIDs   *[]string `json:"memberIds"`
}

// PUT /api/teams/:id
func (a *API) UpdateTeam(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.UUIDParam(w, r, "id")
	if !ok {
		return
	}
	if _, ok := a.requireTeamManage(w, r, id); !ok {
		return
	}

	var req updateTeamRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.Name != nil && *req.Name == "" {
		httpx.Error(w, http.StatusBadRequest, "Team name cannot be empty.")
		return
	}

	patch := repo.TeamPatch{Name: req.Name, Description: req.Description, Color: req.Color}
	if req.LeadID != nil {
		leadID, valid := httpx.OptionalUUID(*req.LeadID)
		if !valid {
			httpx.Error(w, http.StatusBadRequest, "Invalid leadId.")
			return
		}
		patch.LeadID = &leadID
	}
	if req.MemberIDs != nil {
		members := httpx.UUIDs(*req.MemberIDs)
		patch.Members = &members
	}

	if err := a.Teams.Update(r.Context(), id, patch); err != nil {
		respondRepoError(w, err, "Team not found.")
		return
	}
	updated, err := a.Teams.ByID(r.Context(), id)
	if err != nil {
		respondRepoError(w, err, "Team not found.")
		return
	}
	httpx.JSON(w, http.StatusOK, a.populateTeam(r.Context(), *updated))
}

// DELETE /api/teams/:id
func (a *API) DeleteTeam(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.UUIDParam(w, r, "id")
	if !ok {
		return
	}
	if !isAdmin(auth.CurrentUser(r)) {
		httpx.Error(w, http.StatusForbidden, "Only administrators can delete teams.")
		return
	}
	if err := a.Teams.Delete(r.Context(), id); err != nil {
		respondRepoError(w, err, "Team not found.")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type memberRequest struct {
	UserID string `json:"userId"`
}

// POST /api/teams/:id/members
func (a *API) AddTeamMember(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.UUIDParam(w, r, "id")
	if !ok {
		return
	}
	if _, ok := a.requireTeamManage(w, r, id); !ok {
		return
	}
	var req memberRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	userID, err := uuid.Parse(req.UserID)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "Invalid userId.")
		return
	}
	if err := a.Teams.AddMember(r.Context(), id, userID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.respondTeam(w, r, id)
}

// DELETE /api/teams/:id/members/:userId
func (a *API) RemoveTeamMember(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.UUIDParam(w, r, "id")
	if !ok {
		return
	}
	userID, ok := httpx.UUIDParam(w, r, "userId")
	if !ok {
		return
	}
	if _, ok := a.requireTeamManage(w, r, id); !ok {
		return
	}
	if err := a.Teams.RemoveMember(r.Context(), id, userID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.respondTeam(w, r, id)
}

func (a *API) respondTeam(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	t, err := a.Teams.ByID(r.Context(), id)
	if err != nil {
		respondRepoError(w, err, "Team not found.")
		return
	}
	httpx.JSON(w, http.StatusOK, a.populateTeam(r.Context(), *t))
}
