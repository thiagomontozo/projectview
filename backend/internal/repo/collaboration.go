package repo

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"projectview/internal/db"
	"projectview/internal/models"
)

// ---------------------------------------------------------------------------
// Threaded chat: replies, reactions, mentions
// ---------------------------------------------------------------------------

// ErrNestedThread is returned when a reply is aimed at another reply. Threads
// of threads turn a conversation into a tree nobody can follow.
var ErrNestedThread = errors.New("replies cannot be nested more than one level")

func isNestedThreadError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "replies cannot be nested")
}

// Reaction is one emoji on one message, with who used it.
type Reaction struct {
	Emoji string      `json:"emoji"`
	Users []uuid.UUID `json:"users"`
}

// ReplyTo posts a message inside a thread. The parent must be a root message.
func (r *Chat) ReplyTo(ctx context.Context, channelID, parentID, authorID uuid.UUID, body string) (*models.ChatMessage, error) {
	m := models.ChatMessage{
		ID: uuid.New(), ChannelID: channelID, Author: &authorID, Body: body,
		ParentID: &parentID, ReadBy: []uuid.UUID{authorID},
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}

	err := r.store.WithTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO chat_messages (id, channel_id, author_id, body, parent_id, created_at, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7)`,
			m.ID, channelID, authorID, body, parentID, m.CreatedAt, m.UpdatedAt); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO chat_message_reads (message_id, user_id) VALUES ($1,$2)`, m.ID, authorID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE chat_channels SET updated_at = now() WHERE id = $1`, channelID)
		return err
	})
	if isNestedThreadError(err) {
		return nil, ErrNestedThread
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// MessageByID loads one message, without checking who may read it — callers
// resolve the channel and its membership separately.
func (r *Chat) MessageByID(ctx context.Context, id uuid.UUID) (*models.ChatMessage, error) {
	var m models.ChatMessage
	err := r.store.Pool.QueryRow(ctx, `
		SELECT id, channel_id, author_id, body, parent_id, edited_at, created_at, updated_at
		  FROM chat_messages WHERE id = $1`, id).
		Scan(&m.ID, &m.ChannelID, &m.Author, &m.Body, &m.ParentID,
			&m.EditedAt, &m.CreatedAt, &m.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	m.ReadBy = []uuid.UUID{}
	return &m, nil
}

// Thread returns the replies to a message, oldest first.
func (r *Chat) Thread(ctx context.Context, parentID uuid.UUID) ([]models.ChatMessage, error) {
	rows, err := r.store.Pool.Query(ctx, `
		SELECT id, channel_id, author_id, body, parent_id, edited_at, created_at, updated_at
		  FROM chat_messages
		 WHERE parent_id = $1
		 ORDER BY created_at`, parentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.ChatMessage{}
	for rows.Next() {
		var m models.ChatMessage
		if err := rows.Scan(&m.ID, &m.ChannelID, &m.Author, &m.Body, &m.ParentID,
			&m.EditedAt, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, err
		}
		m.ReadBy = []uuid.UUID{}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ReplyCounts returns how many replies each message has, so the channel view
// can show "3 replies" without loading any of them.
func (r *Chat) ReplyCounts(ctx context.Context, messageIDs []uuid.UUID) (map[uuid.UUID]int, error) {
	out := map[uuid.UUID]int{}
	if len(messageIDs) == 0 {
		return out, nil
	}
	rows, err := r.store.Pool.Query(ctx, `
		SELECT parent_id, count(*) FROM chat_messages
		 WHERE parent_id = ANY($1) GROUP BY parent_id`, messageIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

// ToggleReaction adds the emoji if absent and removes it if present, so
// clicking the same reaction twice undoes it. Returns whether it is now on.
func (r *Chat) ToggleReaction(ctx context.Context, messageID, userID uuid.UUID, emoji string) (bool, error) {
	tag, err := r.store.Pool.Exec(ctx, `
		DELETE FROM chat_reactions
		 WHERE message_id = $1 AND user_id = $2 AND emoji = $3`, messageID, userID, emoji)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() > 0 {
		return false, nil
	}

	_, err = r.store.Pool.Exec(ctx, `
		INSERT INTO chat_reactions (message_id, user_id, emoji) VALUES ($1,$2,$3)
		ON CONFLICT DO NOTHING`, messageID, userID, emoji)
	return err == nil, err
}

// ReactionsFor groups reactions by message and emoji in one query.
func (r *Chat) ReactionsFor(ctx context.Context, messageIDs []uuid.UUID) (map[uuid.UUID][]Reaction, error) {
	out := map[uuid.UUID][]Reaction{}
	if len(messageIDs) == 0 {
		return out, nil
	}
	rows, err := r.store.Pool.Query(ctx, `
		SELECT message_id, emoji, array_agg(user_id ORDER BY created_at)
		  FROM chat_reactions
		 WHERE message_id = ANY($1)
		 GROUP BY message_id, emoji
		 ORDER BY emoji`, messageIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var messageID uuid.UUID
		var reaction Reaction
		if err := rows.Scan(&messageID, &reaction.Emoji, &reaction.Users); err != nil {
			return nil, err
		}
		out[messageID] = append(out[messageID], reaction)
	}
	return out, rows.Err()
}

// mentionPattern matches @username. Deliberately conservative: an e-mail
// address in a message must not be read as three mentions.
var mentionPattern = regexp.MustCompile(`(^|[^\w@])@([a-zA-Z0-9._-]{2,50})`)

// ExtractMentions finds the usernames named in a message body.
func ExtractMentions(body string) []string {
	matches := mentionPattern.FindAllStringSubmatch(body, -1)
	seen := map[string]bool{}
	out := []string{}
	for _, match := range matches {
		name := strings.ToLower(match[2])
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}

// RecordMentions resolves usernames to accounts and stores who was named.
// Storing rather than re-parsing on read means a later rename never silently
// changes who was mentioned historically.
func (r *Chat) RecordMentions(ctx context.Context, messageID uuid.UUID, usernames []string) ([]uuid.UUID, error) {
	if len(usernames) == 0 {
		return nil, nil
	}
	rows, err := r.store.Pool.Query(ctx,
		`SELECT id FROM users WHERE username = ANY($1) AND active`, usernames)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	mentioned := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		mentioned = append(mentioned, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for _, userID := range mentioned {
		if _, err := r.store.Pool.Exec(ctx,
			`INSERT INTO chat_mentions (message_id, user_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`,
			messageID, userID); err != nil {
			return nil, err
		}
	}
	return mentioned, nil
}

// RootMessages returns a channel's top-level messages, newest first, trimmed
// to limit and returned in reading order.
func (r *Chat) RootMessages(ctx context.Context, channelID uuid.UUID, limit int) ([]models.ChatMessage, error) {
	rows, err := r.store.Pool.Query(ctx, `
		SELECT id, channel_id, author_id, body, parent_id, edited_at, created_at, updated_at
		  FROM (
		        SELECT id, channel_id, author_id, body, parent_id, edited_at, created_at, updated_at
		          FROM chat_messages
		         WHERE channel_id = $1 AND parent_id IS NULL
		         ORDER BY created_at DESC
		         LIMIT $2
		  ) recent
		 ORDER BY created_at`, channelID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.ChatMessage{}
	for rows.Next() {
		var m models.ChatMessage
		if err := rows.Scan(&m.ID, &m.ChannelID, &m.Author, &m.Body, &m.ParentID,
			&m.EditedAt, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, err
		}
		m.ReadBy = []uuid.UUID{}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Documents
// ---------------------------------------------------------------------------

type Docs struct{ store *db.Store }

func NewDocs(store *db.Store) *Docs { return &Docs{store: store} }

type Doc struct {
	ID        uuid.UUID  `json:"id"`
	SpaceID   *uuid.UUID `json:"spaceId,omitempty"`
	ProjectID *uuid.UUID `json:"projectId,omitempty"`
	ParentID  *uuid.UUID `json:"parentId,omitempty"`
	Title     string     `json:"title"`
	Content   string     `json:"content"`
	Position  int        `json:"position"`
	Archived  bool       `json:"archived"`
	CreatedBy *uuid.UUID `json:"-"`
	UpdatedBy *uuid.UUID `json:"-"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
}

const docColumns = `
	d.id, d.space_id, d.project_id, d.parent_id, d.title, d.content,
	d.position, d.archived, d.created_by, d.updated_by, d.created_at, d.updated_at`

func scanDoc(row pgx.Row) (*Doc, error) {
	var d Doc
	err := row.Scan(&d.ID, &d.SpaceID, &d.ProjectID, &d.ParentID, &d.Title, &d.Content,
		&d.Position, &d.Archived, &d.CreatedBy, &d.UpdatedBy, &d.CreatedAt, &d.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// ForScope lists the docs of a space or a project. Bodies are omitted: a
// sidebar listing does not need every document's full text.
func (r *Docs) ForScope(ctx context.Context, spaceID, projectID *uuid.UUID) ([]Doc, error) {
	rows, err := r.store.Pool.Query(ctx, `
		SELECT d.id, d.space_id, d.project_id, d.parent_id, d.title, '' AS content,
		       d.position, d.archived, d.created_by, d.updated_by, d.created_at, d.updated_at
		  FROM docs d
		 WHERE NOT d.archived
		   AND (($1::uuid IS NOT NULL AND d.space_id = $1)
		     OR ($2::uuid IS NOT NULL AND d.project_id = $2))
		 ORDER BY d.position, d.title`, spaceID, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Doc{}
	for rows.Next() {
		d, err := scanDoc(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

func (r *Docs) ByID(ctx context.Context, id uuid.UUID) (*Doc, error) {
	return scanDoc(r.store.Pool.QueryRow(ctx, `SELECT `+docColumns+` FROM docs d WHERE d.id = $1`, id))
}

func (r *Docs) Create(ctx context.Context, d *Doc, authorID uuid.UUID) error {
	if d.ID == uuid.Nil {
		d.ID = uuid.New()
	}
	return r.store.WithTx(ctx, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			INSERT INTO docs (id, space_id, project_id, parent_id, title, content, position, created_by, updated_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$8)
			RETURNING created_at, updated_at`,
			d.ID, d.SpaceID, d.ProjectID, d.ParentID, d.Title, d.Content, d.Position, authorID).
			Scan(&d.CreatedAt, &d.UpdatedAt)
		if err != nil {
			return err
		}
		// The first revision is the document as created, so history starts at
		// the beginning rather than at the first edit.
		_, err = tx.Exec(ctx, `
			INSERT INTO doc_revisions (doc_id, title, content, author_id)
			VALUES ($1,$2,$3,$4)`, d.ID, d.Title, d.Content, authorID)
		return err
	})
}

// Update saves the document and keeps the previous version. A document people
// edit together without history is one careless paste away from being lost.
func (r *Docs) Update(ctx context.Context, id uuid.UUID, title, content *string, archived *bool, authorID uuid.UUID) error {
	return r.store.WithTx(ctx, func(tx pgx.Tx) error {
		var current Doc
		err := tx.QueryRow(ctx, `SELECT title, content FROM docs WHERE id = $1`, id).
			Scan(&current.Title, &current.Content)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}

		nextTitle, nextContent := current.Title, current.Content
		if title != nil {
			nextTitle = *title
		}
		if content != nil {
			nextContent = *content
		}

		// Only snapshot when the text actually moved: archiving or reordering
		// a document is not a revision of its content.
		if nextTitle != current.Title || nextContent != current.Content {
			if _, err := tx.Exec(ctx, `
				INSERT INTO doc_revisions (doc_id, title, content, author_id)
				VALUES ($1,$2,$3,$4)`, id, nextTitle, nextContent, authorID); err != nil {
				return err
			}
		}

		_, err = tx.Exec(ctx, `
			UPDATE docs
			   SET title = $2, content = $3,
			       archived = COALESCE($4, archived),
			       updated_by = $5, updated_at = now()
			 WHERE id = $1`, id, nextTitle, nextContent, archived, authorID)
		return err
	})
}

func (r *Docs) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.store.Pool.Exec(ctx, `DELETE FROM docs WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

type DocRevision struct {
	ID        int64      `json:"id"`
	Title     string     `json:"title"`
	AuthorID  *uuid.UUID `json:"authorId,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
	// Only filled in by RevisionByID: the listing omits bodies, which would
	// make the response enormous for a long-lived page.
	Content string `json:"content,omitempty"`
}

// Revisions lists a document's history without the bodies, which would make
// the response enormous for a long-lived page.
func (r *Docs) Revisions(ctx context.Context, docID uuid.UUID, limit int) ([]DocRevision, error) {
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	rows, err := r.store.Pool.Query(ctx, `
		SELECT id, title, author_id, created_at
		  FROM doc_revisions
		 WHERE doc_id = $1
		 ORDER BY id DESC LIMIT $2`, docID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []DocRevision{}
	for rows.Next() {
		var rev DocRevision
		if err := rows.Scan(&rev.ID, &rev.Title, &rev.AuthorID, &rev.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, rev)
	}
	return out, rows.Err()
}

// RevisionByID returns one revision with its text. The document id is part of
// the lookup rather than trusted from the caller, so a revision id guessed from
// another document resolves to nothing.
func (r *Docs) RevisionByID(ctx context.Context, docID uuid.UUID, revisionID int64) (*DocRevision, error) {
	var rev DocRevision
	err := r.store.Pool.QueryRow(ctx, `
		SELECT id, title, content, author_id, created_at
		  FROM doc_revisions
		 WHERE doc_id = $1 AND id = $2`, docID, revisionID).
		Scan(&rev.ID, &rev.Title, &rev.Content, &rev.AuthorID, &rev.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &rev, nil
}

// Search runs full-text search over titles and bodies.
func (r *Docs) Search(ctx context.Context, query string, limit int) ([]Doc, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	rows, err := r.store.Pool.Query(ctx, `
		SELECT d.id, d.space_id, d.project_id, d.parent_id, d.title, '' AS content,
		       d.position, d.archived, d.created_by, d.updated_by, d.created_at, d.updated_at
		  FROM docs d
		 WHERE NOT d.archived
		   AND d.search @@ websearch_to_tsquery('simple', $1)
		 ORDER BY ts_rank(d.search, websearch_to_tsquery('simple', $1)) DESC
		 LIMIT $2`, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Doc{}
	for rows.Next() {
		d, err := scanDoc(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Notification preferences
// ---------------------------------------------------------------------------

type Preferences struct{ store *db.Store }

func NewPreferences(store *db.Store) *Preferences { return &Preferences{store: store} }

// ChannelPreference is how one notification type should reach someone.
type ChannelPreference struct {
	InApp bool `json:"inApp"`
	Email bool `json:"email"`
}

type NotificationPreferences struct {
	Channels   map[string]ChannelPreference `json:"channels"`
	Digest     string                       `json:"digest"`
	DigestHour int                          `json:"digestHour"`
	QuietStart *int                         `json:"quietStart,omitempty"`
	QuietEnd   *int                         `json:"quietEnd,omitempty"`
}

// Defaults are applied for any type the row does not mention, so adding a
// notification type never requires backfilling every user.
var defaultChannels = map[string]ChannelPreference{
	models.NotifTaskAssigned:   {InApp: true, Email: true},
	models.NotifTaskDueSoon:    {InApp: true, Email: true},
	models.NotifTaskOverdue:    {InApp: true, Email: true},
	models.NotifCommentMention: {InApp: true, Email: false},
	models.NotifChatMessage:    {InApp: true, Email: false},
	models.NotifGeneral:        {InApp: true, Email: false},
}

func ValidDigest(d string) bool { return d == "off" || d == "daily" || d == "weekly" }

func (r *Preferences) Get(ctx context.Context, userID uuid.UUID) (*NotificationPreferences, error) {
	prefs := &NotificationPreferences{
		Channels:   map[string]ChannelPreference{},
		Digest:     "off",
		DigestHour: 8,
	}

	var stored map[string]ChannelPreference
	err := r.store.Pool.QueryRow(ctx, `
		SELECT channels, digest, digest_hour, quiet_start, quiet_end
		  FROM notification_preferences WHERE user_id = $1`, userID).
		Scan(&stored, &prefs.Digest, &prefs.DigestHour, &prefs.QuietStart, &prefs.QuietEnd)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}

	for notifType, preference := range defaultChannels {
		prefs.Channels[notifType] = preference
	}
	for notifType, preference := range stored {
		prefs.Channels[notifType] = preference
	}
	return prefs, nil
}

func (r *Preferences) Set(ctx context.Context, userID uuid.UUID, prefs NotificationPreferences) error {
	_, err := r.store.Pool.Exec(ctx, `
		INSERT INTO notification_preferences (user_id, channels, digest, digest_hour, quiet_start, quiet_end)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (user_id) DO UPDATE
		   SET channels = EXCLUDED.channels, digest = EXCLUDED.digest,
		       digest_hour = EXCLUDED.digest_hour, quiet_start = EXCLUDED.quiet_start,
		       quiet_end = EXCLUDED.quiet_end, updated_at = now()`,
		userID, prefs.Channels, prefs.Digest, prefs.DigestHour, prefs.QuietStart, prefs.QuietEnd)
	return err
}

// WantsEmail reports whether a notification type should reach this user by
// e-mail right now, honouring both the per-type choice and quiet hours.
func (r *Preferences) WantsEmail(ctx context.Context, userID uuid.UUID, notifType string, at time.Time) (bool, error) {
	prefs, err := r.Get(ctx, userID)
	if err != nil {
		return false, err
	}

	preference, ok := prefs.Channels[notifType]
	if !ok {
		preference = defaultChannels[models.NotifGeneral]
	}
	if !preference.Email {
		return false, nil
	}

	// Inside quiet hours the mail is held back; the digest picks it up.
	if prefs.QuietStart != nil && prefs.QuietEnd != nil {
		hour := at.Hour()
		start, end := *prefs.QuietStart, *prefs.QuietEnd
		quiet := false
		if start <= end {
			quiet = hour >= start && hour < end
		} else {
			// A window that wraps midnight, e.g. 22:00 to 07:00.
			quiet = hour >= start || hour < end
		}
		if quiet {
			return false, nil
		}
	}
	return true, nil
}

// DigestRecipient is a user due a digest, with the notifications to include.
type DigestRecipient struct {
	UserID  uuid.UUID
	Email   string
	Name    string
	Pending []models.Notification
}

// DueForDigest finds users whose digest window has elapsed and who have unread
// notifications worth sending.
func (r *Preferences) DueForDigest(ctx context.Context, now time.Time) ([]DigestRecipient, error) {
	rows, err := r.store.Pool.Query(ctx, `
		SELECT u.id, u.email, u.name
		  FROM notification_preferences p
		  JOIN users u ON u.id = p.user_id
		 WHERE p.digest <> 'off'
		   AND u.active
		   AND EXTRACT(HOUR FROM now()) = p.digest_hour
		   AND (p.last_digest_at IS NULL
		        OR p.last_digest_at < now() - CASE p.digest
		                                        WHEN 'daily'  THEN interval '20 hours'
		                                        ELSE               interval '6 days'
		                                      END)
		   AND EXISTS (SELECT 1 FROM notifications n
		                WHERE n.user_id = u.id AND NOT n.read)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	recipients := []DigestRecipient{}
	for rows.Next() {
		var recipient DigestRecipient
		if err := rows.Scan(&recipient.UserID, &recipient.Email, &recipient.Name); err != nil {
			return nil, err
		}
		recipients = append(recipients, recipient)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i := range recipients {
		pending, err := r.pendingFor(ctx, recipients[i].UserID)
		if err != nil {
			return nil, err
		}
		recipients[i].Pending = pending
	}
	return recipients, nil
}

func (r *Preferences) pendingFor(ctx context.Context, userID uuid.UUID) ([]models.Notification, error) {
	rows, err := r.store.Pool.Query(ctx, `
		SELECT id, user_id, type, title, body, task_id, project_id, read, created_at, updated_at
		  FROM notifications
		 WHERE user_id = $1 AND NOT read
		 ORDER BY created_at DESC
		 LIMIT 50`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.Notification{}
	for rows.Next() {
		var n models.Notification
		if err := rows.Scan(&n.ID, &n.User, &n.Type, &n.Title, &n.Body, &n.Task,
			&n.Project, &n.Read, &n.CreatedAt, &n.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// MarkDigestSent records delivery, so the next window is measured from here.
func (r *Preferences) MarkDigestSent(ctx context.Context, userID uuid.UUID) error {
	_, err := r.store.Pool.Exec(ctx,
		`UPDATE notification_preferences SET last_digest_at = now() WHERE user_id = $1`, userID)
	return err
}
