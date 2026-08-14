package handlers

import (
	"net/http"

	"projectview/internal/auth"
	"projectview/internal/httpx"
)

// ServeWS upgrades GET /ws?token=<jwt> to a WebSocket connection. The token
// is passed as a query parameter (instead of the Authorization header)
// because browsers cannot set custom headers on a WebSocket handshake.
func (a *API) ServeWS(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		httpx.Error(w, http.StatusUnauthorized, "Missing token.")
		return
	}
	claims, err := auth.ParseToken(a.Cfg, token)
	if err != nil {
		httpx.Error(w, http.StatusUnauthorized, "Invalid or expired token.")
		return
	}
	a.Hub.ServeWS(w, r, claims.Subject)
}
