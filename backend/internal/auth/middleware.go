package auth

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"projectview/internal/config"
	"projectview/internal/httpx"
	"projectview/internal/models"
	"projectview/internal/repo"
)

type ctxKey string

const userCtxKey ctxKey = "currentUser"

// RequireAuth verifies the JWT (Authorization header or "token" cookie) and
// attaches the authenticated user to the request context.
func RequireAuth(users *repo.Users, cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := extractToken(r)
			if token == "" {
				httpx.Error(w, http.StatusUnauthorized, "Authentication required.")
				return
			}

			claims, err := ParseToken(cfg, token)
			if err != nil {
				httpx.Error(w, http.StatusUnauthorized, "Invalid or expired session.")
				return
			}

			userID, err := uuid.Parse(claims.Subject)
			if err != nil {
				httpx.Error(w, http.StatusUnauthorized, "Invalid session.")
				return
			}

			user, err := users.ByID(r.Context(), userID)
			if err != nil || !user.Active {
				httpx.Error(w, http.StatusUnauthorized, "Invalid session.")
				return
			}

			ctx := context.WithValue(r.Context(), userCtxKey, user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func extractToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	if cookie, err := r.Cookie("token"); err == nil {
		return cookie.Value
	}
	return ""
}

// CurrentUser reads the authenticated user set by RequireAuth.
func CurrentUser(r *http.Request) *models.User {
	u, _ := r.Context().Value(userCtxKey).(*models.User)
	return u
}

// RequireRole returns a middleware that only allows the given roles through.
func RequireRole(roles ...string) func(http.Handler) http.Handler {
	allowed := make(map[string]bool, len(roles))
	for _, r := range roles {
		allowed[r] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := CurrentUser(r)
			if user == nil || !allowed[user.Role] {
				httpx.Error(w, http.StatusForbidden, "Insufficient permissions.")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
