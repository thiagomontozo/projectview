package handlers

import (
	"errors"
	"net/http"
	"strings"

	"projectview/internal/auth"
	"projectview/internal/httpx"
	"projectview/internal/models"
)

// Searching the directory, so somebody can be put on a team before they have
// ever signed in.
//
// Until this existed the only route into the local user table was just-in-time
// provisioning at first login, which put the cart before the horse: an
// administrator building a team could only pick from people who had already
// logged in, and being put on the team is often the reason to log in at all.

type directoryResult struct {
	Username string `json:"username"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	// Known reports whether this person already has an account here. The
	// interface needs the distinction: picking a known person allocates them,
	// picking an unknown one also creates the account, and somebody choosing
	// from a list deserves to know which of the two they are about to do.
	Known bool `json:"known"`
	// UserID is set only for people who already have an account.
	UserID string `json:"userId,omitempty"`
}

type directoryResponse struct {
	Results []directoryResult `json:"results"`
	// Searched is false when the directory could not be consulted at all -
	// not enabled, or no service account. An empty list then means "we could
	// not look", which is a different thing to tell somebody than "nobody
	// matched", and the interface says so rather than implying the person
	// does not exist.
	Searched bool   `json:"searched"`
	Reason   string `json:"reason,omitempty"`
}

// GET /api/directory/search?q=
//
// Administrators and managers only. A directory search returns names, e-mail
// addresses and usernames of people who may have no account here at all, which
// is a staff list - not something every authenticated user should be able to
// page through one letter at a time.
func (a *API) SearchDirectory(w http.ResponseWriter, r *http.Request) {
	if !canAdministerStructure(auth.CurrentUser(r)) {
		httpx.Error(w, http.StatusForbidden, forbiddenMessage)
		return
	}

	query := strings.TrimSpace(r.URL.Query().Get("q"))
	response := directoryResponse{Results: []directoryResult{}}

	entries, err := auth.SearchDirectory(a.Cfg, query, 20)
	if err != nil {
		if errors.Is(err, auth.ErrDirectorySearchUnavailable) {
			// Not an error the caller can act on by retrying: it is a
			// statement about how this installation is configured, so it is
			// answered as a successful response carrying the reason.
			response.Reason = err.Error()
			httpx.JSON(w, http.StatusOK, response)
			return
		}
		httpx.Error(w, http.StatusBadGateway, err.Error())
		return
	}

	response.Searched = true

	// Everyone found is checked against the local table, so the interface can
	// distinguish "add them" from "create and add them" without a second round
	// trip per row.
	for _, entry := range entries {
		result := directoryResult{Username: entry.Username, Name: entry.Name, Email: entry.Email}
		if existing, err := a.Users.ByLogin(r.Context(), entry.Username); err == nil && existing != nil {
			result.Known = true
			result.UserID = existing.ID.String()
		}
		response.Results = append(response.Results, result)
	}

	httpx.JSON(w, http.StatusOK, response)
}

// provisionFromDirectory turns a directory username into a local account,
// returning the existing one when there is already an account for them.
//
// The account is created exactly as a first login would create it - auth source
// "ad", no password, the default member role - so somebody added to a team
// before they ever sign in is indistinguishable afterwards from somebody who
// signed in first. Anything else would leave two kinds of AD user in the table.
func (a *API) provisionFromDirectory(r *http.Request, username string) (*models.User, error) {
	username = strings.ToLower(strings.TrimSpace(username))
	if username == "" {
		return nil, errors.New("a directory username is required")
	}

	if existing, err := a.Users.ByLogin(r.Context(), username); err == nil && existing != nil {
		return existing, nil
	}

	// Looked up again rather than trusting what the client sends: the name and
	// e-mail stored here should be the directory's, not a caller's idea of it.
	entries, err := auth.SearchDirectory(a.Cfg, username, 5)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.Username == username {
			return a.Users.UpsertFromAD(r.Context(), entry.Username, entry.Name, entry.Email, "")
		}
	}
	return nil, errors.New("that person is no longer in the directory")
}
