<p align="center">
  <img src="docs/brand/mark.svg" alt="PAIMOS" height="80">
</p>

<h1 align="center">PAIMOS</h1>

<p align="center">
  <em>Your Professional &amp; Personal AI Project OS.</em>
</p>

<p align="center">
  <a href="#quick-start">Quick start</a> ·
  <a href="docs/CONFIGURATION.md">Configuration</a> ·
  <a href="#features">Features</a> ·
  <a href="CONTRIBUTING.md">Contributing</a> ·
  <a href="LICENSE">License (AGPL-3.0)</a>
</p>

---

## What is PAIMOS

PAIMOS is a self-hosted project management system built for engineering
teams that treat AI agents as first-class participants alongside humans —
and for solo developers who want a clean PM tool without enterprise
bloat. The same app serves both: the side-project board feels as good as
the team board.

Written as a single Go binary that serves the Vue SPA and JSON API on one
port, backed by SQLite. Docker up, browser open, done.

## Why PAIMOS

- **Agent-native PM.** Issues can be assigned to humans *or* agents;
  tracked the same way in the same hierarchy. The `AI` in the name is
  literal, not a marketing wrapper around an LLM chatbot.
- **FOSS, AGPL-3.0.** Fork it, host it, modify it. Network-copyleft means
  you have to pay that back to your users, not to us.
- **Fully rebrandable.** Product name, company, email-from, TOTP issuer,
  API-key prefix, database filename, MinIO bucket — all env-var driven.
  Every visible string is yours to override. See `docs/CONFIGURATION.md`.
- **Single Go binary + SQLite.** No external database, no Redis, no
  message queue. Attachments and SMTP are optional add-ons that degrade
  gracefully when absent.
- **Keyboard-first UX.** Built for engineers, not middle managers.

## Highlights

- Hierarchical issues: **epic → ticket → task**, with parent-child
  relations, dependency links, and impact links
- **Sprints, accruals, cost units, releases** as first-class concepts
- **Full-text search** with partial issue-key matching (`PAIMOS-15`
  finds `PAIMOS-150`, `PAIMOS-151`, etc.)
- **TOTP 2FA + API keys** (sha256-hashed, `paimos_` prefix) + session
  cookies
- **Magic-link password reset** with 60-minute token TTL
- **Attachments**: drag-drop upload, inline progress UI, image lightbox,
  markdown references
- **Jira integration**: project discovery, field mapping, relation
  mapping, async import jobs
- **Mite integration** (DE/AT time-tracking): note-field user mapping,
  resume date, cleanup
- **CSV import/export**: per-project and cross-project; preflight
  validation before commit
- **Custom views**: saved filter + column sets, per-user ordering, pin
  to sidebar
- **External-user portal** with read-only projects + accept/reject
  workflow and acceptance reports
- **PDF delivery reports** (Lieferbericht) with configurable column
  layout

A [complete feature catalog](#complete-feature-list) lives at the bottom
of this README.

## Quick start

### Docker (fastest)

```bash
git clone https://github.com/markus-barta/paimos.git
cd paimos
ADMIN_PASSWORD='<your-choice>' docker compose up --build
```

Open <http://localhost:8888> and log in with `admin` / your
`ADMIN_PASSWORD`.

### Local dev

Requires Go 1.23+ and Node.js 22+.

```bash
# backend (terminal 1)
cd backend && DATA_DIR=../data STATIC_DIR=../frontend/dist go run .

# frontend (terminal 2)
cd frontend && npm install && npm run dev
```

Frontend dev server: <http://localhost:5173>; API: <http://localhost:8888>;
Vite proxies `/api/*` to the Go backend.

With `devenv`:

```bash
devenv shell -- bash -c "cd backend && DATA_DIR=../data STATIC_DIR=../frontend/dist go run ."
devenv shell -- bash -c "cd frontend && npm run dev"
```

## Architecture

```text
Browser (Vue 3 SPA)
  ↕ JSON
Go server :8888 (chi router)
  ├── SQLite (data/paimos.db, WAL mode)
  ├── MinIO / S3 (optional — attachments)
  └── SMTP (optional — password reset emails)
```

- Single process. The Go server serves the API and the built SPA from
  `/app/static`.
- Schema migrations live in `backend/db/db.go` and run automatically on
  startup. Additive-only.
- MinIO and SMTP are optional. If unset, attachments return 503 and the
  SPA hides drop zones; reset emails are logged to stdout instead of sent.

## Configuration

Everything operator-configurable is in `docs/CONFIGURATION.md`:

- Core server vars (`PORT`, `DATA_DIR`, `STATIC_DIR`, `ADMIN_PASSWORD`,
  `COOKIE_SECURE`, `INSTANCE_LABEL`)
- All `BRAND_*` vars for identity (product name, company, website,
  email from, TOTP issuer, API key prefix, DB filename, MinIO bucket,
  page title, etc.)
- SMTP + MinIO settings
- Set-once caveats for `BRAND_API_KEY_PREFIX`, `BRAND_DB_FILENAME`,
  `BRAND_MINIO_BUCKET`

## Security

- Project create/update/delete is admin-only
- Session cookies: `HttpOnly`, `SameSite=Lax`, `Secure` when
  `COOKIE_SECURE=true`
- Rate-limited auth endpoints (login, forgot, reset, TOTP-verify)
  shared under one window: 5 attempts per 10 minutes per IP+identity
- API keys stored as sha256 hashes, never decryptable
- Password reset tokens: 32-byte random, sha256-stored, single-use,
  60-minute TTL; all active sessions invalidated on reset as defense in
  depth
- TOTP secrets per-user; admin can reset a user's 2FA

Report vulnerabilities privately — see [`SECURITY.md`](SECURITY.md).

## Contributing

PRs welcome. PAIMOS uses the [Developer Certificate of
Origin](DCO.md) — every commit must end with `Signed-off-by: Your Name
<you@example.com>` (add automatically with `git commit -s`). Full
guide: [`CONTRIBUTING.md`](CONTRIBUTING.md).

## License

[AGPL-3.0-or-later](LICENSE). Running PAIMOS as a networked service
triggers AGPL §13: your users have the right to request the source code
of the running version, including your modifications. Publishing a fork
on GitHub and linking to it from the UI footer satisfies that
obligation.

---

## Complete feature list

<details>
<summary>End-user features (click to expand)</summary>

### Issues
- Create / edit / delete issues with title, description, acceptance
  criteria, notes
- Issue types: `epic`, `ticket`, `task`; each with configurable type
  color
- Auto-incremented issue keys per project (e.g., `ACME-1`, `ACME-2`)
- Status workflow: open, in progress, testing, closed, cancelled,
  archived
- Priority levels P0–P4
- Hierarchical parent-child relationships (epic → ticket → task)
- Depends-on and impacts relations between issues
- Clone issue with configurable field mapping
- Complete-epic action: bulk-transitions all children
- Aggregation endpoint for rollup stats
- Full audit history per issue (editor, timestamp, diff)
- Cost/effort estimation: `estimate_hours`, `estimate_lp`, rate
  conversion
- Booked-hours rollup from child time entries
- Budget tracking: `budget_hours` derived from estimates + rates
- Time override field for manual budget adjustments
- Markdown-capable fields (acceptance criteria, notes, comments)
- Issue-key references in comments (`[#ACME-1](...)` pattern)

### Attachments
- Upload to issue (inline drag-drop + file picker)
- Pending attachment workflow (upload before link; cancel before commit)
- Batch-link pending attachments to issue
- Auto-resize / re-encode of images on upload
- Lightbox viewer with zoom / pan
- Inline progress UI with cancel during upload
- Graceful disable when MinIO not configured

### Comments
- Per-issue comments with markdown rendering
- Pagination on comment list
- Delete own comments (admins delete any)
- Reference other issues via markdown links

### Time tracking
- Manual time entries with start/stop fields
- Running timers (active session tracking per user)
- Recent timers list for quick re-entry
- Per-user accrual totals across projects
- Billing lifecycle: `accepted_at`, `invoiced_at`, `invoice_number`
- Time rollup from child issues

### Views, filters, sort
- Custom saved views (filter + column + sort set)
- Pin views to sidebar, reorder, rename, delete
- Filter persistence per user per scope
- Multi-select OR filtering per field
- Sort by any indexed field (title, dates, priority, status, assignee,
  booked, estimate, …)
- Configurable page size, quick filter collapse/expand

### Projects
- Per-project logo, description, status
- Project key auto-suggestion (e.g., "ACME PM 2026" → `APM26`)
- Cost units and releases: distinct-valued free-text fields per project
- Tags (shared across projects and issues)
- Recent projects list per user (auto-updated on visit)

### Sprints
- Sprints across all projects, grouped by year
- Bulk sprint creation (weekly / biweekly templates)
- Sprint states: open, closed, archived
- Move-incomplete-to-next on sprint close
- Drag-drop member reordering within sprint

### Search
- Full-text search across issues, projects, users, tags
- Partial issue-key match (type `ACME-15` to find `ACME-150`, `151`…)

### Profile & auth
- Update username, email, password
- Upload / delete avatar
- TOTP 2FA: setup QR, enable with code, disable with password, status
  endpoint
- Magic-link password reset (forgot → email → validate → reset)
- Personal API keys: create / list / revoke (shown once on create)

### Portal (external role)
- Read-only view of accessible projects
- Submit new request (creates issue in portal project)
- Accept / reject issues with undo
- Project summary (open / in-progress / testing / closed counts)
- Acceptance report (timeline of decisions)

</details>

<details>
<summary>Admin features</summary>

- User management: create, update, disable, delete, reset-TOTP
- Per-user project access grants
- Project CRUD + logo management
- Sprint batch creation, sprint editing, move-incomplete
- Tag CRUD + system tag rules (e.g., "At Risk" thresholds)
- Integration config: Jira, Mite (URL / credentials / test)
- Jira project list, async import job, field-mapping preview
- Mite time-entry import with resume-date, preview, cleanup
- CSV import: per-project + global, with preflight validation
- CSV export: per-project download
- Dev panel: test-report upload + render (for CI artifact viewing)
- Accruals report (per-user time summary)
- Lieferbericht PDF report (German delivery report)
- Issue archive / hard-delete
- Time entries purge (preview + execute) per project / per user

</details>

<details>
<summary>Operator / deployment features</summary>

- Single-binary deploy (Go + embedded SPA via `/app/static`)
- Optional MinIO for attachments (graceful disable)
- Optional SMTP for password reset (dev mode = log to stdout)
- SQLite with WAL + 5-second busy timeout + connection pool
- 63+ additive migrations run on startup
- Health endpoint: `GET /api/health`
- Instance endpoint: `GET /api/instance` (label, hostname,
  attachments-enabled flag)
- Rate-limited auth endpoints (shared 5/10min window across login,
  forgot, reset, TOTP-verify)
- Per-instance branding overlay via `$DATA_DIR/branding.json` (+
  `branding-*.json` variants)
- `$INSTANCE_LABEL` env shows a banner (e.g., "STAGING") in the sidebar
- All identity strings operator-configurable via `BRAND_*` env vars
  (see `docs/CONFIGURATION.md`)

</details>

<details>
<summary>API surface (route groups)</summary>

- **Auth / session**: login, logout, me, password change, avatar
  upload/delete, profile update
- **2FA (TOTP)**: status, setup, enable, disable, verify (login step 2)
- **Password reset**: forgot, validate, reset
- **API keys**: list, create, revoke
- **Projects**: list, get, create/update/delete (admin), logo
  upload/delete (admin), suggest-key
- **Issues**: list per project, create in project, tree, children,
  get/update/delete single, archive (admin), cross-project list, recent,
  clone, aggregation, history, complete-epic
- **Issue relations**: list, create, delete (admin), members-by-relation
- **Attachments**: list per issue, upload & link, fetch, delete (admin),
  upload pending, batch link
- **Comments**: list per issue, create, delete (admin)
- **Time entries**: list per issue, create, update, delete, running
  timers, recent timers
- **Tags**: list, CRUD (admin), attach/detach to issues + projects
- **System tags**: list rules, update rules (admin)
- **Sprints**: list, years, by year, batch create (admin), update
  (admin), move-incomplete (admin), reorder members
- **Project metadata**: cost units, releases (per-project and
  cross-project)
- **Views**: list, CRUD, reorder (admin), pin/unpin
- **Search**: full-text + key-based
- **Users**: list, CRUD (admin), disable (admin), reset-TOTP (admin),
  per-user project access (admin), recent-projects (self)
- **Portal**: projects, issues, requests, accept/reject with undo,
  summary, acceptance-report
- **Integrations**: Jira (config, test, project list, import, jobs,
  debug), Mite (config, test, import, jobs, resume-date, cleanup)
- **CSV / bulk**: preflight + import per project / global, per-project
  CSV export, time-entries purge (preview + execute)
- **Reporting**: acceptance-log, acceptance-report, Lieferbericht
  (JSON + PDF), accruals (admin)
- **Branding / static**: GET branding, list brandings, serve
  logos / avatars, instance, health, dev test-reports (admin)

</details>

## Contributor docs

- `CONTRIBUTING.md` — dev setup, DCO sign-off, PR workflow
- `SECURITY.md` — vulnerability reporting
- `CODE_OF_CONDUCT.md` — Contributor Covenant v2.1
- `DCO.md` — Developer Certificate of Origin
- `docs/CONFIGURATION.md` — every env var, every branding knob
- `docs/brand/BRAND.md` — brand guide (name, mark, voice)
- `docs/DEVELOPER_GUIDE.md` — deeper implementation notes
- `+pm/PRD.md` — product requirements framing
- `+agents/rules/AGENTS.md` — agent collaboration rules
