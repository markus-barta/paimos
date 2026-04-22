# PAIMOS Data Model (Legacy — v0.3.5 snapshot)

> ⚠️ **This document is a historical snapshot of the pre-v1 schema (migrations 1–10).**
> The current schema — including the per-project access-control tables
> (`project_members`, `access_audit`), `time_entries`, `attachments`,
> `issue_relations`, sprints, the renamed status values
> (`backlog` / `in-progress` / `complete` / `canceled`), and much more —
> is documented in [`../DATA_MODEL.md`](../DATA_MODEL.md).
>
> For anything other than archaeology, read `../DATA_MODEL.md` and
> `backend/db/db.go` (the source of truth).

**Version**: 0.3.5  
**Last updated**: 2026-03-06  
**Schema source of truth (now)**: `backend/db/db.go` — all migrations are applied in order on startup.

---

## Entity-Relationship Diagram

```
┌─────────────────────────────────────────────────────────────────────────────────┐
│                                                                                 │
│   USERS                          PROJECTS                                       │
│   ─────────────────────          ──────────────────────────────                 │
│   id          PK  INTEGER        id          PK  INTEGER                        │
│   username    UNIQUE TEXT        name            TEXT                           │
│   password        TEXT           key         UNIQUE TEXT  (e.g. ACME)          │
│   role            TEXT           description     TEXT                           │
│     ∈ {admin,member}             status          TEXT                           │
│   status          TEXT             ∈ {active, archived, deleted}                │
│     ∈ {active,                   created_at      TEXT                           │
│         inactive,                updated_at      TEXT                           │
│         deleted}                                                                │
│   totp_secret     TEXT           │                 │                            │
│   totp_enabled    INTEGER        │                 │                            │
│   created_at      TEXT           │   1             │  1                         │
│        │                         │   │             │  │                         │
│        │ 1                       │   │             │  │                         │
│        ├──────────────────────────── │             │  │                         │
│        │            ┌──────────────────────────────┘  │                         │
│        │            │           N └────────────────── │                         │
│        │            │                                 │ N                       │
│        │            ▼                                 ▼                         │
│        │   ISSUES                         PROJECT_TAGS (join)                   │
│        │   ─────────────────────────────  ────────────────────────              │
│        │   id          PK  INTEGER        project_id FK→projects                │
│        │   project_id  FK→projects        tag_id     FK→tags                    │
│        │     ON DELETE CASCADE            PK(project_id, tag_id)                │
│        │   issue_number     INTEGER       │                                     │
│        │   issue_key  (computed)          │                                     │
│        │   type             TEXT          │                                     │
│        │     ∈ {epic,ticket,task}         │                                     │
│        │   parent_id  FK→issues NULL      │                                     │
│        │     ON DELETE SET NULL           │                                     │
│        │   title            TEXT          │                                     │
│        │   description      TEXT          │                                     │
│        │   acceptance_criteria TEXT       │           TAGS                      │
│        │   notes            TEXT          │           ──────────────────────    │
│        │   status           TEXT          │           id          PK INTEGER    │
│        │     ∈ {open,in-progress,         │           name    UNIQUE TEXT       │
│        │         done,closed}             │           color        TEXT         │
│        │   priority         TEXT          │           description  TEXT         │
│        │     ∈ {low,medium,high}          │           created_at   TEXT         │
│        │   cost_unit        TEXT          │                 │                   │
│        │   release          TEXT          │                 │ N                 │
│        │   depends_on       TEXT          │                 │                   │
│        │     (free-text issue keys,       └─────────────────┤                   │
│        │      e.g. "ACME-1, ACME-3")                        │                   │
│        │   impacts          TEXT                            │                   │
│        │     (free-text issue keys)       ISSUE_TAGS (join) │                   │
│        │   assignee_id  FK→users NULL     ────────────────────────              │
│        │     ON DELETE SET NULL           issue_id  FK→issues                   │
│        │   created_at       TEXT          tag_id    FK→tags                     │
│        │   updated_at       TEXT          PK(issue_id, tag_id)                  │
│        │         │                                                              │
│        │         │ 1─────────────────────────────────────────────────┐          │
│        │         │                                                    │         │
│        │         ├──────────────────┐                                 │         │
│        │         │ 1                │ 1                               │         │
│        │         │                  │                                 │         │
│        │         ▼ N                ▼ N                               │ N       │
│        │  ISSUE_HISTORY       COMMENTS                      ISSUE_TAGS (above)  │
│        │  ──────────────────  ──────────────────────────                        │
│        │  id      PK INTEGER  id        PK INTEGER                              │
│        │  issue_id FK→issues  issue_id  FK→issues                               │
│        │    ON DELETE CASCADE   ON DELETE CASCADE                               │
│        │  changed_by FK→users author_id FK→users NULL                           │
│        │    ON DELETE SET NULL   ON DELETE SET NULL                             │
│        │  snapshot  TEXT JSON  body      TEXT                                   │
│        │  changed_at  TEXT     created_at TEXT                                  │
│        │                                                                        │
│        │ 1                                                                      │
│        ├───────────────────────────────────────────┐                            │
│        │                                           │ N                          │
│        ▼ N                                         │                            │
│   SESSIONS                                    API_KEYS                          │
│   ─────────────────────────                   ──────────────────────────        │
│   id (hex)  PK TEXT                           id          PK INTEGER            │
│   user_id   FK→users                          user_id     FK→users              │
│     ON DELETE CASCADE                           ON DELETE CASCADE               │
│   expires_at    TEXT                          name            TEXT              │
│                                               key_hash    UNIQUE TEXT           │
│                                               key_prefix      TEXT              │
│                                               created_at      TEXT              │
│                                               last_used_at    TEXT NULL         │
│                                                                                 │
│   TOTP_PENDING                INTEGRATIONS              SEARCH_INDEX (FTS5)     │
│   ─────────────────────────   ──────────────────────    ─────────────────────   │
│   token  PK TEXT              id      PK INTEGER        entity_type  TEXT       │
│   user_id FK→users            provider UNIQUE TEXT      entity_id    INTEGER    │
│     ON DELETE CASCADE         config   TEXT JSON        content      TEXT       │
│   expires_at    TEXT          updated_at   TEXT                                 │
│                                                                                 │
└─────────────────────────────────────────────────────────────────────────────────┘
```

---

## Tables — Full Column Reference

### `users`

| Column         | Type    | Constraints                                                 | Notes                                                     |
| -------------- | ------- | ----------------------------------------------------------- | --------------------------------------------------------- |
| `id`           | INTEGER | PK AUTOINCREMENT                                            |                                                           |
| `username`     | TEXT    | NOT NULL UNIQUE                                             |                                                           |
| `password`     | TEXT    | NOT NULL                                                    | bcrypt hash                                               |
| `role`         | TEXT    | NOT NULL DEFAULT 'member' CHECK(role IN ('admin','member')) |                                                           |
| `status`       | TEXT    | NOT NULL DEFAULT 'active'                                   | `active` / `inactive` / `deleted` — enforced in app logic |
| `totp_secret`  | TEXT    | NOT NULL DEFAULT ''                                         | TOTP base32 secret; empty if not set up                   |
| `totp_enabled` | INTEGER | NOT NULL DEFAULT 0                                          | 0 = disabled, 1 = enabled                                 |
| `created_at`   | TEXT    | NOT NULL DEFAULT datetime('now')                            | ISO 8601                                                  |

**Status semantics:**

- `active` — normal login, visible everywhere
- `inactive` — login blocked, data preserved, shown as "Disabled" in admin UI
- `deleted` — login blocked, hidden from normal UI, restorable via `UPDATE users SET status='active'`

---

### `sessions`

| Column       | Type    | Constraints                         | Notes                 |
| ------------ | ------- | ----------------------------------- | --------------------- |
| `id`         | TEXT    | PK                                  | Random 32-char hex    |
| `user_id`    | INTEGER | NOT NULL FK→users ON DELETE CASCADE |                       |
| `expires_at` | TEXT    | NOT NULL                            | 24h TTL from creation |

---

### `totp_pending`

| Column       | Type    | Constraints                         | Notes                          |
| ------------ | ------- | ----------------------------------- | ------------------------------ |
| `token`      | TEXT    | PK                                  | Short-lived login step-2 token |
| `user_id`    | INTEGER | NOT NULL FK→users ON DELETE CASCADE |                                |
| `expires_at` | TEXT    | NOT NULL                            | 5min TTL                       |

---

### `api_keys`

| Column         | Type    | Constraints                         | Notes                           |
| -------------- | ------- | ----------------------------------- | ------------------------------- |
| `id`           | INTEGER | PK AUTOINCREMENT                    |                                 |
| `user_id`      | INTEGER | NOT NULL FK→users ON DELETE CASCADE |                                 |
| `name`         | TEXT    | NOT NULL                            | Human label                     |
| `key_hash`     | TEXT    | NOT NULL UNIQUE                     | SHA-256 of raw key              |
| `key_prefix`   | TEXT    | NOT NULL                            | First 8 chars for display       |
| `created_at`   | TEXT    | NOT NULL DEFAULT datetime('now')    |                                 |
| `last_used_at` | TEXT    | NULL                                | Updated on each successful auth |

---

### `projects`

| Column        | Type    | Constraints                      | Notes                                                     |
| ------------- | ------- | -------------------------------- | --------------------------------------------------------- |
| `id`          | INTEGER | PK AUTOINCREMENT                 |                                                           |
| `name`        | TEXT    | NOT NULL                         |                                                           |
| `key`         | TEXT    | NOT NULL UNIQUE DEFAULT ''       | Uppercase alphanumeric, 3–10 chars (e.g. `ACME`)         |
| `description` | TEXT    | NOT NULL DEFAULT ''              |                                                           |
| `status`      | TEXT    | NOT NULL DEFAULT 'active'        | `active` / `archived` / `deleted` — enforced in app logic |
| `created_at`  | TEXT    | NOT NULL DEFAULT datetime('now') |                                                           |
| `updated_at`  | TEXT    | NOT NULL DEFAULT datetime('now') |                                                           |

**Status semantics:**

- `active` — normal, visible in project lists
- `archived` — intentionally closed, still visible with badge
- `deleted` — hidden from all UI, restorable via `UPDATE projects SET status='active'`

---

### `issues`

| Column                | Type    | Constraints                            | Notes                                          |
| --------------------- | ------- | -------------------------------------- | ---------------------------------------------- |
| `id`                  | INTEGER | PK AUTOINCREMENT                       |                                                |
| `project_id`          | INTEGER | NOT NULL FK→projects ON DELETE CASCADE |                                                |
| `issue_number`        | INTEGER | NOT NULL DEFAULT 0                     | Scoped per-project; forms the issue key        |
| `type`                | TEXT    | NOT NULL DEFAULT 'ticket'              | `epic` / `ticket` / `task`                     |
| `parent_id`           | INTEGER | FK→issues ON DELETE SET NULL NULL      | Self-referential hierarchy                     |
| `title`               | TEXT    | NOT NULL                               |                                                |
| `description`         | TEXT    | NOT NULL DEFAULT ''                    |                                                |
| `acceptance_criteria` | TEXT    | NOT NULL DEFAULT ''                    |                                                |
| `notes`               | TEXT    | NOT NULL DEFAULT ''                    |                                                |
| `status`              | TEXT    | NOT NULL DEFAULT 'open' CHECK(...)     | `open` / `in-progress` / `done` / `closed`     |
| `priority`            | TEXT    | NOT NULL DEFAULT 'medium' CHECK(...)   | `low` / `medium` / `high`                      |
| `cost_unit`           | TEXT    | NOT NULL DEFAULT ''                    | Free-text cost/billing category                |
| `release`             | TEXT    | NOT NULL DEFAULT ''                    | Free-text release tag                          |
| `depends_on`          | TEXT    | NOT NULL DEFAULT ''                    | Free-text issue-key list e.g. `ACME-1, ACME-3` |
| `impacts`             | TEXT    | NOT NULL DEFAULT ''                    | Free-text issue-key list                       |
| `assignee_id`         | INTEGER | FK→users ON DELETE SET NULL NULL       |                                                |
| `created_at`          | TEXT    | NOT NULL DEFAULT datetime('now')       |                                                |
| `updated_at`          | TEXT    | NOT NULL DEFAULT datetime('now')       |                                                |

**Computed field (not in DB):**

- `issue_key` — `project.key + "-" + issue_number` assembled in Go at query time

**Hierarchy rules (enforced in application logic):**

```
epic    → can have no parent
ticket  → parent must be an epic (or null for orphan tickets)
task    → parent must be a ticket (or null for orphan tasks)
```

**Indexes:**

- `idx_issues_project` on `(project_id)`
- `idx_issues_project_number` UNIQUE on `(project_id, issue_number)`
- `idx_issues_parent` on `(parent_id)`
- `idx_issues_type` on `(type)`
- `idx_issues_costunit` on `(cost_unit)`
- `idx_issues_release` on `(release)`

---

### `issue_history`

| Column       | Type    | Constraints                          | Notes                           |
| ------------ | ------- | ------------------------------------ | ------------------------------- |
| `id`         | INTEGER | PK AUTOINCREMENT                     |                                 |
| `issue_id`   | INTEGER | NOT NULL FK→issues ON DELETE CASCADE |                                 |
| `changed_by` | INTEGER | FK→users ON DELETE SET NULL NULL     |                                 |
| `snapshot`   | TEXT    | NOT NULL                             | Full JSON of issue at save time |
| `changed_at` | TEXT    | NOT NULL DEFAULT datetime('now')     |                                 |

---

### `comments`

| Column       | Type    | Constraints                          | Notes                    |
| ------------ | ------- | ------------------------------------ | ------------------------ |
| `id`         | INTEGER | PK AUTOINCREMENT                     |                          |
| `issue_id`   | INTEGER | NOT NULL FK→issues ON DELETE CASCADE |                          |
| `author_id`  | INTEGER | FK→users ON DELETE SET NULL NULL     | NULL after user deletion |
| `body`       | TEXT    | NOT NULL                             |                          |
| `created_at` | TEXT    | NOT NULL DEFAULT datetime('now')     |                          |

---

### `tags`

| Column        | Type    | Constraints                      | Notes                    |
| ------------- | ------- | -------------------------------- | ------------------------ |
| `id`          | INTEGER | PK AUTOINCREMENT                 |                          |
| `name`        | TEXT    | NOT NULL UNIQUE                  |                          |
| `color`       | TEXT    | NOT NULL DEFAULT 'gray'          | Token from fixed palette |
| `description` | TEXT    | NOT NULL DEFAULT ''              |                          |
| `created_at`  | TEXT    | NOT NULL DEFAULT datetime('now') |                          |

---

### `issue_tags` (join table)

| Column     | Type    | Constraints                 |
| ---------- | ------- | --------------------------- |
| `issue_id` | INTEGER | FK→issues ON DELETE CASCADE |
| `tag_id`   | INTEGER | FK→tags ON DELETE CASCADE   |
| PK         |         | `(issue_id, tag_id)`        |

---

### `project_tags` (join table)

| Column       | Type    | Constraints                   |
| ------------ | ------- | ----------------------------- |
| `project_id` | INTEGER | FK→projects ON DELETE CASCADE |
| `tag_id`     | INTEGER | FK→tags ON DELETE CASCADE     |
| PK           |         | `(project_id, tag_id)`        |

---

### `integrations`

| Column       | Type    | Constraints                      | Notes                                     |
| ------------ | ------- | -------------------------------- | ----------------------------------------- |
| `id`         | INTEGER | PK AUTOINCREMENT                 |                                           |
| `provider`   | TEXT    | NOT NULL UNIQUE                  | e.g. `jira`                               |
| `config`     | TEXT    | NOT NULL DEFAULT '{}'            | JSON blob with provider-specific settings |
| `updated_at` | TEXT    | NOT NULL DEFAULT datetime('now') |                                           |

Current providers: `jira` only. Config keys for Jira: `host`, `email`, `token` (stored plain in JSON — see security gap `ACME-1`).

---

### `search_index` (FTS5 virtual table)

| Column        | Type    | Notes                                |
| ------------- | ------- | ------------------------------------ |
| `entity_type` | TEXT    | `project` / `issue` / `user` / `tag` |
| `entity_id`   | INTEGER | UNINDEXED — used for result lookup   |
| `content`     | TEXT    | Space-joined searchable fields       |

Tokenizer: `porter ascii` (English stemming).  
Kept in sync with 12 triggers on `projects`, `issues`, `users`, `tags` (`_ai`, `_au`, `_ad` × 4 entities) plus duplicate triggers `_ai2` / `_au2` / `_ad2` on the recreated `projects` table (migration 10).

---

## Key Relationships Summary

```
users ──1──< sessions
users ──1──< totp_pending
users ──1──< api_keys
users ──1──< issues (as assignee)
users ──1──< issue_history (as changed_by)
users ──1──< comments (as author)

projects ──1──< issues
projects ──M──< project_tags >──M── tags

issues ──1──< issues (parent/child self-join)
issues ──1──< issue_tags >──M── tags
issues ──1──< issue_history
issues ──1──< comments
```

---

## Soft Delete Model

Both `users` and `projects` use a three-phase soft delete pattern:

| Phase                                  | `status` value          | Login | Visible in UI | Restorable        |
| -------------------------------------- | ----------------------- | ----- | ------------- | ----------------- |
| Normal                                 | `active`                | ✓     | ✓             | —                 |
| Disabled (users) / Archived (projects) | `inactive` / `archived` | ✗ / — | admin view    | ✓ one click       |
| Soft-deleted                           | `deleted`               | ✗     | hidden        | ✓ DB command only |

DB restore commands:

```sql
-- restore a user
UPDATE users SET status='active' WHERE username = 'alice';

-- restore a project
UPDATE projects SET status='active' WHERE key = 'ACME';
```

UI restore view is tracked as backlog item `ACME-1`.

---

## Migration History

| Version | Applied    | Summary                                                                                                                                                   |
| ------- | ---------- | --------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1       | 2026-03-04 | Base schema: users, sessions, projects, issues                                                                                                            |
| 2       | 2026-03-04 | projects.key; issues: issue_number, type, parent_id, acceptance_criteria, notes, cost_unit, release; indexes                                              |
| 3       | 2026-03-04 | tags, issue_tags, project_tags; FTS5 search_index + 12 triggers; backfill                                                                                 |
| 4       | 2026-03-04 | issues.depends_on, issues.impacts                                                                                                                         |
| 5       | 2026-03-04 | issue_history table                                                                                                                                       |
| 6       | 2026-03-04 | users.totp_secret, totp_enabled; totp_pending table                                                                                                       |
| 7       | 2026-03-05 | api_keys table                                                                                                                                            |
| 8       | 2026-03-05 | integrations table                                                                                                                                        |
| 9       | 2026-03-05 | comments table                                                                                                                                            |
| 10      | 2026-03-06 | users.status (active/inactive/deleted); projects table recreated to remove restrictive CHECK on status, FK disabled during DROP to prevent cascade delete |

---

## Notes for Refactoring

This section captures known model limitations and areas likely to evolve:

- `depends_on` and `impacts` are free-text fields containing issue key references (e.g. `ACME-1, ACME-3`). They are not FK-backed relations — no referential integrity, no cascades, no query performance. If these become first-class features, they should move to a separate `issue_relations` table.
- `issue_key` is computed at query time in Go (`project.key + "-" + issue_number`). It is not stored in the DB. This means FTS search does not index issue keys directly.
- `cost_unit` and `release` are free-text; they could be normalized into lookup tables if filtering/reporting grows.
- `integrations.config` is a raw JSON blob — typed config tables per provider would be cleaner for schema validation.
- No timestamps on `sessions`, `issue_tags`, `project_tags` — useful for audit if needed.
- `users.password` and `integrations.config` (Jira token) are stored plain in SQLite — encryption at rest is a known gap (`ACME-1`).
