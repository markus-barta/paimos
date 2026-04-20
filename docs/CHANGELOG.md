# Changelog

All notable changes to PAIMOS are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and PAIMOS adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.1.0] — 2026-04-20

### Added

- **Per-project access control.** Every user now has a per-project
  access level (`viewer`, `editor`, or `none`) stored in a new
  `project_members` table. Admins bypass. Members default to editor on
  every non-deleted project. External users get no access until granted
  explicitly. Access changes are written to a new `access_audit` log.
- **Settings → Permissions** tab renders the capability matrix
  (viewer vs. editor vs. admin) straight from the backend, so the
  documentation stays in lockstep with the code.
- **Settings → Users → Access** button opens a per-project matrix
  editor (`viewer` / `editor` / `none`) for any user, backed by the new
  `GET/PUT/DELETE /api/users/{id}/memberships` endpoints.
- `GET /api/permissions/matrix` — public to logged-in users for UI
  rendering.
- `GET /api/access-audit` — admin-only audit trail of grant / update /
  revoke events.
- Backend helpers: `auth.CanViewProject`, `auth.CanEditProject`,
  `auth.AccessibleProjectIDs`, `auth.WithAccessCache`. Frontend store
  exposes `canView()` and `canEdit()` helpers plus a hydrated
  `accessibleProjects` map.
- Per-project chi middlewares: `RequireProjectView`,
  `RequireProjectEdit`, `RequireIssueAccess`, `RequireAttachmentAccess`,
  `RequireTimeEntryAccess`, `RequireCommentAccess` (and their `Edit`
  counterparts). 404 on no-view; 403 on view-only; bypasses for
  orphan sprints.

### Changed

- `/auth/login`, `/auth/me`, and `/auth/totp/verify` now return a
  `{ "user": {...}, "access": {...} }` envelope. The `access` field
  hydrates the frontend's per-project permission cache in a single
  round-trip.
- `CreateUser` (internal roles) and `CreateProject` auto-seed editor
  rows in `project_members` so internal users never lose visibility on
  projects they didn't pre-exist.
- Cross-cutting list endpoints (`/api/projects`, `/api/issues`,
  `/api/issues/recent`, `/api/cost-units`, `/api/releases`,
  `/api/search`, `/api/users/me/recent-projects`) are now filtered by
  the caller's accessible project set.
- `ListIssueRelations` redacts the target title/key (`"RESTRICTED"`)
  when the relation's target lives in a project the caller can't view.

### Removed

- `user_project_access` table. Migration 65 drops it after a
  safety re-insert into `project_members`.

## [1.0.0] — 2026-04-19

### Initial release

PAIMOS v1.0.0 is the first public release. It provides a self-hosted
project management system with first-class support for tracking both
humans and AI agents as participants.

**Core features**

- Hierarchical issues (epic → ticket → task) with parent-child relations
- Rich issue metadata: status, priority, assignee, cost unit, release,
  dependencies, impacts, tags, attachments, comments, history
- Sprints, accruals, cost units, releases
- Per-user time tracking with inline timers and billing lifecycle
- Full-text search with partial issue-key matching
- Custom views (saved filter + column sets), per-user view ordering
- Admin panel: users, projects, tags, integrations
- External-user portal with read-only projects + acceptance workflow
- PDF delivery reports (Lieferbericht) with configurable column layout

**Security**

- Session cookies (HttpOnly, SameSite=Lax, optional Secure)
- TOTP 2FA with QR setup, reset, disable
- API keys (`paimos_` prefix, sha256-hashed storage)
- Magic-link password reset with 60-minute token TTL
- Rate-limited auth endpoints (login / forgot / reset / totp-verify)

**Integrations**

- Jira import (project list, field mapping, issue relations)
- Mite time-tracking import (DE/AT; user mapping via note field)
- CSV import/export (per-project and cross-project)

**Branding & configuration**

- **Live-editable branding** via admin-only **Settings → Branding** tab:
  edit product name, tagline, website, page title, full colour palette,
  upload custom logo + favicon — all applied live with no restart.
- Admin API: `PUT /api/branding`, `POST /api/branding/logo`,
  `POST /api/branding/favicon`. Public `GET /brand/<filename>` for
  pre-auth login-page assets (SVG served with restrictive CSP).
- Runtime branding config at `$DATA_DIR/branding.json`, plus
  `branding-<slug>.json` for multi-brand installs.
- Operator-configurable identity via `BRAND_*` env vars (product name,
  company, website, TOTP issuer, email from, API key prefix, DB filename,
  MinIO bucket — see `docs/CONFIGURATION.md`).
- Optional MinIO-backed attachments (graceful-disable when unset).
- Optional SMTP for password-reset emails (dev mode logs links to stdout).

### Provenance

PAIMOS is forked from an internal prototype and published under AGPL-3.0.
All upstream branding, CI, and deployment infrastructure have been removed.
Git history starts fresh with this release.
