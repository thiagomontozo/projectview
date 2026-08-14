// Package handlers implements every HTTP endpoint of the API.
//
// The JSON contract is identical to the document-store era: ids are still
// opaque strings (UUIDs now instead of ObjectID hex), field names and response
// shapes are unchanged, so the frontend and the end-to-end smoke test both
// carried over untouched across the storage migration.
package handlers

import (
	"projectview/internal/config"
	"projectview/internal/db"
	"projectview/internal/repo"
	"projectview/internal/ws"
)

// API holds the shared dependencies every handler needs: the repositories,
// configuration, and the WebSocket hub used for realtime chat/notifications.
type API struct {
	Store         *db.Store
	Cfg           *config.Config
	Hub           *ws.Hub
	Users         *repo.Users
	Teams         *repo.Teams
	Projects      *repo.Projects
	Tasks         *repo.Tasks
	Chat          *repo.Chat
	Notifications *repo.Notifications
	Dashboard     *repo.Dashboard
}

func New(store *db.Store, cfg *config.Config, hub *ws.Hub) *API {
	return &API{
		Store:         store,
		Cfg:           cfg,
		Hub:           hub,
		Users:         repo.NewUsers(store),
		Teams:         repo.NewTeams(store),
		Projects:      repo.NewProjects(store),
		Tasks:         repo.NewTasks(store),
		Chat:          repo.NewChat(store),
		Notifications: repo.NewNotifications(store),
		Dashboard:     repo.NewDashboard(store),
	}
}
