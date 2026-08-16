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

### 1.3 Browser-level tests (Playwright)

**The strongest evidence in this backlog is the recent history.**

Three defects in a row reached a user despite five green CI jobs, 316 API
assertions and 48 frontend tests:

| Defect | Why nothing caught it |
|---|---|
| Login screen rendered nothing | An anonymous 401 was treated as an expired session; the query cache cleared, refetched, cleared again, and `loading` never settled |
| Every label showed as its own key in Portuguese | `nonExplicitSupportedLngs` against a region-qualified `supportedLngs` resolved to an empty language chain |
| Status and priority selects did nothing | The dropdown was portalled out of the dialog and painted behind it |

All three are invisible to an API test and obvious in a rendered page. Each now
has a targeted guard, but the guards were written *after* the fact — the
pipeline still cannot see a screen. The user administration screen that closed
1.1 is covered the same way as everything before it: thoroughly at the API, not
at all in a browser.

**Done means:** a Playwright suite in CI covering sign-in, creating a project,
moving a card, opening a task and changing its status, switching language, and
opening the settings screen. One test that loads the app and asserts a visible
word would have caught all three above.

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

### 2.2 Move the board off the unbounded listing

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

*(3.1 is done; 3.2 and 3.3 remain.)*

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

### 3.2 Recurring tasks

A weekly report that has to be created by hand every week is the reason people
keep using spreadsheets alongside the tool.

**Done means:** a recurrence rule on a task, the next instance created when the
current one is completed or on schedule, and a clear answer for what happens to
an instance nobody finished.

### 3.3 Templates

Task, list and project templates, including the checklist and custom fields.
Mostly a copy operation once the hierarchy exists, which it does.

---

## Priority 4 — platform work

### 4.1 Signed webhooks and a public API

Deliberately not shipped as a corner of the automation engine. A `POST` that
fails silently is worse than no webhook, so this needs retry with backoff, a
delivery log an operator can inspect, and HMAC signing with a rotatable secret.
That is a phase of its own.

### 4.2 More than one replica

Named honestly in [docs/OPERATIONS.md](docs/OPERATIONS.md) rather than glossed
over. Two blockers, both small and both real:

- **The WebSocket hub is per-process.** A message published on one replica is
  not pushed to clients connected to another. Needs a shared bus; Redis pub/sub
  is the usual answer.
- **The schedulers have no lock.** Alerts, digests and retention run in every
  process, so two replicas send some notifications twice. A PostgreSQL advisory
  lock is enough.

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

## Smaller items

- **Alert and retention cron expressions are not editable** from the settings
  screen. They are read when the scheduler is built, so changing one needs a
  restart, and a field that silently does nothing until the next deploy is
  worse than no field. Making the schedulers rebuildable would close it.
- **Chat has no attachments and no editing**, though the schema has an
  `edited_at` column waiting for it. Tasks have attachments now (3.1) and the
  object storage is in place, so the remaining work is the permission side:
  a chat file is gated by channel membership rather than by project membership,
  which is a different resolution path rather than a wider query.
- **No rich-text editor.** Descriptions and comments are plain text; documents
  are Markdown. A deliberate choice, revisit only if it becomes a complaint.
- **Intake forms** — the last unbuilt item from the collaboration phase.
- **Coverage is unmeasured on the frontend.** The backend reports it in CI; the
  frontend does not.

---

## Recommendation

**1.3, then 2.2.** 2.1 is done — the load test exists and has run; what it
found became 2.2.

1.1, 1.2, 2.1 and 3.1 are done: an administrator can manage accounts from the
interface, cannot accidentally leave the installation with nobody able to
administer it, people can attach files to their work, and the performance
claims are now measured rather than asserted.

What remains at Priority 1 is the browser tests, and the case for them keeps
getting stronger rather than weaker. The attachments screen that closed 3.1 is
covered the same way as everything before it — thoroughly at the API, not at
all in a browser — and it adds the two interactions the API cannot see at all:
a drag-and-drop target and an upload progress bar. A file input that silently
does nothing would pass all 316 assertions.

Everything else below Priority 2 is a feature. Everything at Priority 1 is the
difference between working software and software that works when tested — and
2.2 is now the difference between software that works and software that works
at the size somebody will actually load into it.
