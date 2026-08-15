// Package audit records who changed what.
//
// Auditing is deliberately *not* a generic HTTP middleware that logs every
// request: a request log says "someone called PUT /api/projects/x", which is
// not the same as "Ana changed the project's status from active to on_hold".
// Handlers call Record at the point where they know the resource, the action
// and the shape of the change.
//
// Nothing here can fail a request. A trail that takes the application down
// when the audit table is unavailable trades one problem for a worse one, so
// write errors are logged and swallowed.
package audit

import (
	"context"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"projectview/internal/logger"
	"projectview/internal/models"
	"projectview/internal/repo"
)

// Actions recorded by the application. Kept as constants so a typo shows up at
// compile time rather than as an un-queryable string in the trail.
const (
	ActionLogin          = "auth.login"
	ActionLoginFailed    = "auth.login_failed"
	ActionLogout         = "auth.logout"
	ActionTokenRefresh   = "auth.refresh"
	ActionSessionRevoked = "auth.session_revoked"

	ActionUserCreated     = "user.created"
	ActionUserUpdated     = "user.updated"
	ActionPasswordChanged = "user.password_changed"
	ActionRoleChanged     = "user.role_changed"
	ActionUserDeactivated = "user.deactivated"

	ActionProjectCreated = "project.created"
	ActionProjectUpdated = "project.updated"
	ActionProjectDeleted = "project.deleted"

	ActionTaskCreated   = "task.created"
	ActionTaskUpdated   = "task.updated"
	ActionTaskMoved     = "task.moved"
	ActionTaskDeleted   = "task.deleted"
	ActionTaskCommented = "task.commented"

	ActionTeamCreated = "team.created"
	ActionTeamUpdated = "team.updated"
	ActionTeamDeleted = "team.deleted"

	ActionSpaceCreated  = "space.created"
	ActionSpaceUpdated  = "space.updated"
	ActionSpaceDeleted  = "space.deleted"
	ActionFolderCreated = "folder.created"
	ActionFolderUpdated = "folder.updated"
	ActionFolderDeleted = "folder.deleted"

	ActionPermissionDenied = "access.denied"
)

// Recorder writes entries. A nil Recorder is safe to call, which keeps tests
// that do not care about auditing free of setup.
type Recorder struct {
	repo *repo.Audit
}

func New(r *repo.Audit) *Recorder { return &Recorder{repo: r} }

// Event describes one auditable action.
type Event struct {
	Action       string
	ResourceType string
	ResourceID   string
	Changes      map[string]any
	Status       int
}

// Record writes the event, enriching it with the actor and request metadata
// taken from the request itself.
func (rec *Recorder) Record(r *http.Request, actor *models.User, e Event) {
	if rec == nil || rec.repo == nil {
		return
	}

	entry := repo.Entry{
		Action:       e.Action,
		ResourceType: e.ResourceType,
		ResourceID:   e.ResourceID,
		Changes:      Redact(e.Changes),
		IP:           clientIP(r),
		UserAgent:    truncate(r.UserAgent(), 400),
		RequestID:    middleware.GetReqID(r.Context()),
		Status:       e.Status,
	}
	if actor != nil {
		id := actor.ID
		entry.ActorID = &id
		entry.ActorLabel = actor.Username
	} else {
		entry.ActorLabel = "anonymous"
	}

	// Detached context: the trail must survive the client hanging up, which
	// cancels the request context mid-write.
	if err := rec.repo.Write(context.WithoutCancel(r.Context()), entry); err != nil {
		logger.Error("audit write failed for %s on %s/%s: %v",
			e.Action, e.ResourceType, e.ResourceID, err)
	}
}

// RecordAnonymous records an event with an explicit label instead of a user
// row, used for failed logins where no account is established.
func (rec *Recorder) RecordAnonymous(r *http.Request, label string, e Event) {
	if rec == nil || rec.repo == nil {
		return
	}
	entry := repo.Entry{
		ActorLabel:   truncate(label, 200),
		Action:       e.Action,
		ResourceType: e.ResourceType,
		ResourceID:   e.ResourceID,
		Changes:      Redact(e.Changes),
		IP:           clientIP(r),
		UserAgent:    truncate(r.UserAgent(), 400),
		RequestID:    middleware.GetReqID(r.Context()),
		Status:       e.Status,
	}
	if err := rec.repo.Write(context.WithoutCancel(r.Context()), entry); err != nil {
		logger.Error("audit write failed for %s: %v", e.Action, err)
	}
}

// sensitiveKeys never reach the trail. The audit table is widely readable by
// design - that is the point of an audit - so anything secret must be stripped
// before it gets there.
var sensitiveKeys = []string{
	"password", "passwordhash", "password_hash", "currentpassword",
	"token", "refreshtoken", "accesstoken", "secret", "authorization",
	"cookie", "apikey", "api_key", "privatekey", "bindpassword",
}

// Redact replaces the value of any sensitive-looking key with a placeholder,
// recursively. Matching is on a normalized key, so "Password", "password_hash"
// and "currentPassword" are all caught.
func Redact(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		if isSensitive(k) {
			out[k] = "[redacted]"
			continue
		}
		switch nested := v.(type) {
		case map[string]any:
			out[k] = Redact(nested)
		default:
			out[k] = v
		}
	}
	return out
}

func isSensitive(key string) bool {
	normalized := strings.ToLower(strings.NewReplacer("-", "", "_", "", " ", "").Replace(key))
	for _, s := range sensitiveKeys {
		if strings.Contains(normalized, strings.ReplaceAll(s, "_", "")) {
			return true
		}
	}
	return false
}

// Diff builds a before/after map containing only the fields that actually
// changed, so the trail records the change rather than the whole record.
func Diff(before, after map[string]any) map[string]any {
	changes := map[string]any{}
	for key, newValue := range after {
		oldValue, existed := before[key]
		if !existed || !equal(oldValue, newValue) {
			changes[key] = map[string]any{"from": oldValue, "to": newValue}
		}
	}
	if len(changes) == 0 {
		return nil
	}
	return changes
}

func equal(a, b any) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a == b
}

// Ref is shorthand for the common "one resource, no field detail" event.
func Ref(id uuid.UUID) string { return id.String() }

func clientIP(r *http.Request) string {
	// chi's RealIP middleware has already normalized this when the proxy sent
	// X-Forwarded-For; RemoteAddr is the fallback.
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i != -1 && strings.Count(host, ":") == 1 {
		host = host[:i]
	}
	return host
}

func truncate(s string, max int) string {
	if len(s) > max {
		return s[:max]
	}
	return s
}
