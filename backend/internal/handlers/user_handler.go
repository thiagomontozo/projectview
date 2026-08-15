package handlers

import (
	"net/http"

	"projectview/internal/audit"
	"projectview/internal/auth"
	"projectview/internal/httpx"
	"projectview/internal/logger"
	"projectview/internal/models"
	"projectview/internal/repo"
)

// GET /api/users
func (a *API) ListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := a.Users.ListActive(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	for i := range users {
		users[i].PasswordHash = ""
	}
	httpx.JSON(w, http.StatusOK, users)
}

// GET /api/users/:id
func (a *API) GetUser(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.UUIDParam(w, r, "id")
	if !ok {
		return
	}
	user, err := a.Users.ByID(r.Context(), id)
	if err != nil {
		respondRepoError(w, err, "User not found.")
		return
	}
	user.PasswordHash = ""
	httpx.JSON(w, http.StatusOK, user)
}

type updateUserRequest struct {
	Name          *string `json:"name"`
	Title         *string `json:"title"`
	AvatarColor   *string `json:"avatarColor"`
	NotifyByEmail *bool   `json:"notifyByEmail"`
	Role          *string `json:"role"`
	Active        *bool   `json:"active"`
}

// PUT /api/users/:id
func (a *API) UpdateUser(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.UUIDParam(w, r, "id")
	if !ok {
		return
	}

	requester := auth.CurrentUser(r)
	if !canEditUser(id, requester) {
		httpx.Error(w, http.StatusForbidden, "You can only edit your own profile.")
		return
	}

	var req updateUserRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}

	patch := repo.UserPatch{
		Name:          req.Name,
		Title:         req.Title,
		AvatarColor:   req.AvatarColor,
		NotifyByEmail: req.NotifyByEmail,
	}
	// Role and activation are administrative, even on your own account.
	if isAdmin(requester) {
		if req.Role != nil {
			if !models.ValidRole(*req.Role) {
				httpx.Error(w, http.StatusBadRequest, "Invalid role.")
				return
			}
			patch.Role = req.Role
		}
		patch.Active = req.Active
	}

	if err := a.Users.Update(r.Context(), id, patch); err != nil {
		respondRepoError(w, err, "User not found.")
		return
	}

	// Deactivating an account must end its live sessions at once. Without
	// this the user keeps working until their token happens to expire, which
	// is exactly the window an offboarding is meant to close.
	if patch.Active != nil && !*patch.Active {
		if n, err := a.Sessions.RevokeAllForUser(r.Context(), id); err == nil && n > 0 {
			logger.Info("Revoked %d session(s) for deactivated user %s", n, id)
		}
		a.Audit.Record(r, requester, audit.Event{
			Action: audit.ActionUserDeactivated, ResourceType: "user", ResourceID: id.String(),
			Status: http.StatusOK,
		})
	}
	if patch.Role != nil {
		a.Audit.Record(r, requester, audit.Event{
			Action: audit.ActionRoleChanged, ResourceType: "user", ResourceID: id.String(),
			Changes: map[string]any{"role": *patch.Role}, Status: http.StatusOK,
		})
	}
	a.Audit.Record(r, requester, audit.Event{
		Action: audit.ActionUserUpdated, ResourceType: "user", ResourceID: id.String(),
		Changes: map[string]any{"fields": changedFields(req)}, Status: http.StatusOK,
	})

	user, err := a.Users.ByID(r.Context(), id)
	if err != nil {
		respondRepoError(w, err, "User not found.")
		return
	}
	user.PasswordHash = ""
	httpx.JSON(w, http.StatusOK, user)
}

type changePasswordRequest struct {
	CurrentPassword string `json:"currentPassword"`
	Password        string `json:"password"`
}

const (
	minPasswordLength  = 8
	minPasswordMessage = "Password must be at least 8 characters."
)

// POST /api/users/:id/password
//
// Two distinct flows share this endpoint:
//
//   - Self-service: you must prove possession of the current password, so a
//     stolen session cannot lock the real owner out of their own account.
//   - Administrative reset: an admin sets someone else's password without
//     knowing the old one.
//
// Anything else is refused. This endpoint once performed no check at all, so
// any authenticated account could overwrite the administrator's password.
func (a *API) ChangePassword(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.UUIDParam(w, r, "id")
	if !ok {
		return
	}

	requester := auth.CurrentUser(r)
	isSelf := requester != nil && requester.ID == id
	if !isSelf && !isAdmin(requester) {
		httpx.Error(w, http.StatusForbidden, "You can only change your own password.")
		return
	}

	var req changePasswordRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if len(req.Password) < minPasswordLength {
		httpx.Error(w, http.StatusBadRequest, minPasswordMessage)
		return
	}

	if isSelf {
		if requester.PasswordHash == "" {
			httpx.Error(w, http.StatusBadRequest,
				"This account signs in through Active Directory; its password is managed there.")
			return
		}
		if ok, _ := auth.VerifyPassword(requester.PasswordHash, req.CurrentPassword); !ok {
			httpx.Error(w, http.StatusUnauthorized, "Current password is incorrect.")
			return
		}
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := a.Users.SetPassword(r.Context(), id, hash); err != nil {
		respondRepoError(w, err, "User not found.")
		return
	}

	// A password change ends the account's other sessions. If the change was
	// prompted by a suspected compromise, leaving the attacker's session alive
	// would defeat the point. Someone changing their own password keeps the
	// browser they did it from; an admin reset signs the user out everywhere.
	var revoked int64
	if currentSession, ok := auth.CurrentSession(r); ok && isSelf {
		revoked, _ = a.Sessions.RevokeAllForUserExcept(r.Context(), id, currentSession)
	} else {
		revoked, _ = a.Sessions.RevokeAllForUser(r.Context(), id)
	}
	if revoked > 0 {
		logger.Info("Revoked %d session(s) after password change for %s", revoked, id)
	}

	a.Audit.Record(r, requester, audit.Event{
		Action: audit.ActionPasswordChanged, ResourceType: "user", ResourceID: id.String(),
		Changes: map[string]any{"self": isSelf}, Status: http.StatusOK,
	})

	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// changedFields lists which profile fields an update touched, for the audit
// trail. Values are deliberately omitted here - the field names are enough to
// answer "what changed", without copying personal data into the trail.
func changedFields(req updateUserRequest) []string {
	fields := []string{}
	if req.Name != nil {
		fields = append(fields, "name")
	}
	if req.Title != nil {
		fields = append(fields, "title")
	}
	if req.AvatarColor != nil {
		fields = append(fields, "avatarColor")
	}
	if req.NotifyByEmail != nil {
		fields = append(fields, "notifyByEmail")
	}
	if req.Role != nil {
		fields = append(fields, "role")
	}
	if req.Active != nil {
		fields = append(fields, "active")
	}
	return fields
}

type workloadRow struct {
	User          models.User `json:"user"`
	OpenTasks     int64       `json:"openTasks"`
	EstimateHours float64     `json:"estimateHours"`
	Overdue       int64       `json:"overdue"`
	ProjectCount  int         `json:"projectCount"`
}

// GET /api/users/workload - resource allocation view: workload per user.
func (a *API) Workload(w http.ResponseWriter, r *http.Request) {
	rows, err := a.Users.Workload(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]workloadRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, workloadRow{
			User:          row.User,
			OpenTasks:     row.OpenTasks,
			EstimateHours: row.EstimateHours,
			Overdue:       row.Overdue,
			ProjectCount:  row.ProjectCount,
		})
	}
	httpx.JSON(w, http.StatusOK, out)
}
