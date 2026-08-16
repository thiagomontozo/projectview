-- Recurring tasks and templates.
--
-- Both exist for the same reason: a weekly report somebody retypes every Monday,
-- and a project kickoff somebody rebuilds from memory, are the two things that
-- keep a spreadsheet open beside this tool.

-- ---------------------------------------------------------------------------
-- Recurrence
-- ---------------------------------------------------------------------------
-- The rule lives on the task that currently carries it, not on a series row
-- pointing at many. When an instance spawns the next one, the rule moves with
-- it, so "the recurring task" is always the open one and there is never a
-- second place to look for what happens next.
CREATE TABLE task_recurrences (
    task_id        uuid PRIMARY KEY REFERENCES tasks (id) ON DELETE CASCADE,

    frequency      text    NOT NULL CHECK (frequency IN ('daily', 'weekly', 'monthly')),
    -- "every 2 weeks". Bounded because an interval of zero would spawn forever
    -- and one of ten thousand is a typo, not an intention.
    interval_count integer NOT NULL DEFAULT 1 CHECK (interval_count BETWEEN 1 AND 52),

    -- What drives the next instance, and it is the whole design decision:
    --
    --   on_complete  the next one appears when this one is finished. A chain
    --                that never piles up, and that stops entirely if nobody
    --                does the work - which is honest: nothing was completed,
    --                so nothing came next.
    --   on_schedule  the next one appears when it is due, whatever happened to
    --                this one. Unfinished instances stay open and go overdue,
    --                so a month of ignored reports looks like a month of
    --                ignored reports rather than one tidy row.
    --
    -- Neither silently closes, deletes or reschedules an instance nobody
    -- finished. That is the answer to the question the backlog asked, and it is
    -- the same answer in both modes: the unfinished work stays, and stays
    -- visible.
    mode           text    NOT NULL DEFAULT 'on_complete'
                   CHECK (mode IN ('on_complete', 'on_schedule')),

    -- Optional ends. A recurrence with neither runs until somebody removes it.
    until_date      timestamptz,
    max_occurrences integer CHECK (max_occurrences IS NULL OR max_occurrences > 0),
    -- How many instances this series has already produced, carried forward as
    -- the rule moves so a bounded series does not restart its count each time.
    occurrences     integer NOT NULL DEFAULT 1,

    -- When the scheduler should next act. Only meaningful for on_schedule; a
    -- NULL means "driven by completion instead".
    next_run_at     timestamptz,

    created_by      uuid        REFERENCES users (id) ON DELETE SET NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

-- The sweep asks for due rules only, so the index covers exactly that.
CREATE INDEX task_recurrences_due_idx
    ON task_recurrences (next_run_at)
    WHERE next_run_at IS NOT NULL;

-- Which instance a task was spawned from. Kept for the trail rather than for
-- the logic: ON DELETE SET NULL because removing last week's report must not
-- cascade into this week's.
ALTER TABLE tasks
    ADD COLUMN recurrence_parent_id uuid REFERENCES tasks (id) ON DELETE SET NULL;

CREATE INDEX tasks_recurrence_parent_idx
    ON tasks (recurrence_parent_id)
    WHERE recurrence_parent_id IS NOT NULL;

-- ---------------------------------------------------------------------------
-- Templates
-- ---------------------------------------------------------------------------
-- The body is JSONB rather than a parallel set of tables mirroring tasks,
-- checklists, tags and custom fields.
--
-- That is deliberate. A relational template would have to be migrated in step
-- with every column a task grows, and a template captured last year would then
-- describe a task that no longer exists. A snapshot describes what to create at
-- the moment it was captured; applying it is a translation, and a field the
-- current schema does not recognise is ignored rather than failing the whole
-- application of it.
CREATE TABLE templates (
    id          uuid PRIMARY KEY,
    name        text NOT NULL CHECK (name <> ''),
    description text NOT NULL DEFAULT '',

    -- 'task' creates one task (with its checklist, tags and field values) into
    -- an existing project. 'project' creates a project, its status columns and
    -- every task the template carries.
    kind        text NOT NULL CHECK (kind IN ('task', 'project')),

    -- Scoped to a space, or global when NULL. A kickoff checklist belongs to
    -- the department that runs kickoffs, not to everybody.
    space_id    uuid REFERENCES spaces (id) ON DELETE CASCADE,

    payload     jsonb NOT NULL,

    created_by  uuid        REFERENCES users (id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX templates_kind_idx  ON templates (kind, name);
CREATE INDEX templates_space_idx ON templates (space_id) WHERE space_id IS NOT NULL;
