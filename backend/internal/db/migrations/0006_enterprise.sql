-- Enterprise: goals, saved dashboards, project baselines, service tokens,
-- capacity, SSO identities and the columns privacy erasure needs.

-- --------------------------------------------------------------------------
-- Goals and key results
-- --------------------------------------------------------------------------
-- A goal states an outcome; key results are how progress is measured. Kept as
-- two tables rather than one with repeated columns because a goal has several
-- measures and each is compared against its own target.
CREATE TABLE goals (
    id          uuid        PRIMARY KEY,
    name        text        NOT NULL,
    description text        NOT NULL DEFAULT '',
    space_id    uuid        REFERENCES spaces (id) ON DELETE CASCADE,
    team_id     uuid        REFERENCES teams (id) ON DELETE SET NULL,
    owner_id    uuid        REFERENCES users (id) ON DELETE SET NULL,
    start_date  timestamptz,
    due_date    timestamptz,
    status      text        NOT NULL DEFAULT 'active',
    archived    boolean     NOT NULL DEFAULT false,
    created_by  uuid        REFERENCES users (id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT goals_status_check CHECK (status IN ('active', 'at_risk', 'achieved', 'missed'))
);

CREATE INDEX goals_space_idx ON goals (space_id) WHERE NOT archived;
CREATE INDEX goals_owner_idx ON goals (owner_id) WHERE NOT archived;

-- source decides where current_value comes from:
--   manual          - somebody types it in
--   tasks_completed - share of tasks done in the linked project
--   tasks_count     - number of tasks done in the linked project
-- The derived kinds exist so a goal cannot drift from the work underneath it:
-- a number nobody updates is worse than no number at all.
CREATE TABLE key_results (
    id            uuid   PRIMARY KEY,
    goal_id       uuid   NOT NULL REFERENCES goals (id) ON DELETE CASCADE,
    name          text   NOT NULL,
    source        text   NOT NULL DEFAULT 'manual',
    unit          text   NOT NULL DEFAULT '',
    start_value   double precision NOT NULL DEFAULT 0,
    target_value  double precision NOT NULL DEFAULT 100,
    current_value double precision NOT NULL DEFAULT 0,
    project_id    uuid   REFERENCES projects (id) ON DELETE SET NULL,
    position      integer NOT NULL DEFAULT 0,

    CONSTRAINT key_results_source_check CHECK (source IN ('manual', 'tasks_completed', 'tasks_count')),
    -- A derived measure without a project has nothing to derive from, and
    -- would silently report zero forever.
    CONSTRAINT key_results_source_needs_project
        CHECK (source = 'manual' OR project_id IS NOT NULL)
);

CREATE INDEX key_results_goal_idx ON key_results (goal_id, position);

-- --------------------------------------------------------------------------
-- Saved dashboards
-- --------------------------------------------------------------------------
-- The card layout used to live in the browser, which meant it did not follow
-- anyone to a second machine. The layout is JSONB because it is read and
-- written whole, by one owner, and never queried by its contents.
CREATE TABLE dashboards (
    id         uuid        PRIMARY KEY,
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name       text        NOT NULL DEFAULT 'My dashboard',
    layout     jsonb       NOT NULL DEFAULT '[]'::jsonb,
    is_default boolean     NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- One default per person, enforced rather than assumed: two defaults means the
-- app picks arbitrarily and the layout appears to change on its own.
CREATE UNIQUE INDEX dashboards_one_default_idx ON dashboards (user_id) WHERE is_default;

-- --------------------------------------------------------------------------
-- Baselines
-- --------------------------------------------------------------------------
-- A baseline is the plan as it was approved. Earned value compares today
-- against it, so it must be a snapshot: reading the plan out of the live rows
-- would compare the schedule against itself and report perfection forever.
CREATE TABLE project_baselines (
    id          uuid        PRIMARY KEY,
    project_id  uuid        NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    name        text        NOT NULL,
    snapshot    jsonb       NOT NULL,
    captured_by uuid        REFERENCES users (id) ON DELETE SET NULL,
    captured_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX project_baselines_project_idx ON project_baselines (project_id, captured_at DESC);

-- --------------------------------------------------------------------------
-- Service tokens
-- --------------------------------------------------------------------------
-- Machine credentials for SCIM provisioning and the read-only reporting API.
-- Only the hash is stored: a token readable from the database is a token the
-- database administrator has silently been issued.
CREATE TABLE service_tokens (
    id           uuid        PRIMARY KEY,
    name         text        NOT NULL,
    token_hash   text        NOT NULL UNIQUE,
    scopes       text[]      NOT NULL DEFAULT '{}',
    created_by   uuid        REFERENCES users (id) ON DELETE SET NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz,
    revoked_at   timestamptz
);

CREATE INDEX service_tokens_active_idx ON service_tokens (created_at DESC) WHERE revoked_at IS NULL;

-- --------------------------------------------------------------------------
-- Capacity, external identities and erasure
-- --------------------------------------------------------------------------
-- Capacity planning compares allocation against a real number of hours rather
-- than against an assumption baked into the frontend.
ALTER TABLE users ADD COLUMN weekly_capacity_hours double precision NOT NULL DEFAULT 40;

-- The subject claim from the identity provider, or the SCIM externalId. Kept
-- separate from username: an IdP may rename a person, and the stable id is the
-- only thing that survives it.
ALTER TABLE users ADD COLUMN external_id text;
CREATE UNIQUE INDEX users_external_id_key ON users (external_id) WHERE external_id IS NOT NULL;

-- Erasure anonymises rather than deletes. Deleting a user would cascade
-- through their tasks, comments and time entries, destroying records that
-- belong to the organisation and not to the individual; and the audit trail
-- must remain intact to be worth keeping at all.
ALTER TABLE users ADD COLUMN anonymized_at timestamptz;

ALTER TABLE users DROP CONSTRAINT users_auth_source_check;
ALTER TABLE users ADD CONSTRAINT users_auth_source_check
    CHECK (auth_source IN ('local', 'ad', 'oidc', 'scim'));
