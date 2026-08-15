# Roadmap

Where the project stands and what is left. Updated as each phase lands.

**Legend:** ✅ done and verified · 🚧 in progress · ⬜ not started

---

## Progress

```
Phase 0  Security containment        ████████████████████  100%   ✅
Phase A1 PostgreSQL foundation       ████████████████████  100%   ✅
Phase A2 Domain, security, ops       ████████████████░░░░   80%   ✅
Phase A3 Product engine              ░░░░░░░░░░░░░░░░░░░░    0%   ⬜
Phase B1 UI foundation               ████████████████████  100%   ✅
Phase B2 Views                       ████████████████░░░░   80%   ✅
Phase B3 Collaboration               ░░░░░░░░░░░░░░░░░░░░    0%   ⬜
Phase C  Intelligence & enterprise   ░░░░░░░░░░░░░░░░░░░░    0%   ⬜
                                     ─────────────────────
                              overall  ~5 of 8 phases
```

The product is now usable as a project management tool rather than a kanban
board: the same tasks can be read as a list, a table, a calendar, a Gantt
timeline or a capacity grid. What remains is depth — custom fields,
dependencies, automations — and the collaboration surface.

---

## ✅ Phase 0 — Security containment

Five privilege escalations closed. The worst was found during implementation,
not during the audit: `POST /api/auth/register` was reachable **without
authentication** and honoured a caller-supplied `role`, so anyone who could
reach the API could create themselves an administrator.

| Endpoint | Was | Now |
|---|---|---|
| `POST /api/auth/register` | Anonymous, accepted `role: admin` | `RequireAuth` + `RequireRole(admin)`, role validated |
| `POST /api/users/:id/password` | Anyone could change anyone's password | Self needs current password; admin may reset; else 403 |
| `PUT /api/users/:id` | Anyone could edit any profile | Self or admin only |
| `GET /api/chat/.../messages` | Any user could read any DM | Membership enforced in the query |
| `PUT /api/projects/:id`, `PUT /api/teams/:id` | Raw map into `$set` | Explicit allow-list with validation |

`auth.RequireRole` existed but was wired to nothing; per-resource rules now
live in [`handlers/access.go`](backend/internal/handlers/access.go).

**Verified:** 9 unit tests over the role × resource × action matrix, plus 21
end-to-end assertions that create a real member account and prove each
boundary against a running stack.

## ✅ Phase A1 — PostgreSQL foundation

MongoDB replaced by PostgreSQL 16. What the document model kept in arrays on
the parent document became real tables with foreign keys.

- **15 tables**, cascading deletes, `CHECK` constraints mirroring the domain
  enums, and indexes covering the board, the inbox and the assignee lookups —
  [`0001_init.sql`](backend/internal/db/migrations/0001_init.sql).
- **Migrations embedded in the binary**, applied on boot, tracked in
  `schema_migrations`, each inside its own transaction. Hand-rolled rather than
  framework-driven: the alternatives drag in ClickHouse, SQL Server and SQLite
  drivers to run a few `CREATE TABLE`s.
- **Repository layer** ([`internal/repo`](backend/internal/repo)) holding every
  SQL statement; handlers deal only in domain types.
- **N+1 queries eliminated.** Populating a board issued a users query and a
  count query *per task* — a 200-card board cost 400+ round trips. It now costs
  three, regardless of size.
- **Real transactions.** Deleting a project was three unordered deletes that
  could leave orphans; it is now one statement with `ON DELETE CASCADE`.
- **Alert de-duplication moved into the schema**: `task_alerts_sent` has
  `(task, user, alert_type)` as its primary key, so the nested array scan in Go
  became a `NOT EXISTS` clause.
- **[`cmd/migrate`](backend/cmd/migrate)** copies an existing Mongo dataset in,
  mapping ObjectIDs onto UUIDs deterministically so cross-references survive.

**Verified:** the 75-assertion smoke test passes **unchanged** — the strongest
available evidence that the JSON contract survived the storage swap. Go
toolchain moved to 1.25 (required by pgx).

> ⚠️ This breaks the original brief's "store in MongoDB, configurable via env".
> A deliberate call, made after the trade-off was put on the table. `MONGO_URI`
> became `DATABASE_URL`, equally configurable.

## ✅ Phase A2 — Domain, security and operations

**Hierarchy.** `Space → Folder → List → Task`, with a List able to hang
directly off a Space. A project *is* a List and keeps its name and API. A
database trigger enforces that a folder and its list belong to the same space —
the kind of invariant application code forgets. Existing projects were adopted
into a default Space by the migration, so nothing broke.

**Hierarchical RBAC.** A grant on a Space (`owner`/`admin`/`member`/`guest`)
flows down to everything inside it; the effective permission is the strongest
of the global role, the space role and direct project membership. Private
spaces answer 404 rather than 403 to non-members, so the error does not confirm
they exist.

**Sessions with real revocation.** The JWT used to *be* the session, valid
until expiry with no way to cut it short — deactivating an account left its
token working. Logins now create a server-side session, the token carries its
id, and the middleware checks it on every request. Refresh tokens rotate on
every use and are stored only as hashes. Password resets and deactivations
revoke live sessions.

**Argon2id**, with bcrypt hashes still accepted and upgraded transparently at
next login — no lockout, no mass reset.

**CSRF** on cookie-authenticated state changes only, since a `Bearer` header
cannot be attached cross-site.

**Append-only audit trail.** Who acted, what changed as a before/after diff,
from which IP, under which request id — failed logins included. Sensitive keys
are redacted before they are written, because the trail is widely readable by
design.

**Search and pagination.** Listings used to return the whole table. Now
cursor-paginated (constant cost at any depth, stable under concurrent writes)
with PostgreSQL full-text search over a generated `tsvector` column and a GIN
index. Sort fields come from an allow-list, so the ORDER BY clause cannot be
steered by a caller.

**Observability.** Structured JSON logging via `slog` with request correlation,
RED metrics on `/metrics` labelled by route pattern (not path, which would
explode cardinality), and `/api/ready` as a real readiness probe distinct from
liveness.

**Fixed along the way:** nginx resolved the backend's hostname once at startup
and cached it forever, so redeploying the backend 502'd every request until the
proxy was also restarted. Upstreams are now resolved per request through
Docker's DNS. Verified by recreating the backend alone and watching traffic
keep flowing.

**Verified:** the smoke test grew from 75 to **107 assertions**, including
proof that a revoked token stops working while still cryptographically valid.

**Deferred from A2, with reason:** generated OpenAPI + typed TS client, and the
transactional outbox. Both are most valuable once A3 introduces webhooks and
automations that need them; building them now would be speculative.

## ✅ Phase B1 — UI foundation

Every inline style is gone. The frontend went from one CSS file and
`useEffect`+`fetch` on each page to a real architecture.

**Design system.** Two token layers — a primitive palette, then semantic tokens
that components actually use — so re-theming touches one file instead of every
component. Light, dark and **system** themes, the last setting no attribute at
all so the OS keeps deciding as the day goes on.

**Data layer.** TanStack Query replaces per-page fetching: shared cache, request
deduplication, retry that skips 4xx (a definitive answer is not worth
repeating), and optimistic kanban moves that roll back if the server refuses.

**Silent session refresh.** A 401 now exchanges the refresh cookie for a new
token and replays the request. Concurrent 401s share one in-flight refresh —
without that, six parallel requests would trigger six refreshes and, because
refresh tokens rotate on use, five would present a consumed token and sign the
user out.

**Accessibility.** Radix primitives supply focus trapping, roving focus and
keyboard behaviour. On top: a skip link, landmarks, `:focus-visible` rings that
appear for keyboards but not clicks, honoured `prefers-reduced-motion`, labels
wired to controls with `aria-describedby`, and errors in `role="alert"` so they
are announced rather than merely shown.

**i18n.** pt-BR and en, browser-detected, with `<html lang>` kept in sync.

**Command palette.** `Ctrl/Cmd+K`, searching tasks through the server's
full-text index rather than filtering an already-loaded list — the difference
between a search and a filter.

**A2's work made visible.** Spaces now have a screen showing the hierarchy and
your role on it; Settings lists active sessions and lets you end one. Both were
capabilities that existed only in the API.

**Verified:** 18 frontend tests (Vitest + Testing Library) covering the
formatting rules and the accessibility contracts of the primitives, plus a
clean type-check and production build. The 107-assertion backend smoke test
still passes against the rebuilt stack.

**Two real bugs found by the tests:** the avatar fallback rendered blank for a
frame because a `delayMs` of zero still schedules a timer, and `Card` was typed
to `HTMLDivElement` while being rendered as `li`.

**Deferred:** Playwright browser tests. They belong with B2, when there are
views worth driving end to end; today they would mostly re-test what the smoke
test already covers.

## ✅ Phase B2 — Views

Six ways to read the same tasks, sharing one filter/group/sort model so a
filter means the same thing everywhere.

| View | What it is for |
|---|---|
| **Board** | Kanban, drag between columns (existing) |
| **List** | Grouped by status, assignee or priority; virtualised, so ten thousand rows scroll |
| **Table** | Spreadsheet-style inline editing of title, status, priority and due date |
| **Calendar** | Month grid of tasks on their due dates |
| **Timeline** | Gantt bars from start to due date, dragged to reschedule; zero-duration tasks render as milestone diamonds |
| **Workload** | Capacity against allocation, per person, per week |

**Shared model.** Filtering, grouping and sorting live in one pure module,
which is what keeps the views honest with each other — and made them
straightforward to test without a browser.

**Decisions worth naming:**
- Tasks with no due date sort **last** in both directions. Treating "no date"
  as infinitely early buries everything that does have one.
- A task assigned to two people appears under **both** in the grouped list.
  Listing it only under the first would hide shared work from everyone else.
- The workload view spreads a task's estimate across the days it spans instead
  of charging it to the due date; eighty hours landing on one Friday would show
  a spike that does not exist and hide the six weeks of load that do. Shared
  tasks are split between assignees rather than double-counted, and unestimated
  tasks contribute nothing — a default would make the view fiction.
- Filters are deliberately **not** persisted across reloads, while the chosen
  view is. Reopening a board to a filtered subset with no visible reason looks
  like missing data.
- Inline edits commit on blur or Enter, not per keystroke; Escape abandons.

**Verified:** frontend tests grew from 18 to **33**, covering filter
combination semantics (OR within a facet, AND across facets), the sort rules
above, and the grouping behaviour. Type-check, production build and the
107-assertion backend smoke test all pass.

**Deferred to A3, with reason:** task **dependencies** and the **critical
path** on the timeline. Both need backend modelling that does not exist yet —
drawing dependency arrows over data the server cannot express would be a mock,
not a feature.

---

## What is left

### ⬜ Phase A3 — Product engine
Custom fields · task dependencies and critical path · time tracking ·
recurring tasks · templates · attachments · watchers and mentions ·
automation engine (trigger → condition → action) · signed webhooks · public API.

### ⬜ Phase B3 — Collaboration
Rich-text editor and Docs · chat threads, reactions, mentions, attachments,
presence, typing indicators (needs a bidirectional WebSocket; today it is
push-only) · intake forms · notification digests and granular preferences.

### ⬜ Phase C — Intelligence and enterprise
Goals/OKRs · user-configurable dashboards · portfolio · capacity planning ·
baselines and earned value · exports · SAML/OIDC SSO · SCIM · retention ·
LGPD export and erasure · backup runbooks · HA.

---

## Milestones

| | Criterion | Status |
|---|---|---|
| M0 | Smoke test proves a member cannot escalate privilege; CI green | ✅ |
| M1 | Stack runs on Postgres; smoke test passes unchanged | ✅ |
| M2 | Audit log covers mutations; RBAC matrix tested; sessions revocable | ✅ |
| M3 | A 10k-task board renders under 100 ms p95 under load | ⬜ |
| M4 | Gantt reorders dependencies and recomputes the critical path | 🚧 timeline ships; dependencies need A3 |
| M5 | "Task overdue → notify assignee and change status" runs end to end | ⬜ |

## Effort

Phases 0, A1, A2, B1 and B2 are done. The remaining three are roughly **14
person-weeks**, compressible to about 9–10 calendar weeks with the platform
and product tracks running in parallel.

**A3 is now the highest-value next step**, and it has become the bottleneck:
the timeline is built but cannot draw dependencies, custom fields have nowhere
to live, and there is no automation engine — all of which are backend work
that the views are already waiting on.
