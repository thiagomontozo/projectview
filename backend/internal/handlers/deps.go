// Package handlers implements every HTTP endpoint of the API.
//
// The JSON contract is stable across storage changes: ids are opaque strings,
// field names and response shapes do not move, which is what let the
// end-to-end smoke test carry over untouched through the migration off the
// document store.
package handlers

import (
	"projectview/internal/audit"
	"projectview/internal/config"
	"projectview/internal/db"
	"projectview/internal/obs"
	"projectview/internal/repo"
	"projectview/internal/services"
	"projectview/internal/ws"
)

// API holds the shared dependencies every handler needs.
type API struct {
	Store         *db.Store
	Cfg           *config.Config
	Hub           *ws.Hub
	Metrics       *obs.Metrics
	Audit         *audit.Recorder
	Users         *repo.Users
	Sessions      *repo.Sessions
	Teams         *repo.Teams
	Spaces        *repo.Spaces
	Folders       *repo.Folders
	Projects      *repo.Projects
	Tasks         *repo.Tasks
	Chat          *repo.Chat
	Notifications *repo.Notifications
	Dashboard     *repo.Dashboard
	AuditLog      *repo.Audit
	Dependencies  *repo.Dependencies
	CustomFields  *repo.CustomFields
	Time          *repo.TimeTracking
	Watchers      *repo.Watchers
	Automations   *repo.Automations
	Docs          *repo.Docs
	Preferences   *repo.Preferences
	// Engine runs the automation rules. Set by main once the notifier exists,
	// since the two depend on each other's construction order.
	Engine *services.AutomationEngine
}

func New(store *db.Store, cfg *config.Config, hub *ws.Hub, metrics *obs.Metrics) *API {
	auditRepo := repo.NewAudit(store)
	return &API{
		Store:         store,
		Cfg:           cfg,
		Hub:           hub,
		Metrics:       metrics,
		Audit:         audit.New(auditRepo),
		Users:         repo.NewUsers(store),
		Sessions:      repo.NewSessions(store),
		Teams:         repo.NewTeams(store),
		Spaces:        repo.NewSpaces(store),
		Folders:       repo.NewFolders(store),
		Projects:      repo.NewProjects(store),
		Tasks:         repo.NewTasks(store),
		Chat:          repo.NewChat(store),
		Notifications: repo.NewNotifications(store),
		Dashboard:     repo.NewDashboard(store),
		AuditLog:      auditRepo,
		Dependencies:  repo.NewDependencies(store),
		CustomFields:  repo.NewCustomFields(store),
		Time:          repo.NewTimeTracking(store),
		Watchers:      repo.NewWatchers(store),
		Automations:   repo.NewAutomations(store),
		Docs:          repo.NewDocs(store),
		Preferences:   repo.NewPreferences(store),
	}
}
