-- Settings an administrator can change from the application, overriding what
-- the environment supplied.
--
-- One row per key rather than a single JSONB blob: each value is written and
-- audited on its own, and a row carries who last touched it. A blob would make
-- "who changed the SMTP password" unanswerable.
CREATE TABLE app_settings (
    key        text        PRIMARY KEY,
    -- Secrets are stored encrypted (AES-GCM, key derived from JWT_SECRET), so
    -- a database dump does not hand over the directory bind password and the
    -- mail account alongside it. Non-secrets are stored as written, because
    -- encrypting an LDAP hostname buys nothing and makes it unreadable to the
    -- operator who needs to debug it.
    value      text        NOT NULL,
    is_secret  boolean     NOT NULL DEFAULT false,
    updated_by uuid        REFERENCES users (id) ON DELETE SET NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);
