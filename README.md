# ProjectView — Project Management Dashboard

[![CI](https://github.com/thiagomontozo/projectview/actions/workflows/ci.yml/badge.svg)](https://github.com/thiagomontozo/projectview/actions/workflows/ci.yml)

A web application for managing projects, teams and tasks, in the spirit of
ClickUp: a draggable card dashboard and kanban boards, resource allocation,
per-person deadline alerts, an internal chat, tracking charts, and login
integrated with Active Directory. Fully containerized and served over HTTPS.

**Stack:** **Go** backend, **React + TypeScript** frontend, MongoDB, behind an
**nginx** edge proxy.

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
  messages, with history stored in MongoDB. Messages are sent over REST and
  delivered in real time over WebSocket.
- **Tracking charts** — task distribution by status, workload per resource,
  30-day completion trend, and progress per project.
- **Dashboard with draggable cards** — the landing dashboard is a grid of
  cards (KPIs + charts) that can be rearranged by drag-and-drop, with the
  chosen order persisted per user in the browser. On top of that, every
  project has a kanban board whose task cards drag between configurable
  columns (`@dnd-kit`).
- **MongoDB configurable via environment variable** (`MONGO_URI`), with the
  **schema (collections + indexes) created automatically on first run**, plus
  an admin user, a sample team and a sample project seeded into an empty
  database.
- **HTTPS by default** — an nginx edge proxy terminates TLS, applies the
  security headers and rate limits, and is the only container published to the
  host.
- **Fully containerized** with Docker Compose (proxy, frontend, backend,
  MongoDB).

## Architecture

```
Internet
   │  HTTPS :443  (HTTP :80 redirects)
   ▼
┌─────────┐   /api, /ws   ┌─────────┐        ┌───────┐
│  proxy  │──────────────▶│ backend │───────▶│ mongo │
│ (nginx) │               │  (Go)   │        └───────┘
└────┬────┘               └─────────┘
     │  everything else
     ▼
┌──────────┐
│ frontend │  React SPA (nginx, static)
└──────────┘
```

```
.
├── proxy/       TLS termination + routing/security rules (nginx)
├── backend/     REST API + WebSocket in Go (chi, mongo-driver, go-ldap, JWT, cron)
├── frontend/    React + TypeScript SPA (Vite) + Recharts + @dnd-kit
└── ca/          Optional extra root CAs for the build (see below)
```

- **Proxy (nginx)** — the only service bound to a host port. Terminates TLS,
  redirects HTTP to HTTPS, sets HSTS and the other security headers, rate
  limits the API (and the login endpoint in particular, since every attempt
  reaches Active Directory), and routes `/api` and `/ws` to the backend and
  everything else to the SPA. Because TLS ends here, the backend runs with
  `NODE_ENV=production` and its session cookies are marked `secure`.
- **Backend (Go)** — `net/http` with the [chi](https://github.com/go-chi/chi)
  router, [mongo-driver](https://github.com/mongodb/mongo-go-driver) for
  MongoDB, JWT authentication (httpOnly cookie or Bearer token),
  [go-ldap](https://github.com/go-ldap/ldap) for AD, `net/smtp` for e-mail,
  [robfig/cron](https://github.com/robfig/cron) for deadline alerts, and a
  small WebSocket hub ([gorilla/websocket](https://github.com/gorilla/websocket))
  for chat and real-time notifications.
- **Frontend (React + TypeScript)** — Vite, React Router, Recharts for the
  charts, `@dnd-kit` for the draggable dashboard and kanban board, and a
  native WebSocket client (no extra library) for incoming messages and
  notifications — all writes go through the REST API.
- **Database** — MongoDB. The address is fully configurable through
  `MONGO_URI` and can point at the bundled container or any other MongoDB
  (Atlas, on-prem, replica set, …). On first run the backend creates every
  collection and index it needs.

### About the real-time protocol

Unlike a Socket.IO-style design, the WebSocket here (`/ws?token=<jwt>`) exists
**only for the server to push events** (`notification` and `chat:message`) to
already-connected clients. Every write — creating a task, sending a message,
moving a kanban card — happens over an ordinary REST call; the server then
fans the corresponding event out over the WebSocket to whoever has a tab open.
This keeps the protocol trivial to implement in Go and easy to exercise with
`curl`/Postman, without giving up real-time updates.

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
   - MongoDB: `mongodb://127.0.0.1:27017` (bound to loopback for inspection)

   The backend and frontend containers are deliberately **not** published to
   the host — they are reachable only through the proxy.

4. First login: an admin account is created automatically on first run, using
   the credentials from `BOOTSTRAP_ADMIN_USERNAME` / `BOOTSTRAP_ADMIN_PASSWORD`
   (default: `admin` / `ChangeMe123!`). **Change this password immediately in
   production** (endpoint `POST /api/users/:id/password`).

### Building behind a TLS-intercepting network

If the build fails with `x509: certificate signed by unknown authority` while
running `go mod download` or `npm ci`, something on the network is intercepting
TLS — a corporate inspection proxy, or an antivirus with HTTPS scanning. The
host trusts the interceptor's root certificate but the build containers do
not. Drop that root certificate into [`ca/`](ca/README.md) as a `.crt` file and
rebuild; both build stages pick it up automatically. No certificate is needed
on an ordinary network.

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

## Authorization model

Authentication proves *who* is calling; these rules decide *what* they may do.
They are enforced in [backend/internal/handlers/access.go](backend/internal/handlers/access.go),
where the target document is available, and gated by role in
[the router](backend/internal/router/router.go).

| Action | Who |
|---|---|
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

Reads of projects, tasks and teams are open to any authenticated user: this is
an internal tool where work is meant to be visible across the organization.
Chat is the exception, since it carries private conversation.

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

## Main environment variables

See [.env.example](.env.example) for the full, commented list. Summary:

| Variable | Description |
|---|---|
| `MONGO_URI` | MongoDB connection string (database name is taken from its path) |
| `MONGO_DB_NAME` | Optional override for the database name |
| `JWT_SECRET` | Secret used to sign session tokens |
| `AD_ENABLED`, `AD_URL`, `AD_BASE_DN`, `AD_DOMAIN`, `AD_BIND_DN`, `AD_BIND_PASSWORD` | Active Directory login |
| `SMTP_ENABLED`, `SMTP_HOST`, `SMTP_PORT`, `SMTP_USER`, `SMTP_PASSWORD`, `SMTP_FROM` | Internal e-mail |
| `ALERT_CRON`, `ALERT_WARN_DAYS_BEFORE` | Deadline alert frequency and lead time |
| `BOOTSTRAP_ADMIN_*` | Admin account seeded on first run |
| `TLS_COMMON_NAME`, `PROXY_HTTP_PORT`, `PROXY_HTTPS_PORT` | Edge proxy and TLS |

## Local development (without Docker)

Backend (Go 1.24+):

```bash
cd backend
cp ../.env.example .env   # point MONGO_URI at a local Mongo, e.g. mongodb://localhost:27017/pm_dashboard
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

They cover the MongoDB URI parsing (so `MONGO_URI` really selects the
database, and credentials never reach the logs), JWT signing and the tokens
that must be rejected (wrong secret, expired, `alg=none`), the environment
parsing that decides whether AD and SMTP are enabled, and the HTTP helpers.

**End-to-end smoke test** — runs against a live stack and asserts the
behaviour unit tests cannot reach:

```bash
docker compose up -d --build
scripts/smoke-test.sh                 # defaults to https://localhost
```

75 assertions covering the proxy (HTTPS redirect, security headers, the
backend not being reachable from the host), authentication, the first-run
schema and seed, projects/tasks/sub-tasks with resource allocation and dates,
the kanban move and its `completedAt` handling, the dashboard aggregations,
chat, the WebSocket upgrade, and the login rate limit. It cleans up the
fixtures it creates.

21 of those assertions are **authorization regression tests**: the script
creates an ordinary member account and proves it cannot take over another
account, read a channel it does not belong to, create projects or teams, or
touch a project it is not a member of — while confirming it still can do the
things a member legitimately should.

## CI/CD

[`.github/workflows/ci.yml`](.github/workflows/ci.yml) runs on every push and
pull request:

| Job | What it does |
|---|---|
| `backend` | `gofmt` check, build, `go vet`, `go test -race` with coverage |
| `gencert` | Builds the certificate helper and verifies the certificate it generates (SANs, validity, key) with `openssl` |
| `frontend` | `npm ci`, TypeScript type-check, production build |
| `shellcheck` | Lints the shell scripts |
| `integration` | Brings the full stack up with Docker Compose, waits for every container to report healthy, runs the smoke test, and asserts the collections and indexes were created in MongoDB |

[`.github/workflows/release.yml`](.github/workflows/release.yml) publishes the
`backend`, `frontend` and `proxy` images to the GitHub Container Registry —
after CI passes on `main`, and on `v*` tags. It never publishes an image built
from a commit whose CI failed.

## Data model (summary)

- **User** — local or AD-sourced account, with a role (`admin`/`manager`/`member`).
- **Team** — a team with members and a lead.
- **Project** — a project with configurable status columns (used by the
  kanban board) and members.
- **Task** — a task or sub-task (via `parentTask`), with multiple `assignees`
  (resource allocation), start/due dates, priority, checklist, comments and a
  log of alerts already sent.
- **ChatChannel / ChatMessage** — team/project channels and DMs, with message
  history.
- **Notification** — in-app notifications (assignments, deadlines, comments),
  delivered in real time over WebSocket.

## Security notes / suggested next steps

- Change `JWT_SECRET` and the default admin password before any real use.
- Install a real TLS certificate in `proxy/certs/` — the self-signed fallback
  is for local runs only.
- Set `MONGO_ROOT_USERNAME`/`MONGO_ROOT_PASSWORD` and use a `MONGO_URI` with
  credentials in production.
- Narrow `CORS_ORIGIN` to your domain if you expose the API to external
  clients; traffic from the SPA itself is same-origin behind the proxy.
