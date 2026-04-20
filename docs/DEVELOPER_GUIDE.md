# PAIMOS Developer Guide

For operator-facing configuration, see [`CONFIGURATION.md`](CONFIGURATION.md).
For contribution mechanics (DCO, PR flow, review criteria), see
[`CONTRIBUTING.md`](../CONTRIBUTING.md).

This guide is for people who want to understand, extend, or debug
PAIMOS's internals.

## 1. Architecture

```
Browser (Vue 3 SPA, Vite)
  ↕ JSON via /api/*
Go server :8888
  ├── chi router
  │    ├── /api/auth/*           session + TOTP + API keys
  │    ├── /api/projects/*       project CRUD + issues + reports
  │    ├── /api/issues/*         cross-project issue ops
  │    ├── /api/portal/*         external-role read-mostly view
  │    ├── /api/integrations/*   Jira + Mite config
  │    ├── /api/import/*         CSV + Jira + Mite async imports
  │    └── /api/(branding|health|instance|…)  meta
  ├── SQLite (WAL, single file at $DATA_DIR/paimos.db)
  ├── optional MinIO client (attachments)
  └── optional net/smtp client (password reset)
```

One process, one port. The Go binary serves both the API and the
built SPA (from `$STATIC_DIR`). No frontend dev server in production.

## 2. Repo layout

```
backend/
  main.go              entrypoint; all routes wired here
  brand/               BRAND_* env parsing (single source of truth)
  auth/                sessions, password hashing, TOTP, API keys, rate limiting
  db/                  SQLite open + migrations (each migration inline in db.go)
  models/              shared structs (User, Issue, Project, …)
  handlers/            HTTP handlers; one file per domain
    assets/            embedded files (PDF fonts, report logo)
  storage/             MinIO/S3 client (Init + Put/Get/Delete)
frontend/
  src/
    views/             top-level routed pages
    components/        shared UI pieces (MetaSelect, IssueTree, …)
    composables/       reusable Vue 3 composables (useIssueFilter, useBranding, …)
    api/               thin fetch wrappers per domain
    i18n/              vue-i18n message catalogs
  public/              static assets (logo.svg, favicon.svg, app-icon.svg)
docs/
  CONFIGURATION.md     every env var
  CHANGELOG.md         release notes
  DEVELOPER_GUIDE.md   this file
  DATA_MODEL_v2.md     current schema (tables, relations, enums)
  DATA_MODEL.md        legacy v0.3.5 snapshot — archival only
  AGENT_INTEGRATION.md authenticated API usage for agents
  api-minimal.md       compact API surface reference
  brand/               visual identity (mark + wordmark + brand guide)
+agents/rules/         rules agents follow when editing this codebase
+pm/                   product framing (PRD, roadmap)
scripts/               maintenance helpers
```

## 3. Backend conventions

- **Handlers** are thin HTTP→DB adapters. Complex logic lives in
  sibling files in `backend/handlers/` (e.g., `import_engine.go`,
  `imageutil.go`).
- **DB access** is direct `sql.DB` — no ORM. Prepared statements are
  compiled inline via `db.DB.Query(…)`. Makes it easy to read the exact
  query in context.
- **Migrations** live in `backend/db/db.go` inside `migrate()`. Each
  one is `db.Exec("CREATE TABLE IF NOT EXISTS …")` guarded by the
  `schema_versions` table. Additive-only — never rewrite a past
  migration.
- **Context propagation**: standard Go `context.Context`; auth loads
  the user into context via middleware in `backend/auth/`.
- **Auth**: session cookies by default, `Authorization: Bearer
  paimos_…` API keys checked first. Both paths resolve to the same
  `*models.User` attached to the request context.
- **Error envelopes**: handlers call `jsonError(w, msg, status)` for
  errors and `jsonOK(w, payload)` for success. No panic recovery
  beyond chi's default `middleware.Recoverer`.

## 4. Database

- SQLite only. `WAL` journal mode, 5-second busy timeout, foreign
  keys on.
- Schema migrations are idempotent and run at every startup.
- Default filename is `paimos.db` (override with `BRAND_DB_FILENAME`;
  see caveats in `CONFIGURATION.md`).
- See [`DATA_MODEL_v2.md`](DATA_MODEL_v2.md) for the full schema.

## 4a. Access model (per-project)

PAIMOS uses a three-level per-project access model, stored in the
`project_members` table:

| Level   | Read | Write | Notes                                           |
| ------- | ---- | ----- | ----------------------------------------------- |
| `none`  | no   | no    | Explicit denial; overrides the member default. |
| `viewer`| yes  | no    | Read-only access to the project and its issues.|
| `editor`| yes  | yes   | Full read + write within the project.          |

**Role defaults** (applied when no `project_members` row exists):

- **admin** — always bypasses per-project checks (effectively editor
  everywhere).
- **member** — default `editor` on every non-deleted project.
- **external** — default `none`; must be granted explicitly.

Seeding is automatic:

- `CreateUser` (for admin/member roles) seeds `editor` rows for every
  non-deleted project.
- `CreateProject` seeds `editor` rows for every active admin/member.
- Migration 64 backfills existing portal grants as `viewer` and seeds
  admin/member editors on pre-existing projects.

**API surface**:

- `GET  /api/permissions/matrix` — capabilities × levels matrix for UI.
- `GET  /api/users/{id}/memberships` — per-project effective level.
- `PUT  /api/users/{id}/memberships/{projectId}` — upsert a grant.
- `DELETE /api/users/{id}/memberships/{projectId}` — revert to default.
- `GET  /api/access-audit` — admin-only audit trail.

**Go helpers** (in `backend/auth/access.go`):

- `auth.CanViewProject(r, projectID) bool`
- `auth.CanEditProject(r, projectID) bool`
- `auth.ProjectAccessLevel(r, projectID) AccessLevel`
- `auth.AccessibleProjectIDs(r) []int64` (returns nil for admin)
- `auth.WithAccessCache(ctx)` — attaches a per-request memoization cache
  that batches the lookup; already applied by `auth.Middleware`.

**Middleware** (in `backend/auth/middleware_project.go`):

- `RequireProjectView` / `RequireProjectEdit` — gates a chi route with
  a `{id}` (or `{projectId}`) param on view or edit access.
- `RequireIssueAccess` / `RequireIssueEdit` — resolves the project via
  `issues.project_id`; orphan sprint issues pass through.
- `RequireAttachmentAccess` / `RequireAttachmentEdit`
- `RequireTimeEntryAccess` / `RequireTimeEntryEdit`
- `RequireCommentAccess` / `RequireCommentEdit`

Response convention: 404 on no-view access (no existence oracle), 403
on view-only-when-edit-required.

**Frontend access**:

`/auth/login`, `/auth/me`, and `/auth/totp/verify` return an envelope
`{ user, access }`. `access = { all_projects, levels: { [pid]: "viewer"|"editor" } }`.
The Pinia store (`stores/auth.ts`) exposes `canView(pid)` and
`canEdit(pid)` plus a hydrated `accessibleProjects` Map. Router routes
opt in to per-project guarding by setting
`meta.projectIdParam: 'id'`.

## 5. Frontend conventions

- **Vue 3 + `<script setup lang="ts">`** for all new components.
- **Strict TypeScript** — `npm run typecheck` must pass.
- **State**: Pinia stores for cross-view state; local `ref`/`reactive`
  for component state. No Vuex.
- **Routing**: `vue-router`, lazy-loaded views.
- **i18n**: `vue-i18n`; English and German catalogs in `src/i18n/`.
- **Styling**: scoped `<style>` blocks + a small set of CSS custom
  properties fed from the branding API.
- **localStorage**: all keys prefixed with `paimos:` for namespacing.

## 6. Testing

- **Backend**: `cd backend && go test ./...` — covers handlers,
  auth, imports, sprint logic, reports.
- **Frontend**: `cd frontend && npm test` (Vitest + happy-dom) —
  covers the thin API client and a handful of critical components.
- **Smoke test**: `docker compose up --build`, then visit
  `http://localhost:8888`, create an admin, create a project and
  issue, upload an attachment (MinIO required), log out, reset
  password (dev mode logs the link).

## 7. Running locally

See the "Quick start" and "Local dev" sections of the
[`README`](../README.md). Short version:

```bash
cd backend && DATA_DIR=../data STATIC_DIR=../frontend/dist go run .
cd frontend && npm run dev    # separate terminal
```

With `devenv shell` if you'd rather not manage Go / Node versions
yourself.

## 8. Extending PAIMOS

### Adding an API endpoint

1. Pick the right handlers file (e.g., `handlers/issues.go` for
   issue-centric routes).
2. Write the handler (`func Foo(w, r) { … }`).
3. Wire the route in `backend/main.go` under the right middleware
   group.
4. Add a test in the same handlers package.
5. Consume from the frontend via a thin wrapper in `frontend/src/api/`.

### Adding a migration

1. Append a `CREATE TABLE IF NOT EXISTS` / `ALTER TABLE` block to
   `migrate()` in `backend/db/db.go`.
2. Bump the version counter (see existing pattern).
3. Reflect the schema change in `models/` and `docs/DATA_MODEL_v2.md`.
4. Test on a fresh DB and on a DB with the old schema (migrations are
   one-way).

### Adding an integration

Look at `handlers/integrations.go`, `handlers/jiraimport.go`, and
`handlers/miteimport.go` for the pattern:

- Store credentials (encrypted if sensitive) in a dedicated table
- Provide `GET` / `PUT` config + `POST /test` endpoints, admin-only
- Long-running imports run as async jobs tracked in a jobs table
- Status endpoints poll the jobs table

### Changing brand defaults

PAIMOS's identity is set via `BRAND_*` env vars (loaded once at
startup into `backend/brand/brand.go`). Adding a new brand-shaped
string:

1. Add a field to the `Brand` struct and populate it in `Load()`
2. Read from `brand.Default.<Field>` in the handler
3. Document the env var in `docs/CONFIGURATION.md`
4. Update `.env.example`

## 9. Code style

- `gofmt` + `goimports` for Go. CI blocks unformatted diffs.
- Prefer explicit over clever. A three-line `for` loop is better than
  a chain of unreadable `strings.Map`s.
- Comments for the *why*, not the *what*. Well-named identifiers make
  the "what" obvious.
- No new dependencies without an issue discussion. PAIMOS is
  deliberately small.

## 10. Common pitfalls

- **Don't hardcode identity strings.** Use `brand.Default` instead.
- **Don't write a migration that can't re-run.** `IF NOT EXISTS`
  everywhere.
- **Don't forget the frontend after a backend schema change.** TS
  types in `frontend/src/api/` need updating too.
- **Don't break the graceful-disable behaviors** (MinIO, SMTP). If the
  env var for an optional service is unset, the feature should
  degrade politely — not panic on startup.
