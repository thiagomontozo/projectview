# Backlog

What is left to build, ordered by what actually blocks use rather than by
size. [ROADMAP.md](ROADMAP.md) records the eight phases that are finished;
this file is the list that outlives them.

Every item says **why** it matters and what "done" means, so none of them is a
line somebody has to reverse-engineer later.

---

## Priority 1 — gaps that block ordinary use

### ✅ 1.1 User administration screen — done

**Administration → Users.** Lists every account with its role, status and last
sign-in; creates accounts, promotes and demotes, deactivates and reactivates,
and resets passwords. Administrators only, with the server enforcing that on
every route rather than trusting the hidden menu item.

Two properties worth knowing, both asserted in the smoke test:

- **A role change applies to the session the person already holds.**
  Authorization reads the account on every request rather than trusting the
  role baked into the token, so a promotion does not wait for the next sign-in
  — and neither does a demotion, which is the half that matters.
- **Resetting a password ends every session that account has open.**

### ✅ 1.2 Refuse to remove the last administrator — done

Demoting or deactivating the last active administrator is refused with `409`
and a message saying to promote somebody else first. Enforced on the SCIM path
too, where a directory sync would otherwise do it automatically at three in the
morning. Anonymised accounts do not count towards the total: an erased
tombstone cannot sign in, and counting it would let the last real administrator
lock everybody out.

The screen warns before the server has to, when only one administrator is left.

**A smaller defect surfaced while testing it.** A member sending a role on
their own profile got `200` with the field silently dropped — no escalation,
the role never changed, but the response claimed a change had been applied.
It now refuses with `403`. Harmless to an attacker, who learns nothing either
way; thoroughly misleading to anyone else, including an administrator editing
their own profile.

### ✅ 1.3 Browser-level tests (Playwright) — done

[`e2e/`](e2e/), 21 tests, wired into the `integration` job so they run against
the composed stack on every push. **The pipeline can now see a screen.**

Everything the item asked for is covered — sign-in, creating a project, moving
a card, opening a task and changing its status, switching language, opening the
settings screens — plus a sweep that opens all fourteen routes and asserts each
renders a heading with no error boundary and no leaked translation keys. That
sweep is the cheap one this item predicted would have caught all three defects.

**Three decisions worth knowing:**

- **It authenticates once.** The login endpoint is rate-limited to ten attempts
  a minute on purpose, so a suite signing in per test would have started
  measuring the limiter and failing for a reason unrelated to any screen.
- **It finds things by role and label, not by CSS class.** Two of the three
  defects were accessibility failures wearing another hat — a portalled listbox
  painted behind its dialog, and labels that never resolved — so a test that
  locates its targets the way a screen reader does is testing what actually
  broke. It also means a class rename does not produce a red suite.
- **The dropdown test clicks the option where it is painted.** Playwright
  refuses a click on a covered element, so that click *is* the assertion that
  nothing is on top of it. Setting the value programmatically would have passed
  throughout the original defect.

**The guards were verified by reintroducing the defects.** A listbox forced
behind the dialog with `z-index: -1`, and a leaked key injected into the DOM:
both turned the suite red, as they must. A browser suite that would have stayed
green through the bugs that motivated it is worse than no suite, and the only
way to know which one you have is to break it on purpose.

**Not covered, and named rather than implied:** one browser (Chromium), no
visual regression, and no mobile viewport. Chromium is what the defects
happened in and what an internal tool is opened with; the other two are
different disciplines with their own maintenance costs, worth taking on
deliberately if they are ever wanted.

### The history that motivated it

**The strongest evidence in this backlog was the recent history.**

Three defects in a row reached a user despite five green CI jobs, 316 API
assertions and 48 frontend tests:

| Defect | Why nothing caught it |
|---|---|
| Login screen rendered nothing | An anonymous 401 was treated as an expired session; the query cache cleared, refetched, cleared again, and `loading` never settled |
| Every label showed as its own key in Portuguese | `nonExplicitSupportedLngs` against a region-qualified `supportedLngs` resolved to an empty language chain |
| Status and priority selects did nothing | The dropdown was portalled out of the dialog and painted behind it |

All three are invisible to an API test and obvious in a rendered page. Each got
a targeted guard at the time, but the guards were written *after* the fact —
the pipeline could not see a screen at all, so the next defect of the same
shape would have reached a user the same way.

Kept here rather than deleted with the item: the reasoning is why the suite is
shaped the way it is, and the next person to wonder whether browser tests earn
their maintenance should be able to read the three failures that paid for
them.

---

## Priority 2 — claimed but unproven

### ✅ 2.1 The load test behind M3 — done (the test; the milestone is not met)

Built and run: [`scripts/loadtest/`](scripts/loadtest/) seeds 10,000 tasks in
one project and drives k6 at 10 concurrent users. Full numbers are in
[ROADMAP.md](ROADMAP.md#m3--the-load-test-and-what-it-found).

**What it settled.** Half the premise was right and half was wrong, which is
exactly why it was worth measuring rather than arguing about:

- **Search, the `tsvector` column and cursor pagination hold up**: 49 ms for a
  page of 50 out of 10,000. That part was built for this and works.
- **The board does not**: 9.06 s p95, because `/projects/:id/tasks` returns
  every task in the project — a **10.1 MB** response. The query is 130 ms; the
  rest is producing and shipping ten thousand hydrated objects.

**The plausible surprises were half right too.** The timeline (1.12 s) and the
workload aggregation (698 ms) are indeed the next two, but both are an order of
magnitude behind the board, and both improve on their own once it stops
saturating the process.

**Fixed while measuring:** the capacity report ran a per-assignment correlated
subquery and re-scanned the tasks table once per person — 403 ms → 150 ms with
byte-identical output.

**Not fixed, and moved to 2.2 rather than folded in here:** the board itself.

### ✅ 2.2 Move the board off the unbounded listing — done

The board fetches **a page per kanban column** with a true total behind each,
and every other view reads one paged stream that says how much of the project
it is showing. **9.06 s → 483 ms p95**, and the same run now serves ten times
the requests while every other endpoint gets faster beside it. Numbers in
[ROADMAP.md](ROADMAP.md#m3--the-load-test-and-what-it-found).

**The design question this item existed for, answered:** filtering, grouping
and sorting moved **server-side**, not into a scoped notice. `/api/tasks` now
takes repeated `status`, `priority` and `assigneeId` parameters (repeated means
*or*), a `sort` from an allow-list, and an `offset`; a new
`/projects/:id/tasks/counts` returns the per-column totals in one query. So a
filter still means "everywhere in this project" rather than "within what
happened to load" — which was the whole risk.

Three rules moved out of the client's sort function and into SQL, because with
only a page in hand the browser can no longer order what it cannot see:
severity ordering for priority, the project's own column order for status, and
unscheduled tasks sorting last in **either** direction. They are tested in
`repo.taskOrderBy` now instead of `applyView`, which is deleted.

**What is scoped rather than resolved, and says so:** grouping by assignee or
priority in the list view still buckets what is loaded, and the calendar,
timeline and workload views draw from the same page. Each shows "showing N of
M" with a load-more, so no view claims completeness it does not have. Paging
those by group is a smaller, separate change now that the server can count.

**Offset, not a cursor**, and deliberately. A cursor anchors on the ordering it
was built for; six sort options would mean six cursor encodings. Offset costs
more the deeper it goes, which is the right trade for a column somebody expands
a few times and the wrong one for scrolling a whole table — the cursor path is
untouched for the search endpoint that uses it.

**Two bugs found while doing it**, both invisible without the right test:

- A nil Go slice reaches PostgreSQL as `NULL`, not as an empty array, so
  `cardinality($3) = 0` evaluated to `NULL` rather than true. The guard failed
  open into the filter and **every unfiltered listing returned zero rows**.
  Nothing errored; the endpoints answered 200 and every board was empty.
- Axios brackets array parameters, so `status[]=todo` reached a Go handler
  reading `status`. Every column asked for its own status, none of them
  applied, and **all five columns rendered the same unfiltered page**. Caught
  by the browser suite from 1.3, and by nothing else — the API was answering
  each request correctly.

### 2.2 (original statement, kept for the reasoning)

**This is what M3 is actually waiting on.** The evidence is measured, not
argued: fetching a page per kanban column through `/api/tasks?projectId=&status=
&limit=100` — the paginated endpoint that **already exists** — takes the board
from 9.06 s to 442 ms p95 and drags every other endpoint down with it (search
748 → 218 ms, workload 698 → 459 ms) while serving nine times the requests.

So the backend is ready. The obstacle is on the client, and it is a real design
question rather than a wiring job:

- **Six views share one filter/group/sort module** ([useViewState.ts](frontend/src/views/useViewState.ts)),
  and it filters the full client-side set. With server-side paging a filter
  would silently mean "within the page I happen to have loaded" — a board
  reporting three matches when there are three hundred is worse than a slow
  board, and worse than no filter.
- **The kanban needs per-column paging with a visible affordance.** A column
  that silently shows its first hundred cards is missing data with no way to
  tell.

**Done means:** filtering, grouping and sorting resolved server-side or
explicitly scoped in the interface; each column paged with a "load more" and a
true total; the list, table, calendar, timeline and workload views moved with
it; and `scripts/loadtest/run.sh paginated` re-run with the numbers recorded.

Worth doing before 3.2 and 3.3: both add rows to the same views.

---

## Priority 3 — product features people will ask for

*(all three are done.)*

### ✅ 3.1 Attachments — done

Files on a task or one of its comments, kept in an S3-compatible object store
(MinIO in compose, S3 or an equivalent in production). Every part of what
"done" was defined as is in place: signed time-limited URLs rather than public
ones, per-file and per-task size limits, an optional MIME allow-list, a
virus-scan hook left as a seam, and deletion that removes the object.

Four decisions worth knowing, all of them asserted:

- **Uploads pass through the API; downloads do not.** Buffering the upload is
  what makes the size ceiling, the type rules and the scan hook enforceable — a
  presigned PUT handed to the browser would let a client write whatever it
  liked. A download is a redirect to a signed URL the object store serves
  itself, so the bytes never spend the API's memory.
- **Deleting the row is not deleting the file, and the cascade is the hard
  case.** Removing a task, a project or a space drops attachment rows in one
  statement the application never observes. So a trigger queues every deleted
  row's storage key and a sweeper drains it — the only construction that covers
  a path no handler is on. Verified end to end: uploading, deleting the whole
  *project*, then watching the object leave the bucket.
- **What a file is comes from its bytes**, not from the extension or the
  client's `Content-Type`, both of which the uploader controls. The extension
  decides only what sniffing cannot: every Office document is a ZIP on the
  wire. Executable formats are refused outright, and only the final extension
  counts.
- **`skipped` is not `clean`.** With no scanner wired in the status records
  that *nothing examined the file*. Collapsing the two would hide the scanner's
  absence exactly where it matters.

**Three defects found while building it, all fixed and all now covered:**

- `mime.TypeByExtension` reads the *host's* MIME database, which Windows has
  and the Alpine runtime image does not. Every Office document resolved
  correctly in development and would have arrived as `application/octet-stream`
  in production — where an allow-list naming those types would then have
  refused it. The table is now the application's own. Found only by running the
  unit tests inside the runtime image rather than on the development machine,
  which is the lesson worth keeping.
- Following the download redirect while still holding a session produced a 400:
  the store saw two authentication mechanisms at once, the Bearer header and
  the query signature, and S3 refuses that combination. The proxy now strips
  the header, since the signed URL is the authorization on that leg. Browsers
  never sent it anyway; API clients did.
- A scan that failed left the row `pending` forever — visible in the list and
  permanently refusing to download, with nothing to revisit it. A failed scan
  now refuses the upload and removes the object, which is both fail-closed and
  something the person can act on.

**And one in the proxy, found by the test that was meant to prove the fix:** a
single `proxy_set_header` inside a `location` discards *every* inherited one,
exactly as `add_header` does. Stripping the Authorization header silently
dropped `Host`, so the store received `minio:9000` instead of the name the URL
was signed for and rejected every download. The inherited headers are now
repeated explicitly.

**Not included, deliberately:** chat attachments. The chat schema has no
equivalent hook and the permission model there is channel membership rather
than project membership, so it is a second piece of work rather than a wider
`WHERE` clause. It is listed under the smaller items below.

### ✅ 3.2 Recurring tasks — done

Daily, weekly or monthly, with an interval ("every 2 weeks") and an optional end
by date or by count. The rule lives on the instance currently carrying it and
moves forward in one transaction, so a series never belongs to two tasks or to
none — a delete-then-insert would leave a window where a crash loses the rule
and the task silently stops recurring, which is the failure nobody notices
until the report does not arrive.

**The question this item asked — what happens to an instance nobody finished —
has one answer in both modes: it stays, and stays visible.** Nothing closes,
deletes or reschedules it. What differs is what comes next:

- **`on_complete`** — the next one appears when this one is finished. Never
  piles up, and stops entirely if nobody does the work, which is honest:
  nothing was completed, so nothing came next.
- **`on_schedule`** — the next one appears when due, whatever happened to the
  last. Unfinished instances go overdue, so a month of ignored reports looks
  like a month of ignored reports rather than one tidy row.

**And a neglected series does not catch up.** Six missed weeks produce one next
date, not six new tasks piled on the one nobody did — burying the person who was
already not doing it would be the obvious implementation and the wrong one.

Two rules the date arithmetic encodes, both tested without a database:
**monthly clamps rather than overflowing** (Go's `AddDate` turns 31 January into
3 March, so a monthly task would drift a few days every short month), and the
copy carries the *definition* of the work — title, assignees, checklist,
unticked — while leaving behind everything belonging to one occurrence: the
completion stamp, the comments arguing about last month's numbers, the time
logged against it.

### ✅ 3.3 Templates — done

Task and project templates, with the checklist, tags and custom field values.
A project template is **captured from a project that already exists** rather
than described by hand, because describing twelve tasks in a form is not a
feature anybody wants.

Three decisions worth knowing:

- **Dates are stored as offsets in days**, never as the dates they were. A
  kickoff plan captured in March creates work dated from the week it is used.
- **Assignees are not captured.** A template that silently allocates last
  quarter's team is worse than one that allocates nobody.
- **The body is a JSONB snapshot**, not a relational mirror of tasks and
  checklists. A relational template would need migrating in step with every
  column a task grows, and one captured a year ago would describe a task that no
  longer exists; a snapshot is translated on application, and a field the
  current schema does not recognise is ignored rather than failing the whole
  thing.

Capturing a template is gated like creating structure (`admin`/`manager`), since
it shapes how work gets created; applying a *task* template only needs the right
to work in the project it lands in.

---

## Priority 4 — platform work

### 4.1 Signed webhooks and a public API

Deliberately not shipped as a corner of the automation engine. A `POST` that
fails silently is worse than no webhook, so this needs retry with backoff, a
delivery log an operator can inspect, and HMAC signing with a rotatable secret.
That is a phase of its own.

### 4.2 More than one replica — half done

- ✅ **The schedulers now hold a lock.** Alerts, digests, retention and the
  recurrence sweep each take a PostgreSQL *session* advisory lock before
  running, so a sweep runs once across the installation instead of once per
  process. `pg_try_advisory_lock` never waits: a replica that finds the lock
  taken skips that tick, because a sweep already running is a sweep already
  happening and queueing behind it would only run it twice in a row.

  The honest bound: a process killed mid-sweep releases its lock when the
  connection dies, and another replica may repeat the work on the next tick.
  That turns "always duplicated" into "duplicated only if a process dies
  mid-sweep". The attachment object sweeper is deliberately *not* locked —
  deleting an object is idempotent, so two replicas duplicate work rather than
  corrupt anything.

- ⬜ **The WebSocket hub is still per-process.** A message published on one
  replica is not pushed to clients connected to another. Needs a shared bus;
  Redis pub/sub is the usual answer, and it is the one remaining blocker.

### 4.3 SAML

Every identity provider an internal tool is likely to meet speaks OIDC, which
is shipped. SAML is a second, much larger protocol and should wait until
somebody actually needs it.

### 4.4 Point-in-time recovery

Nightly dumps mean up to a day of loss. WAL archiving closes that, at the cost
of archive storage and monitoring that archiving has not stalled — a deliberate
operational commitment rather than a default.

### 4.5 XLSX and PDF export

CSV covers the real need, which is getting the numbers somewhere they can be
pivoted. XLSX means a library and a format nobody can diff; PDF means shipping
a rendering engine to do what the browser's print dialog already does. Worth
doing only if somebody asks by name.

---

## Reported by a user

### ✅ Teams could be created but never staffed — fixed

**Found by somebody using the product, not by any test**, which is the part
worth recording. `POST /api/teams/{id}/members` and its delete counterpart had
existed since the first phase and worked correctly; nothing in the interface
ever called them. Every API assertion passed the whole time, because the API
was never the problem.

Underneath it was a worse gap: an Active Directory user only entered the local
table by signing in, so a colleague could not be allocated until they had
logged in themselves — and being put on the team is often *why* somebody logs
in. The fix adds a directory search (`GET /api/directory/search`) and lets a
member be added by directory username, provisioning the account exactly as a
first login would.

Three decisions worth knowing:

- **Searching needs a service account** (`AD_BIND_DN` / `AD_BIND_PASSWORD`).
  There is no way around it: the only other credentials this application ever
  sees belong to the person signing in, at the moment they sign in.
- **"Nobody matched" and "nobody could be looked up" are different answers.**
  The endpoint returns `searched: false` with a reason when the directory
  cannot be consulted, and the interface says so rather than showing an empty
  list that implies the person does not exist.
- **The query is escaped before the wildcards are added**, in that order.
  Escaping afterwards would neutralise our own asterisks; adding them first
  would let a caller's asterisk through and turn a search box into an arbitrary
  LDAP filter. Verified against a real directory: `*)(objectClass=*` returns
  nothing rather than the staff list.

Validated against a containerised OpenLDAP with seeded people, since the
deployment this is for does not exist yet. **Enabling AD changed the login
default and broke the browser suite** — the form defaults to the directory, and
the bootstrap administrator is a local account. The suite now picks "local
account" explicitly, so configuring AD no longer breaks every test.

## Smaller items

- ✅ **Alert and retention cron expressions are editable.** The schedules are
  now rebuildable: saving a new expression stops the old cron and starts one
  against the current configuration, so the field changes the timetable instead
  of waiting for the next deploy. `Stop()` lets a sweep already running finish
  rather than cutting a half-sent batch of alerts off mid-flight.
- **Chat has no attachments and no editing**, though the schema has an
  `edited_at` column waiting for it. Tasks have attachments now (3.1) and the
  object storage is in place, so the remaining work is the permission side:
  a chat file is gated by channel membership rather than by project membership,
  which is a different resolution path rather than a wider query.
- ✅ **Rich text on descriptions and comments.** It became a complaint, which
  was the stated trigger. TipTap over ProseMirror, MIT. **Documents keep
  Markdown** — the original reasoning still holds there: it stays greppable,
  diffable and portable, and a proprietary model would be a format the database
  has to understand. What changed is the places where the alternative was not
  Markdown but a bare textarea.

  Stored as HTML, which keeps it a bounded change: the column is already text
  and plain text is valid HTML, so everything written before renders unchanged.
  Reading is done through a **read-only editor rather than
  `dangerouslySetInnerHTML`**, and that is the security boundary: the column
  takes whatever an API client PUTs, so injecting it into the DOM would be
  stored cross-site scripting run by every reader. ProseMirror keeps only what
  its schema can represent, which is a stronger guarantee than a deny-list.
  Asserted: a script tag, an inline handler and an iframe are all dropped while
  the legitimate formatting and old plain text survive.

  Not offered, deliberately: images, tables, colours, fonts. Those turn a
  description into a document, and this application already has documents.

  *Oasis Editor was asked for and not used*: the repository declares no licence
  at all, which makes it all-rights-reserved by default and not something to put
  in a corporate deployment. It also pulls Solid into a React bundle and is
  pre-1.0.
- **Intake forms** — the last unbuilt item from the collaboration phase.
- ✅ **Frontend coverage is measured in CI**, alongside the backend's. A number
  existed for half the codebase and the other half was unmeasured.

---

## Recommendation

**Nothing is blocking.** Priorities 1, 2 and 3 are all done: account
administration that cannot lock everybody out (1.1, 1.2), a browser suite on
every push (1.3), performance measured rather than asserted (2.1), the board off
the unbounded listing (2.2), attachments (3.1), recurring tasks (3.2) and
templates (3.3).

What remains at Priority 4 is platform work, and every item there is a
deliberate deferral with a reason rather than an oversight. The honest reading
is that **none of them should start without somebody asking for it**: SAML until
a provider needs it, XLSX and PDF until named, point-in-time recovery when the
operational commitment is wanted, and webhooks as a phase of their own.

**The one with a standing case is 4.2, more than one replica** — not because
the load demands it today, but because both blockers are small, both are known,
and neither gets easier later. The attachment sweeper added in 3.1 is already
replica-safe; the WebSocket hub and the schedulers are not.

**What is actually left, and nothing here blocks use:**

- **The WebSocket hub is per-process** — the last blocker to a second replica,
  and the only item with a standing case for doing it before somebody asks.
- **Chat has no attachments and no editing.** The object storage is in place;
  the work is the permission side, which is channel membership rather than
  project membership.
- **Intake forms** — the last unbuilt item from the collaboration phase.
- **Grouping by assignee or priority in the list view, and the calendar,
  timeline and workload views, still draw from one loaded page.** Each says so
  on screen; paging them by group is contained now that the server can count.
- **Attachments have no browser coverage** on the drag-and-drop target or the
  upload progress bar. A file input that silently did nothing would still pass
  everything.
- **M3 remains unmet at 407 ms against 100 ms.** Every unbounded read is now
  bounded; what is left is the cost of a synthetic load heavier than a board
  page in real use.
