-- Full-text search, plus the Space -> Folder -> List hierarchy.
--
-- Hierarchy shape (mirrors how ClickUp-style tools organise work):
--
--   Space ── Folder ── List ── Task
--     └────────────────List ── Task      (a List may hang directly off a Space)
--
-- "Project" keeps its name and its API, and simply becomes a List that also
-- carries scheduling metadata. Existing projects are adopted into a default
-- Space at the bottom of this file, so nothing breaks.

-- --------------------------------------------------------------------------
-- Full-text search
-- --------------------------------------------------------------------------
-- Generated columns keep the vector in sync automatically - no trigger to
-- forget, no chance of a stale index after a direct UPDATE.
ALTER TABLE tasks
    ADD COLUMN search tsvector
    GENERATED ALWAYS AS (
        setweight(to_tsvector('simple', coalesce(title, '')), 'A') ||
        setweight(to_tsvector('simple', coalesce(description, '')), 'B')
    ) STORED;

CREATE INDEX tasks_search_idx ON tasks USING GIN (search);

ALTER TABLE projects
    ADD COLUMN search tsvector
    GENERATED ALWAYS AS (
        setweight(to_tsvector('simple', coalesce(name, '')), 'A') ||
        setweight(to_tsvector('simple', coalesce(key, '')), 'A') ||
        setweight(to_tsvector('simple', coalesce(description, '')), 'B')
    ) STORED;

CREATE INDEX projects_search_idx ON projects USING GIN (search);

-- --------------------------------------------------------------------------
-- Hierarchy
-- --------------------------------------------------------------------------
CREATE TABLE spaces (
    id          uuid PRIMARY KEY,
    name        text        NOT NULL,
    description text        NOT NULL DEFAULT '',
    color       text        NOT NULL DEFAULT '#2a78d6',
    team_id     uuid        REFERENCES teams (id) ON DELETE SET NULL,
    -- Private spaces are visible only to their members; public ones to every
    -- authenticated user, which is the default for an internal tool.
    is_private  boolean     NOT NULL DEFAULT false,
    position    integer     NOT NULL DEFAULT 0,
    archived    boolean     NOT NULL DEFAULT false,
    created_by  uuid        REFERENCES users (id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX spaces_name_idx ON spaces (lower(name));

CREATE TABLE space_members (
    space_id uuid NOT NULL REFERENCES spaces (id) ON DELETE CASCADE,
    user_id  uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    -- Role held *at this level*; the effective role on anything underneath is
    -- the strongest grant found walking up the tree.
    role     text NOT NULL DEFAULT 'member'
             CHECK (role IN ('owner', 'admin', 'member', 'guest')),
    PRIMARY KEY (space_id, user_id)
);

CREATE INDEX space_members_user_idx ON space_members (user_id);

-- Folders group Lists inside a Space. They never nest, matching the model
-- users already understand from comparable tools.
CREATE TABLE folders (
    id         uuid PRIMARY KEY,
    space_id   uuid        NOT NULL REFERENCES spaces (id) ON DELETE CASCADE,
    name       text        NOT NULL,
    color      text        NOT NULL DEFAULT '#94a3b8',
    position   integer     NOT NULL DEFAULT 0,
    archived   boolean     NOT NULL DEFAULT false,
    created_by uuid        REFERENCES users (id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX folders_space_idx ON folders (space_id, position);

-- A project is a List. These columns place it in the tree; both are nullable
-- during the transition, and space_id is backfilled below.
ALTER TABLE projects ADD COLUMN space_id  uuid REFERENCES spaces (id)  ON DELETE CASCADE;
ALTER TABLE projects ADD COLUMN folder_id uuid REFERENCES folders (id) ON DELETE SET NULL;
ALTER TABLE projects ADD COLUMN position  integer NOT NULL DEFAULT 0;
ALTER TABLE projects ADD COLUMN archived  boolean NOT NULL DEFAULT false;

CREATE INDEX projects_space_idx  ON projects (space_id, position);
CREATE INDEX projects_folder_idx ON projects (folder_id, position);

-- A folder must live in the same space as the list that references it.
-- Enforced by a trigger because the check spans two tables.
CREATE OR REPLACE FUNCTION project_folder_matches_space() RETURNS trigger AS $$
BEGIN
    IF NEW.folder_id IS NOT NULL THEN
        IF NOT EXISTS (
            SELECT 1 FROM folders f
             WHERE f.id = NEW.folder_id AND f.space_id = NEW.space_id
        ) THEN
            RAISE EXCEPTION 'folder % does not belong to space %', NEW.folder_id, NEW.space_id;
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER projects_folder_space_check
    BEFORE INSERT OR UPDATE OF folder_id, space_id ON projects
    FOR EACH ROW EXECUTE FUNCTION project_folder_matches_space();

-- --------------------------------------------------------------------------
-- Backfill: adopt every existing project into a default Space.
-- --------------------------------------------------------------------------
-- Runs only when projects exist without a space, so a fresh database is
-- untouched and a re-run is a no-op.
DO $$
DECLARE
    default_space_id uuid;
    creator_id       uuid;
BEGIN
    IF EXISTS (SELECT 1 FROM projects WHERE space_id IS NULL) THEN
        SELECT id INTO creator_id FROM users WHERE role = 'admin' ORDER BY created_at LIMIT 1;

        INSERT INTO spaces (id, name, description, created_by)
        VALUES (gen_random_uuid(), 'Workspace',
                'Created automatically when the Space/Folder/List hierarchy was introduced.',
                creator_id)
        RETURNING id INTO default_space_id;

        -- Everyone who was on any project keeps access through the space.
        INSERT INTO space_members (space_id, user_id, role)
        SELECT DISTINCT default_space_id, pm.user_id, 'member'
          FROM project_members pm
        ON CONFLICT DO NOTHING;

        IF creator_id IS NOT NULL THEN
            INSERT INTO space_members (space_id, user_id, role)
            VALUES (default_space_id, creator_id, 'owner')
            ON CONFLICT (space_id, user_id) DO UPDATE SET role = 'owner';
        END IF;

        UPDATE projects SET space_id = default_space_id WHERE space_id IS NULL;
    END IF;
END $$;
