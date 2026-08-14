// Command server boots the ProjectView API: it loads configuration, connects
// to PostgreSQL (applying the embedded migrations, which is how the schema is
// created on first run), seeds a default admin/team/project if the database is
// empty, starts the deadline alert scheduler, and serves the HTTP + WebSocket
// API.
package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	// Embeds the IANA timezone database in the binary, so the runtime image
	// does not need the tzdata package for ALERT_CRON and date formatting.
	_ "time/tzdata"

	"github.com/joho/godotenv"

	"projectview/internal/config"
	"projectview/internal/db"
	"projectview/internal/handlers"
	"projectview/internal/logger"
	"projectview/internal/router"
	"projectview/internal/seed"
	"projectview/internal/services"
	"projectview/internal/ws"
)

func main() {
	_ = godotenv.Load() // optional; environment variables always take precedence in containers

	cfg := config.Load()

	store, err := db.Connect(cfg)
	if err != nil {
		logger.Error("failed to connect to PostgreSQL: %v", err)
		os.Exit(1)
	}
	defer store.Close()

	hub := ws.NewHub()
	api := handlers.New(store, cfg, hub)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	if err := seed.Run(ctx, api.Users, api.Teams, api.Projects, api.Chat, cfg); err != nil {
		logger.Error("failed to seed database: %v", err)
	}
	cancel()

	mailer := services.NewMailer(cfg)
	notifier := services.NewNotifier(api.Notifications, api.Users, hub, mailer)
	alertScheduler := services.NewAlertScheduler(api.Tasks, cfg, notifier)
	alertScheduler.Start()

	handler := router.New(api, cfg, hub)

	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		logger.Info("API + WebSocket server listening on port %s (env: %s)", cfg.Port, cfg.NodeEnv)
		logger.Info("AD authentication: %s", enabledLabel(cfg.AD.Enabled))
		logger.Info("SMTP email: %s", enabledLabel(cfg.SMTP.Enabled))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error: %v", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit

	logger.Info("Shutting down...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	_ = srv.Shutdown(shutdownCtx)
}

func enabledLabel(enabled bool) string {
	if enabled {
		return "ENABLED"
	}
	return "disabled"
}
