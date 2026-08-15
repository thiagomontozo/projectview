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

Three defects in a row reached a user despite five green CI jobs, 284 API
assertions and 45 frontend tests:

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

**1.3, then 2.1.** Roughly a week and a half together.

1.1 and 1.2 are done: an administrator can now manage accounts from the
interface, and cannot accidentally leave the installation with nobody able to
administer it.

What remains at Priority 1 is the browser tests, and the case for them has
only got stronger — the screen that closed 1.1 is itself covered only at the
API. Every defect a user has actually seen in this project was invisible there
and obvious in a rendered page.

Everything below Priority 2 is a feature. Everything at Priority 1 is the
difference between working software and software that works when tested.
