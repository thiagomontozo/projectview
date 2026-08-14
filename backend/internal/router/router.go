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
	"projectview/internal/ws"
)

func New(api *handlers.API, cfg *config.Config, hub *ws.Hub) http.Handler {
	r := chi.NewRouter()
	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(chimw.Logger)
	r.Use(chimw.Recoverer)
	r.Use(corsMiddleware(cfg.CORSOrigin))

	r.Get("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})

	// Realtime push channel (auth via ?token=, see handlers.ServeWS).
	r.Get("/ws", api.ServeWS)

	requireAuth := auth.RequireAuth(api.Users, cfg)

	r.Route("/api/auth", func(r chi.Router) {
		r.Get("/config", api.AuthConfig)
		r.Post("/login", api.Login)
		r.Post("/logout", api.Logout)
		r.With(requireAuth).Get("/me", api.Me)
		// Account creation mints accounts and assigns roles, so it is an
		// administrative action - never an anonymous one.
		r.With(requireAuth, auth.RequireRole(models.RoleAdmin)).Post("/register", api.Register)
	})

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
	})

	r.Route("/api/tasks", func(r chi.Router) {
		r.Use(requireAuth)
		r.Get("/mine", api.MyTasks)
		r.Post("/", api.CreateTask)
		r.Get("/{id}", api.GetTask)
		r.Put("/{id}", api.UpdateTask)
		r.Patch("/{id}/move", api.MoveTask)
		r.Delete("/{id}", api.DeleteTask)
		r.Post("/{id}/comments", api.AddComment)
	})

	r.Route("/api/chat", func(r chi.Router) {
		r.Use(requireAuth)
		r.Get("/channels", api.ListChannels)
		r.Post("/channels", api.CreateChannel)
		r.Get("/channels/{channelId}/messages", api.GetMessages)
		r.Post("/channels/{channelId}/messages", api.PostMessage)
	})

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
