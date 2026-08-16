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

	"github.com/google/uuid"

	"github.com/joho/godotenv"

	"projectview/internal/auth"
	"projectview/internal/config"
	"projectview/internal/db"
	"projectview/internal/handlers"
	"projectview/internal/logger"
	"projectview/internal/obs"
	"projectview/internal/router"
	"projectview/internal/seed"
	"projectview/internal/services"
	"projectview/internal/storage"
	"projectview/internal/ws"
)

func main() {
	_ = godotenv.Load() // optional; environment variables always take precedence in containers

	cfg := config.Load()
	logger.Configure(cfg.Log.Format, cfg.Log.Level, cfg.NodeEnv)

	store, err := db.Connect(cfg)
	if err != nil {
		logger.Error("failed to connect to PostgreSQL: %v", err)
		os.Exit(1)
	}
	defer store.Close()

	hub := ws.NewHub()
	metrics := obs.NewMetrics()
	api := handlers.New(store, cfg, hub, metrics)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	if err := seed.Run(ctx, api.Users, api.Teams, api.Spaces, api.Projects, api.Chat, cfg); err != nil {
		logger.Error("failed to seed database: %v", err)
	}
	cancel()

	// Stored settings override the environment, and they must be in force
	// before anything reads the configuration - the alert scheduler and the
	// OIDC client both capture it at construction.
	settingsCtx, cancelSettings := context.WithTimeout(context.Background(), 10*time.Second)
	if overrides, err := api.Settings.Overrides(settingsCtx); err != nil {
		logger.Error("could not load saved settings, using the environment alone: %v", err)
	} else if len(overrides) > 0 {
		cfg.Apply(overrides)
		logger.Info("Applied %d saved setting(s) over the environment", len(overrides))
	}
	cancelSettings()

	// The mirror is a copy for backup and review; the database is what the
	// process actually reads. Disabled when no path is configured.
	api.EnvMirror = services.NewEnvMirror(os.Getenv("SETTINGS_ENV_FILE"))
	if api.EnvMirror.Enabled() {
		logger.Info("Settings mirror: %s", api.EnvMirror.Path())
	}

	// Object storage for attachments. Absent configuration is a supported
	// deployment, not an error: the attachment endpoints answer 503 and
	// everything else works exactly as before.
	objects, err := storage.New(storage.Config{
		Endpoint:       cfg.Storage.Endpoint,
		PublicURL:      cfg.Storage.PublicURL,
		Region:         cfg.Storage.Region,
		Bucket:         cfg.Storage.Bucket,
		AccessKey:      cfg.Storage.AccessKey,
		SecretKey:      cfg.Storage.SecretKey,
		ForcePathStyle: cfg.Storage.ForcePathStyle,
	})
	if err != nil {
		logger.Error("attachment storage is misconfigured, attachments will be unavailable: %v", err)
	}
	api.Objects = objects

	if objects.Enabled() {
		// Probed at boot so a wrong bucket or a rejected key is a line in the
		// startup log rather than a failure the first person to attach a file
		// discovers. Deliberately not fatal: the rest of the application is
		// perfectly usable without it.
		checkCtx, cancelCheck := context.WithTimeout(context.Background(), 10*time.Second)
		if err := objects.Check(checkCtx); err != nil {
			logger.Error("Attachments: bucket %q is not usable: %v", objects.Bucket(), err)
		} else {
			logger.Info("Attachments: storing in bucket %q (max %d MB per file)",
				objects.Bucket(), cfg.Attachments.MaxBytes>>20)
		}
		cancelCheck()

		// Drains the keys the delete trigger queues, so removing a task or a
		// whole project takes its files with it rather than only its rows.
		services.NewObjectSweeper(api.Attachments, objects).Start()
	} else {
		logger.Info("Attachments: no object storage configured, file uploads are disabled")
	}

	mailer := services.NewMailer(cfg)
	api.Mailer = mailer
	notifier := services.NewNotifier(api.Notifications, api.Users, hub, mailer)

	// The engine needs the notifier, and the handlers need the engine, so it
	// is attached after both exist rather than built inside handlers.New.
	api.Engine = services.NewAutomationEngine(api.Automations, api.Tasks, api.Watchers, notifier)

	alertScheduler := services.NewAlertScheduler(api.Tasks, cfg, notifier, api.Engine)
	alertScheduler.Start()

	services.NewDigestScheduler(api.Preferences, mailer).Start()

	// Retention does nothing unless a policy is configured; see the comment on
	// RetentionSweeper for why the default is to keep everything.
	services.NewRetentionSweeper(api.Privacy, cfg).Start()

	if cfg.OIDC().Enabled {
		api.OIDC = auth.NewOIDC(cfg)
		logger.Info("Single sign-on enabled against %s (auto-provision: %v)",
			cfg.OIDC().IssuerURL, cfg.OIDC().AutoProvision)
	}

	// Presence and typing payloads carry a display name, so clients do not
	// have to hold the whole directory to render "Ana is typing".
	hub.SetNameResolver(func(userID string) string {
		id, err := uuid.Parse(userID)
		if err != nil {
			return ""
		}
		lookupCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if user, err := api.Users.ByID(lookupCtx, id); err == nil {
			return user.Name
		}
		return ""
	})

	// Expired and revoked sessions accumulate forever otherwise; prune them
	// hourly so the table stays proportional to actual usage.
	go pruneSessions(api)

	handler := router.New(api, cfg, hub, metrics)

	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		logger.Info("API + WebSocket server listening on port %s (env: %s)", cfg.Port, cfg.NodeEnv)
		logger.Info("AD authentication: %s", enabledLabel(cfg.AD().Enabled))
		logger.Info("SMTP email: %s", enabledLabel(cfg.SMTP().Enabled))
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

// pruneSessions deletes sessions that expired or were revoked long enough ago
// to be of no forensic interest.
func pruneSessions(api *handlers.API) {
	const retention = 7 * 24 * time.Hour
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		if n, err := api.Sessions.DeleteExpired(context.Background(), retention); err != nil {
			logger.Warn("session cleanup failed: %v", err)
		} else if n > 0 {
			logger.Info("Pruned %d expired session(s)", n)
		}
		<-ticker.C
	}
}

func enabledLabel(enabled bool) string {
	if enabled {
		return "ENABLED"
	}
	return "disabled"
}
