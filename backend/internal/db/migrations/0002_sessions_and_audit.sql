-- Sessions and audit trail.
--
-- Before this, a JWT was the whole session: valid for its full lifetime, with
-- no way to revoke it. Deactivating an account left its live token working
-- until expiry. Sessions are now server-side records that can be revoked, and
-- the JWT becomes a short-lived access token minted from one.

CREATE TABLE sessions (
    id            uuid PRIMARY KEY,
    user_id       uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    -- Only the hash is stored: a leaked database must not yield usable
    -- refresh tokens, exactly as with passwords.
    token_hash    bytea       NOT NULL UNIQUE,
    user_agent    text        NOT NULL DEFAULT '',
    ip            text        NOT NULL DEFAULT '',
    expires_at    timestamptz NOT NULL,
    revoked_at    timestamptz,
    last_used_at  timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX sessions_user_idx    ON sessions (user_id, revoked_at);
CREATE INDEX sessions_expiry_idx  ON sessions (expires_at) WHERE revoked_at IS NULL;

-- Append-only record of who changed what. No UPDATE or DELETE is ever issued
-- against this table by the application; retention is a separate operational
-- concern.
CREATE TABLE audit_log (
    id            bigserial PRIMARY KEY,
    occurred_at   timestamptz NOT NULL DEFAULT now(),
    actor_id      uuid        REFERENCES users (id) ON DELETE SET NULL,
    actor_label   text        NOT NULL DEFAULT '',
    action        text        NOT NULL,
    resource_type text        NOT NULL,
    resource_id   text        NOT NULL DEFAULT '',
    -- What changed, as a redacted before/after pair. Password hashes and
    -- tokens are never written here (see internal/audit).
    changes       jsonb,
    ip            text        NOT NULL DEFAULT '',
    user_agent    text        NOT NULL DEFAULT '',
    request_id    text        NOT NULL DEFAULT '',
    status        integer     NOT NULL DEFAULT 0
);

CREATE INDEX audit_log_time_idx     ON audit_log (occurred_at DESC);
CREATE INDEX audit_log_actor_idx    ON audit_log (actor_id, occurred_at DESC);
CREATE INDEX audit_log_resource_idx ON audit_log (resource_type, resource_id, occurred_at DESC);
