-- Product engine: dependencies, custom fields, time tracking, watchers and
-- automations.

-- --------------------------------------------------------------------------
-- Task dependencies
-- --------------------------------------------------------------------------
-- "task_id depends on depends_on_id": the first cannot proceed until the
-- second is far enough along. Only finish-to-start is modelled for now, which
-- is what a schedule actually uses in practice; the column exists so the other
-- three relations can be added without a migration of the rows.
CREATE TABLE task_dependencies (
    task_id       uuid        NOT NULL REFERENCES tasks (id) ON DELETE CASCADE,
    depends_on_id uuid        NOT NULL REFERENCES tasks (id) ON DELETE CASCADE,
    type          text        NOT NULL DEFAULT 'finish_to_start'
                  CHECK (type IN ('finish_to_start', 'start_to_start', 'finish_to_finish', 'start_to_finish')),
    -- Days of slack required between the two, negative for overlap.
    lag_days      integer     NOT NULL DEFAULT 0,
    created_by    uuid        REFERENCES users (id) ON DELETE SET NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (task_id, depends_on_id),
    CONSTRAINT task_dependencies_no_self CHECK (task_id <> depends_on_id)
);

CREATE INDEX task_dependencies_blocker_idx ON task_dependencies (depends_on_id);

-- A dependency cycle makes the schedule unsolvable: the critical-path walk
-- would not terminate, and no ordering of the work exists. Rejected at write
-- time, in the database, because the check spans rows that application code
-- cannot see in one place.
CREATE OR REPLACE FUNCTION reject_dependency_cycle() RETURNS trigger AS $$
BEGIN
    IF EXISTS (
        WITH RECURSIVE chain(id) AS (
            -- Start from the proposed blocker and follow what *it* depends on.
            SELECT NEW.depends_on_id
            UNION
            SELECT d.depends_on_id
              FROM task_dependencies d
              JOIN chain c ON d.task_id = c.id
        )
        -- Reaching the blocked task again means the edge closes a loop.
        SELECT 1 FROM chain WHERE id = NEW.task_id
    ) THEN
        RAISE EXCEPTION 'dependency would create a cycle'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER task_dependencies_acyclic
    BEFORE INSERT OR UPDATE ON task_dependencies
    FOR EACH ROW EXECUTE FUNCTION reject_dependency_cycle();

-- --------------------------------------------------------------------------
-- Custom fields
-- --------------------------------------------------------------------------
-- Definitions are relational, values are JSONB on the task. The alternative -
-- a row per value - turns every task read into a join and a pivot; JSONB with
-- a GIN index keeps reads flat while still being queryable.
CREATE TABLE custom_field_definitions (
    id          uuid PRIMARY KEY,
    space_id    uuid        REFERENCES spaces (id)   ON DELETE CASCADE,
    project_id  uuid        REFERENCES projects (id) ON DELETE CASCADE,
    key         text        NOT NULL,
    label       text        NOT NULL,
    type        text        NOT NULL
                CHECK (type IN ('text', 'number', 'date', 'select', 'multi_select', 'checkbox', 'url', 'email', 'user')),
    -- Allowed values for the select types.
    options     jsonb       NOT NULL DEFAULT '[]',
    required    boolean     NOT NULL DEFAULT false,
    position    integer     NOT NULL DEFAULT 0,
    created_by  uuid        REFERENCES users (id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),

    -- A definition belongs to exactly one scope: a project, a space, or the
    -- whole installation. Two owners would make "which fields apply here"
    -- ambiguous.
    CONSTRAINT custom_field_single_scope
        CHECK (num_nonnulls(space_id, project_id) <= 1)
);

-- Keys are unique within their scope, so a task never has two fields with the
-- same key competing for one JSONB slot.
CREATE UNIQUE INDEX custom_field_key_project_idx
    ON custom_field_definitions (project_id, key) WHERE project_id IS NOT NULL;
CREATE UNIQUE INDEX custom_field_key_space_idx
    ON custom_field_definitions (space_id, key) WHERE space_id IS NOT NULL;
CREATE UNIQUE INDEX custom_field_key_global_idx
    ON custom_field_definitions (key) WHERE space_id IS NULL AND project_id IS NULL;

ALTER TABLE tasks ADD COLUMN custom_fields jsonb NOT NULL DEFAULT '{}';
CREATE INDEX tasks_custom_fields_idx ON tasks USING GIN (custom_fields);

-- --------------------------------------------------------------------------
-- Time tracking
-- --------------------------------------------------------------------------
CREATE TABLE time_entries (
    id         uuid PRIMARY KEY,
    task_id    uuid        NOT NULL REFERENCES tasks (id) ON DELETE CASCADE,
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    started_at timestamptz NOT NULL,
    -- NULL means the timer is still running.
    ended_at   timestamptz,
    note       text        NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT time_entries_ordered CHECK (ended_at IS NULL OR ended_at >= started_at)
);

CREATE INDEX time_entries_task_idx ON time_entries (task_id, started_at DESC);
CREATE INDEX time_entries_user_idx ON time_entries (user_id, started_at DESC);

-- One running timer per person, enforced by the database rather than by
-- remembering to check: a second concurrent timer would silently double-count
-- the same hour.
CREATE UNIQUE INDEX time_entries_single_running_idx
    ON time_entries (user_id) WHERE ended_at IS NULL;

-- --------------------------------------------------------------------------
-- Watchers
-- --------------------------------------------------------------------------
-- Following a task without being responsible for it. Assignees are notified
-- because it is their work; watchers because they asked to be.
CREATE TABLE task_watchers (
    task_id    uuid        NOT NULL REFERENCES tasks (id) ON DELETE CASCADE,
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (task_id, user_id)
);

CREATE INDEX task_watchers_user_idx ON task_watchers (user_id);

-- --------------------------------------------------------------------------
-- Automations
-- --------------------------------------------------------------------------
CREATE TABLE automations (
    id          uuid PRIMARY KEY,
    project_id  uuid        REFERENCES projects (id) ON DELETE CASCADE,
    space_id    uuid        REFERENCES spaces (id)   ON DELETE CASCADE,
    name        text        NOT NULL,
    enabled     boolean     NOT NULL DEFAULT true,
    trigger     text        NOT NULL
                CHECK (trigger IN ('task.created', 'task.status_changed', 'task.assigned',
                                   'task.overdue', 'task.due_soon')),
    -- [{field, op, value}, ...] - all must hold.
    conditions  jsonb       NOT NULL DEFAULT '[]',
    -- [{type, ...params}, ...] - applied in order.
    actions     jsonb       NOT NULL DEFAULT '[]',
    created_by  uuid        REFERENCES users (id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT automation_single_scope CHECK (num_nonnulls(space_id, project_id) <= 1)
);

CREATE INDEX automations_trigger_idx ON automations (trigger) WHERE enabled;
CREATE INDEX automations_project_idx ON automations (project_id);

-- Every execution is recorded, including the ones that matched nothing. An
-- automation that quietly does not fire is otherwise impossible to debug.
CREATE TABLE automation_runs (
    id            bigserial PRIMARY KEY,
    automation_id uuid        REFERENCES automations (id) ON DELETE CASCADE,
    task_id       uuid        REFERENCES tasks (id) ON DELETE SET NULL,
    status        text        NOT NULL CHECK (status IN ('applied', 'skipped', 'failed')),
    detail        text        NOT NULL DEFAULT '',
    ran_at        timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX automation_runs_automation_idx ON automation_runs (automation_id, ran_at DESC);
