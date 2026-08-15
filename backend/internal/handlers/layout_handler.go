package handlers

import (
	"errors"
	"net/http"

	"projectview/internal/auth"
	"projectview/internal/httpx"
	"projectview/internal/repo"
)

// The dashboard layout used to live in the browser's local storage, which
// meant it did not follow anyone to a second machine and vanished with a
// cleared cache. It is a per-user preference, so it belongs with the user.

// GET /api/dashboard/layout
func (a *API) GetDashboardLayout(w http.ResponseWriter, r *http.Request) {
	requester := auth.CurrentUser(r)
	layout, err := a.Layouts.DefaultFor(r.Context(), requester.ID)
	if errors.Is(err, repo.ErrNotFound) {
		// Absent, not empty: an empty layout would render a dashboard with no
		// cards at all, and the frontend cannot tell the two apart otherwise.
		httpx.JSON(w, http.StatusOK, map[string]any{"layout": nil})
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, layout)
}

type layoutRequest struct {
	Name   string        `json:"name"`
	Layout []repo.Widget `json:"layout"`
}

// PUT /api/dashboard/layout
func (a *API) SaveDashboardLayout(w http.ResponseWriter, r *http.Request) {
	var req layoutRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	// A cap rather than a schema: the server has no business knowing which
	// cards exist, but it does have business refusing a layout that is really
	// a way to store a megabyte of JSON per user.
	if len(req.Layout) > 64 {
		httpx.Error(w, http.StatusBadRequest, "A dashboard holds at most 64 cards.")
		return
	}
	for _, widget := range req.Layout {
		if widget.ID == "" || len(widget.ID) > 64 || len(widget.Type) > 64 {
			httpx.Error(w, http.StatusBadRequest, "Every card needs a short id and type.")
			return
		}
	}

	requester := auth.CurrentUser(r)
	saved, err := a.Layouts.Save(r.Context(), requester.ID, req.Name, req.Layout)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, saved)
}

// DELETE /api/dashboard/layout - back to the default arrangement.
func (a *API) ResetDashboardLayout(w http.ResponseWriter, r *http.Request) {
	requester := auth.CurrentUser(r)
	if err := a.Layouts.Delete(r.Context(), requester.ID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}
