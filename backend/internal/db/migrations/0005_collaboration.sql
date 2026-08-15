-- Collaboration: threaded chat with reactions and mentions, documents, and
-- notification preferences.

-- --------------------------------------------------------------------------
-- Chat: threads, edits, reactions, mentions
-- --------------------------------------------------------------------------
-- A reply points at the message it answers. One level only: threads of threads
-- turn a conversation into a tree nobody can follow, and every tool that has
-- tried it has walked it back.
ALTER TABLE chat_messages ADD COLUMN parent_id uuid REFERENCES chat_messages (id) ON DELETE CASCADE;
ALTER TABLE chat_messages ADD COLUMN edited_at timestamptz;

CREATE INDEX chat_messages_thread_idx ON chat_messages (parent_id, created_at)
    WHERE parent_id IS NOT NULL;

-- Root messages only, for the main channel view. A partial index because
-- replies are read through their parent, never scanned alongside it.
CREATE INDEX chat_messages_root_idx ON chat_messages (channel_id, created_at DESC)
    WHERE parent_id IS NULL;

-- A reply may not itself have replies. Enforced here because the check spans
-- two rows of the same table, which application code cannot see at once.
CREATE OR REPLACE FUNCTION reject_nested_thread() RETURNS trigger AS $$
BEGIN
    IF NEW.parent_id IS NOT NULL AND EXISTS (
        SELECT 1 FROM chat_messages WHERE id = NEW.parent_id AND parent_id IS NOT NULL
    ) THEN
        RAISE EXCEPTION 'replies cannot be nested more than one level'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER chat_messages_flat_threads
    BEFORE INSERT OR UPDATE OF parent_id ON chat_messages
    FOR EACH ROW EXECUTE FUNCTION reject_nested_thread();

CREATE TABLE chat_reactions (
    message_id uuid        NOT NULL REFERENCES chat_messages (id) ON DELETE CASCADE,
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    emoji      text        NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    -- One of each emoji per person per message: clicking twice toggles rather
    -- than stacking.
    PRIMARY KEY (message_id, user_id, emoji)
);

CREATE INDEX chat_reactions_message_idx ON chat_reactions (message_id);

-- Who was named in a message. Stored rather than re-parsed on read, so a
-- rename never silently changes who was mentioned historically.
CREATE TABLE chat_mentions (
    message_id uuid NOT NULL REFERENCES chat_messages (id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    PRIMARY KEY (message_id, user_id)
);

CREATE INDEX chat_mentions_user_idx ON chat_mentions (user_id);

-- --------------------------------------------------------------------------
-- Documents
-- --------------------------------------------------------------------------
-- Markdown rather than a rich-text document model: the content stays
-- greppable, diffable and portable, and the editor is a detail of the client
-- rather than a format the database has to understand.
CREATE TABLE docs (
    id          uuid PRIMARY KEY,
    space_id    uuid        REFERENCES spaces (id)   ON DELETE CASCADE,
    project_id  uuid        REFERENCES projects (id) ON DELETE CASCADE,
    parent_id   uuid        REFERENCES docs (id)     ON DELETE CASCADE,
    title       text        NOT NULL,
    content     text        NOT NULL DEFAULT '',
    position    integer     NOT NULL DEFAULT 0,
    archived    boolean     NOT NULL DEFAULT false,
    created_by  uuid        REFERENCES users (id) ON DELETE SET NULL,
    updated_by  uuid        REFERENCES users (id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),

    search tsvector GENERATED ALWAYS AS (
        setweight(to_tsvector('simple', coalesce(title, '')), 'A') ||
        setweight(to_tsvector('simple', coalesce(content, '')), 'B')
    ) STORED,

    -- A doc belongs to a space or a project, never both.
    CONSTRAINT docs_single_scope CHECK (num_nonnulls(space_id, project_id) <= 1),
    CONSTRAINT docs_no_self_parent CHECK (parent_id IS NULL OR parent_id <> id)
);

CREATE INDEX docs_space_idx   ON docs (space_id, position)   WHERE NOT archived;
CREATE INDEX docs_project_idx ON docs (project_id, position) WHERE NOT archived;
CREATE INDEX docs_parent_idx  ON docs (parent_id);
CREATE INDEX docs_search_idx  ON docs USING GIN (search);

-- Every save keeps the previous version. A document people edit together
-- without history is a document one careless paste can destroy.
CREATE TABLE doc_revisions (
    id         bigserial PRIMARY KEY,
    doc_id     uuid        NOT NULL REFERENCES docs (id) ON DELETE CASCADE,
    title      text        NOT NULL,
    content    text        NOT NULL,
    author_id  uuid        REFERENCES users (id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX doc_revisions_doc_idx ON doc_revisions (doc_id, id DESC);

-- --------------------------------------------------------------------------
-- Notification preferences
-- --------------------------------------------------------------------------
CREATE TABLE notification_preferences (
    user_id     uuid PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    -- {"task_assigned": {"inApp": true, "email": false}, ...}. Absent types
    -- fall back to the defaults in code, so adding a notification type does
    -- not require backfilling every row.
    channels    jsonb       NOT NULL DEFAULT '{}',
    digest      text        NOT NULL DEFAULT 'off'
                CHECK (digest IN ('off', 'daily', 'weekly')),
    -- Local hour the digest is sent, 0-23.
    digest_hour integer     NOT NULL DEFAULT 8
                CHECK (digest_hour BETWEEN 0 AND 23),
    last_digest_at timestamptz,
    -- Outside these hours, e-mail is held back and folded into the digest.
    quiet_start integer CHECK (quiet_start IS NULL OR quiet_start BETWEEN 0 AND 23),
    quiet_end   integer CHECK (quiet_end   IS NULL OR quiet_end   BETWEEN 0 AND 23),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

-- Ties a notification back to the message that caused it, so a digest can
-- show the conversation rather than a bare count.
ALTER TABLE notifications ADD COLUMN message_id uuid REFERENCES chat_messages (id) ON DELETE CASCADE;
