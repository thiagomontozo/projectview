// Command migrate copies an existing MongoDB dataset into PostgreSQL.
//
// It exists for installations that ran the document-store version: point it at
// both databases and it replays users, teams, projects, tasks (with their
// sub-tasks, assignees, tags, checklists and comments), chat and notifications
// into the relational schema.
//
//	migrate -mongo mongodb://localhost:27017/pm_dashboard \
//	        -postgres postgres://projectview:projectview@localhost:5432/projectview?sslmode=disable
//
// ObjectIDs are mapped to UUIDs deterministically (the 12-byte ObjectID is
// zero-extended to 16 bytes), so a document referenced from several places
// resolves to the same row no matter which order things are copied in, and
// re-running the tool is idempotent.
//
// Safe to re-run: every insert is ON CONFLICT DO NOTHING.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func main() {
	mongoURI := flag.String("mongo", os.Getenv("MONGO_URI"), "source MongoDB connection string")
	postgresURL := flag.String("postgres", os.Getenv("DATABASE_URL"), "destination PostgreSQL connection string")
	flag.Parse()

	if *mongoURI == "" || *postgresURL == "" {
		fmt.Fprintln(os.Stderr, "both -mongo and -postgres are required")
		flag.Usage()
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(*mongoURI))
	if err != nil {
		fatal("connecting to MongoDB: %v", err)
	}
	defer client.Disconnect(ctx)
	if err := client.Ping(ctx, nil); err != nil {
		fatal("MongoDB is not reachable: %v", err)
	}
	source := client.Database(databaseFromURI(*mongoURI))

	pool, err := pgxpool.New(ctx, *postgresURL)
	if err != nil {
		fatal("connecting to PostgreSQL: %v", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		fatal("PostgreSQL is not reachable: %v", err)
	}

	m := &migrator{source: source, pool: pool, ctx: ctx}

	// Order matters: every step only references rows the previous ones created.
	steps := []struct {
		name string
		fn   func() (int, error)
	}{
		{"users", m.users},
		{"teams", m.teams},
		{"projects", m.projects},
		{"tasks", m.tasks},
		{"chat channels", m.chatChannels},
		{"chat messages", m.chatMessages},
		{"notifications", m.notifications},
	}
	for _, step := range steps {
		n, err := step.fn()
		if err != nil {
			fatal("migrating %s: %v", step.name, err)
		}
		fmt.Printf("  %-16s %d row(s)\n", step.name, n)
	}
	fmt.Println("migration complete")
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

// databaseFromURI extracts the database name from a Mongo connection string.
func databaseFromURI(uri string) string {
	for _, scheme := range []string{"mongodb+srv://", "mongodb://"} {
		if len(uri) > len(scheme) && uri[:len(scheme)] == scheme {
			rest := uri[len(scheme):]
			for i := 0; i < len(rest); i++ {
				if rest[i] == '/' {
					name := rest[i+1:]
					for j := 0; j < len(name); j++ {
						if name[j] == '?' {
							return name[:j]
						}
					}
					return name
				}
			}
		}
	}
	return "pm_dashboard"
}

type migrator struct {
	source *mongo.Database
	pool   *pgxpool.Pool
	ctx    context.Context
}

// toUUID maps a 12-byte ObjectID onto a UUID by zero-extending it. The mapping
// is deterministic and collision-free, so cross-references survive the copy.
func toUUID(id primitive.ObjectID) uuid.UUID {
	var u uuid.UUID
	copy(u[:12], id[:])
	return u
}

func optUUID(id *primitive.ObjectID) *uuid.UUID {
	if id == nil || id.IsZero() {
		return nil
	}
	u := toUUID(*id)
	return &u
}

func (m *migrator) each(collection string, fn func(bson.M) error) (int, error) {
	cursor, err := m.source.Collection(collection).Find(m.ctx, bson.M{})
	if err != nil {
		return 0, err
	}
	defer cursor.Close(m.ctx)

	n := 0
	for cursor.Next(m.ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			return n, err
		}
		if err := fn(doc); err != nil {
			return n, err
		}
		n++
	}
	return n, cursor.Err()
}

// --- field readers ---------------------------------------------------------

func str(doc bson.M, key string) string {
	if v, ok := doc[key].(string); ok {
		return v
	}
	return ""
}

func boolean(doc bson.M, key string, fallback bool) bool {
	if v, ok := doc[key].(bool); ok {
		return v
	}
	return fallback
}

func number(doc bson.M, key string) float64 {
	switch v := doc[key].(type) {
	case float64:
		return v
	case int32:
		return float64(v)
	case int64:
		return float64(v)
	}
	return 0
}

func objectID(doc bson.M, key string) *primitive.ObjectID {
	if v, ok := doc[key].(primitive.ObjectID); ok {
		return &v
	}
	return nil
}

func timestamp(doc bson.M, key string) *time.Time {
	if v, ok := doc[key].(primitive.DateTime); ok {
		t := v.Time()
		return &t
	}
	return nil
}

func objectIDList(doc bson.M, key string) []primitive.ObjectID {
	out := []primitive.ObjectID{}
	if arr, ok := doc[key].(primitive.A); ok {
		for _, item := range arr {
			if id, ok := item.(primitive.ObjectID); ok {
				out = append(out, id)
			}
		}
	}
	return out
}

func stringList(doc bson.M, key string) []string {
	out := []string{}
	if arr, ok := doc[key].(primitive.A); ok {
		for _, item := range arr {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

func docList(doc bson.M, key string) []bson.M {
	out := []bson.M{}
	if arr, ok := doc[key].(primitive.A); ok {
		for _, item := range arr {
			if d, ok := item.(bson.M); ok {
				out = append(out, d)
			}
		}
	}
	return out
}

func createdAt(doc bson.M) time.Time {
	if t := timestamp(doc, "createdAt"); t != nil {
		return *t
	}
	return time.Now()
}

// --- steps -----------------------------------------------------------------

func (m *migrator) users() (int, error) {
	return m.each("users", func(doc bson.M) error {
		id := toUUID(doc["_id"].(primitive.ObjectID))
		role := str(doc, "role")
		if role != "admin" && role != "manager" && role != "member" {
			role = "member"
		}
		authSource := str(doc, "authSource")
		if authSource != "ad" {
			authSource = "local"
		}
		_, err := m.pool.Exec(m.ctx, `
			INSERT INTO users (id, username, name, email, password_hash, auth_source, role,
			                   title, avatar_color, active, notify_by_email, last_login_at,
			                   created_at, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
			ON CONFLICT (id) DO NOTHING`,
			id, str(doc, "username"), str(doc, "name"), str(doc, "email"),
			str(doc, "passwordHash"), authSource, role, str(doc, "title"),
			defaultString(str(doc, "avatarColor"), "#2a78d6"),
			boolean(doc, "active", true), boolean(doc, "notifyByEmail", true),
			timestamp(doc, "lastLoginAt"), createdAt(doc), createdAt(doc))
		return err
	})
}

func (m *migrator) teams() (int, error) {
	return m.each("teams", func(doc bson.M) error {
		id := toUUID(doc["_id"].(primitive.ObjectID))
		_, err := m.pool.Exec(m.ctx, `
			INSERT INTO teams (id, name, description, color, lead_id, created_by, created_at, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
			ON CONFLICT (id) DO NOTHING`,
			id, str(doc, "name"), str(doc, "description"),
			defaultString(str(doc, "color"), "#0ea5e9"),
			optUUID(objectID(doc, "leadId")), optUUID(objectID(doc, "createdBy")),
			createdAt(doc), createdAt(doc))
		if err != nil {
			return err
		}
		return m.link("team_members", "team_id", id, objectIDList(doc, "members"))
	})
}

func (m *migrator) projects() (int, error) {
	return m.each("projects", func(doc bson.M) error {
		id := toUUID(doc["_id"].(primitive.ObjectID))
		status := str(doc, "status")
		switch status {
		case "planning", "active", "on_hold", "completed", "archived":
		default:
			status = "planning"
		}
		_, err := m.pool.Exec(m.ctx, `
			INSERT INTO projects (id, name, key, description, color, status, team_id, owner_id,
			                      start_date, end_date, created_by, created_at, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
			ON CONFLICT (id) DO NOTHING`,
			id, str(doc, "name"), str(doc, "key"), str(doc, "description"),
			defaultString(str(doc, "color"), "#8b5cf6"), status,
			optUUID(objectID(doc, "team")), optUUID(objectID(doc, "owner")),
			timestamp(doc, "startDate"), timestamp(doc, "endDate"),
			optUUID(objectID(doc, "createdBy")), createdAt(doc), createdAt(doc))
		if err != nil {
			return err
		}
		if err := m.link("project_members", "project_id", id, objectIDList(doc, "members")); err != nil {
			return err
		}
		for i, s := range docList(doc, "statuses") {
			order := int(number(s, "order"))
			if order == 0 {
				order = i
			}
			if _, err := m.pool.Exec(m.ctx, `
				INSERT INTO project_statuses (project_id, key, label, position, color)
				VALUES ($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`,
				id, str(s, "key"), str(s, "label"), order,
				defaultString(str(s, "color"), "#94a3b8")); err != nil {
				return err
			}
		}
		return nil
	})
}

func (m *migrator) tasks() (int, error) {
	// Two passes: every task row must exist before parent links are set, since
	// parent_task_id is a self-referencing foreign key.
	n, err := m.each("tasks", func(doc bson.M) error {
		id := toUUID(doc["_id"].(primitive.ObjectID))
		priority := str(doc, "priority")
		switch priority {
		case "low", "medium", "high", "urgent":
		default:
			priority = "medium"
		}
		project := optUUID(objectID(doc, "project"))
		if project == nil {
			return fmt.Errorf("task %s has no project", id)
		}
		_, err := m.pool.Exec(m.ctx, `
			INSERT INTO tasks (id, title, description, project_id, status, priority,
			                   start_date, due_date, completed_at, estimate_hours, position,
			                   created_by, created_at, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
			ON CONFLICT (id) DO NOTHING`,
			id, str(doc, "title"), str(doc, "description"), *project,
			defaultString(str(doc, "status"), "backlog"), priority,
			timestamp(doc, "startDate"), timestamp(doc, "dueDate"), timestamp(doc, "completedAt"),
			number(doc, "estimateHours"), number(doc, "order"),
			optUUID(objectID(doc, "createdBy")), createdAt(doc), createdAt(doc))
		if err != nil {
			return err
		}

		if err := m.link("task_assignees", "task_id", id, objectIDList(doc, "assignees")); err != nil {
			return err
		}
		for _, tag := range stringList(doc, "tags") {
			if _, err := m.pool.Exec(m.ctx,
				`INSERT INTO task_tags (task_id, tag) VALUES ($1,$2) ON CONFLICT DO NOTHING`,
				id, tag); err != nil {
				return err
			}
		}
		for i, item := range docList(doc, "checklist") {
			itemID := uuid.New()
			if oid := objectID(item, "_id"); oid != nil {
				itemID = toUUID(*oid)
			}
			if _, err := m.pool.Exec(m.ctx, `
				INSERT INTO task_checklist_items (id, task_id, text, done, position)
				VALUES ($1,$2,$3,$4,$5) ON CONFLICT (id) DO NOTHING`,
				itemID, id, str(item, "text"), boolean(item, "done", false), i); err != nil {
				return err
			}
		}
		for _, comment := range docList(doc, "comments") {
			commentID := uuid.New()
			if oid := objectID(comment, "_id"); oid != nil {
				commentID = toUUID(*oid)
			}
			if _, err := m.pool.Exec(m.ctx, `
				INSERT INTO task_comments (id, task_id, author_id, body, created_at)
				VALUES ($1,$2,$3,$4,$5) ON CONFLICT (id) DO NOTHING`,
				commentID, id, optUUID(objectID(comment, "author")),
				str(comment, "body"), createdAt(comment)); err != nil {
				return err
			}
		}
		for _, alert := range docList(doc, "alertsSent") {
			alertType := str(alert, "type")
			if alertType != "due_soon" && alertType != "overdue" {
				continue
			}
			user := optUUID(objectID(alert, "user"))
			if user == nil {
				continue
			}
			sentAt := time.Now()
			if t := timestamp(alert, "sentAt"); t != nil {
				sentAt = *t
			}
			if _, err := m.pool.Exec(m.ctx, `
				INSERT INTO task_alerts_sent (task_id, user_id, alert_type, sent_at)
				VALUES ($1,$2,$3,$4) ON CONFLICT DO NOTHING`,
				id, *user, alertType, sentAt); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return n, err
	}

	// Second pass: attach sub-tasks now that every row exists.
	_, err = m.each("tasks", func(doc bson.M) error {
		parent := optUUID(objectID(doc, "parentTask"))
		if parent == nil {
			return nil
		}
		id := toUUID(doc["_id"].(primitive.ObjectID))
		_, err := m.pool.Exec(m.ctx,
			`UPDATE tasks SET parent_task_id = $2 WHERE id = $1`, id, *parent)
		return err
	})
	return n, err
}

func (m *migrator) chatChannels() (int, error) {
	return m.each("chatchannels", func(doc bson.M) error {
		id := toUUID(doc["_id"].(primitive.ObjectID))
		channelType := str(doc, "type")
		switch channelType {
		case "team", "project", "dm":
		default:
			channelType = "team"
		}
		_, err := m.pool.Exec(m.ctx, `
			INSERT INTO chat_channels (id, name, type, team_id, project_id, created_by, created_at, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
			ON CONFLICT (id) DO NOTHING`,
			id, str(doc, "name"), channelType,
			optUUID(objectID(doc, "team")), optUUID(objectID(doc, "project")),
			optUUID(objectID(doc, "createdBy")), createdAt(doc), createdAt(doc))
		if err != nil {
			return err
		}
		return m.link("chat_channel_members", "channel_id", id, objectIDList(doc, "members"))
	})
}

func (m *migrator) chatMessages() (int, error) {
	return m.each("chatmessages", func(doc bson.M) error {
		id := toUUID(doc["_id"].(primitive.ObjectID))
		channel := optUUID(objectID(doc, "channel"))
		if channel == nil {
			return nil // orphan message, nothing to attach it to
		}
		_, err := m.pool.Exec(m.ctx, `
			INSERT INTO chat_messages (id, channel_id, author_id, body, created_at, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6)
			ON CONFLICT (id) DO NOTHING`,
			id, *channel, optUUID(objectID(doc, "author")), str(doc, "body"),
			createdAt(doc), createdAt(doc))
		if err != nil {
			return err
		}
		for _, reader := range objectIDList(doc, "readBy") {
			if _, err := m.pool.Exec(m.ctx, `
				INSERT INTO chat_message_reads (message_id, user_id) VALUES ($1,$2)
				ON CONFLICT DO NOTHING`, id, toUUID(reader)); err != nil {
				return err
			}
		}
		return nil
	})
}

func (m *migrator) notifications() (int, error) {
	return m.each("notifications", func(doc bson.M) error {
		id := toUUID(doc["_id"].(primitive.ObjectID))
		user := optUUID(objectID(doc, "user"))
		if user == nil {
			return nil
		}
		_, err := m.pool.Exec(m.ctx, `
			INSERT INTO notifications (id, user_id, type, title, body, task_id, project_id, read, created_at, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
			ON CONFLICT (id) DO NOTHING`,
			id, *user, defaultString(str(doc, "type"), "general"), str(doc, "title"),
			str(doc, "body"), optUUID(objectID(doc, "task")), optUUID(objectID(doc, "project")),
			boolean(doc, "read", false), createdAt(doc), createdAt(doc))
		return err
	})
}

// link writes rows into a two-column join table.
func (m *migrator) link(table, parentCol string, parentID uuid.UUID, members []primitive.ObjectID) error {
	for _, member := range members {
		if _, err := m.pool.Exec(m.ctx,
			fmt.Sprintf(`INSERT INTO %s (%s, user_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, table, parentCol),
			parentID, toUUID(member)); err != nil {
			return err
		}
	}
	return nil
}

func defaultString(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
