package handlers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"projectview/internal/audit"
	"projectview/internal/httpx"
	"projectview/internal/models"
	"projectview/internal/repo"
)

// SCIM 2.0, the Users resource.
//
// Implemented because provisioning by hand is where an internal tool leaks: a
// person leaves, HR closes their directory account, and the project tool keeps
// their session alive because nobody remembered it existed. Groups are not
// implemented - team membership here is a project decision, not a directory
// one, and mapping it from the IdP would let the directory quietly rearrange
// who can see what.
const (
	scimUserSchema  = "urn:ietf:params:scim:schemas:core:2.0:User"
	scimListSchema  = "urn:ietf:params:scim:api:messages:2.0:ListResponse"
	scimErrorSchema = "urn:ietf:params:scim:api:messages:2.0:Error"
)

type scimName struct {
	Formatted string `json:"formatted,omitempty"`
}

type scimEmail struct {
	Value   string `json:"value"`
	Primary bool   `json:"primary,omitempty"`
}

type scimMeta struct {
	ResourceType string `json:"resourceType"`
	Created      string `json:"created"`
	LastModified string `json:"lastModified"`
	Location     string `json:"location"`
}

type scimUser struct {
	Schemas     []string    `json:"schemas"`
	ID          string      `json:"id"`
	ExternalID  string      `json:"externalId,omitempty"`
	UserName    string      `json:"userName"`
	DisplayName string      `json:"displayName,omitempty"`
	Name        scimName    `json:"name"`
	Emails      []scimEmail `json:"emails"`
	Active      bool        `json:"active"`
	Meta        scimMeta    `json:"meta"`
}

func toSCIM(u *models.User) scimUser {
	external := ""
	if u.ExternalID != nil {
		external = *u.ExternalID
	}
	return scimUser{
		Schemas:     []string{scimUserSchema},
		ID:          u.ID.String(),
		ExternalID:  external,
		UserName:    u.Username,
		DisplayName: u.Name,
		Name:        scimName{Formatted: u.Name},
		Emails:      []scimEmail{{Value: u.Email, Primary: true}},
		Active:      u.Active,
		Meta: scimMeta{
			ResourceType: "User",
			Created:      u.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
			LastModified: u.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"),
			Location:     "/scim/v2/Users/" + u.ID.String(),
		},
	}
}

// scimError answers in the shape the specification requires. A provisioning
// client that receives this application's ordinary error body will log
// something unhelpful and retry forever.
func scimError(w http.ResponseWriter, status int, detail string) {
	httpx.JSON(w, status, map[string]any{
		"schemas": []string{scimErrorSchema},
		"status":  strconv.Itoa(status),
		"detail":  detail,
	})
}

type serviceTokenKey struct{}

// RequireServiceToken authenticates a machine credential and checks its scope.
// Deliberately separate from the user middleware: a provisioning client has no
// session, no refresh token and no CSRF, and pretending otherwise would mean
// widening the user path to accommodate it.
func (a *API) RequireServiceToken(scope string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			if !strings.HasPrefix(header, "Bearer ") {
				scimError(w, http.StatusUnauthorized, "A bearer service token is required.")
				return
			}

			token, err := a.Tokens.Authenticate(r.Context(), strings.TrimPrefix(header, "Bearer "))
			if err != nil {
				scimError(w, http.StatusUnauthorized, "Unknown or revoked token.")
				return
			}
			if !token.HasScope(scope) {
				scimError(w, http.StatusForbidden, "This token does not carry the "+scope+" scope.")
				return
			}

			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), serviceTokenKey{}, token)))
		})
	}
}

func currentServiceToken(r *http.Request) *repo.ServiceToken {
	token, _ := r.Context().Value(serviceTokenKey{}).(*repo.ServiceToken)
	return token
}

// GET /scim/v2/Users?filter=userName eq "x"&startIndex=1&count=100
func (a *API) SCIMListUsers(w http.ResponseWriter, r *http.Request) {
	// Only the one filter every provisioning client sends before creating a
	// user. A partial SCIM filter parser that silently ignores what it does
	// not understand would return the whole directory for a query meant to
	// match one person.
	if filter := r.URL.Query().Get("filter"); filter != "" {
		username, ok := parseUserNameFilter(filter)
		if !ok {
			scimError(w, http.StatusBadRequest, `Only filters of the form: userName eq "value" are supported.`)
			return
		}
		user, err := a.Users.ByLogin(r.Context(), username)
		if err != nil {
			httpx.JSON(w, http.StatusOK, scimList(nil, 0, 1))
			return
		}
		httpx.JSON(w, http.StatusOK, scimList([]scimUser{toSCIM(user)}, 1, 1))
		return
	}

	startIndex := 1
	if raw := r.URL.Query().Get("startIndex"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			startIndex = n
		}
	}
	count := 100
	if raw := r.URL.Query().Get("count"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= 500 {
			count = n
		}
	}

	users, total, err := a.Users.ListAll(r.Context(), count, startIndex-1)
	if err != nil {
		scimError(w, http.StatusInternalServerError, err.Error())
		return
	}
	resources := make([]scimUser, 0, len(users))
	for i := range users {
		resources = append(resources, toSCIM(&users[i]))
	}
	httpx.JSON(w, http.StatusOK, scimList(resources, total, startIndex))
}

func scimList(resources []scimUser, total, startIndex int) map[string]any {
	if resources == nil {
		resources = []scimUser{}
	}
	return map[string]any{
		"schemas":      []string{scimListSchema},
		"totalResults": total,
		"startIndex":   startIndex,
		"itemsPerPage": len(resources),
		"Resources":    resources,
	}
}

// parseUserNameFilter understands exactly `userName eq "value"`, case
// insensitively on the attribute and operator.
func parseUserNameFilter(filter string) (string, bool) {
	fields := strings.SplitN(strings.TrimSpace(filter), " ", 3)
	if len(fields) != 3 {
		return "", false
	}
	if !strings.EqualFold(fields[0], "userName") || !strings.EqualFold(fields[1], "eq") {
		return "", false
	}
	value := strings.TrimSpace(fields[2])
	value = strings.TrimPrefix(value, `"`)
	value = strings.TrimSuffix(value, `"`)
	if value == "" {
		return "", false
	}
	return value, true
}

// GET /scim/v2/Users/{id}
func (a *API) SCIMGetUser(w http.ResponseWriter, r *http.Request) {
	user, ok := a.scimLoadUser(w, r)
	if !ok {
		return
	}
	httpx.JSON(w, http.StatusOK, toSCIM(user))
}

func (a *API) scimLoadUser(w http.ResponseWriter, r *http.Request) (*models.User, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		scimError(w, http.StatusNotFound, "User not found.")
		return nil, false
	}
	user, err := a.Users.ByID(r.Context(), id)
	if err != nil {
		scimError(w, http.StatusNotFound, "User not found.")
		return nil, false
	}
	return user, true
}

// POST /scim/v2/Users
func (a *API) SCIMCreateUser(w http.ResponseWriter, r *http.Request) {
	var req scimUser
	if err := decodeSCIM(r, &req); err != nil {
		scimError(w, http.StatusBadRequest, "Malformed SCIM payload.")
		return
	}
	if req.UserName == "" {
		scimError(w, http.StatusBadRequest, "userName is required.")
		return
	}

	email := req.UserName
	if len(req.Emails) > 0 && req.Emails[0].Value != "" {
		email = req.Emails[0].Value
	}
	name := req.DisplayName
	if name == "" {
		name = req.Name.Formatted
	}
	if name == "" {
		name = req.UserName
	}

	// A provisioning client that retries must not create a duplicate, and the
	// specification says so: an existing userName is a 409.
	if existing, err := a.Users.ByLogin(r.Context(), req.UserName); err == nil {
		scimError(w, http.StatusConflict, "A user with this userName already exists: "+existing.ID.String())
		return
	}

	user := &models.User{
		Username:      req.UserName,
		Name:          name,
		Email:         email,
		AuthSource:    models.AuthSourceSCIM,
		Role:          models.RoleMember,
		AvatarColor:   "#2a78d6",
		Active:        true,
		NotifyByEmail: true,
	}
	if req.ExternalID != "" {
		user.ExternalID = &req.ExternalID
	}
	// No password is set. A provisioned account signs in through the identity
	// provider; giving it a local credential would create a way in that
	// deprovisioning does not close.

	if err := a.Users.Create(r.Context(), user); err != nil {
		scimError(w, http.StatusInternalServerError, err.Error())
		return
	}

	a.Audit.Record(r, nil, audit.Event{
		Action: audit.ActionSCIMProvison, ResourceType: "user", ResourceID: user.ID.String(),
		Changes: map[string]any{"userName": user.Username, "token": tokenName(r)},
		Status:  http.StatusCreated,
	})
	httpx.JSON(w, http.StatusCreated, toSCIM(user))
}

// PUT /scim/v2/Users/{id} - full replace.
func (a *API) SCIMReplaceUser(w http.ResponseWriter, r *http.Request) {
	user, ok := a.scimLoadUser(w, r)
	if !ok {
		return
	}
	var req scimUser
	if err := decodeSCIM(r, &req); err != nil {
		scimError(w, http.StatusBadRequest, "Malformed SCIM payload.")
		return
	}

	patch := repo.UserPatch{Active: &req.Active}
	if name := firstNonEmpty(req.DisplayName, req.Name.Formatted); name != "" {
		patch.Name = &name
	}
	if len(req.Emails) > 0 && req.Emails[0].Value != "" {
		patch.Email = &req.Emails[0].Value
	}
	if req.UserName != "" && req.UserName != user.Username {
		patch.Username = &req.UserName
	}
	if req.ExternalID != "" {
		patch.ExternalID = &req.ExternalID
	}

	if err := a.Users.Update(r.Context(), user.ID, patch); err != nil {
		scimError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.scimAfterDeactivation(r, user.ID, req.Active)

	updated, err := a.Users.ByID(r.Context(), user.ID)
	if err != nil {
		scimError(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, toSCIM(updated))
}

type scimPatchOp struct {
	Schemas    []string `json:"schemas"`
	Operations []struct {
		Op    string `json:"op"`
		Path  string `json:"path"`
		Value any    `json:"value"`
	} `json:"Operations"`
}

// PATCH /scim/v2/Users/{id}
//
// In practice this carries exactly one operation - setting active to false -
// and that operation is the entire reason SCIM is here.
func (a *API) SCIMPatchUser(w http.ResponseWriter, r *http.Request) {
	user, ok := a.scimLoadUser(w, r)
	if !ok {
		return
	}
	var req scimPatchOp
	if err := decodeSCIM(r, &req); err != nil {
		scimError(w, http.StatusBadRequest, "Malformed SCIM payload.")
		return
	}

	patch := repo.UserPatch{}
	deactivated := false
	for _, op := range req.Operations {
		if !strings.EqualFold(op.Op, "replace") && !strings.EqualFold(op.Op, "add") {
			continue
		}
		switch strings.ToLower(strings.TrimPrefix(op.Path, "urn:ietf:params:scim:schemas:core:2.0:User:")) {
		case "active":
			active := truthy(op.Value)
			patch.Active = &active
			deactivated = !active
		case "displayname", "name.formatted":
			if name := str(op.Value); name != "" {
				patch.Name = &name
			}
		case "":
			// A pathless replace carries an object of attributes.
			if values, ok := op.Value.(map[string]any); ok {
				if raw, present := values["active"]; present {
					active := truthy(raw)
					patch.Active = &active
					deactivated = !active
				}
				if name := str(values["displayName"]); name != "" {
					patch.Name = &name
				}
			}
		}
	}

	if patch.Active == nil && patch.Name == nil {
		// Nothing recognised. Answering 200 with the unchanged resource would
		// tell the client the change was applied.
		scimError(w, http.StatusBadRequest, "No supported attribute in the patch.")
		return
	}

	if err := a.Users.Update(r.Context(), user.ID, patch); err != nil {
		scimError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if deactivated {
		a.scimAfterDeactivation(r, user.ID, false)
	}

	updated, err := a.Users.ByID(r.Context(), user.ID)
	if err != nil {
		scimError(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, toSCIM(updated))
}

// DELETE /scim/v2/Users/{id} - deactivates.
//
// Never a row deletion: the person's tasks, comments and time entries belong
// to the organisation, and a directory that removes an employee is not asking
// for the project history to be destroyed. Erasure is a separate, deliberate
// act with its own endpoint and its own confirmation.
func (a *API) SCIMDeleteUser(w http.ResponseWriter, r *http.Request) {
	user, ok := a.scimLoadUser(w, r)
	if !ok {
		return
	}
	inactive := false
	if err := a.Users.Update(r.Context(), user.ID, repo.UserPatch{Active: &inactive}); err != nil {
		scimError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.scimAfterDeactivation(r, user.ID, false)
	w.WriteHeader(http.StatusNoContent)
}

// scimAfterDeactivation makes deprovisioning actually take effect.
//
// Flipping a boolean would leave every existing session working until its
// token expired, which is the exact gap provisioning exists to close.
func (a *API) scimAfterDeactivation(r *http.Request, userID uuid.UUID, active bool) {
	if active {
		return
	}
	if _, err := a.Sessions.RevokeAllForUser(r.Context(), userID); err != nil {
		return
	}
	a.Audit.Record(r, nil, audit.Event{
		Action: audit.ActionUserDeactivated, ResourceType: "user", ResourceID: userID.String(),
		Changes: map[string]any{"via": "scim", "token": tokenName(r)},
		Status:  http.StatusOK,
	})
}

func tokenName(r *http.Request) string {
	if token := currentServiceToken(r); token != nil {
		return token.Name
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// str reads a string out of a decoded JSON value without panicking on a
// client that sent a number where a name belongs.
func str(v any) string {
	s, _ := v.(string)
	return s
}

// decodeSCIM reads a request body with a size limit. SCIM clients are trusted
// integrations, but a trusted integration with a bug is still a way to run the
// server out of memory.
func decodeSCIM(r *http.Request, dst any) error {
	return json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(dst)
}

func truthy(v any) bool {
	switch value := v.(type) {
	case bool:
		return value
	case string:
		// Some clients send "True"/"false" as strings.
		return strings.EqualFold(value, "true")
	default:
		return false
	}
}
