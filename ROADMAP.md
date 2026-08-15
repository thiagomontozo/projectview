# Roadmap

Where the project stands and what is left. Updated as each phase lands.

**Legend:** ✅ done and verified · 🚧 in progress · ⬜ not started

---

## Progress

```
Phase 0  Security containment        ████████████████████  100%   ✅
Phase A1 PostgreSQL foundation       ████████████████████  100%   ✅
Phase A2 Domain, security, ops       ████████████████░░░░   80%   ✅
Phase A3 Product engine              ████████████████░░░░   80%   ✅
Phase B1 UI foundation               ████████████████████  100%   ✅
Phase B2 Views                       ████████████████░░░░   80%   ✅
Phase B3 Collaboration               ████████████████░░░░   80%   ✅
Phase C  Intelligence & enterprise   ░░░░░░░░░░░░░░░░░░░░    0%   ⬜
                                     ─────────────────────
                              overall  ~7 of 8 phases
```

Schedules have real dependencies and a critical path, tasks carry typed
custom fields and tracked time, rules act on changes without anyone asking,
and the conversation around the work — threads, reactions, mentions,
documents, presence — happens in the product. What remains is the enterprise
layer.

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

## ✅ Phase A3 — Product engine

The phase the views were waiting on.

**Dependencies and the critical path.** A task can wait on another, and the
timeline draws the arrows. Cycles are refused by a database trigger rather than
by convention: a cycle makes the schedule unsolvable and the longest-path walk
non-terminating, so it must never reach storage. The critical path is the
heaviest chain by duration — the sequence where any slip moves the end date —
computed by a memoised walk in Go rather than SQL, because the recursion has to
carry accumulated weight and revisit nodes reachable by several routes.

**Custom fields.** Typed per project or space, stored as JSONB with a GIN
index. Definitions are relational, values are not: a row per value turns every
task read into a join and a pivot. Writes merge rather than replace, so a
client that knows about three fields cannot erase a fourth it has never heard
of.

**Time tracking.** A timer, manual entries, and tracked against estimated. One
running timer per person is a partial unique index, not a check someone has to
remember — a second concurrent timer would silently double-count the same hour.

**Watchers.** Following a task without being responsible for it. Assignees are
notified because it is their work; watchers because they asked.

**Automations.** `trigger → condition → action`, with a deliberately small
closed set of actions: a rule engine that runs arbitrary expressions is a
scripting environment without any of the safeguards of one. Rules run
synchronously after the change but never fail the request that triggered them.
Every evaluation is recorded — including the skips, with the reason — because a
rule that silently does not fire is otherwise undebuggable. A status-changing
rule triggered by status changes is guarded against looping on itself.

**Verified:** Go test packages grew from 6 to 8, covering the critical-path
walk (weighting, diamonds, unknown blockers, projects with no dependencies) and
the condition evaluator (including that an unimplemented operator fails
closed). The smoke test grew from 107 to **138 assertions**, driving an
automation end to end.

**One bug the smoke test caught:** custom field values were written but never
read back — the column was missing from the task SELECT, so the API accepted a
write it could not return.

**Deferred, with reason:** recurring tasks, templates, attachments (needs
object storage, which is infrastructure rather than product), signed webhooks
and public API tokens. Webhooks in particular need retry, backoff and a
delivery log to be worth shipping — a phase of their own, not a corner of this
one.

## ✅ Phase B3 — Collaboration

The work had a record; the conversation around it did not.

**A bidirectional socket.** The WebSocket was push-only: the server talked, the
client listened. It now carries *ephemeral* state in both directions — presence
and typing — while everything that persists still goes through REST, where
validation, authorization and the audit trail live. Presence changes only on
the first and last connection, because a second tab does not make someone twice
as online. Typing entries carry a timestamp rather than a flag and expire on
their own: a client that closes its laptop mid-word never sends the stop frame.
Keep-alive frames are swallowed rather than rebroadcast, so one person typing
does not become a flood.

**Threads and reactions.** A reply belongs to its thread, not to the channel,
so a threaded exchange appears once in the transcript with a reply count rather
than twice in full. Replies cannot nest — a trigger enforces it, not a
convention — because a tree of replies is a different product with different
navigation. Reactions toggle: tapping the same emoji twice removes it instead
of stacking duplicates.

**Mentions.** `@name` is resolved against real usernames, and the pattern
deliberately refuses to fire inside an e-mail address. Naming someone who
cannot read the channel notifies nobody: the notification itself would confirm
the conversation exists.

**Docs.** Markdown, not a rich-text document model — the content stays
greppable, diffable and portable, and the editor is a detail of one screen
rather than a format the database has to understand. Every save keeps the
previous version, and an unchanged save keeps none, so history holds edits
rather than noise. Old versions can be read back and restored, because a
history nobody can open is decorative. A document carries no access list of its
own: it is exactly as visible, and exactly as editable, as the space or project
containing it, so a permission is granted in one place and revoked in one
place.

**Notification preferences and digests.** Per type, per channel, with quiet
hours and a daily or weekly digest. Turning immediate e-mail off does not mean
going blind: what was held back arrives as one message. Quiet hours wrap
midnight correctly, and the digest is marked sent only after the send succeeds.

**Verified:** a new `ws` test package drives real WebSocket connections through
an `httptest` server and proves presence announces only transitions, keep-alive
typing frames are not rebroadcast, a disconnect clears the typing entry, and an
unparseable frame does not drop the connection. The smoke test grew from 138 to
**172 assertions**, covering threads, reactions, mentions, presence, documents
with their history, and preference validation.

**Two gaps the assertions found:** documents had no authorization at all — any
authenticated user could read any document, including one in a private space —
and revisions were stored without any way to read their text back. Both are
closed and both now have negative tests.

**Deferred, with reason:** a rich-text editor (TipTap), attachments and intake
forms. Attachments need object storage, which is infrastructure rather than
product; a rich-text model would replace a portable format with a proprietary
one for a gain the Markdown editor already delivers.

---

## What is left

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
| M4 | Gantt reorders dependencies and recomputes the critical path | ✅ |
| M5 | "Task overdue → notify assignee and change status" runs end to end | ✅ |

## Effort

Phases 0, A1, A2, A3, B1, B2 and B3 are done. Phase C is roughly **4
person-weeks**.

**Phase C is now the only phase left, and it is the one that decides whether
this can be deployed to an organisation rather than to a team.** SSO, SCIM
provisioning and LGPD export/erasure are not features anyone asks for by name;
they are the conditions under which an internal tool is allowed to hold real
people's data. Goals/OKRs and portfolio reporting are the visible half.

Also outstanding across earlier phases, none of it blocking: the load test
behind **M3** (a 10k-task board at p95 < 100 ms), attachments and recurring
tasks from A3, and signed webhooks — which need retry, backoff and a delivery
log to be worth shipping, so they are a phase of their own rather than a corner
of another.
