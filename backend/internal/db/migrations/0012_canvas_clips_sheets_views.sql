-- The four products the interface was missing, plus saved views.
--
-- One migration because they share a shape: each is a document that belongs to
-- a project, is edited by whoever can work in that project, and is worth
-- nothing without the screen that draws it. Splitting them into four files
-- would suggest they can be adopted separately, and the point of doing them
-- together is that a "Whiteboards" menu item leading to one empty screen is
-- worse than no menu item at all.

-- A view somebody arranged and wants back.
--
-- The arrangement already existed in the interface; it just lived in a React
-- state that a reload discarded. Which meant every person rebuilt the same
-- filter every morning, and no two people could agree on what "the board" is.
CREATE TABLE saved_views (
    id             uuid PRIMARY KEY,
    project_id     uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    name           text NOT NULL,
    kind           text NOT NULL,
    group_by       text NOT NULL DEFAULT 'status',
    -- The filter set, stored as the interface expresses it. A relational
    -- mirror would need migrating in step with every new filter, and a view
    -- saved last year would describe filters that no longer exist.
    filters        jsonb NOT NULL DEFAULT '{}'::jsonb,
    sort_by        text,
    sort_direction text,
    created_by     uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    -- Two views called "My week" on one project is a naming accident, not a
    -- feature: nobody could tell them apart in the tab strip.
    UNIQUE (project_id, name)
);
CREATE INDEX saved_views_project_idx ON saved_views (project_id, created_at);

-- A whiteboard.
--
-- The scene is one JSONB document rather than a row per shape. A board is read
-- and written whole - it is drawn in a single pass and dragged around
-- continuously - so a row per shape would mean a hundred writes for one drag
-- and a join for every render, buying granularity nothing here asks for.
CREATE TABLE whiteboards (
    id         uuid PRIMARY KEY,
    project_id uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    title      text NOT NULL,
    scene      jsonb NOT NULL DEFAULT '{"items":[]}'::jsonb,
    -- Optimistic concurrency, and the reason it is here rather than left out:
    -- two people on one board is the ordinary case, not the edge one. Last
    -- write wins would mean somebody's work vanishing with nothing on screen
    -- to say it happened.
    version    integer NOT NULL DEFAULT 1,
    created_by uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX whiteboards_project_idx ON whiteboards (project_id, updated_at DESC);

-- A spreadsheet. Same document-shaped storage, same reasoning, same guard.
CREATE TABLE spreadsheets (
    id         uuid PRIMARY KEY,
    project_id uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    title      text NOT NULL,
    -- Sparse by construction: {"A1": {"v": "Hours"}, "B2": {"f": "=SUM(B3:B9)"}}.
    -- A dense array would store fifty thousand nulls to hold nine numbers.
    cells      jsonb NOT NULL DEFAULT '{}'::jsonb,
    row_count  integer NOT NULL DEFAULT 50,
    col_count  integer NOT NULL DEFAULT 12,
    version    integer NOT NULL DEFAULT 1,
    created_by uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX spreadsheets_project_idx ON spreadsheets (project_id, updated_at DESC);

-- A clip: a screen recording made in the browser.
--
-- The bytes go to the same object storage as attachments and are reached the
-- same way, through a signed URL this server issues after checking who is
-- asking. A separate table rather than a flag on attachments because a clip
-- has a duration and a poster frame, and because it belongs to a project
-- rather than to a task - the two authorization questions are different, and
-- the attachments table already learned that lesson with chat files.
CREATE TABLE clips (
    id           uuid PRIMARY KEY,
    project_id   uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    -- Optional: a clip recorded while looking at a task stays with it.
    task_id      uuid REFERENCES tasks (id) ON DELETE SET NULL,
    title        text NOT NULL,
    storage_key  text NOT NULL UNIQUE,
    content_type text NOT NULL,
    size_bytes   bigint NOT NULL,
    duration_ms  integer,
    created_by   uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX clips_project_idx ON clips (project_id, created_at DESC);
CREATE INDEX clips_task_idx ON clips (task_id) WHERE task_id IS NOT NULL;
