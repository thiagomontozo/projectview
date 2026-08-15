# Backlog

What is left to build, ordered by what actually blocks use rather than by
size. [ROADMAP.md](ROADMAP.md) records the eight phases that are finished;
this file is the list that outlives them.

Every item says **why** it matters and what "done" means, so none of them is a
line somebody has to reverse-engineer later.

---

## Priority 1 — gaps that block ordinary use

### 1.1 User administration screen

**The API can do this today. The interface cannot.**

An administrator can already promote somebody to `admin`, deactivate an
account, reset a password and create a user — all of it authorised, validated
and written to the audit trail as `user.role_changed`, `user.deactivated` and
`user.created`. Verified end to end against a running stack.

What is missing is a screen. There is no `/admin/users` route, nothing calls
`PUT /api/users/:id` with a role, and nothing calls `POST /api/auth/register`.
In practice an administrator has to use `curl` to add a colleague, which is not
a feature anybody can be asked to use.

**Done means:** a page listing every account with its role, status and last
sign-in; create, promote/demote, deactivate/reactivate, reset password and
erase from it; visible only to administrators, with the server still enforcing
that on every route.

```bash
# What works now, and what the screen should be doing for you:
curl -X PUT https://<host>/api/users/<id> \
     -H "Authorization: Bearer <admin token>" \
     -H 'Content-Type: application/json' \
     -d '{"role":"admin"}'
```

### 1.2 Refuse to remove the last administrator

Found while checking 1.1. `UpdateUser` lets any administrator set any role on
any account — **including their own**. Nothing stops the only administrator
demoting themselves, or deactivating their own account, and there is no
recovery path short of an `UPDATE` statement against the database.

Erasure already refuses to run against your own account. Role changes and
deactivation do not.

**Done means:** demoting or deactivating an account is refused when it would
leave the installation with no active administrator, with a message that says
so; a regression test proves it; the smoke test asserts it.

Cheap to build, and the kind of thing that is only ever discovered at the worst
possible moment.

### 1.3 Browser-level tests (Playwright)

**The strongest evidence in this backlog is the recent history.**

Three defects in a row reached a user despite five green CI jobs, 270 API
assertions and 45 frontend tests:

| Defect | Why nothing caught it |
|---|---|
| Login screen rendered nothing | An anonymous 401 was treated as an expired session; the query cache cleared, refetched, cleared again, and `loading` never settled |
| Every label showed as its own key in Portuguese | `nonExplicitSupportedLngs` against a region-qualified `supportedLngs` resolved to an empty language chain |
| Status and priority selects did nothing | The dropdown was portalled out of the dialog and painted behind it |

All three are invisible to an API test and obvious in a rendered page. Each now
has a targeted guard, but the guards were written *after* the fact — the
pipeline still cannot see a screen.

**Done means:** a Playwright suite in CI covering sign-in, creating a project,
moving a card, opening a task and changing its status, switching language, and
opening the settings screen. One test that loads the app and asserts a visible
word would have caught all three above.

---

## Priority 2 — claimed but unproven

### 2.1 The load test behind M3

The only milestone still open. Cursor pagination, the board index and the
generated `tsvector` columns were all built for this, but **designed is not
measured**.

**Done means:** k6 against the containerised stack with 10,000 tasks in one
project, p95 under 100 ms on the board, the list and the search; the numbers
recorded in the roadmap; anything that misses the target either fixed or
written down with its reason.

The plausible surprises are the workload aggregation and the timeline query,
both of which fan out per person.

---

## Priority 3 — product features people will ask for

### 3.1 Attachments

The most visible functional gap for anyone using the product. Needs object
storage — MinIO in compose, S3 in production — which is why it has stayed out:
it is infrastructure, not a corner of an existing screen.

**Done means:** upload on a task and a comment, virus-scan hook left as a
seam, signed time-limited URLs rather than public ones, size and type limits,
and deletion that actually removes the object rather than only the row.

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
  `edited_at` column waiting for it.
- **No rich-text editor.** Descriptions and comments are plain text; documents
  are Markdown. A deliberate choice, revisit only if it becomes a complaint.
- **Intake forms** — the last unbuilt item from the collaboration phase.
- **Coverage is unmeasured on the frontend.** The backend reports it in CI; the
  frontend does not.

---

## Recommendation

**1.1, 1.2 and 1.3, in that order.** Roughly two weeks together.

The first two are a real hole an administrator hits on day one — there is no
way to add a colleague from the interface, and one careless click can leave the
installation with nobody able to administer it. The third closes the gap that
has produced every defect a user has actually seen in this project.

Everything below Priority 2 is a feature. Everything at Priority 1 is the
difference between working software and software that works when tested.
