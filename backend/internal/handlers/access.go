package handlers

import (
	"errors"
	"net/http"

	"github.com/google/uuid"

	"projectview/internal/auth"
	"projectview/internal/httpx"
	"projectview/internal/models"
	"projectview/internal/repo"
)

// Authorization model
// ===================
//
// Authentication proves *who* is calling. These rules decide *what* they may
// do, in increasing order of privilege:
//
//	canAdministerStructure  create projects and teams
//	canWorkOnProject        create/edit/move/delete tasks, comment
//	canManageProject        rename, reconfigure or delete the project itself
//	isAdmin                 everything, including user administration
//
// Reads of projects, tasks and teams stay open to any authenticated user: this
// is an internal corporate tool where work is meant to be visible across the
// organization. Chat is the exception - it carries private conversation, so
// both reading and writing require channel membership, enforced at the query
// level by repo.Chat.ChannelForMember.
//
// Every predicate is a pure function of already-loaded documents, so the whole
// role x resource x action matrix is unit-testable without a database. See
// access_test.go.

const forbiddenMessage = "You do not have permission to perform this action."

func isAdmin(u *models.User) bool {
	return u != nil && u.Role == models.RoleAdmin
}

// canAdministerStructure gates creating the containers that work lives in.
// Members join existing structure; they do not open new workstreams.
func canAdministerStructure(u *models.User) bool {
	return u != nil && (u.Role == models.RoleAdmin || u.Role == models.RoleManager)
}

// isProjectMember reports raw membership, ignoring role. The owner counts as a
// member even if absent from the members list.
func isProjectMember(p *models.Project, u *models.User) bool {
	if p == nil || u == nil {
		return false
	}
	if p.Owner != nil && *p.Owner == u.ID {
		return true
	}
	for _, m := range p.Members {
		if m == u.ID {
			return true
		}
	}
	return false
}

// canWorkOnProject decides who may create and change tasks inside a project.
func canWorkOnProject(p *models.Project, u *models.User) bool {
	return isAdmin(u) || isProjectMember(p, u)
}

// canManageProject is deliberately narrower than canWorkOnProject: deleting a
// project cascades to every task in it, so a plain member must not be able to
// wipe out everyone else's work.
func canManageProject(p *models.Project, u *models.User) bool {
	if isAdmin(u) {
		return true
	}
	if p == nil || u == nil {
		return false
	}
	if p.Owner != nil && *p.Owner == u.ID {
		return true
	}
	return u.Role == models.RoleManager && isProjectMember(p, u)
}

// canManageTeam allows admins and the team's own lead.
func canManageTeam(t *models.Team, u *models.User) bool {
	if isAdmin(u) {
		return true
	}
	if t == nil || u == nil {
		return false
	}
	return t.LeadID != nil && *t.LeadID == u.ID
}

// canEditUser allows people to edit their own profile, and admins to edit
// anyone's. Role and active-flag changes are additionally admin-gated inside
// the handler.
func canEditUser(targetID uuid.UUID, u *models.User) bool {
	return isAdmin(u) || (u != nil && u.ID == targetID)
}

// ---------------------------------------------------------------------------
// Loaders. Each writes its own error response and returns ok=false, so callers
// can simply `return`.
// ---------------------------------------------------------------------------

// respondRepoError maps a repository error onto the right status code.
func respondRepoError(w http.ResponseWriter, err error, notFound string) {
	if errors.Is(err, repo.ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, notFound)
		return
	}
	if repo.IsUniqueViolation(err) {
		httpx.Error(w, http.StatusConflict, "That value is already taken.")
		return
	}
	httpx.Error(w, http.StatusInternalServerError, err.Error())
}

func (a *API) loadProject(w http.ResponseWriter, r *http.Request, id uuid.UUID) (*models.Project, bool) {
	p, err := a.Projects.ByID(r.Context(), id)
	if err != nil {
		respondRepoError(w, err, "Project not found.")
		return nil, false
	}
	return p, true
}

// requireProjectWork loads a project and stops the request unless the caller
// may change the work inside it.
func (a *API) requireProjectWork(w http.ResponseWriter, r *http.Request, id uuid.UUID) (*models.Project, bool) {
	p, ok := a.loadProject(w, r, id)
	if !ok {
		return nil, false
	}
	if !canWorkOnProject(p, auth.CurrentUser(r)) {
		httpx.Error(w, http.StatusForbidden, forbiddenMessage)
		return nil, false
	}
	return p, true
}

// requireProjectManage loads a project and stops the request unless the caller
// may reconfigure or delete the project itself.
func (a *API) requireProjectManage(w http.ResponseWriter, r *http.Request, id uuid.UUID) (*models.Project, bool) {
	p, ok := a.loadProject(w, r, id)
	if !ok {
		return nil, false
	}
	if !canManageProject(p, auth.CurrentUser(r)) {
		httpx.Error(w, http.StatusForbidden, forbiddenMessage)
		return nil, false
	}
	return p, true
}

// requireTaskWork resolves a task together with the project owning it, and
// authorizes the caller against that project. Task permissions are always
// inherited from the project - a task never carries its own ACL.
func (a *API) requireTaskWork(w http.ResponseWriter, r *http.Request, taskID uuid.UUID) (*models.Task, *models.Project, bool) {
	t, err := a.Tasks.ByID(r.Context(), taskID)
	if err != nil {
		respondRepoError(w, err, "Task not found.")
		return nil, nil, false
	}
	p, ok := a.requireProjectWork(w, r, t.ProjectID)
	if !ok {
		return nil, nil, false
	}
	return t, p, true
}

// requireTeamManage loads a team and stops the request unless the caller may
// administer it.
func (a *API) requireTeamManage(w http.ResponseWriter, r *http.Request, id uuid.UUID) (*models.Team, bool) {
	t, err := a.Teams.ByID(r.Context(), id)
	if err != nil {
		respondRepoError(w, err, "Team not found.")
		return nil, false
	}
	if !canManageTeam(t, auth.CurrentUser(r)) {
		httpx.Error(w, http.StatusForbidden, forbiddenMessage)
		return nil, false
	}
	return t, true
}
