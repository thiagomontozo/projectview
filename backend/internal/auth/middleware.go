package auth

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"projectview/internal/config"
	"projectview/internal/httpx"
	"projectview/internal/models"
	"projectview/internal/repo"
)

type ctxKey string

const (
	userCtxKey    ctxKey = "currentUser"
	sessionCtxKey ctxKey = "currentSession"
)

// CSRFCookieName holds the double-submit token. It is deliberately readable by
// JavaScript - the client has to echo it back in a header, which is the whole
// mechanism.
const CSRFCookieName = "csrf_token"

// CSRFHeaderName is where the client echoes the cookie value back.
const CSRFHeaderName = "X-CSRF-Token"

// RequireAuth verifies the access token, confirms the session behind it is
// still live, and attaches the user to the request context.
func RequireAuth(users *repo.Users, sessions *repo.Sessions, cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, fromCookie := extractToken(r)
			if token == "" {
				httpx.Error(w, http.StatusUnauthorized, "Authentication required.")
				return
			}

			// CSRF only applies when the cookie alone authenticates the
			// request. A Bearer header cannot be attached by a cross-site form
			// or image, so those requests are not forgeable this way.
			if fromCookie && isStateChanging(r.Method) && !validCSRF(r) {
				httpx.Error(w, http.StatusForbidden, "Missing or invalid CSRF token.")
				return
			}

			claims, err := ParseToken(cfg, token)
			if err != nil {
				httpx.Error(w, http.StatusUnauthorized, "Invalid or expired session.")
				return
			}

			sessionID, ok := claims.SessionUUID()
			if !ok {
				httpx.Error(w, http.StatusUnauthorized, "Invalid session.")
				return
			}
			// This lookup is what makes revocation immediate: a logout, an
			// admin deactivating the account, or a password change ends access
			// on the very next request rather than at token expiry.
			live, err := sessions.IsLive(r.Context(), sessionID)
			if err != nil || !live {
				httpx.Error(w, http.StatusUnauthorized, "Session has been revoked.")
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
			ctx = context.WithValue(ctx, sessionCtxKey, sessionID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func isStateChanging(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}
	return true
}

func validCSRF(r *http.Request) bool {
	cookie, err := r.Cookie(CSRFCookieName)
	if err != nil || cookie.Value == "" {
		return false
	}
	header := r.Header.Get(CSRFHeaderName)
	if header == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(header)) == 1
}

// extractToken returns the access token and whether it came from the cookie
// rather than the Authorization header - the distinction CSRF turns on.
func extractToken(r *http.Request) (token string, fromCookie bool) {
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer "), false
	}
	if cookie, err := r.Cookie("token"); err == nil {
		return cookie.Value, true
	}
	return "", false
}

// CurrentUser reads the authenticated user set by RequireAuth.
func CurrentUser(r *http.Request) *models.User {
	u, _ := r.Context().Value(userCtxKey).(*models.User)
	return u
}

// CurrentSession reads the session id backing this request.
func CurrentSession(r *http.Request) (uuid.UUID, bool) {
	id, ok := r.Context().Value(sessionCtxKey).(uuid.UUID)
	return id, ok
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
