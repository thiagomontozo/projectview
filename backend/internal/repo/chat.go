package repo

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"projectview/internal/db"
	"projectview/internal/models"
)

type Chat struct{ store *db.Store }

func NewChat(store *db.Store) *Chat { return &Chat{store: store} }

const channelColumns = `c.id, c.name, c.type, c.team_id, c.project_id, c.created_by, c.created_at, c.updated_at`

func scanChannel(row pgx.Row) (*models.ChatChannel, error) {
	var c models.ChatChannel
	err := row.Scan(&c.ID, &c.Name, &c.Type, &c.TeamID, &c.ProjectID,
		&c.CreatedBy, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	c.Members = []uuid.UUID{}
	return &c, nil
}

// ChannelsForUser lists the channels a user belongs to, most recently active
// first, with all memberships resolved in a second query.
func (r *Chat) ChannelsForUser(ctx context.Context, userID uuid.UUID) ([]models.ChatChannel, error) {
	rows, err := r.store.Pool.Query(ctx, `
		SELECT `+channelColumns+`
		  FROM chat_channels c
		  JOIN chat_channel_members m ON m.channel_id = c.id
		 WHERE m.user_id = $1
		 ORDER BY c.updated_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	channels := []models.ChatChannel{}
	index := map[uuid.UUID]int{}
	ids := []uuid.UUID{}
	for rows.Next() {
		c, err := scanChannel(rows)
		if err != nil {
			return nil, err
		}
		index[c.ID] = len(channels)
		ids = append(ids, c.ID)
		channels = append(channels, *c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return channels, nil
	}

	memberRows, err := r.store.Pool.Query(ctx,
		`SELECT channel_id, user_id FROM chat_channel_members WHERE channel_id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	defer memberRows.Close()
	for memberRows.Next() {
		var channelID, memberID uuid.UUID
		if err := memberRows.Scan(&channelID, &memberID); err != nil {
			return nil, err
		}
		if i, ok := index[channelID]; ok {
			channels[i].Members = append(channels[i].Members, memberID)
		}
	}
	return channels, memberRows.Err()
}

// ChannelForMember loads a channel only if the user belongs to it. Both
// reading and posting go through this, so membership can never be checked in
// one path and forgotten in the other.
func (r *Chat) ChannelForMember(ctx context.Context, channelID, userID uuid.UUID) (*models.ChatChannel, error) {
	c, err := scanChannel(r.store.Pool.QueryRow(ctx, `
		SELECT `+channelColumns+`
		  FROM chat_channels c
		  JOIN chat_channel_members m ON m.channel_id = c.id
		 WHERE c.id = $1 AND m.user_id = $2`, channelID, userID))
	if err != nil {
		return nil, err
	}
	if c.Members, err = r.channelMemberIDs(ctx, channelID); err != nil {
		return nil, err
	}
	return c, nil
}

func (r *Chat) channelMemberIDs(ctx context.Context, channelID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := r.store.Pool.Query(ctx,
		`SELECT user_id FROM chat_channel_members WHERE channel_id = $1`, channelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (r *Chat) CreateChannel(ctx context.Context, c *models.ChatChannel) error {
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	return r.store.WithTx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO chat_channels (id, name, type, team_id, project_id, created_by)
			VALUES ($1,$2,$3,$4,$5,$6)`,
			c.ID, c.Name, c.Type, c.TeamID, c.ProjectID, c.CreatedBy)
		if err != nil {
			return err
		}
		return replaceMembers(ctx, tx, "chat_channel_members", "channel_id", c.ID, c.Members)
	})
}

// FindDM looks for an existing direct-message channel whose membership is
// exactly the given set, so opening a DM twice reuses the same conversation.
func (r *Chat) FindDM(ctx context.Context, members []uuid.UUID) (*models.ChatChannel, error) {
	c, err := scanChannel(r.store.Pool.QueryRow(ctx, `
		SELECT `+channelColumns+`
		  FROM chat_channels c
		 WHERE c.type = 'dm'
		   AND (SELECT count(*) FROM chat_channel_members m WHERE m.channel_id = c.id) = cardinality($1::uuid[])
		   AND NOT EXISTS (
		         SELECT 1 FROM chat_channel_members m
		          WHERE m.channel_id = c.id AND NOT (m.user_id = ANY($1))
		   )
		 LIMIT 1`, members))
	if err != nil {
		return nil, err
	}
	if c.Members, err = r.channelMemberIDs(ctx, c.ID); err != nil {
		return nil, err
	}
	return c, nil
}

// Messages returns the most recent messages of a channel in chronological
// order, capped at limit.
func (r *Chat) Messages(ctx context.Context, channelID uuid.UUID, limit int) ([]models.ChatMessage, error) {
	rows, err := r.store.Pool.Query(ctx, `
		SELECT id, channel_id, author_id, body, created_at, updated_at
		  FROM (
		        SELECT id, channel_id, author_id, body, created_at, updated_at
		          FROM chat_messages
		         WHERE channel_id = $1
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
		if err := rows.Scan(&m.ID, &m.ChannelID, &m.Author, &m.Body, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, err
		}
		m.ReadBy = []uuid.UUID{}
		out = append(out, m)
	}
	return out, rows.Err()
}

// PostMessage stores the message and bumps the channel's activity timestamp so
// the channel list stays ordered by recency.
func (r *Chat) PostMessage(ctx context.Context, channelID, authorID uuid.UUID, body string) (*models.ChatMessage, error) {
	m := models.ChatMessage{
		ID: uuid.New(), ChannelID: channelID, Author: &authorID, Body: body,
		ReadBy: []uuid.UUID{authorID}, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	err := r.store.WithTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO chat_messages (id, channel_id, author_id, body, created_at, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6)`,
			m.ID, channelID, authorID, body, m.CreatedAt, m.UpdatedAt); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO chat_message_reads (message_id, user_id) VALUES ($1,$2)`,
			m.ID, authorID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE chat_channels SET updated_at = now() WHERE id = $1`, channelID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return &m, nil
}
