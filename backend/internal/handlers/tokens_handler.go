package handlers

import (
	"net/http"

	"projectview/internal/audit"
	"projectview/internal/auth"
	"projectview/internal/httpx"
	"projectview/internal/repo"
)

// GET /api/service-tokens
func (a *API) ListServiceTokens(w http.ResponseWriter, r *http.Request) {
	tokens, err := a.Tokens.List(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, tokens)
}

type serviceTokenRequest struct {
	Name   string   `json:"name"`
	Scopes []string `json:"scopes"`
}

// POST /api/service-tokens
//
// The response carries the only copy of the secret that will ever exist. Every
// later read returns the metadata alone, because a credential the database can
// hand back is a credential everyone with database access already holds.
func (a *API) CreateServiceToken(w http.ResponseWriter, r *http.Request) {
	var req serviceTokenRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.Name == "" {
		httpx.Error(w, http.StatusBadRequest, "Name the token after whatever will use it.")
		return
	}
	if len(req.Scopes) == 0 {
		httpx.Error(w, http.StatusBadRequest, "A token with no scopes can do nothing.")
		return
	}
	for _, scope := range req.Scopes {
		if !repo.ValidScope(scope) {
			httpx.Error(w, http.StatusBadRequest, "Unknown scope: "+scope)
			return
		}
	}

	requester := auth.CurrentUser(r)
	token, err := a.Tokens.Create(r.Context(), req.Name, req.Scopes, requester.ID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	a.Audit.Record(r, requester, audit.Event{
		Action: audit.ActionTokenCreated, ResourceType: "service_token", ResourceID: token.ID.String(),
		// The scopes, never the secret.
		Changes: map[string]any{"name": token.Name, "scopes": token.Scopes},
		Status:  http.StatusCreated,
	})
	httpx.JSON(w, http.StatusCreated, token)
}

// DELETE /api/service-tokens/:id
func (a *API) RevokeServiceToken(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.UUIDParam(w, r, "id")
	if !ok {
		return
	}
	if err := a.Tokens.Revoke(r.Context(), id); err != nil {
		respondRepoError(w, err, "Token not found.")
		return
	}
	a.Audit.Record(r, auth.CurrentUser(r), audit.Event{
		Action: audit.ActionTokenRevoked, ResourceType: "service_token", ResourceID: id.String(),
		Status: http.StatusOK,
	})
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}
