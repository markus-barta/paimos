# PAIMOS Data Model

**Status**: Current with known maintenance follow-up (active API schema `2.0.0`)
**Last verified**: 2026-06-24 against `backend/db/db.go`, `backend/handlers/schema.go`, and live ppm/pmo `/api/schema`
**Schema source of truth**: `backend/db/db.go` — migrations run in order on startup.
**Legacy**: `docs/archive/DATA_MODEL.md` captures the v0.3.5 pre-release baseline and is kept for archival reference only.

> This is the canonical data-model document. The v1.0.0 / v1.1.1
> releases added `project_members`, `access_audit`, `time_entries`,
> `attachments`, `issue_relations`, sprints, etc.; those are documented
> below alongside the original v1→v2 structural changes.

---

## Core Concept

The entity hierarchy changes from a strict tree to a **mixed model**:

- Groups/Sprints → Tickets use **M:N relations** (a ticket can belong to multiple groups and sprints)
- Tickets → Tasks keep **strict 1:1 parent** (unchanged)

Group types (Epic, Cost Unit, Release) and Sprint are **different views into the same set of tickets**, not separate containers. All live in the `issues` table with a `type` discriminator.

---

## Entity Hierarchy

```
PROJECT
  │
  ├──1:N──► GROUP (type = epic | cost_unit | release)
  │           │
  │           │  M:N via issue_relations (type = 'groups')
  │           │
  │           ▼
  │         TICKET ◄── M:N via issue_relations (type = 'sprint') ──► SPRINT
  │           │
  │           │  1:N strict (parent_id)
  │           │
  │           ▼
  │         TASK
  │
  │         issue_relations also handles:
  │           type = 'depends_on'  (ticket/task → ticket/task)
  │           type = 'impacts'     (ticket/task → ticket/task)
  │
  ├──1:N──► TIME_ENTRIES   (on tickets, per user, start/stop tracking)
  ├──1:N──► COMMENTS       (on any issue)
  ├──1:N──► ISSUE_HISTORY  (on any issue)
  └──M:N──► TAGS           (on project or any issue)
```

---

## Resolved Design Decisions

### Single `issues` table with type discriminator

All entity types live in one `issues` table. Reasons:

- Shared behavior: key generation, tags, comments, search, history, CSV, CRUD lifecycle
- No duplicated handler/component logic
- Sparse nullable columns have zero performance cost at this scale
- `type` field discriminates; frontend renders different views/columns per type

### `issue_relations` — one table for all relationships

Replaces the current `parent_id` for group→ticket links **and** the free-text `depends_on`/`impacts` fields.

```sql
issue_relations (
    source_id   INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    target_id   INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    type        TEXT NOT NULL,
    PRIMARY KEY (source_id, target_id, type)
)
```

| Relation `type` | `source_id` | `target_id` | Meaning |
|-----------------|-------------|-------------|---------|
| `groups` | group (epic/cost_unit/release) | ticket | Ticket belongs to this group |
| `sprint` | sprint | ticket | Ticket is in this sprint |
| `depends_on` | any issue | any issue | Source depends on target |
| `impacts` | any issue | any issue | Source impacts target |
| `follows_from` | any issue | any issue | Source is a sequel/follow-up of target |
| `blocks` | any issue | any issue | Source blocks target's progress |
| `related` | any issue | any issue | Loose "see also" link with no causal direction |
| `applies_to_memory` | ticket | memory entry | Ticket's lesson surfaces against this memory (PAI-342 / M97) |

- A ticket can have 0..N group relations of different group types
- A ticket can be in 0..N sprints (tickets flow between sprints)
- Dependency/impact links work between any issue types
- Application logic enforces type constraints where needed

### Strict 1:1 for ticket→task via `parent_id`

Tasks always have exactly one ticket parent. This stays on `issues.parent_id`, unchanged from today.

### Free-text fields that go away

| Field | Replaced by |
|-------|------------|
| `cost_unit` (free-text on issues) | Relation to a `cost_unit` group entity via `issue_relations` |
| `release` (free-text on issues) | Relation to a `release` group entity via `issue_relations` |
| `depends_on` (free-text on issues) | `issue_relations` with `type = 'depends_on'` |
| `impacts` (free-text on issues) | `issue_relations` with `type = 'impacts'` |

---

## Expanded `type` Values

| Type | Level | Description |
|------|-------|-------------|
| `epic` | Group | Feature grouping; has billing/budget fields |
| `cost_unit` | Group | Billing/accounting grouping; has billing/budget fields |
| `release` | Group | Version/release grouping; has dates and release state |
| `sprint` | Sprint | Time-boxed iteration; tickets flow in/out between sprints |
| `ticket` | Ticket | Work item; can belong to multiple groups and sprints |
| `task` | Task | Sub-item of a ticket; strict single parent |
| `memory` | Knowledge | Reusable lesson / rule the agents must follow (PAI-338) |
| `runbook` | Knowledge | Operator playbook for a known scenario |
| `external_system` | Knowledge | Pointer to an external service the project depends on |
| `related_project` | Knowledge | Cross-project reference card |
| `guideline` | Knowledge | Soft convention that isn't a hard rule |

The knowledge types share the issue infrastructure — history, comments, tags, FTS, soft-delete, undo all work the same. They differ via the `category_metadata` JSON column and the `slug` lookup key (see "Knowledge plane + agent attribution" below).

---

## New and Changed Fields on `issues`

### Group-level fields (nullable; only meaningful when type is a group type)

| Field | Type | Applies to | Notes |
|-------|------|------------|-------|
| `billing_type` | TEXT | epic, cost_unit | Enum: `time_and_material`, `fixed_price` |
| `total_budget` | REAL | epic, cost_unit | Currency amount |
| `rate_hourly` | REAL | epic, cost_unit | €/h |
| `rate_package` | REAL | epic, cost_unit | €/P (package rate) |
| `start_date` | TEXT | release, sprint | ISO date |
| `end_date` | TEXT | release, sprint | ISO date |
| `group_state` | TEXT | release | `unreleased` / `released` |
| `sprint_state` | TEXT | sprint | `planned` / `active` / `complete` |
| `jira_id` | TEXT | epic, cost_unit, sprint | External Jira ID for mapping |
| `jira_version` | TEXT | release | External Jira version for mapping |

### Fields that go away

| Field | Replaced by |
|-------|------------|
| `cost_unit` (free-text) | Relation to `cost_unit` group entity |
| `release` (free-text) | Relation to `release` group entity |
| `depends_on` (free-text) | `issue_relations` with `type = 'depends_on'` |
| `impacts` (free-text) | `issue_relations` with `type = 'impacts'` |

### Fields that stay unchanged

title, description, acceptance_criteria, notes, priority, assignee_id, created_at, updated_at, issue_number/issue_key.

### `report_summary` — added in v3.5.0 (PAI-418)

| Field | Type | Notes |
|-------|------|-------|
| `report_summary` | TEXT NOT NULL DEFAULT '' | Customer-facing Projektbericht copy. Populated by two AI actions (`customer_rewrite`, `exec_summary`); read by the PDF endpoint when `text_source=report`. One field, two style options at generation time — the audience orientation is per-customer, not per-ticket. |

Indexes: covered by the existing LIKE / FTS coverage on issue body fields (`backend/handlers/issues_list.go`).

### Soft-delete (`deleted_at` / `deleted_by`) — added in v1.1.2

| Field | Type | Notes |
|-------|------|-------|
| `deleted_at` | TEXT NULL | ISO timestamp. `NULL` = live, non-NULL = in the Trash. |
| `deleted_by` | INTEGER NULL | `users.id` of whoever moved the row to Trash (plain integer, no FK — stale id after a user purge is acceptable; shown for display only). |

Index: `idx_issues_deleted_at` on `deleted_at`.

**Semantics:**
- `DELETE /api/issues/{id}` stamps `deleted_at` + `deleted_by` and cascades the stamp to every descendant reachable via `parent_id` (so tasks under a trashed ticket disappear alongside the ticket).
- `issue_relations` rows are **not** touched on soft-delete — a trashed ticket keeps its `groups` / `sprint` / `depends_on` / `impacts` links, so restoring re-attaches automatically.
- Every user-facing list / search / tree / report query filters `deleted_at IS NULL`. Trashed rows only appear via `GET /api/issues/trash` (admin-only).
- `POST /api/issues/{id}/restore` clears `deleted_at` on that row alone — cascaded children stay trashed (restore is deliberately explicit).
- `DELETE /api/issues/{id}/purge` hard-deletes a trashed row (and its cascade-bound rows: comments, history, tags, time_entries, attachments, issue_relations). Only works when already trashed, so the UI flow is always two-step.

### `parent_id` behavior change

| Relationship | Before (v1) | After (v2) |
|-------------|-------------|-------------|
| epic → ticket | `parent_id` | `issue_relations` (type=groups, M:N) |
| cost_unit → ticket | free-text string | `issue_relations` (type=groups, M:N) |
| release → ticket | free-text string | `issue_relations` (type=groups, M:N) |
| sprint → ticket | n/a | `issue_relations` (type=sprint, M:N) |
| ticket → task | `parent_id` | `parent_id` (unchanged, strict 1:1) |
| depends_on | free-text issue keys | `issue_relations` (type=depends_on) |
| impacts | free-text issue keys | `issue_relations` (type=impacts) |

`parent_id` remains on `issues` but is now only used for the task→ticket relationship.

---

## Unified Status Model

All issue types share one status enum. The enum grew beyond the
original v2-rename plan to cover the full **billing lifecycle** needed
by cost-unit / release reporting. Current CHECK constraint (source of
truth: `backend/db/db.go`):

```
CHECK(status IN (
    'new','backlog','in-progress','qa','done',
    'delivered','accepted','invoiced','cancelled'
))
```

| Status         | Meaning                                                    |
| -------------- | ---------------------------------------------------------- |
| `new`          | Just created; not yet triaged.                             |
| `backlog`      | Triaged, not yet started. (renamed from v1 `open`)         |
| `in-progress`  | Actively being worked.                                     |
| `qa`           | Work done; under review / quality check.                   |
| `done`         | QA passed; ready for delivery. (renamed from v1 `done`)    |
| `delivered`    | Shipped to customer / stakeholder.                         |
| `accepted`     | Customer / PO has signed off.                              |
| `invoiced`     | Billed to customer (final lifecycle state).                |
| `cancelled`    | Will not be done. (renamed from v1 `closed`; note double-L)|

Migration history: v1→v2 renamed `open→backlog`, `done→complete`,
`closed→canceled`; a later migration expanded the enum, renamed
`complete→done`, and switched `canceled→cancelled` (double-L) to match
the UK spelling used elsewhere.

Additional type-specific states live in separate fields, not in `status`:
- `group_state` on releases: `unreleased` / `released`
- `sprint_state` on sprints: `planned` / `active` / `complete`

---

## Project Fields (additions)

| Field | Type | Notes |
|-------|------|-------|
| `product_owner` | INTEGER NULL | FK→users — project lead |
| `customer_label` | TEXT | Legacy external/customer label retained after M70 |
| `customer_id` | INTEGER NULL | FK→customers.id — assigned customer record |

---

## New Table: `time_entries`

Ticket-level time tracking. Per user, start/stop based.

```sql
time_entries (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    ticket_id   INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    started_at  TEXT NOT NULL,
    stopped_at  TEXT NULL,           -- NULL = timer currently running
    override    REAL NULL,           -- manual override in hours
    comment     TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL DEFAULT (datetime('now'))
)
```

- Only tracks time on issues of type ticket/task (not groups)
- A running timer has `stopped_at = NULL`
- `override` allows manual correction without deleting the entry
- `internal_rate_hourly` (REAL, nullable) added in a later migration for per-entry internal rate snapshots
- **Shipped in v1.0.0.** API surface under `/api/time-entries` and `/api/issues/{id}/time-entries`

---

## Frontend View Model

Same tickets, multiple views:

```
Project Detail View
  ├── [Tab: Epics]       → tickets grouped by epic relations
  ├── [Tab: Cost Units]  → tickets grouped by cost_unit relations
  ├── [Tab: Releases]    → tickets grouped by release relations
  ├── [Tab: Sprints]     → tickets grouped by sprint relations (deferred)
  └── [Tab: All Tickets] → flat/filtered list (existing behavior)
```

Each tab shows the same ticket pool from a different organizational perspective. A ticket with no group relations appears as "ungrouped" in each view.

---

## Migration Strategy (high-level)

1. **Before anything**: create a tagged backup on live (`pre-v2-migration`)
2. Add new nullable columns to `issues` (group-level fields, sprint fields) — additive, safe
3. Create `issue_relations` table — new table, safe
4. Migrate existing `parent_id` epic→ticket relationships into `issue_relations` (type=groups) — data migration
5. Migrate existing free-text `cost_unit` values: create group entities of type `cost_unit`, then insert relations — data migration
6. Migrate existing free-text `release` values: create group entities of type `release`, then insert relations — data migration
7. Migrate existing free-text `depends_on`/`impacts` values: parse issue keys, resolve IDs, insert relations — data migration
8. Add `product_owner` (FK→users) and `customer_id` to `projects` — additive, safe
9. Align status values: `UPDATE issues SET status='backlog' WHERE status='open'` etc. — data migration
10. Deprecate old free-text columns (leave in DB, stop using in code) — or drop later
11. Create `time_entries` table — new table, safe, deferred

**Critical**: steps 4–7 and 9 are data migrations that transform existing live data. Each should be tested locally against a copy of the live DB before deploying.

---

## Implementation Priority

| Phase | What | Risk |
|-------|------|------|
| 1 | `issue_relations` table + group-level columns + status rename | Medium — data migration of existing relationships |
| 2 | Frontend views: epic/cost_unit/release tabs | Low — additive UI |
| 3 | Sprint type + sprint view | Low — additive |
| 4 | `time_entries` table + tracking UI | Low — new table, no migration |

---

## Open Questions (remaining)

- Search index — do group-level fields (budget, rates) need to be FTS-searchable? **Deferred.**
- Sprint Jira fields — **resolved**: keep both `jira_id` (numeric Jira ID) and `jira_text` (Jira text key) as separate columns on sprint issues. Reason: both may be needed during import for reliable mapping.

---

## Permission Model (v1.1.1)

PAIMOS uses a **two-layer** permission model:

1. **Role** (on `users.role`) — `admin` / `member` / `external`.
2. **Per-project access level** (on `project_members.access_level`) —
   `none` / `viewer` / `editor`.

| Level    | Read | Write | Notes                                               |
| -------- | ---- | ----- | --------------------------------------------------- |
| `none`   | no   | no    | Explicit denial; overrides the member default.      |
| `viewer` | yes  | no    | Read-only access to the project and its issues.    |
| `editor` | yes  | yes   | Full read + write within the project.              |

**Role defaults** when no `project_members` row exists:
- **admin** — always bypasses per-project checks (effectively editor everywhere).
- **member** — default `editor` on every non-deleted project.
- **external** — default `none`; must be granted explicitly.

**Auto-seeding:**
- `CreateUser` (admin/member) seeds `editor` rows for every non-deleted project.
- `CreateProject` seeds `editor` rows for every active admin/member.
- Migration 64 backfilled existing portal grants as `viewer` and seeded
  admin/member editors on pre-existing projects.

**Access audit** (`access_audit` table) logs grant / update / revoke
events with actor, old level, new level, and timestamp. Admin-only
read via `GET /api/access-audit`.

**Backend enforcement** — see `backend/auth/middleware_project.go` and
`backend/auth/access.go`:
- `RequireProjectView` / `RequireProjectEdit`
- `RequireIssueAccess` / `RequireIssueEdit`
- `RequireAttachmentAccess` / `RequireAttachmentEdit`
- `RequireTimeEntryAccess` / `RequireTimeEntryEdit`
- `RequireCommentAccess` / `RequireCommentEdit`
- Admin-only routes (project CRUD, user CRUD, etc.) use `auth.RequireAdmin`.

Response convention: **404** on no-view access (no existence oracle),
**403** on view-only-when-edit-required.

**Frontend:** `/auth/login`, `/auth/me`, `/auth/totp/verify` return
`{ user, access }`. The Pinia store exposes `canView(pid)` / `canEdit(pid)`
plus a hydrated `accessibleProjects` map. Router per-project guarding
via `meta.projectIdParam`.

See `docs/DEVELOPER_GUIDE.md` section 4a for the implementation walkthrough.

---

## Session & auth columns (v2.7.1+)

Four columns landed in the `v2.7.x` window. None are env-configurable;
all are operator-visible via the API or admin UI.

| Migration | Column                          | Ticket   | Purpose                                                                                                                |
| --------- | ------------------------------- | -------- | ---------------------------------------------------------------------------------------------------------------------- |
| M89       | `sessions.created_at`           | PAI-322  | Anchors the 90-day absolute lifetime cap independent of the sliding `expires_at`.                                       |
| M90       | `users.permissions_epoch`       | PAI-320  | Counter bumped on role / membership / status change. Surfaced as `X-Permissions-Epoch`; mismatch invalidates sessions. |
| M91       | `users.must_change_password`    | PAI-321  | Force-password-change gate. Default `1` for new users; cleared on first successful `POST /auth/password`.              |
| M92       | `users.is_super_admin`          | PAI-335  | Compatibility boolean for legacy super-admin reads.                                                                    |
| M105      | `users.role_key`                | PAI-336  | Canonical public role enum: `admin`, `member`, `external`, `super_admin`; writes mirror into legacy `role`/flag.       |
| M106      | `sessions.actor_user_id`        | PAI-389  | Real operator while a super-admin impersonation session is active.                                                     |
| M106      | `sessions.acting_as_user_id`    | PAI-389  | Effective user while a super-admin impersonation session is active.                                                    |

`sessions` is touched on every authenticated write — keep changes
additive. `users.role` keeps the older SQLite CHECK constraint as a
compatibility shim; application code reads `users.role_key`.

PAI-336 also adds `role_permissions` for seeded role capability checks
and `super_admin_audit` for queryable privileged-action traceability.
PAI-389 extends that audit feed with impersonation start/end rows and
mutating-request rows while the impersonation frame is active.

---

## Knowledge plane + agent attribution (v2.8.x — M93..M101)

The v2.8.x release cycle introduced the knowledge plane (project agents,
five new knowledge issue types, per-(user, device, project) sync watches)
and the agent-attribution split (who/which-session caused each mutation).
All migrations are additive — no data backfill, existing rows stay NULL
or default-valued.

### Knowledge plane on `issues` (M96 — PAI-338)

`issues` gained two columns plus extended CHECK constraints to host
knowledge entries as first-class rows:

| Column | Type | Notes |
|---|---|---|
| `slug` | TEXT NULL | Knowledge-type lookup key. Pattern `[a-z][a-z0-9_-]*`, max 64 chars, application-enforced. NULL on non-knowledge issues. |
| `category_metadata` | TEXT NULL | Per-type tail fields (e.g. `external_system.url`) as JSON-as-text. |

Plus the `type` CHECK now includes `memory`, `runbook`, `external_system`,
`related_project`, `guideline`; the `status` CHECK adds `archived` and
`proposed`. Index: `UNIQUE(type, slug, project_id) WHERE slug IS NOT NULL`
— scoped via partial index so non-knowledge issues stay unconstrained.

### Cross-scope memory + reference tracking (M99 — PAI-345, M100 — PAI-347)

| Migration | Column | Type | Purpose |
|---|---|---|---|
| M99 | `issues.user_id` | INTEGER NULL REFERENCES users(id) | Discriminator for the three memory scopes: `(project_id NOT NULL, user_id NULL)` = project memory; `(project_id NULL, user_id NOT NULL)` = user memory; both NULL = instance memory (admin-only). Enforced application-side. |
| M100 | `issues.reference_count` | INTEGER NOT NULL DEFAULT 0 | Increments on each `paimos session start --bundle full` resolve (PAI-340) and on auto-suggest surface (PAI-342). |
| M100 | `issues.last_referenced_at` | TEXT NULL | Wall-clock of the most recent reference. Pre-M100 rows treated as "freshly referenced" by the stale-proposal logic so the migration day doesn't flood the archive queue. |

Index: `idx_issues_user_type` partial — only rows with `user_id IS NOT NULL`.

### Project agents + inventories (M94 — PAI-326, M95 — PAI-329)

The "what agents work this project" definition lives in project metadata
instead of per-repo local files. Three new tables — one for the declarable
agents themselves, two for inventories the agent artifacts inherit from.

#### `project_agents` (M94 + M95 extensions)

```sql
project_agents (
    id                   INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id           INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name                 TEXT NOT NULL,
    description          TEXT NOT NULL DEFAULT '',
    slash_command_name   TEXT NOT NULL DEFAULT '',
    lane_tags            TEXT NOT NULL DEFAULT '[]',   -- JSON array
    metadata             TEXT NOT NULL DEFAULT '{}',   -- JSON object
    body                 TEXT NOT NULL DEFAULT '',     -- M95: markdown freetext (rendered skill body)
    bootstrap_steps      TEXT NOT NULL DEFAULT '[]',   -- M95: JSON array of {title, command, rationale}
    non_negotiable_rules TEXT NOT NULL DEFAULT '[]',   -- M95: JSON array of {title, body, memory_ref}
    created_at           TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at           TEXT NOT NULL DEFAULT (datetime('now'))
)
```

- UNIQUE INDEX on `(project_id, name)` — one agent name per project.
- `memory_ref` inside `non_negotiable_rules` is just a string here; resolution into an actual memory entry happens at render time (PAI-330).

#### `project_environments` + `project_deploy_recipes` (M95)

Project-level shared inventories the agent artifacts can reference by
name. Same shape — separate tables (mirrors the M75 `project_repos`
precedent: one row per item, no JSON-blob editing dance).

| Table | Key columns | Purpose |
|---|---|---|
| `project_environments` | `name`, `url`, `host_alias`, `host_ip`, `sort_order` | Staging vs prod, named hosts the agent body can address by alias |
| `project_deploy_recipes` | `name`, `command`, `summary`, `sort_order` | Named deployment shorthand the agent body can reference by name |

Both: UNIQUE on `(project_id, name)`; ordering index on `(project_id, sort_order, id)`.

`project_repos` (existing, M75) is the third leg of project-level
inventory and is reused as-is; the canonical agent-artifact endpoint
inlines all three.

### Auto-watch sync subscriptions (M98 — PAI-331)

Per-(user, device, project) opt-in for the sync engine's SSE push
channel. Default OFF — a fresh (device, project) tuple does not
auto-receive updates. PAI-341 (knowledge-plane sync) reuses this table
verbatim; one row covers all kinds for that triple.

```sql
auto_watch_subscriptions (
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    device_id   TEXT NOT NULL,
    project_id  INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    enabled     INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at  TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (user_id, device_id, project_id)
)
```

Toggling `enabled` OFF invalidates the device's active SSE connection
server-side. Subscriptions are managed under
**Settings → Account → Auto-watch sync**.

### Agent/session attribution (M93 — PAI-324, M101 — PAI-354)

Two parallel nullable-column adds to capture who/which-session caused a
mutation, without forcing a backfill:

| Migration | Table | Column |
|---|---|---|
| M93 | `issue_history` | `agent_name TEXT NULL`, `session_id TEXT NULL` |
| M101 | `mutation_log` | `agent_name TEXT NULL` (session_id already arrived via M83) |

Write endpoints persist the values from the `X-Paimos-Agent-Name` and
`X-Paimos-Session-Id` headers when present; existing rows pre-M93/M101
stay NULL. Length cap is enforced application-side (64 chars each,
`handlers.agentAttrCap`) before the INSERT — SQLite `ALTER TABLE` can't
add CHECK retroactively. PAI-209 undo/redo is unaffected; the new
columns are purely informational.

### Report, portal, comment visibility, idempotency, issue counters (M107-M114)

The post-M101 migration ledger is active in `backend/db/db.go` and should stay reflected here:

| Migration | Area | Purpose |
|---|---|---|
| M107 | `project_cooperation`, `project_report_permissions`, `project_report_snapshots` | Projektbericht report metadata, immutable snapshot evidence, report-facing permissions. |
| M108 | `issues.report_summary` | Customer-facing report text used by Projektbericht export and portal acceptance pages. |
| M109 | `CUSTOMERPORTAL` system tag | The marker that makes issues visible in the customer portal. |
| M110 | customer-portal backfill | One-time terminal-status visibility backfill plus `mutation_log` audit rows with `undoable=0`. |
| M111 | `comments.visibility` | Internal vs external comment visibility; new comments default to `internal`. |
| M112 | `idempotency_keys` | Duplicate-prone create-write idempotency cache scoped by key, user, route, and method. |
| M113 | `project_issue_counters`, `idx_issues_project_number_unique` | Atomic per-project issue-number allocation plus a database uniqueness backstop (PAI-554). |
| M114 | `customers.tax_id`, `customers.company_register_number` | Explicit customer legal identifiers: UID/tax number and Firmenbuchnummer/FN (PAI-558). |

PAI-553 tracks the remaining hardening: keep this ledger and the published schema version aligned whenever future migrations land.

---

## Related

- Legacy v0.3.5 schema snapshot: `docs/archive/DATA_MODEL.md`
- Implementation guide: `docs/DEVELOPER_GUIDE.md`
