<p align="center">
  <img src="docs/brand/favicon.png" alt="PAIMOS" height="96">
</p>

<h1 align="center">PAIMOS</h1>

<p align="center">
  <em>Your Professional &amp; Personal AI Project OS.</em>
</p>

<p align="center">
  <code>phase 2 — platform</code> · <code>v2.5.2</code> · <code>AGPL-3.0</code>
</p>

<p align="center">
  <a href="https://paimos.com">paimos.com</a> ·
  <a href="#quick-start">Quick start</a> ·
  <a href="docs/AGENT_INTERFACE.md">Agent Guide</a> ·
  <a href="docs/CONFIGURATION.md">Configuration</a> ·
  <a href="#highlights">Features</a> ·
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

**v2.0 marks the brand's Phase 1 → Phase 2 transition** ([details](docs/brand/BRAND.md#phasing-plan)).
The Platform reading of the name (the "OS" in PAIMOS earning a literal
read) was reserved by the brand guide until two of four trigger criteria
held; v2.0 cleared two — workflow orchestration through the
`POST /api/ai/action` dispatcher and a public API surface (OpenAPI,
self-describing schema, the agent-context layer, MCP). Phase 1 (FOSS)
stays active; the four readings now live side by side on the
[About page](https://paimos.com/about.html).

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

- **Project Context for code-aware agents (new in v2.0).** A
  structured surface above tickets: linked `repos`, project
  `manifests` (stack, commands, services, owners, NFRs, ADRs),
  issue→file `anchors`, a typed entity `graph`, and a mixed-context
  `retrieve` API. Agents stop grepping six issues to figure out
  which repo to clone and where the work actually lives. The manifest
  editor is a three-tab surface (Manifest / Guardrails / Glossary)
  with a per-tab "Structure with AI" button that turns prose into
  structured JSON via the unified `/ai/action` dispatcher (v2.1.16).
  See [`AGENT_INTEGRATION.md` §1a](docs/AGENT_INTEGRATION.md#1a-reading-project-context-for-coding-agents)
  and the route group in [`api-minimal.md`](docs/api-minimal.md#agent-context).
- **In-app AI assist.** Eleven admin-tunable text actions —
  optimize · translate · spec-out · suggest-enhancement (six sub-actions)
  · find parent · generate sub-tasks · estimate effort · detect
  duplicates · UI generation · tone check — across textareas and
  issue-level menus. Live model picker (frontier / value / fastest /
  cheapest / open-weights / free) backed by OpenRouter. Per-user
  daily token cap with admin-override header. Audit lines are
  metadata-only — prompt and response bodies are never logged. See
  [`docs/CONFIGURATION.md` § AI assist](docs/CONFIGURATION.md#ai-assist-pai-146--pai-159--pai-183).
- **Agent-native toolchain.** Official [`paimos` CLI](docs/AGENT_INTERFACE.md) +
  [`paimos-mcp`](docs/AGENT_INTERFACE.md#6-mcp-integration) facade for
  Claude Desktop and friends. File-first multi-line inputs, `--dry-run`,
  `--json`, idempotent transitions, declarative YAML `apply` — no more
  shell-quoted-JSON foot-gun.
- **Self-describing API.** `GET /api/schema` is the single source of
  truth for enums, status transitions, relation types, and field
  shapes. Strong-ETagged, versioned, cached client-side.
- **Keys or ids, pick either.** Every `/issues/{id}/*` endpoint
  accepts `PAI-83` or `462` — same request, same result.
- Hierarchical issues: **epic → ticket → task**, with parent-child
  relations and seven relation types (`groups`, `sprint`,
  `depends_on`, `impacts`, `follows_from`, `blocks`, `related`)
- **Sprints, accruals, cost units, releases** as first-class concepts
- **Bulk operations** — atomic create-many (`POST /projects/{key}/issues/batch`)
  and update-many (`PATCH /issues`) with same-batch cross-refs,
  100-item cap, transactional rollback
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
- **Opt-in session audit** — tag every mutation with an
  `X-PAIMOS-Session-Id`; replay what an agent did via
  `GET /api/sessions/:id/activity`. Off by default; one env var
  to enable.

A [complete feature catalog](#complete-feature-list) lives at the bottom
of this README.

## Quick start

```bash
git clone https://github.com/markus-barta/paimos.git
cd paimos
docker build -t paimos:local .
docker run -p 8888:8888 -v paimos-data:/app/data paimos:local
```

Open <http://localhost:8888> — default admin login: `admin` / `admin`
(change immediately after first login).

### Docker Compose

```bash
git clone https://github.com/markus-barta/paimos.git
cd paimos
ADMIN_PASSWORD='<your-choice>' docker compose up --build
```

Open <http://localhost:8888> and log in with `admin` / your
`ADMIN_PASSWORD`.

### Local dev

Requires Go 1.25+ and Node.js 22+.

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

For agent-driven UI work that needs an authenticated session locally,
see [`docs/DEV_LOGIN.md`](docs/DEV_LOGIN.md) — `just dev-up` builds the
backend with the dev-login build tag, seeds fixture users + projects,
and prints the `curl` recipe for grabbing a session cookie. The
dev-login route is build-tag-gated and **does not exist in
production binaries** (CI re-asserts this on every push).

## Agent integration

PAIMOS is built for humans **and** AI agents. The recommended way to
drive it as an agent is the **`paimos` CLI** — it handles auth,
multi-line markdown inputs, key resolution, error shapes, and shell
safety so you don't have to.

→ **[Agent Interface Guide](docs/AGENT_INTERFACE.md)** (this is the one
to read first.)  
→ [REST integration patterns](docs/AGENT_INTEGRATION.md) (HTTP-only agents)  
→ [REST reference](docs/api-minimal.md)

```bash
# Install both binaries (Go 1.25+)
go install github.com/markus-barta/paimos/backend/cmd/paimos@latest
go install github.com/markus-barta/paimos/backend/cmd/paimos-mcp@latest

# Interactive login — writes ~/.paimos/config.yaml (0600)
paimos auth login

# Read a ticket by key or numeric id
paimos issue get PAI-83
paimos issue list --project PAI --status backlog --limit 20

# Create an issue with multi-line markdown — no shell quoting
paimos issue create --project PAI --type ticket \
  --title "Refactor auth middleware" \
  --description-file /tmp/desc.md \
  --ac-file /tmp/ac.md

# Idempotent status transitions — safe to re-run
paimos issue ensure-status PAI-83 done

# Close with a structured note in one atomic-ish command
paimos issue update PAI-83 --status done \
  --close-note-file /tmp/close.md

# Scaffold an epic + N children + relations in one shot
paimos apply --from-file plan.yaml

# Preflight check — safe in CI; exit 0/1/2 (ok/warn/fail)
paimos doctor
```

### MCP (Claude Desktop)

```json
{
  "mcpServers": {
    "paimos": {
      "command": "/Users/you/go/bin/paimos-mcp",
      "env": { "PAIMOS_INSTANCE": "default" }
    }
  }
}
```

Exposes six tools: `paimos_schema`, `paimos_issue_get`, `_list`,
`_create`, `_update`, `paimos_relation_add`. Bulk ops are deliberately
CLI-only — MCP context grows fast.

### Still just HTTP

```bash
# Everything the CLI does is available as REST.
curl -s -H "Authorization: Bearer $KEY" -H "User-Agent: my-agent/1.0" \
  https://paimos.example.com/api/issues/PAI-83
```

Keys (`PAI-83`) and numeric ids are interchangeable on every
`/issues/{id}/*` endpoint. Full details in
[`docs/api-minimal.md`](docs/api-minimal.md).

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
- Issue types: `epic`, `cost_unit`, `release`, `sprint`, `ticket`,
  `task`; each with configurable type color
- Auto-incremented issue keys per project (e.g., `ACME-1`, `ACME-2`);
  `/issues/{id}/*` endpoints accept either the key or the numeric id
- Status workflow: `new`, `backlog`, `in-progress`, `qa`, `done`,
  `delivered`, `accepted`, `invoiced`, `cancelled`
- Priorities: `low`, `medium`, `high`
- Hierarchical parent-child relationships (epic → ticket → task)
- Seven relation types between issues: `groups`, `sprint`,
  `depends_on`, `impacts`, `follows_from`, `blocks`, `related` —
  directional types tag each side with `outgoing` / `incoming`
  for correct inverse rendering
- Bulk create/update with atomic rollback and 100-item cap
- Clone issue with configurable field mapping
- Complete-epic action: bulk-transitions all children
- Soft-delete + Trash with restore and admin-only hard purge
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
- 68 additive migrations run on startup
- Health endpoint: `GET /api/health` → `{ "status": "ok", "service": "...", "version": "<VERSION>" }` (`version` is stamped from `VERSION` at build time; local non-Docker builds report `"dev"`)
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

- **Agent Context (new in v2.0)**: per-project `/api/projects/{id}/repos`
  (CRUD), `/api/projects/{id}/manifest` (GET/PUT structured project
  facts), `/api/projects/{id}/anchors` (POST bulk-ingest of issue→file
  locations), `/api/projects/{id}/graph` (typed entity graph
  traversal), `/api/projects/{id}/retrieve` (mixed-context query),
  plus per-issue `/api/issues/{id}/anchors`
- **AI assist**: `POST /api/ai/action` (unified action dispatcher),
  `GET /api/ai/actions` (catalogue), `GET /api/ai/models`
  (server-cached vendor-diverse picks), `GET /api/ai/usage`
  (per-user daily token totals), full CRUD on `/api/ai/prompts`
  with `/{id}/dry-run` and `/{id}/reset`, `POST /api/ai/test`
  (smoke test the configured provider/model). Admin-only;
  audit lines are metadata-only
- **Schema discovery**: `GET /api/schema` — versioned enums,
  transitions, entity shapes, conventions. Public, strong-ETagged,
  `Cache-Control: public, max-age=300`
- **Bulk**: `POST /api/projects/{key}/issues/batch` (atomic
  create-many with same-batch `parent_ref:"#N"` cross-refs),
  `PATCH /api/issues` (atomic update-many by ref),
  `GET /api/issues?keys=PAI-1,PAI-2,…` (ordered pick list,
  missing refs marked)
- **Session audit (opt-in)**: `GET /api/sessions/{id}/activity` —
  admin-only, keyset-paginated replay of mutations tagged by an
  `X-PAIMOS-Session-Id` header. Controlled by
  `PAIMOS_AUDIT_SESSIONS=true`
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
- `docs/AGENT_INTERFACE.md` — **driving PAIMOS as an agent** (CLI, MCP, patterns)
- `docs/api-minimal.md` — REST reference
- `docs/CHANGELOG.md` — version history
- `docs/CONFIGURATION.md` — every env var, every branding knob
- `docs/brand/BRAND.md` — brand guide (name, mark, voice)
- `docs/DEVELOPER_GUIDE.md` — deeper implementation notes
- `+pm/PRD.md` — product requirements framing (long-form only — tickets live in PAIMOS at <https://pm.barta.cm>, project PAI)
- `+agents/rules/AGENTS.md` — agent collaboration rules
