# PAIMOS Data Model v2

**Status**: Implemented (v0.4.x)  
**Date**: 2026-03-06  
**Basis**: Team meeting whiteboard + discussion with Markus  
**Previous model**: `docs/DATA_MODEL.md` (v0.3.5-poc baseline)

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

All issue types use the same status values:

| v1 status | v2 status | Notes |
|-----------|-----------|-------|
| `open` | `backlog` | Renamed |
| `in-progress` | `in-progress` | Unchanged |
| `done` | `complete` | Renamed |
| `closed` | `canceled` | Renamed — semantics change from "done+closed" to explicitly canceled |

Additional type-specific states live in separate fields, not in `status`:
- `group_state` on releases: `unreleased` / `released`
- `sprint_state` on sprints: `planned` / `active` / `complete`

---

## Project Fields (additions)

| Field | Type | Notes |
|-------|------|-------|
| `product_owner` | INTEGER NULL | FK→users — project lead |
| `customer_id` | TEXT | External customer reference |

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

- Only tracks time on tickets, not tasks or groups
- A running timer has `stopped_at = NULL`
- `override` allows manual correction without deleting the entry
- Implementation deferred but schema is planned

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

## Permission Model (Model A — v0.4.4)

Two roles: `admin` and `member`.

| Action | Member | Admin |
|--------|--------|-------|
| Create issue | ✅ | ✅ |
| Edit issue | ✅ | ✅ |
| Delete issue | ❌ | ✅ |
| Delete issue relation | ❌ | ✅ |
| Add/remove tags (issues & projects) | ✅ | ✅ |
| Log time entry | ✅ | ✅ |
| Delete own time entry | ✅ | ✅ |
| Delete others' time entries | ❌ | ✅ |
| Create comment | ✅ | ✅ |
| Delete own comment | ✅ | ✅ |
| Delete others' comments | ❌ | ✅ |
| Create/edit/delete project | ❌ | ✅ |

**Backend enforcement:**
- `DELETE /issues/{id}` — `auth.RequireAdmin` middleware (`backend/main.go`)
- `DELETE /issues/{id}/relations` — `auth.RequireAdmin` middleware (`backend/main.go`)
- `DELETE /time_entries/{id}` — own-or-admin check in handler (`backend/handlers/time_entries.go`)
- `DELETE /comments/{id}` — own-or-admin check in handler (`backend/handlers/comments.go`)
- `POST/PUT/DELETE /projects` — `auth.RequireAdmin` middleware (`backend/main.go`)

**Frontend enforcement (UI hide):** Delete buttons hidden for members in `IssueList.vue` and `IssueDetailView.vue`.

---

## Related

- Current data model: `docs/DATA_MODEL.md`
