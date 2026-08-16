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
Phase C  Intelligence & enterprise   ████████████████░░░░   80%   ✅
                                     ─────────────────────
                              overall   8 of 8 phases
```

Schedules have real dependencies and a critical path, tasks carry typed
custom fields and tracked time, rules act on changes without anyone asking,
and the conversation around the work — threads, reactions, mentions,
documents, presence — happens in the product. Goals are fed by the tasks
underneath them, the portfolio derives its own health, earned value measures
against a frozen plan, and the enterprise plumbing an organisation needs to
hold real people's data is in place.

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

## ✅ Phase C — Intelligence and enterprise

The phase that decides whether this can be deployed to an organisation rather
than to a team.

**Goals and OKRs.** An objective with key results, and a key result is either
typed in or **read from the tasks of a project**. The derived kind is the point:
a goal whose number nobody updates is worse than no goal at all. A derived
measure refuses a hand-typed value, because the next read would overwrite it and
accepting the write would be a lie about what was stored. Progress is measured
from the starting value, not from zero — moving a number from 80 to 90 is at 0%
on the day it is written, not at 89%.

**Portfolio and capacity.** Every project at once, with health **derived**
rather than typed: a RAG status somebody maintains by hand is green until the
week it is red, which is the week it stopped being useful. The thresholds are
deliberate — a tenth of the work overdue before a project turns amber, so one
stale task cannot paint a healthy project red, and the worst signal wins so
"late *and* over budget" does not read as merely at risk. Capacity compares
committed hours against a real declared figure, with a task's estimate shared
between its assignees, because a task with three people on it is not three times
the work.

**Baselines and earned value.** A baseline is the plan as approved, snapshotted:
reading the plan out of the live rows would compare the schedule against itself
and report perfection forever. Earned value uses the 0/100 rule — a task earns
its budget when it is finished and nothing before — because self-reported
percentages are the part of every EVM implementation that lies. Planned value
accrues linearly between baselined start and due dates. Indices are **omitted
rather than reported as zero** when their denominator is zero: a CPI of 0 reads
as catastrophe when the truth is "no data yet". Measured in **hours**, not
currency: the system holds estimates and tracked time but no rates.

**Saved dashboards.** The card arrangement moved out of one browser's local
storage and onto the server, so it follows a person between machines. A saved
layout is reconciled with the cards the running build actually has, so an older
arrangement never hides a card that shipped since.

**Single sign-on.** OIDC with PKCE, written directly rather than pulled from a
library — the flow is four HTTP calls, and the alternative brings its own
session model and its own opinions about cookies, both of which this application
already has. State and verifier travel in short-lived cookies rather than server
memory, so the replica that answers the callback need not be the one that
started the flow. Auto-provisioning is off by default. SAML was **deferred**: it
is a second, much larger protocol, and every provider an internal tool is likely
to meet speaks OIDC.

**SCIM 2.0 provisioning.** Because provisioning by hand is where an internal
tool leaks: a person leaves, HR closes their directory account, and the project
tool keeps their session alive because nobody remembered it existed. A SCIM
delete deactivates and **revokes every live session immediately** — flipping a
flag and letting existing tokens run to expiry is the exact gap provisioning
exists to close. Groups are not implemented, deliberately: team membership here
is a project decision, and mapping it from the directory would let the directory
quietly rearrange who can see what. Machine credentials are stored as hashes
only.

**Privacy and retention.** Self-service export; erasure that **anonymises rather
than deletes**, because deleting the row would cascade through work that belongs
to the organisation and punch holes in the audit trail. Retention windows are
zero by default: deleting records because a config file was left empty is what a
retention policy exists to prevent. Unread notifications are never expired.

**Operations.** [docs/OPERATIONS.md](docs/OPERATIONS.md) covers backup, a
restore procedure that says how to *verify* it worked, retention, SSO and SCIM
setup, privacy requests, and exactly what would have to change before running a
second replica — the WebSocket hub is per-process and the schedulers have no
lock, and both are named rather than glossed over.

**Verified:** the smoke test grew from 172 to **247 assertions**, including the
full SCIM lifecycle against a real service token, an erasure that leaves a
tombstone and an audit entry, and the negative cases (a member cannot read the
portfolio, mint a token, capture a baseline or declare an organisation-wide
goal). New unit tests cover the earned-value maths — the 0/100 rule, deleted
tasks keeping their budget, indices absent rather than zero — the health
thresholds, key-result progress including measures that count downwards, and the
SCIM filter parser.

**Three bugs the assertions caught:** adding two columns to the user query broke
the workload report's scan; the privacy export queried `audit_log` by column
names that do not exist; and the edge proxy routed `/scim` to the SPA, so every
provisioning call reached the frontend instead of the API.

**Deferred, with reason:** SAML, WAL-based point-in-time recovery (a different
operational commitment, and one to take deliberately), XLSX and PDF export (a
library and a rendering engine to solve what CSV and the browser's print dialog
already solve), and multi-replica support — which needs a shared bus for the
WebSocket hub and a lock for the schedulers, both named in the runbook.

---

## What is left

Nothing in the roadmap: all eight phases are delivered. The outstanding work is
tracked in **[BACKLOG.md](BACKLOG.md)**, ordered by what blocks use rather than
by size.

Three of its items have since shipped — a user administration screen, a refusal
to demote or deactivate the last administrator, and **attachments**, which
closes the deferral carried through phases A3 and B3 by adding the object
storage those phases named as the reason to wait.

Attachments also cost the WebSocket hub a bug fix. Running the suite inside the
runtime image surfaced an intermittent failure in a test nothing had touched:
`unregister` announced a disconnect before clearing the departing client's
typing entry, so for a moment the hub answered "not online" and "still typing"
at once. Both facts now change under the same lock. It had been flaky in CI
rather than wrong-looking, which is how it survived this long.

What is left at the top:

- **Browser-level tests.** Every defect a user has actually hit was invisible to
  the API tests and obvious in a rendered page.
- **Moving the board off the unbounded listing.** The load test has been built
  and run; what it found is below, and the board is what M3 is waiting on.

---

## Milestones

| | Criterion | Status |
|---|---|---|
| M0 | Smoke test proves a member cannot escalate privilege; CI green | ✅ |
| M1 | Stack runs on Postgres; smoke test passes unchanged | ✅ |
| M2 | Audit log covers mutations; RBAC matrix tested; sessions revocable | ✅ |
| M3 | A 10k-task board renders under 100 ms p95 under load | ❌ **measured, not met** — see below |
| M4 | Gantt reorders dependencies and recomputes the critical path | ✅ |
| M5 | "Task overdue → notify assignee and change status" runs end to end | ✅ |
| M6 | Deactivating in the directory revokes the session here immediately | ✅ |

## M3 — the load test, and what it found

Run with [`scripts/loadtest/run.sh`](scripts/loadtest/run.sh): a fixture of
**10,000 tasks in one project** — 13,334 assignments across 25 people, 10,000
tags, 6,000 checklist items, 2,500 comments and 2,000 dependencies arranged as
chains and diamonds so the critical-path walk is actually exercised — then k6
at 10 concurrent users with one second of think time.

**The milestone is not met.** The numbers, rather than an adjective:

| Endpoint | p95 today | p95 paginated | Isolated, one request |
|---|---|---|---|
| Board (`/projects/:id/tasks`) | **9.06 s** | 442 ms | 1.6 s |
| List (same collection) | **8.88 s** | 296 ms | 1.6 s |
| Search (`/tasks?q=`) | 748 ms | 218 ms | **49 ms** ✅ |
| Timeline (`/projects/:id/schedule`) | 1.12 s | 750 ms | 181 ms |
| Workload (`/users/workload`) | 698 ms | 459 ms | 118 ms |
| Data transferred over the run | 814 MB | 264 MB | — |

**The board is the whole finding, and it is not an indexing problem.** The main
query is 130 ms; the response is **10.1 MB**, because the endpoint returns every
task in the project fully hydrated — 1,010 bytes each, times ten thousand. No
index and no field-trimming rescues that: even a maximally trimmed card object
would be several megabytes. The only fix is to stop asking for all of them.

**Two things the run settles that were previously assertions:**

- **The search half of the premise holds.** The `tsvector` column, its GIN
  index and the cursor pagination do what they were built for: 49 ms for a
  page of 50 out of 10,000. Search only appears slow above because it is
  queued behind boards.
- **The board is also what breaks everything else.** Every other endpoint
  improves when the board stops shipping 10 MB, without one line changing in
  any of them — search 748 → 218 ms, workload 698 → 459 ms — while the run
  serves **nine times more requests** (1,825 against 196).

The "paginated" column is measured, not projected: it fetches a page per kanban
column through `/api/tasks?projectId=&status=&limit=100`, the paginated endpoint
that **already exists and is already fast**. The board page simply never moved
onto it — which the comment on `SearchTasks` half-predicted, describing itself
as the replacement for "the return-every-row behaviour the other listings had".
Closing that is [BACKLOG 2.2](BACKLOG.md), scoped there, because the obstacle is
not the query: it is that filtering, grouping and sorting live in one client-side
module shared by six views, and server-side paging changes what a filter means.

**Fixed on the way:** the capacity report ran a correlated subquery per
assignment — "how many people share this task", 10,002 executions — and scanned
the whole tasks table once per person. One pass with a window function instead:
**403 ms → 150 ms**, byte-identical output.

**Not met, and honestly so:** even paginated, the board is 442 ms p95 rather
than 100 ms at this concurrency, with the remaining cost dominated by the
timeline (512 KB of dependencies) and the workload aggregation. Both are named
in the backlog. A single board page in isolation is ~100 ms, so the target is
reachable, but not while four other unbounded reports run beside it.

## Effort

Every phase of the plan is done. Against the original estimate of ~30
person-weeks, the eight phases landed as **9 verified deliveries**.

What the system is now, measured rather than asserted: **8 embedded migrations**
applied on boot, **10 Go test packages**, **316 end-to-end assertions** against
a real containerised stack, **48 frontend tests**, and **5 CI jobs** gating
every push.

The honest remaining risk is no longer that **M3** is unmeasured — it has been
measured, and it failed. The board ships every task in the project in one
response, which is 10.1 MB and nine seconds at ten thousand tasks. That is now a
known quantity with a known fix rather than a risk, which is the whole reason
for running the test; everything else is either shipped or named above with a
reason. The product works, and it works at the size most installations will
reach. What it does not yet do is hold up at ten thousand tasks in a single
project, and that is the next thing worth doing.
