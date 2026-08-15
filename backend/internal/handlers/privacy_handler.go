package handlers

import (
	"fmt"
	"net/http"

	"projectview/internal/audit"
	"projectview/internal/auth"
	"projectview/internal/httpx"
)

// GET /api/users/:id/data-export
//
// Anyone may export their own record; only an administrator may export
// somebody else's, and doing so is itself recorded - an unlogged way to read
// everything about a colleague is a surveillance feature, not a privacy one.
func (a *API) ExportPersonalData(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.UUIDParam(w, r, "id")
	if !ok {
		return
	}
	requester := auth.CurrentUser(r)
	if requester.ID != id && !isAdmin(requester) {
		httpx.Error(w, http.StatusForbidden, forbiddenMessage)
		return
	}

	export, err := a.Privacy.Export(r.Context(), id)
	if err != nil {
		respondRepoError(w, err, "User not found.")
		return
	}

	a.Audit.Record(r, requester, audit.Event{
		Action: audit.ActionDataExported, ResourceType: "user", ResourceID: id.String(),
		Changes: map[string]any{"self": requester.ID == id}, Status: http.StatusOK,
	})

	// Offered as a download: the point of the export is that someone can keep
	// a copy, not that they can look at JSON in a browser tab.
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="projectview-export-%s.json"`, id))
	httpx.JSON(w, http.StatusOK, export)
}

type erasureRequest struct {
	// Confirm must repeat the username. Erasure is irreversible, and a button
	// that fires on a single click will eventually be clicked by accident.
	Confirm string `json:"confirm"`
}

// POST /api/users/:id/erase - anonymises the account.
func (a *API) ErasePersonalData(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.UUIDParam(w, r, "id")
	if !ok {
		return
	}
	requester := auth.CurrentUser(r)
	if !isAdmin(requester) {
		httpx.Error(w, http.StatusForbidden, forbiddenMessage)
		return
	}

	target, err := a.Users.ByID(r.Context(), id)
	if err != nil {
		respondRepoError(w, err, "User not found.")
		return
	}
	// An administrator erasing themselves would lock the last administrator
	// out of their own installation.
	if target.ID == requester.ID {
		httpx.Error(w, http.StatusBadRequest, "Erase your account from another administrator's session.")
		return
	}

	var req erasureRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.Confirm != target.Username {
		httpx.Error(w, http.StatusBadRequest, "Confirm the erasure by repeating the username.")
		return
	}

	if err := a.Privacy.Anonymize(r.Context(), id); err != nil {
		respondRepoError(w, err, "User not found.")
		return
	}

	// Recorded before the response, and deliberately without the old name: the
	// audit trail must show that an erasure happened without undoing it.
	a.Audit.Record(r, requester, audit.Event{
		Action: audit.ActionUserErased, ResourceType: "user", ResourceID: id.String(),
		Status: http.StatusOK,
	})
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}
