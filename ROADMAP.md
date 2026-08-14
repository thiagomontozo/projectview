# Roadmap

Where the project stands and what is left. Updated as each phase lands.

**Legend:** ✅ done and verified · 🚧 in progress · ⬜ not started

---

## Progress

```
Phase 0  Security containment        ████████████████████  100%   ✅
Phase A1 PostgreSQL foundation       ████████████████████  100%   ✅
Phase A2 Domain, security, ops       ░░░░░░░░░░░░░░░░░░░░    0%   ⬜
Phase A3 Product engine              ░░░░░░░░░░░░░░░░░░░░    0%   ⬜
Phase B1 UI foundation               ░░░░░░░░░░░░░░░░░░░░    0%   ⬜
Phase B2 Views                       ░░░░░░░░░░░░░░░░░░░░    0%   ⬜
Phase B3 Collaboration               ░░░░░░░░░░░░░░░░░░░░    0%   ⬜
Phase C  Intelligence & enterprise   ░░░░░░░░░░░░░░░░░░░░    0%   ⬜
                                     ─────────────────────
                              overall  ~2 of 8 phases
```

Roughly **25% of the phases** are complete, but they are the two that
everything else stands on: the system is no longer exploitable, and the data
layer can now express what a professional product needs.

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

---

## What is left

### ⬜ Phase A2 — Domain, security and operations
Space → Folder → List hierarchy · hierarchical RBAC with inheritance ·
append-only audit log · refresh tokens, session revocation, argon2id, CSRF ·
cursor pagination, filtering, sorting and full-text search on every listing ·
generated OpenAPI + typed TS client · transactional outbox · structured logging,
OpenTelemetry, metrics.

### ⬜ Phase A3 — Product engine
Custom fields · task dependencies and critical path · time tracking ·
recurring tasks · templates · attachments · watchers and mentions ·
automation engine (trigger → condition → action) · signed webhooks · public API.

### ⬜ Phase B1 — UI foundation
Design tokens and a component library on Radix · TanStack Query replacing the
per-page `useEffect`+`fetch` · error boundaries and skeletons · i18n ·
WCAG AA · command palette · Vitest and Playwright.

### ⬜ Phase B2 — Views
List, Table (inline editing), Calendar, Gantt (drag, dependencies, milestones,
baseline), Timeline, Workload, Activity · saved views · virtualization ·
bulk editing.

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
| M2 | Audit log covers 100% of mutations; RBAC matrix tested | ⬜ |
| M3 | A 10k-task board renders under 100 ms p95 under load | ⬜ |
| M4 | Gantt reorders dependencies and recomputes the critical path | ⬜ |
| M5 | "Task overdue → notify assignee and change status" runs end to end | ⬜ |

## Effort

Phase 0 and A1 are done. The remaining six phases are roughly **26
person-weeks**, compressible to about 15–17 calendar weeks with the platform
and product tracks running in parallel.
