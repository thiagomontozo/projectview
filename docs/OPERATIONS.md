# Operations

Running ProjectView as an internal service: backup and restore, retention,
provisioning, single sign-on, and what has to change to run more than one
instance.

Everything here is written to be executed, not admired. Each procedure states
what it assumes, and the restore procedure states how to verify it worked —
a backup nobody has restored is a hypothesis, not a backup.

---

## First run

The stack creates one administrator the first time it starts against an empty
database, and nothing else. Everything below is what to do before anyone else
can reach it.

| | Default | Variable |
|---|---|---|
| Username | `admin` | `BOOTSTRAP_ADMIN_USERNAME` |
| Password | `ChangeMe123!` | `BOOTSTRAP_ADMIN_PASSWORD` |
| E-mail | `admin@example.com` | `BOOTSTRAP_ADMIN_EMAIL` |

**These are published in the repository. Treat them as known to everybody.**

1. **Set `BOOTSTRAP_ADMIN_PASSWORD` before the first start**, or change it
   immediately after from **Settings → Password**. The variable is read only
   against an empty database — once the account exists, editing `.env` does
   nothing, which is the mistake worth knowing about in advance. Changing your
   own password requires the current one even as an administrator, and ends
   every other session the account has open.
2. **Set `JWT_SECRET` to a long random value.** It signs every session token,
   and it is also what the settings screen derives its encryption key from, so
   rotating it later makes stored integration secrets unreadable (they are
   dropped with a warning, not silently mangled — see below).
3. **Set `POSTGRES_PASSWORD`**, and in production point `DATABASE_URL` at
   credentials of your own with `sslmode=require`.
4. **Install a real TLS certificate** in `proxy/certs/`. The self-signed
   fallback exists so a local run works, not so a deployment can skip this.
5. **Create a second administrator** from **Administration → Users**. The last
   active administrator cannot be demoted or deactivated, which protects you
   from a lockout — but it also means one administrator is a single point of
   failure the day that person leaves.

```bash
# Confirm the account exists and nothing else was seeded by accident:
docker compose exec -T postgres psql -U projectview -d projectview   -c "SELECT username, role, active FROM users;"
```

---

## Backup

The only stateful component is PostgreSQL. The application containers hold
nothing that is not in the database or the image, so a database backup plus the
repository is a complete recovery position.

### Nightly dump

```bash
docker compose exec -T postgres \
  pg_dump -U projectview -d projectview --format=custom --compress=9 \
  > "backup-$(date +%F).dump"
```

`--format=custom` rather than plain SQL: it restores in parallel, allows
selective restore of a single table, and compresses. The output is a binary
file — `pg_restore` reads it, `psql` does not.

Schedule it with whatever already runs on the host (cron, systemd timer, Task
Scheduler). Two things matter more than the schedule:

- **Store it somewhere the database host cannot reach.** A backup on the same
  disk survives a dropped table and nothing else.
- **Keep several.** Corruption is often noticed days later, and a single
  rolling backup will by then be a faithful copy of the corruption.

### What is *not* in the dump

- **`JWT_SECRET`.** Restoring with a different secret invalidates every access
  token, which is harmless — people sign in again. Restoring with the *same*
  secret keeps tokens that were issued before the restore valid, which is
  usually what you want during a rollback.
- **TLS certificates** (`proxy/certs/`). Regenerated automatically on boot if
  absent, as self-signed. A real certificate is yours to keep a copy of.
- **`.env`.** Back it up separately, and not into the same store as the
  database: together they are a complete impersonation kit.

---

## Restore

```bash
# 1. Stop everything that writes.
docker compose stop backend

# 2. Recreate an empty database.
docker compose exec -T postgres psql -U projectview -d postgres \
  -c "DROP DATABASE projectview WITH (FORCE);" \
  -c "CREATE DATABASE projectview OWNER projectview;"

# 3. Restore.
docker compose exec -T postgres \
  pg_restore -U projectview -d projectview --no-owner --jobs=4 < backup-2026-08-15.dump

# 4. Start the application. Migrations run on boot and are idempotent, so a
#    dump from an older schema is brought forward automatically.
docker compose start backend
```

### Verifying a restore

Do not accept "the container started" as proof. Run the smoke test against the
restored stack:

```bash
bash scripts/smoke-test.sh https://localhost
```

It creates and deletes its own fixtures, so it is safe against a real
installation, and it exercises the paths a silent restore failure hides in:
authentication, the hierarchy, search, and the audit trail.

Then check three counts against what you expect:

```sql
SELECT count(*) FROM users WHERE active;
SELECT count(*) FROM tasks;
SELECT max(occurred_at) FROM audit_log;   -- how fresh the backup really was
```

### Point-in-time recovery

Nightly dumps mean up to a day of loss. If that is unacceptable, enable WAL
archiving on the PostgreSQL container (`archive_mode`, `archive_command`) and
keep base backups with `pg_basebackup`. That is a different operational
commitment — archive storage, monitoring that archiving has not stalled — and
should be a deliberate decision rather than a default.

---

## Retention

Two tables grow forever unless told otherwise. Both are off by default:
deleting records because a configuration file was left empty is exactly what a
retention policy exists to prevent.

| Variable | Effect |
|---|---|
| `AUDIT_RETENTION_DAYS` | Deletes audit entries older than N days. `0` keeps everything. |
| `NOTIFICATION_RETENTION_DAYS` | Deletes **read** notifications older than N days. Unread ones are never expired — that would destroy the only copy of a message meant for someone. |
| `RETENTION_CRON` | When the sweep runs. Default `30 3 * * *`. |

Before setting `AUDIT_RETENTION_DAYS`, check what your organisation requires
you to keep. The audit log is the record of who changed what; a retention
window shorter than the obligation is a compliance problem wearing the costume
of a tidy database.

---

## Changing settings without a redeploy

**Administration → System settings**, administrators only.

AD/LDAP, SMTP, single sign-on, the alert lead time and the retention windows
can all be changed there. The override is stored in PostgreSQL and applied to
the running process immediately: the next login uses the new directory, the
next notification the new mail server. Nothing needs restarting.

The screen also runs two live checks against the settings in force — a real
test message, and a real bind against the directory with credentials that are
used for the attempt and never stored.

**Back up the mirror, or the database, or both.** Saving writes a
`.env`-shaped copy to `SETTINGS_ENV_FILE` (`./config/app.env` by default). It
carries the directory bind password and the mail account in the clear, exactly
as `.env` does, so treat it the same way. The database is what the application
reads; the file is for backup, review and rebuilding elsewhere.

**Rotating `JWT_SECRET` makes stored secrets unreadable.** They are sealed with
a key derived from it. A rotated secret leaves them undecryptable, and the
application drops them with a warning in the log rather than passing gibberish
to the directory. Re-enter them on the settings screen afterwards; everything
else survives.

The environment still matters. It supplies the starting values, it is what a
fresh installation boots with, and clearing an override on the screen reverts
that key to it.

## Single sign-on (OIDC)

Works with any compliant provider — Entra ID, Okta, Keycloak, Google
Workspace. Endpoints are discovered from the issuer, so a provider that moves
them does not require a redeploy.

```bash
OIDC_ENABLED=true
OIDC_ISSUER_URL=https://login.microsoftonline.com/<tenant>/v2.0
OIDC_CLIENT_ID=<application id>
OIDC_CLIENT_SECRET=<client secret>
OIDC_REDIRECT_URL=https://projectview.example.com/api/auth/oidc/callback
OIDC_SCOPES="openid profile email"
OIDC_AUTO_PROVISION=false
```

These are the starting values; the same settings are editable from the
settings screen once the stack is up, which is usually easier than a redeploy.

Register `OIDC_REDIRECT_URL` with the provider exactly as written — a redirect
URI mismatch is the single most common cause of a failed first setup.

**`OIDC_AUTO_PROVISION` is off deliberately.** With it on, anyone the identity
provider will authenticate has an account here, which is a wider door than most
organisations intend. With it off, an account must already exist; sign-in
matches it by the provider's subject claim, falling back to e-mail for accounts
that predate SSO, and remembers the subject afterwards so a later rename does
not orphan anybody.

SSO does not replace local accounts. Keep at least one local administrator: an
installation whose only way in is an external provider is an installation that
is unreachable during that provider's outage.

---

## Provisioning (SCIM 2.0)

Endpoint: `https://<host>/scim/v2/Users`. Authenticated by a service token,
never by a user session.

1. Settings → Service tokens → create one with the `scim` scope.
2. Copy the secret. It is shown **once**; only its hash is stored.
3. Configure it as the bearer token in your identity provider's provisioning
   settings.

Supported: list (with the `userName eq "…"` filter), get, create, replace,
patch, delete. A delete **deactivates** — it never removes the row, because the
person's tasks, comments and time entries belong to the organisation, and a
directory removing an employee is not a request to destroy project history.

Deactivating through SCIM also **revokes every live session immediately**.
Flipping a flag and leaving existing tokens working until they expire is the
exact gap provisioning exists to close.

Groups are not implemented. Team membership here is a project decision, not a
directory one, and mapping it from the identity provider would let the
directory quietly rearrange who can see what.

Rotate a token by creating the new one, updating the provider, then revoking
the old one — in that order, so provisioning never has a window with no valid
credential.

---

## Privacy requests

| Request | How |
|---|---|
| "Give me my data" | Settings → Your data → Download. Self-service; also available to an administrator for any account, and every export is recorded in the audit trail. |
| "Delete me" | `POST /api/users/{id}/erase`, administrator only, confirming with the username. |

Erasure **anonymises** rather than deletes. Deleting the row would cascade
through tasks, comments and time entries — work that belongs to the
organisation — and would punch holes in the audit trail, which is the one table
that has to stay intact to be worth keeping. Identifiers are replaced,
credentials destroyed, sessions revoked and the account deactivated. What
remains is a tombstone.

Free text the person wrote is left alone. It can name other people, and
rewriting history to remove one author's words corrupts conversations that are
not theirs alone. A genuinely sensitive comment is a deletion request against
that comment, not against the account.

---

## Running more than one instance

The application is stateless apart from two things, and both are already
handled:

- **Sessions** live in PostgreSQL, not in memory, so any replica can serve any
  request.
- **The single sign-on flow** carries its state and PKCE verifier in
  short-lived cookies rather than in server memory, so the replica that answers
  the callback need not be the one that started the flow.

What is *not* yet distributed, and would need to be before scaling out:

- **The WebSocket hub is per-process.** Two replicas mean a message published
  on one is not pushed to clients connected to the other. Fixing it means a
  shared bus — Redis pub/sub is the usual answer — and it is not a change to
  make speculatively. Until then, run one backend replica, or accept that
  real-time push is per-replica while REST stays correct.
- **The alert, digest and retention schedulers run in every process.** Two
  replicas would send some notifications twice. They need a lock — an advisory
  lock in PostgreSQL is sufficient — before a second replica is safe.

For PostgreSQL itself, use your platform's managed high availability rather
than building it here. The application needs one connection string; how many
machines are behind it is not its concern.

---

## Health, metrics and logs

| Endpoint | Purpose |
|---|---|
| `/api/health` | Liveness. Answers as long as the process is up. |
| `/api/ready` | Readiness. **Fails when the database is unreachable**, so an orchestrator stops routing traffic during an outage instead of serving errors. |
| `/metrics` | Prometheus. Deliberately **not** published through the edge proxy — reaching it from outside would hand a stranger the shape of the whole installation. Scrape it on the container network. |

Logs are structured JSON carrying a request id, so a report can be traced back
to the calls behind it:

```bash
docker compose logs backend --no-color | grep '"request_id":"<id>"'
```
