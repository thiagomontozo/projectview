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
	"projectview/internal/repo"
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

		// Single sign-on. The whole flow is anonymous by definition: it is how
		// somebody who has no session yet obtains one.
		r.Get("/oidc/config", api.OIDCConfig)
		r.Get("/oidc/start", api.OIDCStart)
		r.Get("/oidc/callback", api.OIDCCallback)
	})

	// SCIM 2.0 provisioning. Authenticated by a service token, never by a user
	// session: a provisioning client is not a person and must not be able to
	// act as one.
	r.Route("/scim/v2", func(r chi.Router) {
		r.Use(api.RequireServiceToken(repo.ScopeSCIM))
		r.Get("/Users", api.SCIMListUsers)
		r.Post("/Users", api.SCIMCreateUser)
		r.Get("/Users/{id}", api.SCIMGetUser)
		r.Put("/Users/{id}", api.SCIMReplaceUser)
		r.Patch("/Users/{id}", api.SCIMPatchUser)
		r.Delete("/Users/{id}", api.SCIMDeleteUser)
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

		// The two privacy rights. Export is self-service; erasure is not.
		r.Get("/{id}/data-export", api.ExportPersonalData)
		r.With(auth.RequireRole(models.RoleAdmin)).Post("/{id}/erase", api.ErasePersonalData)
	})

	r.Route("/api/goals", func(r chi.Router) {
		r.Use(requireAuth)
		r.Get("/", api.ListGoals)
		r.Post("/", api.CreateGoal)
		r.Get("/{id}", api.GetGoal)
		r.Put("/{id}", api.UpdateGoal)
		r.Delete("/{id}", api.DeleteGoal)
		r.Post("/{id}/key-results", api.AddKeyResult)
		r.Put("/{id}/key-results/{keyResultId}", api.SetKeyResultValue)
		r.Delete("/{id}/key-results/{keyResultId}", api.DeleteKeyResult)
	})

	r.Route("/api/portfolio", func(r chi.Router) {
		r.Use(requireAuth)
		r.Get("/", api.PortfolioSummary)
		r.Get("/export.csv", api.ExportPortfolio)
		r.Get("/capacity", api.CapacityReport)
		r.Get("/capacity/export.csv", api.ExportCapacity)
	})

	r.With(requireAuth).Delete("/api/baselines/{id}", api.DeleteBaseline)

	// Configuration decides who may sign in and where mail goes. Reading it is
	// close to being able to take the installation over, so the whole group is
	// administrators only - reads included.
	r.Route("/api/settings", func(r chi.Router) {
		r.Use(requireAuth, auth.RequireRole(models.RoleAdmin))
		r.Get("/", api.GetSettings)
		r.Put("/", api.SaveSettings)
		r.Get("/env", api.DownloadEnv)
		r.Post("/test/smtp", api.TestSMTP)
		r.Post("/test/ad", api.TestAD)
	})

	// Machine credentials are administrative: a token is a way in that outlives
	// whoever created it.
	r.Route("/api/service-tokens", func(r chi.Router) {
		r.Use(requireAuth, auth.RequireRole(models.RoleAdmin))
		r.Get("/", api.ListServiceTokens)
		r.Post("/", api.CreateServiceToken)
		r.Delete("/{id}", api.RevokeServiceToken)
	})

	// Searching Active Directory for people who may have no account here yet.
	// Role-gated inside the handler, where the answer can also explain why the
	// search is unavailable rather than merely refusing it.
	r.With(requireAuth).Get("/api/directory/search", api.SearchDirectory)

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
		// How many tasks sit in each board column under the current filters,
		// so a paged column can say "100 of 3,412" rather than implying it is
		// showing everything.
		r.Get("/{projectId}/tasks/counts", api.TaskCounts)
		r.Post("/{projectId}/tasks", api.CreateTaskForProject)
		// Everything the timeline needs to draw arrows and highlight the
		// chain where a slip moves the end date.
		r.Get("/{projectId}/schedule", api.ProjectSchedule)
		r.Get("/{projectId}/intake", api.ListIntakeForms)
		r.Post("/{projectId}/intake", api.CreateIntakeForm)
		r.Get("/{projectId}/fields", api.ListCustomFields)
		r.Post("/{projectId}/fields", api.CreateCustomField)
		r.Get("/{projectId}/automations", api.ListAutomations)
		r.Post("/{projectId}/automations", api.CreateAutomation)

		// Baselines and earned value. The route parameter is {id} here rather
		// than {projectId} because these handlers load the project directly.
		r.Get("/{id}/baselines", api.ListBaselines)
		r.Post("/{id}/baselines", api.CaptureBaseline)
		r.Get("/{id}/earned-value", api.EarnedValue)
		r.Get("/{id}/export.csv", api.ExportProjectTasks)
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

		r.Get("/{id}/attachments", api.ListAttachments)
		r.Post("/{id}/attachments", api.UploadAttachment)

		// The recurrence rule lives on the task currently carrying it, so it is
		// read and written through that task rather than as a series of its own.
		r.Get("/{id}/recurrence", api.GetRecurrence)
		r.Put("/{id}/recurrence", api.SetRecurrence)
		r.Delete("/{id}/recurrence", api.DeleteRecurrence)
	})

	// Templates. Listing is open to any authenticated user - a template is a
	// suggestion, and one nobody can see is one nobody uses - while capturing,
	// applying a project template and deleting are gated in the handlers, where
	// the target scope is known.
	r.Route("/api/intake", func(r chi.Router) {
		r.Use(requireAuth)
		r.Patch("/{id}", api.SetIntakeFormEnabled)
		r.Delete("/{id}", api.DeleteIntakeForm)
		r.Get("/{id}/submissions", api.IntakeSubmissions)
	})

	// The public half of intake. Outside every authenticated group, so it
	// cannot inherit a permission by being nested under one - that, not the
	// URL prefix, is the safety property.
	//
	// It stays under /api because the edge proxy routes by prefix and anything
	// else reaches the SPA instead; /public says plainly what it is, and leaves
	// /intake/:slug free for the page that renders the form. A private form
	// still answers 404 to anyone not signed in.
	r.Get("/api/public/intake/{slug}", api.PublicIntakeForm)
	r.Post("/api/public/intake/{slug}", api.SubmitIntakeForm)

	r.Route("/api/templates", func(r chi.Router) {
		r.Use(requireAuth)
		r.Get("/", api.ListTemplates)
		r.Post("/", api.CreateTemplate)
		r.Post("/{id}/apply", api.ApplyTemplate)
		r.Delete("/{id}", api.DeleteTemplate)
	})

	// Attachments are addressed by their own id once they exist, because a
	// download link has to survive being pasted somewhere: routing it under the
	// task would break the moment a file moved between a task and one of its
	// comments.
	r.Route("/api/attachments", func(r chi.Router) {
		r.Use(requireAuth)
		r.Get("/config", api.AttachmentConfig)
		// Redirects to a time-limited signed URL served by the object store;
		// the bytes never pass through this process.
		r.Get("/{id}", api.DownloadAttachment)
		r.Get("/{id}/url", api.AttachmentURL)
		r.Delete("/{id}", api.DeleteAttachment)
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
		// Editing is the author's own, and stamped. Attachments hang off the
		// message and are gated by membership of the channel rather than of a
		// project - a different resolution, which is why it took its own path.
		r.Put("/messages/{id}", api.EditMessage)
		r.Post("/messages/{id}/attachments", api.UploadMessageAttachment)
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

		// The card arrangement, saved per person so it follows them between
		// machines instead of living in one browser's local storage.
		r.Get("/layout", api.GetDashboardLayout)
		r.Put("/layout", api.SaveDashboardLayout)
		r.Delete("/layout", api.ResetDashboardLayout)
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
