package handlers

import (
	"context"
	"net/http"
	"time"

	"projectview/internal/httpx"
)

func contextWithTimeout(r *http.Request, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), d)
}

// GET /api/ready
//
// Readiness, as distinct from liveness. /api/health answers as long as the
// process is up; this one actually reaches the database, so an orchestrator
// can stop sending traffic to an instance whose dependencies are gone instead
// of returning errors to users.
func (a *API) Ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r, 2*time.Second)
	defer cancel()

	if err := a.Store.Pool.Ping(ctx); err != nil {
		if a.Metrics != nil {
			a.Metrics.RecordDBFailure()
		}
		httpx.JSON(w, http.StatusServiceUnavailable, map[string]any{
			"status":   "unavailable",
			"database": "unreachable",
		})
		return
	}

	httpx.JSON(w, http.StatusOK, map[string]any{
		"status":   "ready",
		"database": "ok",
	})
}
