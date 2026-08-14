// Package handlers implements every HTTP endpoint of the API, mirroring the
// original Node/Express routes 1:1 so the frontend contract stays the same.
package handlers

import (
	"projectview/internal/config"
	"projectview/internal/db"
	"projectview/internal/ws"
)

// API holds the shared dependencies every handler needs: the Mongo store,
// configuration, and the WebSocket hub used for realtime chat/notifications.
type API struct {
	Store *db.Store
	Cfg   *config.Config
	Hub   *ws.Hub
}

func New(store *db.Store, cfg *config.Config, hub *ws.Hub) *API {
	return &API{Store: store, Cfg: cfg, Hub: hub}
}
