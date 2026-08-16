-- Attachments: files on a task or on one of its comments.
--
-- The bytes live in an S3-compatible object store; this table holds the
-- metadata and is the only thing that decides who may reach them. Storing the
-- file itself in PostgreSQL was the alternative, and it is the wrong one: a
-- database whose backup is dominated by screenshots is a database nobody
-- restores promptly, and the row would then have to be streamed through the
-- API for every download.
CREATE TABLE attachments (
    id           uuid PRIMARY KEY,
    -- Always set, comment attachments included. Authorization resolves through
    -- the task to its project, so a file can never be reachable by a path that
    -- skips the permission check.
    task_id      uuid   NOT NULL REFERENCES tasks (id)          ON DELETE CASCADE,
    comment_id   uuid            REFERENCES task_comments (id)  ON DELETE CASCADE,

    -- The name as uploaded, kept verbatim so "relatório final.pdf" comes back
    -- as itself. It is never part of the storage key; see storage/objects.go.
    filename     text   NOT NULL CHECK (filename <> ''),
    content_type text   NOT NULL,
    size_bytes   bigint NOT NULL CHECK (size_bytes > 0),

    -- Opaque, generated, and unique because it is what a signed URL addresses:
    -- two rows pointing at one object would make either row's deletion break
    -- the other's download.
    storage_key  text   NOT NULL UNIQUE,
    -- SHA-256 of the bytes as received. Lets a re-upload be recognised and
    -- gives an operator something to check a restored object against.
    checksum     text   NOT NULL,

    -- The virus-scan seam. 'skipped' is deliberately distinct from 'clean':
    -- with no scanner wired in, nothing examined the file, and recording that
    -- as clean would be a claim the installation cannot support.
    --
    -- 'pending' is the column default but not a state the upload path stores:
    -- a scan that fails refuses the upload rather than keeping a file nothing
    -- will ever come back to. It exists for an asynchronous scanner, and the
    -- download path already refuses to sign a URL for a row in it.
    scan_status  text   NOT NULL DEFAULT 'pending'
                 CHECK (scan_status IN ('pending', 'clean', 'infected', 'skipped')),
    scanned_at   timestamptz,

    uploaded_by  uuid            REFERENCES users (id) ON DELETE SET NULL,
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX attachments_task_idx    ON attachments (task_id, created_at);
CREATE INDEX attachments_comment_idx ON attachments (comment_id) WHERE comment_id IS NOT NULL;

-- ---------------------------------------------------------------------------
-- Deleting the row is not deleting the file
-- ---------------------------------------------------------------------------
--
-- The object has to go too, and the hard case is not the delete button - it is
-- the cascade. Removing a task, a project or a whole space takes its
-- attachment rows with it in one statement the application never sees, and
-- application-level cleanup cannot cover a path it is not on.
--
-- So the database records the intent instead: a trigger queues every deleted
-- row's storage key, whatever removed it, and a sweeper drains the queue
-- against the object store. Objects outlive their rows by minutes rather than
-- forever, and a bucket that quietly accumulates every file ever uploaded -
-- including the ones somebody deleted on purpose - stops being a possibility.
CREATE TABLE attachment_deletions (
    storage_key text PRIMARY KEY,
    queued_at   timestamptz NOT NULL DEFAULT now(),
    attempts    integer     NOT NULL DEFAULT 0,
    last_error  text
);

-- Failing rows are retried, so the sweep takes the oldest untried keys first
-- rather than beating on one object the store keeps refusing.
CREATE INDEX attachment_deletions_pending_idx ON attachment_deletions (attempts, queued_at);

CREATE FUNCTION queue_attachment_object_delete() RETURNS trigger AS $$
BEGIN
    INSERT INTO attachment_deletions (storage_key)
    VALUES (OLD.storage_key)
    ON CONFLICT (storage_key) DO NOTHING;
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER attachments_queue_object_delete
    AFTER DELETE ON attachments
    FOR EACH ROW
    EXECUTE FUNCTION queue_attachment_object_delete();
