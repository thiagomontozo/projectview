-- Files in chat, edited messages, and intake forms.

-- ---------------------------------------------------------------------------
-- Attachments on chat messages
-- ---------------------------------------------------------------------------
-- The object storage and the whole upload path already exist; what was missing
-- was that an attachment could only belong to a task. A chat file hangs off a
-- message instead, and the difference that mattered is authorization: a task
-- attachment is reached through its project, a chat one through membership of
-- the channel. Two different resolutions, which is why this was not simply a
-- wider WHERE clause.
ALTER TABLE attachments
    ADD COLUMN message_id uuid REFERENCES chat_messages (id) ON DELETE CASCADE;

-- task_id stops being mandatory now that a second kind of owner exists.
ALTER TABLE attachments ALTER COLUMN task_id DROP NOT NULL;

-- Exactly one owner. Without this a row could belong to both, and the two
-- permission paths would disagree about who may read it - the worst possible
-- outcome for an access check.
ALTER TABLE attachments
    ADD CONSTRAINT attachments_one_owner CHECK (
        (task_id IS NOT NULL AND message_id IS NULL) OR
        (task_id IS NULL AND message_id IS NOT NULL)
    );

-- A comment belongs to a task, so it can never accompany a message.
ALTER TABLE attachments
    ADD CONSTRAINT attachments_comment_needs_task CHECK (
        comment_id IS NULL OR task_id IS NOT NULL
    );

CREATE INDEX attachments_message_idx
    ON attachments (message_id, created_at)
    WHERE message_id IS NOT NULL;

-- ---------------------------------------------------------------------------
-- Intake forms
-- ---------------------------------------------------------------------------
-- A form somebody fills in to raise work, without needing to understand the
-- board it lands on. The last unbuilt item from the collaboration phase.
--
-- Submissions become ordinary tasks. That is the whole design: an intake queue
-- that is a separate kind of record is a second inbox nobody watches, and the
-- point of intake is that a request turns into work in the place work already
-- lives.
CREATE TABLE intake_forms (
    id          uuid PRIMARY KEY,
    project_id  uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,

    title       text NOT NULL CHECK (title <> ''),
    description text NOT NULL DEFAULT '',

    -- The fields to show, as JSONB. Same reasoning as templates: a relational
    -- mirror would need migrating in step with every field type, and a form
    -- built last year would then describe inputs that no longer exist.
    fields      jsonb NOT NULL DEFAULT '[]'::jsonb,

    -- Where a submission lands, and how it is labelled.
    target_status   text NOT NULL DEFAULT '',
    target_priority text NOT NULL DEFAULT 'medium'
                    CHECK (target_priority IN ('low', 'medium', 'high', 'urgent')),

    -- A form nobody can reach is a draft. Closing one keeps the submissions it
    -- already produced, which is why this is a flag rather than a delete.
    enabled     boolean NOT NULL DEFAULT true,

    -- Anonymous submission is off by default, and the default is the careful
    -- one: a form reachable without signing in is reachable by anyone who
    -- learns the URL, which is occasionally the point and never the assumption.
    public      boolean NOT NULL DEFAULT false,
    -- The unguessable part of a public URL. Present even for private forms so
    -- turning one public later does not change its address.
    slug        text NOT NULL UNIQUE,

    created_by  uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX intake_forms_project_idx ON intake_forms (project_id);

-- What was submitted, kept beside the task it produced.
--
-- The answers are stored as well as being written into the task, because the
-- task is edited afterwards - retitled, re-prioritised, its description
-- rewritten - and the record of what somebody actually asked for should not
-- change with it.
CREATE TABLE intake_submissions (
    id           uuid PRIMARY KEY,
    form_id      uuid NOT NULL REFERENCES intake_forms (id) ON DELETE CASCADE,
    -- SET NULL rather than CASCADE: deleting the task that came out of a
    -- request must not erase the evidence that the request was made.
    task_id      uuid REFERENCES tasks (id) ON DELETE SET NULL,

    answers      jsonb NOT NULL DEFAULT '{}'::jsonb,
    -- Null for an anonymous submission through a public form.
    submitted_by uuid REFERENCES users (id) ON DELETE SET NULL,
    submitter_name  text NOT NULL DEFAULT '',
    submitter_email text NOT NULL DEFAULT '',

    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX intake_submissions_form_idx ON intake_submissions (form_id, created_at DESC);
