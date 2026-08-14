package handlers

import (
	"net/http"

	"projectview/internal/auth"
	"projectview/internal/httpx"
)

// GET /api/notifications
func (a *API) ListNotifications(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	notifications, err := a.Notifications.ForUser(r.Context(), user.ID, 100)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, notifications)
}

// POST /api/notifications/:id/read
//
// Scoped to the caller's own rows, so a guessed id cannot flip somebody
// else's notification.
func (a *API) MarkNotificationRead(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.UUIDParam(w, r, "id")
	if !ok {
		return
	}
	user := auth.CurrentUser(r)
	if err := a.Notifications.MarkRead(r.Context(), id, user.ID); err != nil {
		respondRepoError(w, err, "Notification not found.")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// POST /api/notifications/read-all
func (a *API) MarkAllNotificationsRead(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	if err := a.Notifications.MarkAllRead(r.Context(), user.ID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}
