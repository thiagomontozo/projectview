# ProjectView — Project Management Dashboard

[![CI](https://github.com/thiagomontozo/projectview/actions/workflows/ci.yml/badge.svg)](https://github.com/thiagomontozo/projectview/actions/workflows/ci.yml)

A web application for managing projects, teams and tasks, in the spirit of
ClickUp: a draggable card dashboard and kanban boards, resource allocation,
per-person deadline alerts, an internal chat, tracking charts, and login
integrated with Active Directory. Fully containerized and served over HTTPS.

**Stack:** **Go** backend, **React + TypeScript** frontend, PostgreSQL, behind an
**nginx** edge proxy.

Progress by phase is in [ROADMAP.md](ROADMAP.md); what is still to build is in
[BACKLOG.md](BACKLOG.md).

## Features

- **Active Directory login** — authentication against LDAP/AD
  (`AD_ENABLED=true`), with a fallback to local accounts (username/password
  hashed with bcrypt) when AD is not configured.
- **Teams, projects, tasks and sub-tasks** — full CRUD. A sub-task is simply a
  task whose `parentTask` points at its parent, which allows nesting.
- **Resource allocation** — anyone can be assigned to many tasks and projects
  at once. The "Resource Allocation" screen shows each person's workload (open
  tasks, estimated hours, projects, overdue items).
- **Start and due dates per task**, with overdue tasks flagged visually.
- **Individual deadline alerts** — a scheduled job (cron, configurable) sweeps
  tasks and notifies *each assignee* — in real time over WebSocket and by
  e-mail — when a task is approaching its due date or already overdue. Each
  (task, user, alert type) combination fires only once, so nobody is spammed
  on every tick.
- **Internal e-mail integration (SMTP)** — used for deadline alerts and task
  assignment notifications, fully configurable through `.env`. When disabled,
  e-mails are logged instead of sent, so the rest of the system keeps working.
- **Internal chat** — team/project channels (created automatically) and direct
  messages, with history stored in PostgreSQL. Messages are sent over REST and
  delivered in real time over WebSocket. Conversations have **threads**
  (replies stay in the thread rather than repeating in the transcript),
  **emoji reactions** that toggle, **@mentions** that notify only people who
  can actually read the channel, plus **presence** and **typing indicators**.
- **Docs** — Markdown documents per space or project, with a version kept on
  every save that changes the text. Old versions can be read back and
  restored. A document is exactly as visible, and exactly as editable, as the
  space or project that contains it.
- **Notification preferences** — per notification type and per channel (in-app
  or e-mail), with quiet hours and an optional daily or weekly digest that
  gathers whatever immediate delivery was turned off.
- **Tracking charts** — task distribution by status, workload per resource,
  30-day completion trend, and progress per project.
- **Dashboard with draggable cards** — the landing dashboard is a grid of
  cards (KPIs + charts) that can be rearranged by drag-and-drop, with the
  chosen order persisted per user in the browser.
- **Task dependencies and the critical path** — a task can wait on another;
  cycles are refused by the database, since they make the schedule unsolvable.
  The timeline draws the arrows and highlights the chain where any slip moves
  the project's end date.
- **Attachments** — files on a task or on one of its comments, kept in an
  S3-compatible object store (MinIO in the bundled stack, S3 or an equivalent
  in production) rather than in the database. Uploads pass through the API, so
  the size and type limits and the virus-scan hook are enforceable; downloads
  do not, because the browser is handed a **time-limited signed URL** and
  fetches the object directly. Nothing in the bucket is public. Deleting an
  attachment deletes the object, and so does deleting the task, the project or
  the whole space that contained it.
- **Custom fields** — typed per project or space (text, number, date, select,
  multi-select, checkbox, url, email, user), stored as JSONB with a GIN index.
- **Time tracking** — a timer (one per person, enforced by the database),
  manual entries, and tracked hours against the estimate.
- **Watchers** — follow a task without being responsible for it.
- **Automations** — `trigger → condition → action` rules that set status or
  priority, assign, add watchers or notify. Every evaluation is recorded,
  including the ones whose conditions did not hold, because a rule that
  silently does not fire is otherwise impossible to debug.
- **Six views over the same tasks** — every project can be seen as a **Board**
  (kanban, drag between columns), a **List** (grouped and virtualised), a
  **Table** (spreadsheet-style inline editing), a **Calendar**, a **Timeline**
  (Gantt bars you drag to reschedule, with milestones), or a **Workload** grid
  showing capacity against allocation per person per week. Filters, grouping
  and sorting are shared across them, and the chosen view is remembered per
  project. Multi-select supports bulk status and priority changes.
- **PostgreSQL configurable via environment variable** (`DATABASE_URL`), with
  the **schema created automatically on first run** by migrations embedded in
  the binary, plus an admin user, a sample team and a sample project seeded
  into an empty database.
- **HTTPS by default** — an nginx edge proxy terminates TLS, applies the
  security headers and rate limits, and is the only container published to the
  host.
- **Goals and OKRs** — objectives with key results that are either typed in or
  **read from the tasks themselves**, so a goal cannot drift from the work
  underneath it.
- **Portfolio** — every project at once with a **derived** health rating (a RAG
  status somebody updates by hand is green until the week it is red), plus
  **capacity planning**: committed hours against the hours each person actually
  has, with a task's estimate shared between its assignees.
- **Baselines and earned value** — freeze the approved plan and measure against
  it: PV, EV, AC, SPI, CPI, EAC and VAC, in **hours**. The system holds
  estimates and tracked time but no rates, and inventing one would produce a
  number that looks authoritative and is not.
- **CSV exports** for the task list, the portfolio and the capacity report.
- **Saved dashboards** — the card arrangement is stored per person on the
  server, so it follows them between machines.
- **Single sign-on (OIDC)** alongside AD and local accounts, and **SCIM 2.0
  provisioning** so deactivating someone in the directory revokes their
  sessions here immediately.
- **Privacy** — self-service data export, administrator-performed erasure that
  anonymises rather than deletes, and optional retention windows for the audit
  trail and read notifications.
- **User administration** — an administrator can create accounts, **promote and
  demote**, deactivate and reactivate, and reset passwords from the interface.
  A role change applies to the session the person already holds, and the last
  active administrator cannot be demoted or deactivated: the path back from
  that is an `UPDATE` against the database.
- **Settings screen for administrators** — AD/LDAP, SMTP, single sign-on,
  alert lead time and retention are editable in the application and take
  effect immediately, with no restart. Only administrators may read or change
  them, credentials are stored encrypted and never read back, and every change
  is recorded in the audit trail.
- **Fully containerized** with Docker Compose (proxy, frontend, backend,
  PostgreSQL, MinIO).

## Architecture

```
Internet
   │  HTTPS :443  (HTTP :80 redirects)
   ▼
┌─────────┐   /api, /ws   ┌─────────┐        ┌───────┐
│  proxy  │──────────────▶│ backend │───────▶│  pg   │
│ (nginx) │               │  (Go)   │        └───────┘
└──┬───┬──┘               └────┬────┘
   │   │  everything else      │ PUT / DELETE
   │   ▼                       ▼
   │ ┌──────────┐         ┌─────────┐
   │ │ frontend │         │  minio  │  attachments
   │ └──────────┘         └─────────┘
   │                           ▲
   └───────────────────────────┘
     /attachments/…  GET with a time-limited signed URL
```

Attachment bytes never pass through the backend on the way out: it signs a
short-lived URL and the browser fetches the object through the proxy directly.

```
.
├── proxy/       TLS termination + routing/security rules (nginx)
├── backend/     REST API + WebSocket in Go (chi, pgx, go-ldap, JWT, cron)
├── frontend/    React + TypeScript SPA (Vite) + Recharts + @dnd-kit
└── ca/          Optional extra root CAs for the build (see below)
```

The stack also runs **MinIO** for attachments. Like PostgreSQL it is reachable
only on the internal network; the browser gets at an object solely through a
signed URL the proxy routes to it.

- **Proxy (nginx)** — the only service bound to a host port. Terminates TLS,
  redirects HTTP to HTTPS, sets HSTS and the other security headers, rate
  limits the API (and the login endpoint in particular, since every attempt
  reaches Active Directory), and routes `/api` and `/ws` to the backend and
  everything else to the SPA. Because TLS ends here, the backend runs with
  `NODE_ENV=production` and its session cookies are marked `secure`.
- **Backend (Go)** — `net/http` with the [chi](https://github.com/go-chi/chi)
  router, [pgx](https://github.com/jackc/pgx) for PostgreSQL, JWT
  authentication (httpOnly cookie or Bearer token),
  [go-ldap](https://github.com/go-ldap/ldap) for AD, `net/smtp` for e-mail,
  [robfig/cron](https://github.com/robfig/cron) for deadline alerts, and a
  small WebSocket hub ([gorilla/websocket](https://github.com/gorilla/websocket))
  for chat and real-time notifications.
- **Frontend (React + TypeScript)** — Vite, React Router with route-level code
  splitting, [TanStack Query](https://tanstack.com/query) for the data layer,
  [Radix](https://www.radix-ui.com/) primitives under a token-based design
  system, `i18next` for pt-BR/en, Recharts for the charts, `@dnd-kit` for the
  draggable dashboard and kanban board, and a native WebSocket client for
  incoming messages and notifications — all writes go through the REST API.

### Frontend architecture

| Concern | Approach |
|---|---|
| Design system | Two-layer CSS tokens (primitive palette → semantic tokens) in [tokens.css](frontend/src/styles/tokens.css); components reference only the semantic layer, so re-theming touches one file |
| Theme | Light / dark / **system**, the last setting no attribute at all so the OS decides and keeps deciding |
| Data | TanStack Query with centralised query keys, retry that skips 4xx, and optimistic kanban moves that roll back on failure |
| Sessions | Silent token refresh on 401 with a shared in-flight promise, so six concurrent requests trigger one refresh rather than six — which would otherwise sign the user out, since refresh tokens rotate on use |
| Accessibility | Radix primitives for focus trapping and keyboard behaviour, skip link, landmarks, `:focus-visible` rings, `prefers-reduced-motion`, labelled controls with `role="alert"` errors |
| i18n | Bundled pt-BR and en dictionaries, browser detection, `<html lang>` kept in sync |
| Keyboard | `Ctrl/Cmd+K` command palette that searches tasks through the server's full-text index, not a client-side filter |
| States | Skeletons shaped like the content, distinct empty vs. error states, and an error boundary so one broken component cannot blank the app |
| Views | Filtering, grouping and sorting live in one pure module ([useViewState.ts](frontend/src/views/useViewState.ts)) shared by every view, so a filter means the same thing in the table as on the board |
| Large lists | The list view is virtualised with TanStack Virtual — group headers and rows are flattened into one array so a single virtualizer covers the whole list |
- **Object storage** — any S3-compatible store, addressed through a small
  client written directly against the HTTP API (`internal/storage`) rather than
  through an SDK: the surface used is PUT, DELETE, HEAD and a presigned GET,
  while an SDK brings a credential-resolution chain, a retry policy and a
  middleware stack this application already has opinions about. The SigV4
  signing is checked against Amazon's own published test vectors, because a
  signature is either byte-for-byte correct or an opaque 403.
- **Database** — PostgreSQL 16. The address is fully configurable through
  `DATABASE_URL` and can point at the bundled container or any other instance
  (RDS, Cloud SQL, on-prem, …). On first run the backend applies the migrations
  embedded in its binary, creating every table and index it needs. Hand-written
  SQL in a repository layer ([backend/internal/repo](backend/internal/repo));
  no ORM.

### About the real-time protocol

The WebSocket (`/ws?token=<jwt>`) carries two kinds of traffic, and the split
is deliberate.

**Server → client**, the majority: `notification`, `chat:message`,
`chat:reaction`, `presence`, `typing`. Every write — creating a task, sending a
message, moving a kanban card — happens over an ordinary REST call, because
that is where validation, authorization and the audit trail live. The server
then fans the corresponding event out to whoever has a tab open, and the client
treats it as an invalidation rather than as data: it refetches through the
query that already knows the shape, instead of maintaining a second code path
that can drift.

**Client → server**, deliberately tiny: `typing`, `typing:stop`, `ping`.
Nothing here is persisted and nothing is authoritative — it is state that is
obsolete within seconds and costs nothing to lose on a reconnect. A frame the
server cannot parse is ignored rather than treated as a reason to drop the
connection, so a client on an older build keeps working.

Because it stays this small, the protocol is trivial to implement in Go and the
whole REST surface remains exercisable with `curl`/Postman.

## Running it (Docker)

1. *(Optional but recommended)* Copy the environment file and adjust it:

   ```bash
   cp .env.example .env
   ```

   `docker-compose.yml` ships working defaults for every variable, so
   `docker compose up` works without a `.env`. For any real use, set
   `JWT_SECRET` to a long random value. For AD, set `AD_ENABLED=true` and the
   `AD_*` fields. For e-mail, set `SMTP_ENABLED=true` and the `SMTP_*` fields.

2. Start the stack:

   ```bash
   docker compose up --build
   ```

3. Open **`https://localhost`**.

   With no certificate in `proxy/certs/`, the proxy generates a self-signed one
   on first boot, so the browser will show a warning — expected for a local
   run, not acceptable in production. See
   [proxy/certs/README.md](proxy/certs/README.md) for installing a real
   certificate (including the Let's Encrypt flow).

   - Proxy health probe: `https://localhost/healthz`
   - API health probe: `https://localhost/api/health`
   - PostgreSQL: `postgres://projectview@127.0.0.1:5432/projectview` (bound to
     loopback for inspection)

   The backend and frontend containers are deliberately **not** published to
   the host — they are reachable only through the proxy.

4. **First sign-in.** An administrator account is created automatically the
   first time the stack starts against an empty database:

   | | Default | Variable |
   |---|---|---|
   | Username | `admin` | `BOOTSTRAP_ADMIN_USERNAME` |
   | Password | `ChangeMe123!` | `BOOTSTRAP_ADMIN_PASSWORD` |
   | E-mail | `admin@example.com` | `BOOTSTRAP_ADMIN_EMAIL` |
   | Name | `Administrator` | `BOOTSTRAP_ADMIN_NAME` |

   > ⚠️ **This password is published in this repository.** Change it before
   > anyone else can reach the installation. Two ways:
   >
   > - **Before the first start**, set `BOOTSTRAP_ADMIN_PASSWORD` in `.env`.
   >   It is only read against an empty database — on an installation that has
   >   already been seeded, the account exists and the variable is ignored.
   > - **After signing in**, from **Settings → Password**. The current password
   >   is required even for an administrator: holding the role is not the same
   >   as having proved you are the person at the keyboard. Every other session
   >   the account has open is ended, so a leaked password stops working
   >   immediately.

   Create the rest of the accounts from **Administration → Users**, which is
   also where you promote somebody to administrator. Keep at least two
   administrators: the last active one cannot be demoted or deactivated, which
   protects you from a lockout but also means a single administrator is a
   single point of failure for offboarding.

### Building behind a TLS-intercepting network

If the build fails with `x509: certificate signed by unknown authority` while
running `go mod download` or `npm ci`, something on the network is intercepting
TLS — a corporate inspection proxy, or an antivirus with HTTPS scanning. The
host trusts the interceptor's root certificate but the build containers do
not. Drop that root certificate into [`ca/`](ca/README.md) as a `.crt` file and
rebuild; both build stages pick it up automatically. No certificate is needed
on an ordinary network.

The same applies to any *ad-hoc* container that reaches the network — running
npm by hand to regenerate `package-lock.json`, for instance. Without the
certificate npm does not fail fast; it retries with backoff and appears to
hang. Pass the CA explicitly:

```bash
docker run --rm -v "$PWD/frontend:/app" -w /app \
  -v "$PWD/ca/your-root.crt:/tmp/ca.crt" \
  -e NODE_EXTRA_CA_CERTS=/tmp/ca.crt \
  node:20-alpine npm install --package-lock-only
```

## HTTPS and the edge proxy

All routing and hardening rules live in [proxy/nginx.conf](proxy/nginx.conf):

| Rule | Detail |
|---|---|
| TLS termination | TLS 1.2/1.3, HTTP/2, session cache; HTTP :80 redirects to HTTPS |
| Security headers | HSTS (1 year, `includeSubDomains`), `X-Content-Type-Options`, `X-Frame-Options`, `Referrer-Policy`, `Permissions-Policy` |
| Login rate limit | 10 requests/minute per IP on `/api/auth/login` (burst 5) — limits credential stuffing and avoids tripping AD lockout policies |
| API rate limit | 30 requests/second per IP on `/api/` (burst 60), plus 64 concurrent connections per IP |
| WebSocket | Upgrade handshake on `/ws` with a 1-hour read timeout |
| Forwarded headers | `X-Real-IP`, `X-Forwarded-For`, `X-Forwarded-Proto`, `X-Forwarded-Host` |
| Compression | gzip for text, JSON, JavaScript, CSS and SVG |

To use a real certificate, drop `fullchain.pem` and `privkey.pem` into
`proxy/certs/` (gitignored) and restart the proxy. `/.well-known/acme-challenge/`
stays reachable over plain HTTP for Let's Encrypt http-01 challenges.

## Attachments

Files live in an object store, never in PostgreSQL. A database whose backup is
dominated by screenshots is a database nobody restores promptly, and the row
would then have to be streamed through the API on every download.

Four properties are deliberate:

- **Uploads go through the API; downloads do not.** An upload is buffered by
  the backend, which is what makes the size ceiling, the type rules and the
  virus-scan hook enforceable — a presigned PUT handed to the browser would let
  a client write whatever it liked, whatever the form said. A download is a
  redirect to a **time-limited signed URL** (15 minutes by default), so a
  hundred people opening a video do not spend a hundred copies of it through
  the API's memory, and a link that leaks stops working on its own.
- **Deleting the row is not deleting the file.** The hard case is not the
  delete button, it is the cascade: removing a task, a project or a whole space
  takes the attachment rows with it in one statement the application never
  sees. So a database trigger queues every deleted row's storage key and a
  sweeper drains that queue against the store. Deleting an object is
  idempotent, so a retry — or a second replica — is harmless.
- **What a file is comes from its bytes.** The multipart `Content-Type` is
  whatever the client typed and the extension is whatever the uploader named
  it, so both are sniffed against the actual content. The extension only
  decides the cases sniffing cannot: every modern Office document is a ZIP
  archive on the wire. Those extensions are carried in the application's own
  table rather than looked up in the operating system's MIME database, so the
  decision is the same in the Alpine runtime image as on a developer's machine
  — the host's database is absent in the former and complete in the latter.
- **`skipped` is not `clean`.** The virus-scan seam is left as an interface
  with a default that records that *nothing examined the file*. Shipping
  something that returns "clean" without looking would be worse than having no
  scanner, because it hides the scanner's absence exactly where it matters. A
  file that is `pending` or `infected` gets no signed URL at all, and a scan
  that *fails* refuses the upload rather than storing something nothing will
  ever come back to look at.

Executable formats (`.exe`, `.msi`, `.js`, `.ps1`, …) are refused outright, and
only the final extension counts — `invoice.pdf.exe` is an executable. SVG
uploads fine but always downloads rather than rendering: an SVG is a document
that can carry script, and displaying one inline from the application's own
origin would hand an uploader a stored cross-site scripting vector.

Reading an attachment follows the task it belongs to, which any authenticated
user may read; attaching or removing one takes the permission to change the
task. Removing somebody else's file additionally requires the right to manage
the project — adding your own and dropping someone else's are not the same act.

Configure it with `STORAGE_*` and `ATTACHMENT_*` (see
[.env.example](.env.example)). Leaving `STORAGE_BUCKET` empty switches the
feature off: the endpoints answer 503 with an explanation, the upload controls
disappear, and nothing else changes.

> The proxy routes the bucket's path to the object store, so **changing
> `STORAGE_BUCKET` means changing the matching `location` block** in
> [proxy/nginx.conf](proxy/nginx.conf). That is the price of not proxying every
> download through the API. Note also that `STORAGE_PUBLIC_URL` must be the
> host the *browser* uses: the signature covers it, so signing for
> `minio:9000` and fetching from `localhost` fails verification — the usual
> cause of a download that answers `SignatureDoesNotMatch`.

## Hierarchy

Work is organised the way comparable tools do it:

```
Space ── Folder ── List (project) ── Task ── Sub-task
  └──────────────  List (project) ── Task
```

A **project is a List** that also carries scheduling metadata; it keeps its
name and its API throughout. Folders never nest, which keeps permission
resolution at a fixed depth. A List may hang directly off a Space.

Spaces can be public (visible to every authenticated user, the default for an
internal tool) or private (visible only to their members).

## Sessions

A login creates a server-side session. The JWT is a short-lived **access
token** carrying that session's id, and the middleware checks the session on
every request — so revoking it ends access immediately rather than whenever the
token happens to expire.

- **Refresh tokens** are rotated on every use, so a stolen one is good for at
  most a single exchange before the legitimate client invalidates it. Only a
  SHA-256 hash is stored.
- **Deactivating an account** or **an admin resetting a password** revokes
  every live session for that user. Changing your own password keeps the
  browser you did it from and signs out everywhere else.
- `GET /api/auth/sessions` lists where you are signed in;
  `DELETE /api/auth/sessions/{id}` signs one device out.
- **CSRF** protection applies only when the cookie alone authenticates the
  request (double-submit token in `X-CSRF-Token`). A `Bearer` header cannot be
  attached by a cross-site request, so those are not forgeable this way.
- Passwords are hashed with **Argon2id**. Existing bcrypt hashes keep working
  and are upgraded transparently the next time their owner signs in.

## Audit trail

Every mutation is recorded append-only in `audit_log`: who acted, what
changed (as a before/after diff), from which IP, under which request id.
Failed logins are recorded too, which is what an investigation actually needs.

Values for sensitive keys are redacted before they are written — the trail is
widely readable by design, so it must never become a place secrets accumulate.

`GET /api/audit` is admin-only and supports filtering by actor, resource,
action and time, with cursor pagination.

## Authorization model

Authentication proves *who* is calling; these rules decide *what* they may do.
They are enforced in [backend/internal/handlers/access.go](backend/internal/handlers/access.go),
where the target document is available, and gated by role in
[the router](backend/internal/router/router.go).

| Action | Who |
|---|---|
| Read or download an attachment | Any authenticated user (it follows the task) |
| Attach a file to a task | Project members (owner counts), or `admin` |
| Remove your own attachment | The uploader, with the right to change the task |
| Remove someone else's attachment | Project owner, a `manager` who is a member, or `admin` |
| Create accounts, assign roles, deactivate users | `admin` |
| Reset someone else's password | `admin` |
| Change your own password | You, **proving the current password** |
| Edit a user profile | The user themselves, or an `admin` |
| Create projects and teams | `admin`, `manager` |
| Create/edit/move/delete tasks, comment | Project members (owner counts), or `admin` |
| Rename, reconfigure or delete a project | Project owner, a `manager` who is a member, or `admin` |
| Delete a team | `admin` |
| Edit a team, add/remove its members | Team lead, or `admin` |
| Read or post in a chat channel | Channel members only |
| Read the audit trail | `admin` |
| Create spaces | `admin`, `manager` |
| Rename or archive a space | Space `admin` or `owner` |
| Delete a space | Space `owner`, or a global `admin` |
| Create folders and lists in a space | Space `member` and above |

Reads of projects, tasks and teams are open to any authenticated user: this is
an internal tool where work is meant to be visible across the organization.
Chat is the exception, since it carries private conversation, and private
spaces are invisible to non-members — a private space a caller holds no grant
on answers 404, so the error itself does not confirm it exists.

**Permissions are inherited.** A grant on a Space flows down to every Folder,
List and Task inside it: the effective permission is the strongest of the
global role, the space role, and direct membership on the project itself. The
pre-hierarchy model still applies, so nothing that worked before stopped
working.

The predicates are pure functions of the loaded documents, so the whole
role × resource × action matrix is unit-tested without a database
([access_test.go](backend/internal/handlers/access_test.go)), and the smoke
test re-proves each boundary against a live stack as an ordinary member.

## Active Directory login

Set in `.env`:

```
AD_ENABLED=true
AD_URL=ldap://dc.yourcompany.com:389
AD_BASE_DN=dc=yourcompany,dc=com
AD_DOMAIN=yourcompany.com
# Optional: service account used to look up the user's DN before binding
AD_BIND_DN=cn=svc-projectview,ou=service accounts,dc=yourcompany,dc=com
AD_BIND_PASSWORD=********
AD_USERNAME_ATTRIBUTE=sAMAccountName
```

On the first successful AD login a matching local user is created
automatically (just-in-time provisioning) with the default `member` role. An
administrator can promote them to `admin`/`manager` afterwards.

## Single sign-on, provisioning and privacy

Setup, backup/restore and the two privacy rights are covered in
[docs/OPERATIONS.md](docs/OPERATIONS.md). In brief:

- **OIDC single sign-on** works with any compliant provider; endpoints are
  discovered from the issuer. Auto-provisioning is **off** by default — with it
  on, anyone the identity provider will authenticate has an account here, which
  is a wider door than most organisations intend. Keep at least one local
  administrator: an installation whose only way in is an external provider is
  unreachable during that provider's outage.
- **SCIM 2.0** at `/scim/v2/Users`, authenticated by a service token rather
  than a user session. A SCIM delete **deactivates and revokes every live
  session immediately** — it never removes the row, because the person's tasks,
  comments and time entries belong to the organisation.
- **Data export** is self-service; **erasure** is an administrator's action,
  requires the username as confirmation, and anonymises rather than deletes.

## Configuration: environment, then the settings screen

The environment supplies the starting values. An administrator can then change
the integrations from **Administration → System settings**, which stores the
override in PostgreSQL and applies it to the running process at once — the next
login uses the new directory, the next notification the new mail server.

Three properties are deliberate:

- **An allow-list, not a deny-list.** Only AD, SMTP, OIDC, the alert lead time
  and the retention windows are editable. `DATABASE_URL`, `JWT_SECRET`, the
  bootstrap admin and the ports are not, and the server refuses them by name.
  None could be applied without a restart anyway, and an installation able to
  rewrite its own database connection from a web form is one compromised
  administrator away from being somebody else's.
- **Secrets go in encrypted and never come back out.** Bind passwords, the SMTP
  password and the client secret are sealed with a key derived from
  `JWT_SECRET`. The screen shows only whether a value is set; leaving the field
  blank keeps what is stored, so saving the form without retyping a password
  cannot wipe it.
- **The database is authoritative; the `.env` mirror is a copy.** Saving also
  writes a `.env`-shaped file (`SETTINGS_ENV_FILE`, mounted at `./config` by
  default) for backup and review, updating only the keys it manages and leaving
  your comments and unrelated variables untouched. The application does not read
  it back — compose interpolates its environment once, at parse time, so a file
  the application rewrote would need a restart to matter, which is the delay the
  screen exists to remove.

Clearing an override reverts the key to whatever the environment supplied,
rather than to empty.

## Main environment variables

See [.env.example](.env.example) for the full, commented list. Summary:

| Variable | Description |
|---|---|
| `DATABASE_URL` | PostgreSQL connection string |
| `DATABASE_MAX_CONNS` | Connection pool size |
| `JWT_SECRET` | Secret used to sign session tokens |
| `AD_ENABLED`, `AD_URL`, `AD_BASE_DN`, `AD_DOMAIN`, `AD_BIND_DN`, `AD_BIND_PASSWORD` | Active Directory login |
| `SMTP_ENABLED`, `SMTP_HOST`, `SMTP_PORT`, `SMTP_USER`, `SMTP_PASSWORD`, `SMTP_FROM` | Internal e-mail |
| `ALERT_CRON`, `ALERT_WARN_DAYS_BEFORE` | Deadline alert frequency and lead time |
| `BOOTSTRAP_ADMIN_*` | Admin account seeded on first run |
| `OIDC_ENABLED`, `OIDC_ISSUER_URL`, `OIDC_CLIENT_ID`, `OIDC_CLIENT_SECRET`, `OIDC_REDIRECT_URL`, `OIDC_AUTO_PROVISION` | Single sign-on |
| `AUDIT_RETENTION_DAYS`, `NOTIFICATION_RETENTION_DAYS`, `RETENTION_CRON` | Retention; both windows default to 0, which keeps everything |
| `STORAGE_ENDPOINT`, `STORAGE_PUBLIC_URL`, `STORAGE_BUCKET`, `STORAGE_ACCESS_KEY`, `STORAGE_SECRET_KEY`, `STORAGE_REGION`, `STORAGE_FORCE_PATH_STYLE`, `STORAGE_URL_TTL_MINUTES` | Attachment object storage; an empty bucket disables attachments |
| `ATTACHMENT_MAX_MB`, `ATTACHMENT_MAX_TASK_MB`, `ATTACHMENT_ALLOWED_TYPES` | Per-file and per-task ceilings, and an optional MIME allow-list |
| `SETTINGS_ENV_FILE` | Where the settings screen mirrors its `.env` copy; empty disables mirroring |
| `TLS_COMMON_NAME`, `PROXY_HTTP_PORT`, `PROXY_HTTPS_PORT` | Edge proxy and TLS |

## Local development (without Docker)

Backend (Go 1.25+):

```bash
cd backend
cp ../.env.example .env   # point DATABASE_URL at a local Postgres
go run ./cmd/server
```

Frontend (Node 20+):

```bash
cd frontend
npm install
npm run dev
```

Vite proxies `/api` and `/ws` to `http://localhost:4000` (override with
`VITE_API_PROXY_TARGET`). In this mode you talk to the backend directly over
plain HTTP, without the edge proxy — set `NODE_ENV=development` so session
cookies are not marked `secure`.

## Tests

**Backend unit tests** — pure logic, no database required:

```bash
cd backend
go test ./...
```

They cover the connection-string masking (so credentials never reach the
logs), the embedded migrations, JWT signing and the tokens
that must be rejected (wrong secret, expired, `alg=none`), the environment
parsing that decides whether AD and SMTP are enabled, and the HTTP helpers.

**End-to-end smoke test** — runs against a live stack and asserts the
behaviour unit tests cannot reach:

```bash
docker compose up -d --build
scripts/smoke-test.sh                 # defaults to https://localhost
```

316 assertions covering the proxy (HTTPS redirect, security headers, the
backend not being reachable from the host), authentication and session
revocation, the first-run schema and seed, the Space/Folder/List hierarchy,
projects/tasks/sub-tasks with resource allocation and dates, the kanban move
and its `completedAt` handling, search and pagination, the audit trail, the
dashboard aggregations, chat, the WebSocket upgrade, readiness, and the login
rate limit. It cleans up the fixtures it creates.

It also drives the product engine end to end: a dependency chain and its
critical path, a refused cycle, custom field values surviving a partial write,
the one-timer-per-person rule, and an automation that raises a task's priority
when its status changes — including the run log entry proving a non-matching
rule was recorded as skipped rather than silently ignored.

The collaboration surface is covered the same way: a threaded reply that stays
out of the channel transcript while raising the parent's reply count, a
reaction that toggles off when tapped twice, documents whose history keeps one
version per real change and none for an unchanged save, an old version read
back by id, and preference validation. The negative cases matter most here — a
member can read a document in an open space but cannot edit or delete it, and a
private space's documents answer 404 rather than 403, so the error itself does
not confirm they exist.

**Attachments** get the same treatment, and the assertion that matters is the
one only a live stack can make: the signed URL is followed all the way into the
object store and the file comes back. A signature is either byte-for-byte
correct or an opaque 403, so nothing short of fetching the object proves which.
Alongside it: the content type decided from the bytes rather than the name, the
storage key never leaving the server, an executable and a `.pdf.exe` both
refused, an oversized and an empty file refused, the same object answering 403
without its signature, a file attached to a comment staying with that comment,
and a removal that leaves no row behind.

**Frontend tests** — 48 assertions over the pure view logic (filtering,
sorting, grouping), the byte formatting and the accessibility contracts of the
primitives:

```bash
cd frontend
npm test
```

Among them are **authorization regression tests**: the script creates an
ordinary member account and proves it cannot take over another account, read a
channel it does not belong to, create spaces, projects or teams, read the audit
trail, or touch a project it is not a member of — while confirming it still can
do the things a member legitimately should. It also proves a revoked token
stops working immediately even though it is still cryptographically valid.

**Browser tests** — Playwright against the running stack, through the proxy:

```bash
docker compose up -d
e2e/run.sh                      # the whole suite
e2e/run.sh board.spec.ts        # one file
```

21 tests. They exist because of a specific history: three defects reached users
while every other job stayed green — a login screen that rendered nothing,
every label showing as its own translation key, and the status and priority
dropdowns doing nothing because Radix portalled the listbox behind the dialog
that opened it. Each was invisible to an API assertion and obvious in a
rendered page.

So the suite is shaped around that rather than around re-verifying the API:

| Guard | What it would have caught |
|---|---|
| Every route renders a heading, with no error boundary | A page that answers 200, mounts cleanly and paints nothing |
| No visible text matches the *shape* of a translation key | The whole class of i18n resolution failures, not one string |
| The status dropdown is opened, its option clicked where it is painted, and the change survives a reload | A control that is on the page and unreachable |
| A card is dragged between columns with real pointer movement | dnd-kit's sensor never firing |
| Sign-in, project creation, language switching, the settings screens | The flows the backlog named |

Two properties worth knowing. It authenticates **once** and shares the session,
because the login endpoint is rate-limited to ten attempts a minute and a suite
that signed in per test would start measuring the limiter. And the guards were
verified by reintroducing the defects — a listbox forced behind the dialog, a
leaked key injected into the DOM — and confirming the tests go red, because a
browser suite that would have stayed green through the original bugs is worse
than none.

**Load test** — k6 against the running stack with a project of 10,000 tasks:

```bash
docker compose up -d
scripts/loadtest/run.sh              # seeds the fixture, then measures
scripts/loadtest/run.sh --clean      # removes the fixture afterwards
```

The fixture is applied as SQL rather than through the API, deliberately:
performance at size is a statement about *reads*, so building ten thousand
tasks is a precondition rather than part of the measurement, and a benchmark
too slow to repeat is one nobody runs twice.

It reports p95 per endpoint rather than one aggregate, because a fast search
would otherwise hide a slow board inside the same number. Two profiles run:
what ships today, and the same page fetched a column at a time through the
paginated endpoint — so the recommendation in the backlog rests on a measured
number rather than an argument. **The results, including the one it fails, are
in [ROADMAP.md](ROADMAP.md#m3--the-load-test-and-what-it-found).**

## Known limits at scale

Measured rather than guessed, and named here rather than left for somebody to
discover:

- **A single very large project will feel the board.**
  `GET /api/projects/:id/tasks` returns every task in the project in one
  response — measured at 10,000 tasks: 10.1 MB, 1.6 s on its own and about
  nine seconds under concurrent use. Cost is linear in the task count, so a
  project of a thousand costs about a tenth of that; the point where it stops
  being comfortable is somewhere in between, and that part is arithmetic rather
  than measurement. The fix is scoped as [BACKLOG 2.2](BACKLOG.md); the
  paginated endpoint it needs already exists and answers a kanban column in
  under 130 ms.
- **Search, by contrast, holds.** Full-text over 10,000 tasks answers a page of
  50 in 49 ms, which is what the `tsvector` column and its GIN index were built
  for.
- **One backend replica.** The WebSocket hub is per-process and the schedulers
  hold no lock; both are explained in
  [docs/OPERATIONS.md](docs/OPERATIONS.md#running-more-than-one-instance).

## CI/CD

[`.github/workflows/ci.yml`](.github/workflows/ci.yml) runs on every push and
pull request:

| Job | What it does |
|---|---|
| `backend` | `gofmt` check, build, `go vet`, `go test -race` with coverage |
| `gencert` | Builds the certificate helper and verifies the certificate it generates (SANs, validity, key) with `openssl` |
| `frontend` | `npm ci`, TypeScript type-check, production build |
| `shellcheck` | Lints the shell scripts |
| `integration` → *browser tests* | Playwright against the composed stack, so the pipeline can finally see a screen |
| `integration` | Brings the full stack up with Docker Compose, waits for every container to report healthy, runs the smoke test, asserts the tables, indexes and foreign keys were created in PostgreSQL, and restarts the backend to prove migrations are idempotent |

[`.github/workflows/release.yml`](.github/workflows/release.yml) publishes the
`backend`, `frontend` and `proxy` images to the GitHub Container Registry —
after CI passes on `main`, and on `v*` tags. It never publishes an image built
from a commit whose CI failed.

## Migrating from the MongoDB version

Earlier releases stored data in MongoDB. [`cmd/migrate`](backend/cmd/migrate)
copies an existing dataset into PostgreSQL:

```bash
cd backend
go run ./cmd/migrate \
  -mongo    "mongodb://localhost:27017/pm_dashboard" \
  -postgres "postgres://projectview:projectview@localhost:5432/projectview?sslmode=disable"
```

ObjectIDs are mapped onto UUIDs deterministically, so references between
documents survive the copy, and every insert is `ON CONFLICT DO NOTHING` —
the tool is safe to re-run.

## Data model (summary)

- **User** — local or AD-sourced account, with a role (`admin`/`manager`/`member`).
- **Team** — a team with members and a lead.
- **Project** — a project with configurable status columns (used by the
  kanban board) and members.
- **Task** — a task or sub-task (via `parentTask`), with multiple `assignees`
  (resource allocation), start/due dates, priority, checklist, comments and a
  log of alerts already sent.
- **ChatChannel / ChatMessage** — team/project channels and DMs, with message
  history. A message may have a `parentId`, which makes it a reply; a database
  trigger keeps threads one level deep.
- **ChatReaction / ChatMention** — emoji reactions (unique per message, user
  and emoji, so toggling cannot duplicate) and the record of who was named.
- **Doc / DocRevision** — Markdown documents scoped to a space or a project,
  with a snapshot kept for every save that changed the text.
- **Attachment** — metadata for a file on a task or a comment: the name as
  uploaded, the content type decided from the bytes, the size, a SHA-256 and
  the scan status. The storage key is the object's address and never leaves the
  server. A companion table holds keys whose objects still have to be deleted,
  filled by a trigger so a cascade cannot orphan a file.
- **NotificationPreference** — per-user delivery choices by notification type
  and channel, quiet hours, and digest cadence.
- **Notification** — in-app notifications (assignments, deadlines, comments),
  delivered in real time over WebSocket.
- **Goal / KeyResult** — an objective and the measures against it. A measure is
  manual or derived from a project's tasks; a derived one refuses a hand-typed
  value, because the next read would overwrite it.
- **ProjectBaseline** — the plan as approved, snapshotted as JSONB. Earned
  value compares today against it; reading the plan from the live rows would
  compare the schedule against itself and report perfection forever.
- **Dashboard** — one saved card layout per person, with a partial unique index
  enforcing a single default.
- **AppSetting** — one row per configuration override, with who changed it and
  when. Secrets are stored sealed rather than in the clear.
- **ServiceToken** — machine credentials for SCIM and reporting. Only the hash
  is stored: a token the database can hand back is a token every database
  administrator has silently been issued.

## Security notes / suggested next steps

- Change `JWT_SECRET` and the default admin password (`admin` /
  `ChangeMe123!`, both published here) before any real use. The
  [first-run checklist](docs/OPERATIONS.md#first-run) is the ordered version.
- Install a real TLS certificate in `proxy/certs/` — the self-signed fallback
  is for local runs only.
- Change `POSTGRES_PASSWORD` and use a `DATABASE_URL` with strong credentials
  and `sslmode=require` in production.
- Narrow `CORS_ORIGIN` to your domain if you expose the API to external
  clients; traffic from the SPA itself is same-origin behind the proxy.
