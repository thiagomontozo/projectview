package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"projectview/internal/auth"
	"projectview/internal/httpx"
	"projectview/internal/models"
	"projectview/internal/repo"
)

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Mode     string `json:"mode"`
}

type loginResponse struct {
	Token string      `json:"token"`
	User  models.User `json:"user"`
}

func (a *API) setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     "token",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   a.Cfg.NodeEnv == "production",
		MaxAge:   a.Cfg.JWT.ExpiresInHours * 3600,
	})
}

// GET /api/auth/config
func (a *API) AuthConfig(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, map[string]bool{"adEnabled": a.Cfg.AD.Enabled})
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
		if a.Cfg.AD.Enabled {
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
			httpx.Error(w, http.StatusUnauthorized, "Invalid username or password.")
			return
		}
		if bcrypt.CompareHashAndPassword([]byte(found.PasswordHash), []byte(req.Password)) != nil {
			httpx.Error(w, http.StatusUnauthorized, "Invalid username or password.")
			return
		}
		_ = a.Users.TouchLogin(ctx, found.ID)
		user = found
	}

	if !user.Active {
		httpx.Error(w, http.StatusForbidden, "This account has been deactivated.")
		return
	}

	token, err := auth.SignToken(a.Cfg, user.ID.String(), user.Role)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "Failed to create session.")
		return
	}
	a.setSessionCookie(w, token)
	user.PasswordHash = ""
	httpx.JSON(w, http.StatusOK, loginResponse{Token: token, User: *user})
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

	// Only the three known roles are accepted. Anything else is rejected
	// rather than silently downgraded, so a typo in an automation surfaces
	// instead of quietly creating an under-privileged account.
	role := models.RoleMember
	if req.Role != "" {
		if !models.ValidRole(req.Role) {
			httpx.Error(w, http.StatusBadRequest, "Invalid role. Expected admin, manager or member.")
			return
		}
		role = req.Role
	}

	hash, err := hashPassword(req.Password)
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

	user.PasswordHash = ""
	// A brand-new account belongs to no team yet; keep it an empty array so
	// the JSON stays [] rather than null, as clients expect.
	user.Teams = []uuid.UUID{}
	httpx.JSON(w, http.StatusCreated, map[string]interface{}{"user": user})
}

// GET /api/auth/me
func (a *API) Me(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	copy := *user
	copy.PasswordHash = ""
	httpx.JSON(w, http.StatusOK, map[string]interface{}{"user": copy})
}

// POST /api/auth/logout
func (a *API) Logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: "token", Value: "", Path: "/", MaxAge: -1})
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// notFound reports whether err means "no such row".
func notFound(err error) bool { return errors.Is(err, repo.ErrNotFound) }
