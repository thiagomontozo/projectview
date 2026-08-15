// Package seed bootstraps a brand-new database with a default admin user, a
// starter team and a starter project, so the app is usable immediately after
// "docker compose up" even before AD/SMTP are configured.
package seed

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"projectview/internal/auth"
	"projectview/internal/config"
	"projectview/internal/logger"
	"projectview/internal/models"
	"projectview/internal/repo"
)

// Run creates the starter workspace when the database has no users yet.
func Run(ctx context.Context, users *repo.Users, teams *repo.Teams, spaces *repo.Spaces, projects *repo.Projects, chat *repo.Chat, cfg *config.Config) error {
	count, err := users.Count(ctx)
	if err != nil {
		return err
	}
	if count > 0 {
		logger.Info("Database already has users, skipping bootstrap seed.")
		return nil
	}

	logger.Info("No users found - creating default admin account and sample workspace.")

	hash, err := auth.HashPassword(cfg.Bootstrap.AdminPassword)
	if err != nil {
		return err
	}

	admin := &models.User{
		ID:            uuid.New(),
		Username:      strings.ToLower(cfg.Bootstrap.AdminUsername),
		Name:          cfg.Bootstrap.AdminName,
		Email:         strings.ToLower(cfg.Bootstrap.AdminEmail),
		PasswordHash:  string(hash),
		AuthSource:    models.AuthSourceLocal,
		Role:          models.RoleAdmin,
		AvatarColor:   "#2a78d6",
		Active:        true,
		NotifyByEmail: true,
	}
	if err := users.Create(ctx, admin); err != nil {
		return err
	}

	team := &models.Team{
		ID:          uuid.New(),
		Name:        "Default Team",
		Description: "Automatically created on first run. Rename or replace as needed.",
		Color:       "#0ea5e9",
		Members:     []uuid.UUID{admin.ID},
		LeadID:      &admin.ID,
		CreatedBy:   &admin.ID,
	}
	if err := teams.Create(ctx, team); err != nil {
		return err
	}

	// The hierarchy starts populated, so a fresh install already shows the
	// Space -> List structure rather than an empty shell.
	space := &repo.Space{
		ID:          uuid.New(),
		Name:        "Workspace",
		Description: "Top level of the hierarchy. Create more spaces per department, client or initiative.",
		Color:       "#2a78d6",
		CreatedBy:   &admin.ID,
	}
	if err := spaces.Create(ctx, space, []repo.SpaceMember{
		{UserID: admin.ID, Role: repo.SpaceRoleOwner},
	}); err != nil {
		return err
	}

	project := &models.Project{
		ID:          uuid.New(),
		Name:        "Sample Project",
		Key:         "SAMPLE",
		Description: "A starter project you can rename, archive, or delete.",
		Color:       "#8b5cf6",
		Status:      models.ProjectStatusPlanning,
		SpaceID:     &space.ID,
		TeamID:      &team.ID,
		Members:     []uuid.UUID{admin.ID},
		Owner:       &admin.ID,
		Statuses:    models.DefaultStatuses(),
		CreatedBy:   &admin.ID,
	}
	if err := projects.Create(ctx, project); err != nil {
		return err
	}

	channel := &models.ChatChannel{
		ID:        uuid.New(),
		Name:      "# " + project.Name,
		Type:      models.ChannelTypeProject,
		ProjectID: &project.ID,
		TeamID:    &team.ID,
		Members:   []uuid.UUID{admin.ID},
		CreatedBy: &admin.ID,
	}
	if err := chat.CreateChannel(ctx, channel); err != nil {
		return err
	}

	logger.Info(
		"Bootstrap complete. Login with username %q and the password from BOOTSTRAP_ADMIN_PASSWORD. Change it immediately in production.",
		cfg.Bootstrap.AdminUsername,
	)
	return nil
}
