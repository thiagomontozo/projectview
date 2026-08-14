// Package db owns the MongoDB connection and is responsible for creating
// the full schema (collections + indexes) the very first time the app boots
// against an empty database.
package db

import (
	"context"
	"net/url"
	"regexp"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"projectview/internal/config"
	"projectview/internal/logger"
)

type Store struct {
	Client *mongo.Client
	DB     *mongo.Database
}

const (
	ColUsers         = "users"
	ColTeams         = "teams"
	ColProjects      = "projects"
	ColTasks         = "tasks"
	ColChatChannels  = "chatchannels"
	ColChatMessages  = "chatmessages"
	ColNotifications = "notifications"
)

var maskCreds = regexp.MustCompile(`//([^:]+):([^@]+)@`)

func maskURI(uri string) string {
	return maskCreds.ReplaceAllString(uri, "//$1:****@")
}

// dbNameFromURI extracts the default database from a MongoDB connection
// string, i.e. the path component of "mongodb://host:27017/<db>?options".
// Returns "" when the URI carries no database, letting the caller fall back
// to MONGO_DB_NAME or the built-in default.
func dbNameFromURI(uri string) string {
	for _, scheme := range []string{"mongodb+srv://", "mongodb://"} {
		if !strings.HasPrefix(uri, scheme) {
			continue
		}
		rest := uri[len(scheme):]
		slash := strings.Index(rest, "/")
		if slash == -1 {
			return ""
		}
		name := rest[slash+1:]
		if q := strings.IndexAny(name, "?"); q != -1 {
			name = name[:q]
		}
		if unescaped, err := url.PathUnescape(name); err == nil {
			name = unescaped
		}
		return name
	}
	return ""
}

// Connect dials MongoDB using the configured URI and materializes the full
// schema (collections + indexes) for a brand-new database.
func Connect(cfg *config.Config) (*Store, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	clientOpts := options.Client().ApplyURI(cfg.Mongo.URI)
	client, err := mongo.Connect(ctx, clientOpts)
	if err != nil {
		return nil, err
	}

	if err := client.Ping(ctx, nil); err != nil {
		return nil, err
	}

	dbName := cfg.Mongo.DBName
	if dbName == "" {
		dbName = dbNameFromURI(cfg.Mongo.URI)
		if dbName == "" {
			dbName = "pm_dashboard"
		}
	}

	store := &Store{Client: client, DB: client.Database(dbName)}
	logger.Info("MongoDB connected -> %s (db=%s)", maskURI(cfg.Mongo.URI), dbName)

	if err := store.ensureSchema(ctx); err != nil {
		return nil, err
	}

	return store, nil
}

// ensureSchema creates every collection (if missing) and its indexes. This
// implements the "create schema on first run" requirement: on an empty
// database, every collection + index defined below is materialized here.
func (s *Store) ensureSchema(ctx context.Context) error {
	collections := []string{ColUsers, ColTeams, ColProjects, ColTasks, ColChatChannels, ColChatMessages, ColNotifications}
	for _, name := range collections {
		if err := s.DB.CreateCollection(ctx, name); err != nil {
			// Already exists is fine; anything else is logged but non-fatal
			// so a partially-provisioned DB doesn't block boot entirely.
			logger.Warn("createCollection(%s): %v", name, err)
		}
	}

	indexModels := map[string][]mongo.IndexModel{
		ColUsers: {
			{Keys: bson.D{{Key: "email", Value: 1}}, Options: options.Index().SetUnique(true)},
			{Keys: bson.D{{Key: "username", Value: 1}}, Options: options.Index().SetUnique(true)},
		},
		ColTeams: {
			{Keys: bson.D{{Key: "name", Value: 1}}, Options: options.Index().SetUnique(true)},
		},
		ColProjects: {
			{Keys: bson.D{{Key: "key", Value: 1}}, Options: options.Index().SetUnique(true)},
			{Keys: bson.D{{Key: "team", Value: 1}}},
		},
		ColTasks: {
			{Keys: bson.D{{Key: "project", Value: 1}, {Key: "status", Value: 1}, {Key: "order", Value: 1}}},
			{Keys: bson.D{{Key: "assignees", Value: 1}}},
			{Keys: bson.D{{Key: "dueDate", Value: 1}}},
			{Keys: bson.D{{Key: "parentTask", Value: 1}}},
		},
		ColChatChannels: {
			{Keys: bson.D{{Key: "members", Value: 1}}},
			{Keys: bson.D{{Key: "project", Value: 1}}},
			{Keys: bson.D{{Key: "team", Value: 1}}},
		},
		ColChatMessages: {
			{Keys: bson.D{{Key: "channel", Value: 1}, {Key: "createdAt", Value: -1}}},
		},
		ColNotifications: {
			{Keys: bson.D{{Key: "user", Value: 1}, {Key: "read", Value: 1}, {Key: "createdAt", Value: -1}}},
		},
	}

	for col, models := range indexModels {
		_, err := s.DB.Collection(col).Indexes().CreateMany(ctx, models)
		if err != nil {
			logger.Warn("ensureIndexes(%s): %v", col, err)
			continue
		}
		logger.Info("Schema ready for collection %q", col)
	}

	return nil
}

func (s *Store) Users() *mongo.Collection         { return s.DB.Collection(ColUsers) }
func (s *Store) Teams() *mongo.Collection         { return s.DB.Collection(ColTeams) }
func (s *Store) Projects() *mongo.Collection      { return s.DB.Collection(ColProjects) }
func (s *Store) Tasks() *mongo.Collection         { return s.DB.Collection(ColTasks) }
func (s *Store) ChatChannels() *mongo.Collection  { return s.DB.Collection(ColChatChannels) }
func (s *Store) ChatMessages() *mongo.Collection  { return s.DB.Collection(ColChatMessages) }
func (s *Store) Notifications() *mongo.Collection { return s.DB.Collection(ColNotifications) }
