# PRD: PAIMOS Project Management Online

**Created**: 2026-02-09
**Updated to match current codebase reality**: 2026-04-20
**Version**: 1.1.1

---

## Problem

PAIMOS needs a lightweight, self-hosted project management tool for internal project and issue tracking. Commercial tools are too heavy for the actual scale and create unnecessary operational and cost overhead.

---

## Product Overview

PAIMOS Project Management Online is a minimal self-hosted PM tool with project tracking, hierarchical issues, lightweight collaboration, search, import/export, and basic administration. It is intentionally simpler than Jira, but already broader than a CRUD-only MVP.

### Stack

| Layer | Choice | Rationale |
| --- | --- | --- |
| Frontend | Vue 3 + Vite + TypeScript | Lightweight SPA, fast dev/build, strict typing |
| Backend | Go (chi) | Tiny runtime footprint, simple deployment, one binary |
| Database | SQLite | Zero-overhead persistence, fits target scale |
| Auth | Session cookies + API keys | Simple browser auth, optional programmatic access |
| Security add-on | TOTP 2FA | Optional second factor for user accounts |
| Deploy | Docker on staging/live host | Single service, port 8888 |

### Hardware Constraints (staging host)

| Resource | Available | Budget for PAIMOS |
| --- | --- | --- |
| CPU | Core 2 Duo 2.53GHz | ~50% max |
| RAM | 8GB (6.3GB free) | ~200MB max |
| Disk | 467GB ZFS free | Minimal |

### Scale

- <50 projects/year
- <50 users
- <300 issues/project
- Single-digit concurrent users

---

## Product Goals

- Keep project and issue tracking fast, local, and understandable
- Cover the common internal workflow without enterprise overhead
- Support import/migration from external tools where useful
- Preserve low operational complexity on old hardware

---

## Current Implemented Feature Set

### Projects

- Create, edit, archive, delete projects
- Stable uppercase project keys with suggestion endpoint
- Project tags
- Active-project oriented dashboard/listing

### Issues

- Mixed issue model: group types (`epic`, `cost_unit`, `release`),
  `sprint`, `ticket`, `task`. Group↔ticket and sprint↔ticket are M:N
  via `issue_relations`; ticket→task stays strict 1:1 via `parent_id`.
- Create, edit, delete issues inside projects
- Computed issue keys from project key + issue number
- Fields include:
  - title
  - description
  - acceptance criteria
  - notes
  - status (full billing lifecycle: `new`, `backlog`, `in-progress`,
    `qa`, `done`, `delivered`, `accepted`, `invoiced`, `cancelled`)
  - priority (`low`, `medium`, `high`)
  - assignee
  - budget / rate / estimate fields on group-level types
  - sprint state + date range on sprints
  - release state on releases
- Dependency / impact links via `issue_relations` (typed graph, not free text)
- Tree view and child issue lookups
- Recent issues endpoint
- Issue history snapshots on save

### Collaboration / Metadata

- Comments on issues
- Global tags with attach/detach on issues and projects
- Search across projects, issues, users, and tags

### Users & Auth

- Username/password login with session cookies
- Roles: `admin`, `member`, `external`
- **Per-project access control** (`none` / `viewer` / `editor`)
  layered on top of roles; stored in `project_members`, audited in
  `access_audit`. Admins bypass. Members default to editor on every
  non-deleted project. External users start with no access and must
  be granted explicitly.
- Admin UI: Settings → Users → **Access** button opens a per-project
  matrix editor; Settings → **Permissions** tab renders the capability
  matrix straight from the backend.
- First-run admin seeding only when `ADMIN_PASSWORD` is explicitly set
- Basic in-memory rate limiting on login and TOTP verification
- Password change for authenticated users
- Optional TOTP 2FA
- Personal API keys for programmatic access

### Import / Export / Integration

- CSV export per project
- CSV import with preflight checks
- Global CSV import endpoints
- Jira integration settings storage and connection test
- Jira project listing and Jira issue import into existing or new projects

### Settings / Admin

- Account settings
- User management
- Tag management
- Jira integration management
- Appearance settings

---

## Non-Goals / Not Implemented

These are still not part of the current product based on code review:

- Real-time collaboration / websockets
- Gantt charts
- Mobile app
- OIDC / ID Austria authentication
- Public external API contract with versioning guarantees

(File attachments, time tracking, sprints, and basic SMTP password-reset
emails are all shipped as of v1.0.0.)

---

## API Surface (Current)

Representative current endpoints:

```text
GET    /api/health

POST   /api/auth/login
POST   /api/auth/logout
GET    /api/auth/me
POST   /api/auth/password
GET    /api/auth/totp/status
GET    /api/auth/totp/setup
POST   /api/auth/totp/enable
POST   /api/auth/totp/disable
POST   /api/auth/totp/verify

GET    /api/auth/api-keys
POST   /api/auth/api-keys
DELETE /api/auth/api-keys/:id

GET    /api/projects
POST   /api/projects
GET    /api/projects/:id
PUT    /api/projects/:id
DELETE /api/projects/:id
GET    /api/projects/suggest-key

GET    /api/projects/:id/issues
POST   /api/projects/:id/issues
GET    /api/projects/:id/issues/tree
GET    /api/projects/:id/cost-units
GET    /api/projects/:id/releases
GET    /api/projects/:id/export/csv
POST   /api/projects/:id/import/csv/preflight
POST   /api/projects/:id/import/csv

POST   /api/import/csv/preflight
POST   /api/import/csv

GET    /api/issues/recent
GET    /api/issues/:id
PUT    /api/issues/:id
DELETE /api/issues/:id
GET    /api/issues/:id/children
GET    /api/issues/:id/history
GET    /api/issues/:id/comments
POST   /api/issues/:id/comments
POST   /api/issues/:id/tags
DELETE /api/issues/:id/tags/:tag_id

DELETE /api/comments/:id

GET    /api/users
POST   /api/users
PUT    /api/users/:id

GET    /api/tags
POST   /api/tags
PUT    /api/tags/:id
DELETE /api/tags/:id
POST   /api/projects/:id/tags
DELETE /api/projects/:id/tags/:tag_id

GET    /api/integrations/jira
PUT    /api/integrations/jira
POST   /api/integrations/jira/test
GET    /api/import/jira/projects
POST   /api/import/jira

GET    /api/search
```

Notes:

- Most routes require authentication
- Admin-only write operations exist for users, parts of import, and integrations
- API keys can authenticate via `Authorization: Bearer paimos_...`

---

## UX Scope Today

- Desktop-first responsive web UI
- Login screen plus authenticated app shell
- Dashboard, project list/detail, issue detail, search, settings, and import screens

---

## Data Model Highlights

- SQLite database in `data/paimos.db`
- Additive schema migrations in code
- WAL mode enabled
- Foreign keys enabled
- Core entities:
  - users
  - sessions
  - projects
  - project_members (per-project access level)
  - access_audit (grant / update / revoke log)
  - issues (all types: epic / cost_unit / release / sprint / ticket / task)
  - issue_relations (typed graph: groups / sprint / depends_on / impacts)
  - tags
  - issue_tags
  - project_tags
  - issue_history
  - time_entries
  - attachments
  - comments
  - api_keys
  - integrations (Jira, Mite)
  - totp_pending
  - search_index (FTS5)

See `docs/DATA_MODEL_v2.md` for the full schema; `docs/DATA_MODEL.md` is the
legacy v0.3.5 snapshot kept for archival reference.

---

## Operational Constraints

- Single-container deploy target on staging/live host
- Data persistence matters more than deployment convenience
- Schema migrations must remain additive
- Remote `data/` must not be overwritten during deploy

---

## Acceptance Criteria For Current Reality

- [ ] Projects CRUD works with stable project keys
- [ ] Hierarchical issues CRUD works within projects
- [ ] Search works across core entities
- [ ] Tags can be managed and attached to projects/issues
- [ ] Comments can be added to issues
- [ ] User login/logout works with session cookies
- [ ] Password change works for authenticated users
- [ ] TOTP 2FA can be enabled and used at login
- [ ] Personal API keys can be created and revoked
- [ ] CSV export/import flows work
- [ ] Jira connection test and import work for configured admins
- [ ] Docker container runs on deploy host port 8888
- [ ] SQLite data persists across container restarts
- [ ] Responsive layout works for normal desktop/mobile browser use

---

## Obvious Gaps / Missing Decisions

These are important gaps surfaced by the current code/doc audit.

### Security / Hardening Gaps

- No documented CSRF strategy for cookie-authenticated browser requests
- No documented rate limiting / login throttling / API abuse controls
- Session cookies are `HttpOnly` and `SameSite=Lax`, but there is no documented `Secure` cookie behavior for production/TLS
- Jira credentials are stored in SQLite config JSON; no encryption-at-rest story is documented
- No documented API key expiry, rotation, or forced revocation policy
- No documented audit/security event logging policy
- Default seeded admin password behavior exists if no `ADMIN_PASSWORD` is set; operational docs should make this impossible in real deployments

### Ops / Reliability Gaps

- No clear backup/restore runbook for SQLite data
- No documented disaster recovery or migration rollback procedure
- No explicit retention policy for sessions, issue history, comments, or pending TOTP tokens
- No documented observability baseline beyond app logs

### Product / Process Gaps

- PRD now reflects what exists, but feature ownership and priority beyond current scope are still vague
- No clear policy for public/stable API support vs internal-only endpoints
- No import/export compatibility guarantees documented

---

## Future Considerations

- OIDC / ID Austria authentication
- Stronger admin/security controls
- Better backup/restore tooling and docs
- More explicit API contract/versioning if external automation grows
- Richer import/export mapping and validation

---

## Related

- Deep implementation guide: `docs/DEVELOPER_GUIDE.md`
- Deployment target: Docker on staging/live host, port 8888
- Repository: `github.com/markus-barta/paimos`
- Backlog / task tracking: PAIMOS live instance — `https://paimos.com/projects/2` (SSOT)
