// Package router wires every HTTP route to its handler and middleware.
//
// Authorization note: beyond the role gates wired below, per-resource rules
// (project membership, team leadership, self-vs-admin) are enforced inside the
// handlers, where the target row is available. See handlers/access.go.
package router

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"projectview/internal/auth"
	"projectview/internal/config"
	"projectview/internal/handlers"
	"projectview/internal/models"
	"projectview/internal/obs"
	"projectview/internal/ws"
)

func New(api *handlers.API, cfg *config.Config, hub *ws.Hub, metrics *obs.Metrics) http.Handler {
	r := chi.NewRouter()
	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	// Replaces chi's text logger: one structured record per request, carrying
	// the request id so a report can be traced back to the calls behind it.
	r.Use(obs.Observe(metrics))
	r.Use(chimw.Recoverer)
	r.Use(corsMiddleware(cfg.CORSOrigin))

	r.Get("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})

	// Readiness is distinct from liveness: this one fails when the database
	// is unreachable, so an orchestrator stops routing traffic here.
	r.Get("/api/ready", api.Ready)

	// Prometheus scrape target. Not published through the edge proxy.
	r.Get("/metrics", metrics.Handler())

	// Realtime push channel (auth via ?token=, see handlers.ServeWS).
	r.Get("/ws", api.ServeWS)

	requireAuth := auth.RequireAuth(api.Users, api.Sessions, cfg)

	r.Route("/api/auth", func(r chi.Router) {
		r.Get("/config", api.AuthConfig)
		r.Post("/login", api.Login)
		r.Post("/logout", api.Logout)
		// Refresh authenticates with the refresh cookie itself, so it sits
		// outside RequireAuth: the access token is expected to be expired.
		r.Post("/refresh", api.Refresh)
		r.With(requireAuth).Get("/me", api.Me)
		r.With(requireAuth).Get("/sessions", api.ListSessions)
		r.With(requireAuth).Delete("/sessions/{id}", api.RevokeSession)
		// Account creation mints accounts and assigns roles, so it is an
		// administrative action - never an anonymous one.
		r.With(requireAuth, auth.RequireRole(models.RoleAdmin)).Post("/register", api.Register)
	})

	r.Route("/api/spaces", func(r chi.Router) {
		r.Use(requireAuth)
		r.Get("/", api.ListSpaces)
		r.Post("/", api.CreateSpace)
		r.Get("/{id}", api.GetSpace)
		r.Put("/{id}", api.UpdateSpace)
		r.Delete("/{id}", api.DeleteSpace)
		r.Post("/{id}/members", api.SetSpaceMember)
		r.Delete("/{id}/members/{userId}", api.RemoveSpaceMember)
		r.Get("/{id}/folders", api.ListFolders)
		r.Post("/{id}/folders", api.CreateFolder)
	})

	r.Route("/api/folders", func(r chi.Router) {
		r.Use(requireAuth)
		r.Put("/{id}", api.UpdateFolder)
		r.Delete("/{id}", api.DeleteFolder)
	})

	// The audit trail records the whole installation's activity, including
	// failed logins, so reading it is an administrative action.
	r.With(requireAuth, auth.RequireRole(models.RoleAdmin)).Get("/api/audit", api.ListAudit)

	r.Route("/api/users", func(r chi.Router) {
		r.Use(requireAuth)
		r.Get("/", api.ListUsers)
		r.Get("/workload", api.Workload)
		r.Get("/{id}", api.GetUser)
		r.Put("/{id}", api.UpdateUser)
		r.Post("/{id}/password", api.ChangePassword)
	})

	r.Route("/api/teams", func(r chi.Router) {
		r.Use(requireAuth)
		r.Get("/", api.ListTeams)
		r.Post("/", api.CreateTeam)
		r.Get("/{id}", api.GetTeam)
		r.Put("/{id}", api.UpdateTeam)
		r.Delete("/{id}", api.DeleteTeam)
		r.Post("/{id}/members", api.AddTeamMember)
		r.Delete("/{id}/members/{userId}", api.RemoveTeamMember)
	})

	r.Route("/api/projects", func(r chi.Router) {
		r.Use(requireAuth)
		r.Get("/", api.ListProjects)
		r.Post("/", api.CreateProject)
		r.Get("/{id}", api.GetProject)
		r.Put("/{id}", api.UpdateProject)
		r.Delete("/{id}", api.DeleteProject)
		r.Get("/{projectId}/tasks", api.ListTasksForProject)
		r.Post("/{projectId}/tasks", api.CreateTaskForProject)
		// Everything the timeline needs to draw arrows and highlight the
		// chain where a slip moves the end date.
		r.Get("/{projectId}/schedule", api.ProjectSchedule)
		r.Get("/{projectId}/fields", api.ListCustomFields)
		r.Post("/{projectId}/fields", api.CreateCustomField)
		r.Get("/{projectId}/automations", api.ListAutomations)
		r.Post("/{projectId}/automations", api.CreateAutomation)
	})

	r.Route("/api/fields", func(r chi.Router) {
		r.Use(requireAuth)
		r.Delete("/{id}", api.DeleteCustomField)
	})

	r.Route("/api/automations", func(r chi.Router) {
		r.Use(requireAuth)
		r.Delete("/{id}", api.DeleteAutomation)
		r.Get("/{id}/runs", api.AutomationRuns)
	})

	r.Route("/api/time", func(r chi.Router) {
		r.Use(requireAuth)
		r.Get("/running", api.RunningTimer)
		r.Post("/stop", api.StopTimer)
	})

	r.Route("/api/tasks", func(r chi.Router) {
		r.Use(requireAuth)
		r.Get("/", api.SearchTasks)
		r.Get("/mine", api.MyTasks)
		r.Post("/", api.CreateTask)
		r.Get("/{id}", api.GetTask)
		r.Put("/{id}", api.UpdateTask)
		r.Patch("/{id}/move", api.MoveTask)
		r.Delete("/{id}", api.DeleteTask)
		r.Post("/{id}/comments", api.AddComment)

		r.Post("/{id}/dependencies", api.AddDependency)
		r.Delete("/{id}/dependencies/{dependsOn}", api.RemoveDependency)

		r.Put("/{id}/fields", api.SetTaskCustomFields)

		r.Get("/{id}/time", api.TaskTimeEntries)
		r.Post("/{id}/time", api.LogTime)
		r.Post("/{id}/time/start", api.StartTimer)

		r.Post("/{id}/watch", api.WatchTask)
		r.Delete("/{id}/watch", api.UnwatchTask)
	})

	r.Route("/api/chat", func(r chi.Router) {
		r.Use(requireAuth)
		r.Get("/channels", api.ListChannels)
		r.Post("/channels", api.CreateChannel)
		r.Get("/channels/{channelId}/messages", api.GetMessages)
		r.Post("/channels/{channelId}/messages", api.PostMessage)
		r.Get("/messages/{id}/replies", api.MessageThread)
		r.Post("/messages/{id}/replies", api.ReplyToMessage)
		r.Post("/messages/{id}/reactions", api.ToggleReaction)
	})

	r.Route("/api/docs", func(r chi.Router) {
		r.Use(requireAuth)
		r.Get("/", api.ListDocs)
		r.Post("/", api.CreateDoc)
		r.Get("/{id}", api.GetDoc)
		r.Put("/{id}", api.UpdateDoc)
		r.Delete("/{id}", api.DeleteDoc)
		r.Get("/{id}/revisions", api.DocRevisions)
		r.Get("/{id}/revisions/{revisionId}", api.DocRevision)
	})

	r.With(requireAuth).Get("/api/presence", api.Presence)

	r.Route("/api/dashboard", func(r chi.Router) {
		r.Use(requireAuth)
		r.Get("/overview", api.Overview)
		r.Get("/status-breakdown", api.StatusBreakdown)
		r.Get("/workload-chart", api.WorkloadChart)
		r.Get("/project-progress", api.ProjectProgress)
		r.Get("/completion-trend", api.CompletionTrend)
	})

	r.Route("/api/notifications", func(r chi.Router) {
		r.Use(requireAuth)
		r.Get("/", api.ListNotifications)
		r.Get("/preferences", api.GetNotificationPreferences)
		r.Put("/preferences", api.SetNotificationPreferences)
		r.Post("/{id}/read", api.MarkNotificationRead)
		r.Post("/read-all", api.MarkAllNotificationsRead)
	})

	return r
}

func corsMiddleware(origin string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
