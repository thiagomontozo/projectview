-- Initial relational schema.
--
-- Replaces the seven MongoDB collections. What the document model kept inside
-- arrays on the parent document (assignees, members, checklist, comments,
-- alerts, tags) becomes real tables with foreign keys, so the database itself
-- enforces what application code used to hope for.

CREATE EXTENSION IF NOT EXISTS citext;

-- --------------------------------------------------------------------------
-- Users
-- --------------------------------------------------------------------------
CREATE TABLE users (
    id              uuid PRIMARY KEY,
    username        citext      NOT NULL UNIQUE,
    name            text        NOT NULL,
    email           citext      NOT NULL UNIQUE,
    password_hash   text        NOT NULL DEFAULT '',
    auth_source     text        NOT NULL DEFAULT 'local'
                    CHECK (auth_source IN ('local', 'ad')),
    role            text        NOT NULL DEFAULT 'member'
                    CHECK (role IN ('admin', 'manager', 'member')),
    title           text        NOT NULL DEFAULT '',
    avatar_color    text        NOT NULL DEFAULT '#2a78d6',
    active          boolean     NOT NULL DEFAULT true,
    notify_by_email boolean     NOT NULL DEFAULT true,
    last_login_at   timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX users_active_name_idx ON users (active, name);

-- --------------------------------------------------------------------------
-- Teams
-- --------------------------------------------------------------------------
CREATE TABLE teams (
    id          uuid PRIMARY KEY,
    name        text        NOT NULL UNIQUE,
    description text        NOT NULL DEFAULT '',
    color       text        NOT NULL DEFAULT '#0ea5e9',
    lead_id     uuid        REFERENCES users (id) ON DELETE SET NULL,
    created_by  uuid        REFERENCES users (id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE team_members (
    team_id uuid NOT NULL REFERENCES teams (id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    PRIMARY KEY (team_id, user_id)
);

CREATE INDEX team_members_user_idx ON team_members (user_id);

-- --------------------------------------------------------------------------
-- Projects
-- --------------------------------------------------------------------------
CREATE TABLE projects (
    id          uuid PRIMARY KEY,
    name        text        NOT NULL,
    key         text        NOT NULL UNIQUE,
    description text        NOT NULL DEFAULT '',
    color       text        NOT NULL DEFAULT '#8b5cf6',
    status      text        NOT NULL DEFAULT 'planning'
                CHECK (status IN ('planning', 'active', 'on_hold', 'completed', 'archived')),
    team_id     uuid        REFERENCES teams (id) ON DELETE SET NULL,
    owner_id    uuid        REFERENCES users (id) ON DELETE SET NULL,
    start_date  timestamptz,
    end_date    timestamptz,
    created_by  uuid        REFERENCES users (id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX projects_team_idx ON projects (team_id);

-- Kanban columns. Ordered by position; the first one is where new tasks land.
CREATE TABLE project_statuses (
    project_id uuid    NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    key        text    NOT NULL,
    label      text    NOT NULL,
    position   integer NOT NULL DEFAULT 0,
    color      text    NOT NULL DEFAULT '#94a3b8',
    PRIMARY KEY (project_id, key)
);

CREATE TABLE project_members (
    project_id uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    PRIMARY KEY (project_id, user_id)
);

CREATE INDEX project_members_user_idx ON project_members (user_id);

-- --------------------------------------------------------------------------
-- Tasks
-- --------------------------------------------------------------------------
-- A sub-task is a task whose parent_task_id is set; the self-reference allows
-- arbitrary nesting, and ON DELETE CASCADE removes a whole subtree in one
-- statement instead of the two unordered deletes the Mongo version issued.
CREATE TABLE tasks (
    id             uuid PRIMARY KEY,
    title          text             NOT NULL,
    description    text             NOT NULL DEFAULT '',
    project_id     uuid             NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    parent_task_id uuid             REFERENCES tasks (id) ON DELETE CASCADE,
    status         text             NOT NULL,
    priority       text             NOT NULL DEFAULT 'medium'
                   CHECK (priority IN ('low', 'medium', 'high', 'urgent')),
    start_date     timestamptz,
    due_date       timestamptz,
    completed_at   timestamptz,
    estimate_hours double precision NOT NULL DEFAULT 0,
    position       double precision NOT NULL DEFAULT 0,
    created_by     uuid             REFERENCES users (id) ON DELETE SET NULL,
    created_at     timestamptz      NOT NULL DEFAULT now(),
    updated_at     timestamptz      NOT NULL DEFAULT now(),

    -- A task cannot be its own parent.
    CONSTRAINT tasks_parent_not_self CHECK (parent_task_id IS NULL OR parent_task_id <> id)
);

CREATE INDEX tasks_board_idx    ON tasks (project_id, status, position);
CREATE INDEX tasks_parent_idx   ON tasks (parent_task_id);
CREATE INDEX tasks_due_date_idx ON tasks (due_date) WHERE due_date IS NOT NULL;

CREATE TABLE task_assignees (
    task_id uuid NOT NULL REFERENCES tasks (id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    PRIMARY KEY (task_id, user_id)
);

-- Drives "my tasks" and the workload report.
CREATE INDEX task_assignees_user_idx ON task_assignees (user_id);

CREATE TABLE task_tags (
    task_id uuid NOT NULL REFERENCES tasks (id) ON DELETE CASCADE,
    tag     text NOT NULL,
    PRIMARY KEY (task_id, tag)
);

CREATE TABLE task_checklist_items (
    id       uuid PRIMARY KEY,
    task_id  uuid    NOT NULL REFERENCES tasks (id) ON DELETE CASCADE,
    text     text    NOT NULL,
    done     boolean NOT NULL DEFAULT false,
    position integer NOT NULL DEFAULT 0
);

CREATE INDEX task_checklist_task_idx ON task_checklist_items (task_id, position);

CREATE TABLE task_comments (
    id         uuid PRIMARY KEY,
    task_id    uuid        NOT NULL REFERENCES tasks (id) ON DELETE CASCADE,
    author_id  uuid        REFERENCES users (id) ON DELETE SET NULL,
    body       text        NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX task_comments_task_idx ON task_comments (task_id, created_at);

-- Deadline alerts already sent. The primary key *is* the de-duplication rule
-- the alert sweep used to enforce by scanning an array in application code.
CREATE TABLE task_alerts_sent (
    task_id    uuid        NOT NULL REFERENCES tasks (id) ON DELETE CASCADE,
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    alert_type text        NOT NULL CHECK (alert_type IN ('due_soon', 'overdue')),
    sent_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (task_id, user_id, alert_type)
);

-- --------------------------------------------------------------------------
-- Chat
-- --------------------------------------------------------------------------
CREATE TABLE chat_channels (
    id         uuid PRIMARY KEY,
    name       text        NOT NULL DEFAULT '',
    type       text        NOT NULL CHECK (type IN ('team', 'project', 'dm')),
    team_id    uuid        REFERENCES teams (id) ON DELETE CASCADE,
    project_id uuid        REFERENCES projects (id) ON DELETE CASCADE,
    created_by uuid        REFERENCES users (id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX chat_channels_project_idx ON chat_channels (project_id);
CREATE INDEX chat_channels_team_idx    ON chat_channels (team_id);

CREATE TABLE chat_channel_members (
    channel_id uuid NOT NULL REFERENCES chat_channels (id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    PRIMARY KEY (channel_id, user_id)
);

CREATE INDEX chat_channel_members_user_idx ON chat_channel_members (user_id);

CREATE TABLE chat_messages (
    id         uuid PRIMARY KEY,
    channel_id uuid        NOT NULL REFERENCES chat_channels (id) ON DELETE CASCADE,
    author_id  uuid        REFERENCES users (id) ON DELETE SET NULL,
    body       text        NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX chat_messages_channel_idx ON chat_messages (channel_id, created_at DESC);

CREATE TABLE chat_message_reads (
    message_id uuid NOT NULL REFERENCES chat_messages (id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    PRIMARY KEY (message_id, user_id)
);

-- --------------------------------------------------------------------------
-- Notifications
-- --------------------------------------------------------------------------
CREATE TABLE notifications (
    id         uuid PRIMARY KEY,
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    type       text        NOT NULL,
    title      text        NOT NULL,
    body       text        NOT NULL DEFAULT '',
    task_id    uuid        REFERENCES tasks (id) ON DELETE CASCADE,
    project_id uuid        REFERENCES projects (id) ON DELETE CASCADE,
    read       boolean     NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX notifications_inbox_idx ON notifications (user_id, read, created_at DESC);
