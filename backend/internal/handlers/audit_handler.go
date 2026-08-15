package handlers

import (
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"projectview/internal/httpx"
	"projectview/internal/repo"
)

// GET /api/audit
//
// Admin-only: the trail records who did what across the whole installation,
// including failed logins, so it is not something ordinary users browse.
//
// Filters: actor, resourceType, resourceId, action, since. Paginated by
// cursor over the descending id.
func (a *API) ListAudit(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	query := repo.AuditQuery{
		ResourceType: q.Get("resourceType"),
		ResourceID:   q.Get("resourceId"),
		Action:       q.Get("action"),
	}

	if raw := q.Get("actor"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, "Invalid actor id.")
			return
		}
		query.ActorID = &id
	}
	if raw := q.Get("since"); raw != "" {
		since, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, "Invalid 'since': expected an RFC3339 timestamp.")
			return
		}
		query.Since = &since
	}
	if raw := q.Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			query.Limit = n
		}
	}
	if raw := q.Get("cursor"); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, "Invalid cursor.")
			return
		}
		query.Cursor = &n
	}

	limit := query.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	query.Limit = limit + 1 // over-fetch by one to detect a further page

	entries, err := a.AuditLog.List(r.Context(), query)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	page := httpx.NewPage(entries, limit, func(e repo.Entry) string {
		return strconv.FormatInt(e.ID, 10)
	})
	httpx.JSON(w, http.StatusOK, page)
}
