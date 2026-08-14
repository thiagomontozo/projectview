// Package seed bootstraps a brand-new database with a default admin user,
// a starter team and a starter project, so the app is usable immediately
// after "docker compose up" even before AD/SMTP are configured.
package seed

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"golang.org/x/crypto/bcrypt"

	"projectview/internal/config"
	"projectview/internal/db"
	"projectview/internal/logger"
	"projectview/internal/models"
)

func Run(ctx context.Context, store *db.Store, cfg *config.Config) error {
	count, err := store.Users().CountDocuments(ctx, bson.M{})
	if err != nil {
		return err
	}
	if count > 0 {
		logger.Info("Database already has users, skipping bootstrap seed.")
		return nil
	}

	logger.Info("No users found - creating default admin account and sample workspace.")

	hash, err := bcrypt.GenerateFromPassword([]byte(cfg.Bootstrap.AdminPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	now := time.Now()
	admin := models.User{
		ID:            primitive.NewObjectID(),
		Username:      cfg.Bootstrap.AdminUsername,
		Name:          cfg.Bootstrap.AdminName,
		Email:         cfg.Bootstrap.AdminEmail,
		PasswordHash:  string(hash),
		AuthSource:    models.AuthSourceLocal,
		Role:          models.RoleAdmin,
		AvatarColor:   "#2a78d6",
		Teams:         []primitive.ObjectID{},
		Active:        true,
		NotifyByEmail: true,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if _, err := store.Users().InsertOne(ctx, admin); err != nil {
		return err
	}

	team := models.Team{
		ID:          primitive.NewObjectID(),
		Name:        "Default Team",
		Description: "Automatically created on first run. Rename or replace as needed.",
		Color:       "#0ea5e9",
		Members:     []primitive.ObjectID{admin.ID},
		LeadID:      &admin.ID,
		CreatedBy:   admin.ID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if _, err := store.Teams().InsertOne(ctx, team); err != nil {
		return err
	}

	if _, err := store.Users().UpdateByID(ctx, admin.ID, bson.M{"$addToSet": bson.M{"teams": team.ID}}); err != nil {
		return err
	}

	project := models.Project{
		ID:          primitive.NewObjectID(),
		Name:        "Sample Project",
		Key:         "SAMPLE",
		Description: "A starter project you can rename, archive, or delete.",
		Color:       "#8b5cf6",
		Status:      "planning",
		Team:        &team.ID,
		Members:     []primitive.ObjectID{admin.ID},
		Owner:       admin.ID,
		Statuses:    models.DefaultStatuses(),
		CreatedBy:   admin.ID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if _, err := store.Projects().InsertOne(ctx, project); err != nil {
		return err
	}

	channel := models.ChatChannel{
		ID:        primitive.NewObjectID(),
		Name:      "# " + project.Name,
		Type:      "project",
		Project:   &project.ID,
		Team:      &team.ID,
		Members:   []primitive.ObjectID{admin.ID},
		CreatedBy: admin.ID,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if _, err := store.ChatChannels().InsertOne(ctx, channel); err != nil {
		return err
	}

	logger.Info(
		"Bootstrap complete. Login with username %q and the password from BOOTSTRAP_ADMIN_PASSWORD. Change it immediately in production.",
		cfg.Bootstrap.AdminUsername,
	)
	return nil
}
