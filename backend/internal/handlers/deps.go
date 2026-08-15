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
	}
}
