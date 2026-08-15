package handlers

import (
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"projectview/internal/audit"
	"projectview/internal/auth"
	"projectview/internal/httpx"
	"projectview/internal/logger"
	"projectview/internal/models"
	"projectview/internal/repo"
)

const refreshCookieName = "refresh_token"

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Mode     string `json:"mode"`
}

type loginResponse struct {
	Token string      `json:"token"`
	User  models.User `json:"user"`
}

func (a *API) refreshTTL() time.Duration {
	return time.Duration(a.Cfg.JWT.RefreshDays) * 24 * time.Hour
}

func (a *API) secureCookies() bool { return a.Cfg.NodeEnv == "production" }

func (a *API) setSessionCookies(w http.ResponseWriter, accessToken, refreshToken string) {
	http.SetCookie(w, &http.Cookie{
		Name:     "token",
		Value:    accessToken,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   a.secureCookies(),
		MaxAge:   a.Cfg.JWT.ExpiresInHours * 3600,
	})
	// Scoped to the refresh endpoint: it is never sent on ordinary API calls,
	// so the long-lived credential is exposed on exactly one route.
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookieName,
		Value:    refreshToken,
		Path:     "/api/auth",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   a.secureCookies(),
		MaxAge:   int(a.refreshTTL().Seconds()),
	})

	// Double-submit CSRF token. Readable by JavaScript on purpose: the client
	// echoes it in a header, which a cross-site request cannot do.
	if csrf, err := repo.NewToken(); err == nil {
		http.SetCookie(w, &http.Cookie{
			Name:     auth.CSRFCookieName,
			Value:    csrf,
			Path:     "/",
			HttpOnly: false,
			SameSite: http.SameSiteLaxMode,
			Secure:   a.secureCookies(),
			MaxAge:   int(a.refreshTTL().Seconds()),
		})
	}
}

func (a *API) clearSessionCookies(w http.ResponseWriter) {
	for _, c := range []struct{ name, path string }{
		{"token", "/"},
		{refreshCookieName, "/api/auth"},
		{auth.CSRFCookieName, "/"},
	} {
		http.SetCookie(w, &http.Cookie{Name: c.name, Value: "", Path: c.path, MaxAge: -1})
	}
}

// GET /api/auth/config
func (a *API) AuthConfig(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, map[string]bool{"adEnabled": a.Cfg.AD().Enabled})
}

// POST /api/auth/login
func (a *API) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.Username == "" || req.Password == "" {
		httpx.Error(w, http.StatusBadRequest, "Username and password are required.")
		return
	}

	mode := req.Mode
	if mode != "local" {
		if a.Cfg.AD().Enabled {
			mode = "ad"
		} else {
			mode = "local"
		}
	}

	ctx := r.Context()
	var user *models.User

	if mode == "ad" {
		profile, err := auth.AuthenticateAD(a.Cfg, req.Username, req.Password)
		if err != nil {
			a.Audit.RecordAnonymous(r, req.Username, audit.Event{
				Action: audit.ActionLoginFailed, ResourceType: "session",
				Changes: map[string]any{"mode": "ad"}, Status: http.StatusUnauthorized,
			})
			httpx.Error(w, http.StatusUnauthorized, "Invalid Active Directory credentials.")
			return
		}
		user, err = a.Users.UpsertFromAD(ctx, profile.Username, profile.Name, profile.Email, "#2a78d6")
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, "Failed to provision user.")
			return
		}
	} else {
		found, err := a.Users.ByLogin(ctx, strings.ToLower(req.Username))
		if err != nil || found.PasswordHash == "" {
			a.Audit.RecordAnonymous(r, req.Username, audit.Event{
				Action: audit.ActionLoginFailed, ResourceType: "session",
				Status: http.StatusUnauthorized,
			})
			httpx.Error(w, http.StatusUnauthorized, "Invalid username or password.")
			return
		}
		ok, needsUpgrade := auth.VerifyPassword(found.PasswordHash, req.Password)
		if !ok {
			a.Audit.RecordAnonymous(r, req.Username, audit.Event{
				Action: audit.ActionLoginFailed, ResourceType: "session",
				ResourceID: found.ID.String(), Status: http.StatusUnauthorized,
			})
			httpx.Error(w, http.StatusUnauthorized, "Invalid username or password.")
			return
		}
		// Transparent migration: a correct password against a legacy bcrypt
		// hash is re-stored with Argon2id, so accounts upgrade as people sign
		// in rather than through a forced reset.
		if needsUpgrade {
			if upgraded, err := auth.HashPassword(req.Password); err == nil {
				if err := a.Users.SetPassword(ctx, found.ID, upgraded); err != nil {
					logger.Warn("could not upgrade password hash for %s: %v", found.Username, err)
				}
			}
		}
		_ = a.Users.TouchLogin(ctx, found.ID)
		user = found
	}

	if !user.Active {
		httpx.Error(w, http.StatusForbidden, "This account has been deactivated.")
		return
	}

	accessToken, refreshToken, err := a.openSession(r, user)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "Failed to create session.")
		return
	}
	a.setSessionCookies(w, accessToken, refreshToken)

	a.Audit.Record(r, user, audit.Event{
		Action: audit.ActionLogin, ResourceType: "session", ResourceID: user.ID.String(),
		Changes: map[string]any{"mode": mode}, Status: http.StatusOK,
	})

	user.PasswordHash = ""
	httpx.JSON(w, http.StatusOK, loginResponse{Token: accessToken, User: *user})
}

// openSession creates the session row and mints the access token bound to it.
func (a *API) openSession(r *http.Request, user *models.User) (accessToken, refreshToken string, err error) {
	session, refreshToken, err := a.Sessions.Create(r.Context(), user.ID,
		a.refreshTTL(), r.UserAgent(), r.RemoteAddr)
	if err != nil {
		return "", "", err
	}
	accessToken, err = auth.SignToken(a.Cfg, user.ID.String(), user.Role, session.ID)
	if err != nil {
		return "", "", err
	}
	return accessToken, refreshToken, nil
}

// POST /api/auth/refresh - exchanges a refresh token for a new access token.
//
// The refresh token is rotated on every use, so a stolen one is good for at
// most a single exchange before the legitimate client's next refresh
// invalidates it.
func (a *API) Refresh(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(refreshCookieName)
	if err != nil || cookie.Value == "" {
		httpx.Error(w, http.StatusUnauthorized, "No refresh token.")
		return
	}

	ctx := r.Context()
	session, err := a.Sessions.ByToken(ctx, cookie.Value)
	if err != nil {
		a.clearSessionCookies(w)
		httpx.Error(w, http.StatusUnauthorized, "Refresh token is invalid or expired.")
		return
	}

	user, err := a.Users.ByID(ctx, session.UserID)
	if err != nil || !user.Active {
		_ = a.Sessions.Revoke(ctx, session.ID)
		a.clearSessionCookies(w)
		httpx.Error(w, http.StatusUnauthorized, "Account is no longer active.")
		return
	}

	rotated, newRefresh, err := a.Sessions.Rotate(ctx, session, a.refreshTTL(), r.UserAgent(), r.RemoteAddr)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "Failed to refresh session.")
		return
	}
	accessToken, err := auth.SignToken(a.Cfg, user.ID.String(), user.Role, rotated.ID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "Failed to refresh session.")
		return
	}
	a.setSessionCookies(w, accessToken, newRefresh)

	a.Audit.Record(r, user, audit.Event{
		Action: audit.ActionTokenRefresh, ResourceType: "session",
		ResourceID: rotated.ID.String(), Status: http.StatusOK,
	})

	user.PasswordHash = ""
	httpx.JSON(w, http.StatusOK, loginResponse{Token: accessToken, User: *user})
}

type registerRequest struct {
	Username string `json:"username"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

// POST /api/auth/register - creates a local account.
//
// Administrative endpoint: it mints accounts and can assign any role, so the
// router gates it behind RequireAuth + RequireRole(admin). It was once
// reachable unauthenticated while honouring a caller-supplied "role", which
// let anyone who could reach the API create themselves an administrator.
func (a *API) Register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.Username == "" || req.Name == "" || req.Email == "" || req.Password == "" {
		httpx.Error(w, http.StatusBadRequest, "username, name, email and password are all required.")
		return
	}
	if len(req.Password) < minPasswordLength {
		httpx.Error(w, http.StatusBadRequest, minPasswordMessage)
		return
	}

	role := models.RoleMember
	if req.Role != "" {
		if !models.ValidRole(req.Role) {
			httpx.Error(w, http.StatusBadRequest, "Invalid role. Expected admin, manager or member.")
			return
		}
		role = req.Role
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "Failed to hash password.")
		return
	}

	user := &models.User{
		Username:      strings.ToLower(req.Username),
		Name:          req.Name,
		Email:         strings.ToLower(req.Email),
		PasswordHash:  hash,
		AuthSource:    models.AuthSourceLocal,
		Role:          role,
		AvatarColor:   "#2a78d6",
		Active:        true,
		NotifyByEmail: true,
	}
	if err := a.Users.Create(r.Context(), user); err != nil {
		if repo.IsUniqueViolation(err) {
			httpx.Error(w, http.StatusConflict, "A user with that username or email already exists.")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, "Failed to create user.")
		return
	}

	a.Audit.Record(r, auth.CurrentUser(r), audit.Event{
		Action: audit.ActionUserCreated, ResourceType: "user", ResourceID: user.ID.String(),
		Changes: map[string]any{"username": user.Username, "role": user.Role},
		Status:  http.StatusCreated,
	})

	user.PasswordHash = ""
	// A brand-new account belongs to no team yet; keep it an empty array so
	// the JSON stays [] rather than null, as clients expect.
	user.Teams = []uuid.UUID{}
	httpx.JSON(w, http.StatusCreated, map[string]interface{}{"user": user})
}

// GET /api/auth/me
func (a *API) Me(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	copied := *user
	copied.PasswordHash = ""
	httpx.JSON(w, http.StatusOK, map[string]interface{}{"user": copied})
}

// POST /api/auth/logout - revokes the session behind this request.
func (a *API) Logout(w http.ResponseWriter, r *http.Request) {
	// Best effort: logging out must succeed even from an already-dead session.
	if cookie, err := r.Cookie(refreshCookieName); err == nil && cookie.Value != "" {
		if session, err := a.Sessions.ByToken(r.Context(), cookie.Value); err == nil {
			_ = a.Sessions.Revoke(r.Context(), session.ID)
		}
	}
	if sessionID, ok := auth.CurrentSession(r); ok {
		_ = a.Sessions.Revoke(r.Context(), sessionID)
	}
	if user := auth.CurrentUser(r); user != nil {
		a.Audit.Record(r, user, audit.Event{
			Action: audit.ActionLogout, ResourceType: "session", Status: http.StatusOK,
		})
	}

	a.clearSessionCookies(w)
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type sessionResponse struct {
	ID         uuid.UUID  `json:"id"`
	UserAgent  string     `json:"userAgent,omitempty"`
	IP         string     `json:"ip,omitempty"`
	Current    bool       `json:"current"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
	ExpiresAt  time.Time  `json:"expiresAt"`
	CreatedAt  time.Time  `json:"createdAt"`
}

// GET /api/auth/sessions - where the current user is signed in.
func (a *API) ListSessions(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	sessions, err := a.Sessions.ListForUser(r.Context(), user.ID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	current, _ := auth.CurrentSession(r)

	out := make([]sessionResponse, 0, len(sessions))
	for _, s := range sessions {
		out = append(out, sessionResponse{
			ID: s.ID, UserAgent: s.UserAgent, IP: s.IP, Current: s.ID == current,
			LastUsedAt: s.LastUsedAt, ExpiresAt: s.ExpiresAt, CreatedAt: s.CreatedAt,
		})
	}
	httpx.JSON(w, http.StatusOK, out)
}

// DELETE /api/auth/sessions/:id - sign out of one device.
func (a *API) RevokeSession(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.UUIDParam(w, r, "id")
	if !ok {
		return
	}
	user := auth.CurrentUser(r)

	// Only your own sessions: the id alone must not be enough to end someone
	// else's login.
	sessions, err := a.Sessions.ListForUser(r.Context(), user.ID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	owned := false
	for _, s := range sessions {
		if s.ID == id {
			owned = true
			break
		}
	}
	if !owned {
		httpx.Error(w, http.StatusNotFound, "Session not found.")
		return
	}

	if err := a.Sessions.Revoke(r.Context(), id); err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.Audit.Record(r, user, audit.Event{
		Action: audit.ActionSessionRevoked, ResourceType: "session",
		ResourceID: id.String(), Status: http.StatusOK,
	})
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}
