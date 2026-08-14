package handlers

import (
	"net/http"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"golang.org/x/crypto/bcrypt"

	"projectview/internal/auth"
	"projectview/internal/httpx"
	"projectview/internal/models"
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
	var user models.User

	if mode == "ad" {
		profile, err := auth.AuthenticateAD(a.Cfg, req.Username, req.Password)
		if err != nil {
			httpx.Error(w, http.StatusUnauthorized, "Invalid Active Directory credentials.")
			return
		}

		err = a.Store.Users().FindOne(ctx, bson.M{"email": profile.Email}).Decode(&user)
		now := time.Now()
		if err != nil {
			user = models.User{
				ID:            primitive.NewObjectID(),
				Username:      profile.Username,
				Name:          profile.Name,
				Email:         profile.Email,
				AuthSource:    models.AuthSourceAD,
				Role:          models.RoleMember,
				AvatarColor:   "#2a78d6",
				Teams:         []primitive.ObjectID{},
				Active:        true,
				NotifyByEmail: true,
				LastLoginAt:   &now,
				CreatedAt:     now,
				UpdatedAt:     now,
			}
			if _, err := a.Store.Users().InsertOne(ctx, user); err != nil {
				httpx.Error(w, http.StatusInternalServerError, "Failed to provision user.")
				return
			}
		} else {
			user.Name = profile.Name
			user.AuthSource = models.AuthSourceAD
			user.LastLoginAt = &now
			user.UpdatedAt = now
			_, _ = a.Store.Users().UpdateByID(ctx, user.ID, bson.M{"$set": bson.M{
				"name": user.Name, "authSource": user.AuthSource, "lastLoginAt": now, "updatedAt": now,
			}})
		}
	} else {
		lookup := strings.ToLower(req.Username)
		err := a.Store.Users().FindOne(ctx, bson.M{"$or": []bson.M{{"username": lookup}, {"email": lookup}}}).Decode(&user)
		if err != nil || user.PasswordHash == "" {
			httpx.Error(w, http.StatusUnauthorized, "Invalid username or password.")
			return
		}
		if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
			httpx.Error(w, http.StatusUnauthorized, "Invalid username or password.")
			return
		}
		now := time.Now()
		user.LastLoginAt = &now
		_, _ = a.Store.Users().UpdateByID(ctx, user.ID, bson.M{"$set": bson.M{"lastLoginAt": now, "updatedAt": now}})
	}

	if !user.Active {
		httpx.Error(w, http.StatusForbidden, "This account has been deactivated.")
		return
	}

	token, err := auth.SignToken(a.Cfg, user.ID.Hex(), user.Role)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "Failed to create session.")
		return
	}
	a.setSessionCookie(w, token)
	user.PasswordHash = ""
	httpx.JSON(w, http.StatusOK, loginResponse{Token: token, User: user})
}

type registerRequest struct {
	Username string `json:"username"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

// POST /api/auth/register - creates a local account.
func (a *API) Register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.Username == "" || req.Name == "" || req.Email == "" || req.Password == "" {
		httpx.Error(w, http.StatusBadRequest, "username, name, email and password are all required.")
		return
	}

	ctx := r.Context()
	username := strings.ToLower(req.Username)
	email := strings.ToLower(req.Email)

	count, _ := a.Store.Users().CountDocuments(ctx, bson.M{"$or": []bson.M{{"username": username}, {"email": email}}})
	if count > 0 {
		httpx.Error(w, http.StatusConflict, "A user with that username or email already exists.")
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "Failed to hash password.")
		return
	}

	role := models.RoleMember
	if req.Role == models.RoleAdmin {
		role = models.RoleAdmin
	}

	now := time.Now()
	user := models.User{
		ID:            primitive.NewObjectID(),
		Username:      username,
		Name:          req.Name,
		Email:         email,
		PasswordHash:  string(hash),
		AuthSource:    models.AuthSourceLocal,
		Role:          role,
		AvatarColor:   "#2a78d6",
		Teams:         []primitive.ObjectID{},
		Active:        true,
		NotifyByEmail: true,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if _, err := a.Store.Users().InsertOne(ctx, user); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "Failed to create user.")
		return
	}
	user.PasswordHash = ""
	httpx.JSON(w, http.StatusCreated, map[string]interface{}{"user": user})
}

// GET /api/auth/me
func (a *API) Me(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	user.PasswordHash = ""
	httpx.JSON(w, http.StatusOK, map[string]interface{}{"user": user})
}

// POST /api/auth/logout
func (a *API) Logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: "token", Value: "", Path: "/", MaxAge: -1})
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}
