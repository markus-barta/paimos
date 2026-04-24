# Changelog

All notable changes to PAIMOS are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and PAIMOS adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.5.5] — 2026-04-24

### Added — cooperation metadata + CRM provider docs (PAI-61 / PAI-62 / PAI-107)

Closes the PAI-28 epic frontend work plus the deferred plugin-layer
docs.

- **Per-project cooperation metadata** (PAI-61). New
  `project_cooperation` table (1:1 with projects, M71): structured
  fields for engagement type, code ownership, environment
  responsibility, plus an SLA bundle (uptime, response time, backup
  + on-call flags) and two markdown freeform fields. Two endpoints:
  `GET /api/projects/:id/cooperation` returns the row or zero-value
  defaults; `PUT /api/projects/:id/cooperation` upserts (admin-only).
  CHECK constraints validated server-side; informational in v1, no
  behavioural effects elsewhere.
- **Cooperation profile section on project detail** (PAI-62).
  View/edit toggle; structured fields shown as labelled value pills;
  SLA section conditional on `has_sla`; freeform fields rendered
  through the existing `useMarkdown` composable. Empty state shows a
  "Set up profile" button for admins so first-time use isn't a
  hidden form.
- **CRM provider developer docs** (PAI-107). New
  [`docs/CRM_PROVIDERS.md`](CRM_PROVIDERS.md) — interface
  walkthrough, copy/paste skeleton, config schema field types, error
  handling conventions, and a worked Pipedrive example. Linked from
  `docs/AGENT_INTERFACE.md` and `docs/DEVELOPER_GUIDE.md`.

## [1.5.4] — 2026-04-24

### Added — customers + CRM provider UI (PAI-28 frontend; PAI-101 admin)

Frontend for the customer / document / CRM-plugin layer that landed in
v1.5.3. Manual customers and CRM-linked customers render through the
same components — provider affordances are conditional, not a
missing-state stub.

- **Customers in the sidebar** (PAI-57). New nav entry below Projects.
  Card-grid list view at `/customers` with a search box and a
  split "+ New customer" button: primary action is always manual
  create (the no-CRM path); the dropdown only lights up when at
  least one CRM provider is enabled + configured.
- **Customer detail** (PAI-58). Sticky identity header with the
  customer name, optional provider badge, default rates, and a
  state-machine sync button (idle → loading → success / error).
  Two-column contact + notes section. Project cards listed inline
  with effective rates and an inheritance indicator. Documents
  section reuses the same component as the project page.
- **DocumentsSection** — scope-agnostic component used by both
  customer and project detail. The whole section is the drop-target
  (no separate dropzone modal). PDFs preview lazily via inline
  `<iframe>` thumbnails; status pills (active / expired / draft);
  20 MB cap; admin-only writes.
- **Admin CRM tab** (PAI-105). New Settings → CRM tab. Each
  registered provider gets a card with status pill (`Disabled` /
  `Enabled` / `Needs configuration`), an enable toggle, and an
  expandable config form rendered from the provider's
  `ConfigSchema`. Secret fields are never echoed: stored values
  show as `••••• currently set · Replace · Clear`. The toggle
  refuses to enable a misconfigured provider both client- and
  server-side.
- **Project detail polish** (PAI-59 / PAI-60). Customer pill in the
  page header (links through to the customer); customer
  assignment dropdown in the edit modal; rate inputs show an
  inline "Inherits €X from {Customer}" hint when left blank;
  project documents section appended below the issue list.
- **Projects view** (PAI-63). Customer filter dropdown in the
  toolbar (with an "Unassigned" option); each project card shows
  a small linked customer pill when assigned.
- **`useExternalProvider`** composable (PAI-106). Single shared
  fetch of `/api/integrations/crm`; consumers ask for a provider
  by id, get back logo / name / deep-link template — no place in
  the UI hardcodes a CRM name.

Visual language stays consistent with the existing app — DM Sans,
existing color tokens, card hover-lift, monospace numerals, sticky
headers.

## [1.5.3] — 2026-04-24

### Added — customers + documents + CRM provider plugin layer (PAI-28 + PAI-101)

Backend foundation for the customer-management epic, refined for CRM
independence. PAIMOS now owns the customer / document / rate-cascading
data model directly; external CRM sync (HubSpot is the first provider)
plugs in through a small Go interface.

- **Customers** (PAI-53): new `customers` table with CRM-agnostic
  `external_*` columns (NULL = manual customer, fully supported as a
  primary mode). Full CRUD endpoints under `/api/customers`. FTS5
  indexed for search; admin-only writes; pair-validation triggers
  enforce that `external_id` and `external_provider` are both set or
  both null at the DB layer.
- **Project ↔ customer FK + rate cascading** (PAI-54): legacy
  freeform `projects.customer_id` column renamed to `customer_label`;
  new `customer_id` is an INTEGER FK to `customers.id`. List + detail
  responses now carry `effective_rate_hourly`, `effective_rate_lp`,
  and `rate_inherited` so the UI can show inherited-vs-overridden
  without computing it client-side.
- **Documents** (PAI-55): new `documents` table with a `scope`
  (`customer` | `project`) constraint that enforces exactly one of the
  two FKs is set per row. File bytes go to MinIO under the
  `documents/<scope>/<id>/…` key namespace (same bucket as
  attachments — single object store for both). 20 MB cap; PDF / image
  / Office MIME allowlist; status badges (`draft` | `active` |
  `expired`) + validity dates.
- **CRM provider plugin layer** (PAI-101 + children): new
  `backend/handlers/crm` package defining the `Provider` Go
  interface, in-process registry, generic
  `POST /api/customers/import`, `POST /api/customers/:id/sync`, and
  the admin Integrations endpoints (`/api/integrations/crm`).
  Provider configs persist in a new `provider_configs` table with
  AES-GCM-encrypted secrets at rest (key from `PAIMOS_SECRET_KEY` env
  or auto-generated under `$DATA_DIR/.secret-key`). Secrets are never
  echoed in API responses or log lines — only a `has_value` flag per
  field so the admin UI can render "••••• has value".
- **HubSpot provider** (PAI-56): first reference implementation.
  Imports a HubSpot company by URL or bare ID, supports manual
  re-sync (preserves PAIMOS-only fields), builds deep-link URLs back
  into HubSpot. Wired in `main.go` via blank import — adding a new
  provider is one line there + one new subpackage.

Frontend types updated for the renamed `customer_label` column;
sidebar / customer list / customer detail views (PAI-57 / PAI-58) and
the admin CRM Integrations UI (PAI-105) ship in a follow-up.

## [1.5.2] — 2026-04-24

### Changed — appearance-state ownership (PAI-84)

Carved out of PAI-40. Three categories of appearance state previously
had ~3 owners each, with `useBranding` and `useTableAppearance` racing
to set the same `--table-row-*` CSS vars on first paint.

- New `useTypeColors` composable: sole owner of `--type-{epic,ticket,task}`.
  `SettingsAppearanceTab` now consumes it via thin computed get/set
  wrappers instead of holding duplicate refs + watchers.
- `useTableAppearance` is now sole owner of `--table-row-border` and
  `--table-row-alt`. Reset via the settings tab now reverts the CSS
  vars to branding defaults live (was a silent bug — required a page
  reload).
- New `useSidePanelWidth` composable shared by `IssueSidePanel` and
  `IssueList`. `IssueList` no longer reaches past the panel boundary
  to read `LS_SIDEBAR_WIDTH` directly. Double-click reset on the
  resize handle now also updates the IssueList offset (previously
  unsynced).
- `useBranding.applyToDOM` no longer touches type or table-row CSS
  vars — it only supplies the defaults the two composables fall back
  to when no user override is set.

Pure structural cleanup. No user-facing behavior changes.

### Fixed — deploy script resilience

- `scripts/deploy.sh` now tolerates fish-shell quirks in remote SSH
  paths, strips PTY artifacts from captured output, and handles
  root-owned bind mounts without aborting the backup step.

## [1.5.1] — 2026-04-24

### Added — agent interface docs (PAI-96, PAI-85 epic step K)

- New `docs/AGENT_INTERFACE.md`: the single place to learn how to
  drive PAIMOS as an agent. Covers install + `auth login`, core
  patterns (keys-or-ids, file-first multi-line, `--dry-run`,
  `--json`), an end-to-end transcript of a real session (ticket →
  branch → deploy → close-with-note), bulk ops (`batch-update`,
  `apply`), schema discovery, MCP wiring for Claude Desktop, and
  explicit failure-mode guarantees.
- Linked from `DEVELOPER_GUIDE.md` as the top-of-document pointer
  for agent-driving readers.

### Closes the PAI-85 epic

All 12 children (PAI-86 → PAI-97) shipped across 12 PRs in one day:

- **API** (A–D): keys everywhere, `/api/schema`, bulk endpoints,
  new relation types.
- **CLI** (E–I): bootstrap, write commands + close-note,
  batch-update + apply, schema + doctor, shell completions.
- **MCP** (J): 6-tool stdio facade.
- **Docs** (K): this file.
- **Session audit** (L): opt-in UUIDv7 mutation trail.

The shell-quoted-JSON foot-gun, the numeric-id lookups, the
project-ID mismatch between instances, and the status-enum guessing
are all gone from the agent hot-path.

## [1.5.0] — 2026-04-21

### Added — MCP facade (PAI-95, PAI-85 epic step J)

- New `paimos-mcp` binary at `backend/cmd/paimos-mcp/`. Hand-rolled
  JSON-RPC 2.0 over stdio (newline-delimited frames). Spawned by
  Claude Desktop / other MCP clients as a subprocess.
- **6 tools** exposed in v1 (allowlist from the epic pickup doc):
  - `paimos_schema` — fetch `/api/schema` for pre-call validation.
  - `paimos_issue_get` — ref (key or id).
  - `paimos_issue_list` — project_key + status/type/priority filters.
  - `paimos_issue_create` — title + project_key required, full field set.
  - `paimos_issue_update` — partial update by ref.
  - `paimos_relation_add` — all 7 PAI-89 relation types.
- **Deliberately NOT exposed**: `batch-update`, `apply`. MCP context
  grows fast — if an agent needs bulk, it shells out to the
  `paimos` CLI.
- Shares `~/.paimos/config.yaml` with the CLI (same file authored
  by `paimos auth login`). `PAIMOS_CONFIG` / `PAIMOS_INSTANCE` /
  `PAIMOS_SESSION_ID` env vars for overrides.
- Errors from tool bodies surface as `isError=true` MCP results so
  agents see "issue not found" without the JSON-RPC envelope
  eating the message.

#### Example: register with Claude Desktop

```json
{
  "mcpServers": {
    "paimos": {
      "command": "/path/to/paimos-mcp",
      "env": {
        "PAIMOS_INSTANCE": "ppm"
      }
    }
  }
}
```

Live smoke (stdio): initialize → tools/list (6 tools) → 4 tools/call
invocations (schema, issue_get, issue_list, issue_get error path)
all return correct MCP-shaped responses.

## [1.4.1] — 2026-04-21

### Added — CLI batch-update + apply (PAI-92, PAI-85 epic step G)

- `paimos issue batch-update --from-file ops.jsonl` — streams JSONL
  (one `{"ref": …, "fields": {…}}` per line), chunks at 100 (the
  server's batch cap), each chunk is one `PATCH /api/issues`
  transaction. Reports per-chunk progress + a final summary. `-`
  reads stdin.
- `paimos apply --from-file plan.yaml` — declarative scaffolding:
  a single command creates an epic + N children + relations in
  one go. Named refs (`name: epic` on a create item) let later
  rows reference the same-plan item; the CLI translates to the
  server's positional `parent_ref: "#N"` before POSTing the batch.
- Both support `--dry-run` to print the resolved payload(s) without
  sending. **Not idempotent in v1** — running `apply` twice
  duplicates. Use `ensure-status` / `batch-update` for subsequent
  changes to scaffolded work.

#### Example plan

```yaml
project: PAI
create:
  - name: epic
    type: epic
    title: Quarter refactor
  - name: child1
    type: ticket
    title: Extract auth module
    parent: epic
relations:
  - source: epic
    type: related
    target: PAI-85
```

## [1.4.0] — 2026-04-21

### Added — session-scoped mutation audit (PAI-97, PAI-85 epic step L)

- New table `session_activity` (migration M68).
- `SessionAuditMiddleware` records mutation requests (POST / PUT /
  PATCH / DELETE) with method, path, status code, user id, and the
  value of the `X-PAIMOS-Session-Id` header. Reads are skipped so
  UI browsing noise doesn't bloat the table.
- **Off by default in v1.** Enable via `PAIMOS_AUDIT_SESSIONS=true`
  env var (reads per-request so flipping it at runtime works).
  Rationale: single-user instances don't need the data yet; flip
  the default once multi-agent use proves it valuable.
- Missing/malformed header is non-fatal: mutation succeeds, row
  written with `session_id = NULL` (operators can still spot
  untagged traffic).
- `GET /api/sessions/{id}/activity` (admin only): keyset-paginated
  by `id > cursor`, default limit 100, max 1000. Response includes
  `next_cursor` pointing past the last returned row (null on last
  page). Keyset not offset — audit tables grow unboundedly.

### Added — CLI session tagging

- The `paimos` CLI generates a UUIDv7 per invocation and sends it as
  `X-PAIMOS-Session-Id` on every request. `PAIMOS_SESSION_ID` env
  var overrides for multi-step shell flows that need to share one
  session across invocations.

## [1.3.3] — 2026-04-21

### Added — CLI shell completions (PAI-94, PAI-85 epic step I)

- `paimos completion bash|zsh|fish|powershell` — Cobra-native.
  Emits the completion script to stdout; install once per shell.
- **Enum-aware completions** driven by the schema cache:
  - Flags `--status`, `--type`, `--priority` on `issue list` /
    `issue create` / `issue update`.
  - Positional args: `issue ensure-status <ref> <status>` (second
    arg), `relation add <source> <type> <target>` (middle arg).
  - Zero network on tab-press — reads `~/.paimos/schema-<instance>.json`.
    Run `paimos schema` once per instance to populate.

#### Install examples

```sh
# fish
paimos completion fish > ~/.config/fish/completions/paimos.fish

# zsh (persistent)
paimos completion zsh > "${fpath[1]}/_paimos"

# bash
paimos completion bash > /etc/bash_completion.d/paimos   # or ~/.bash_completion
```

## [1.3.2] — 2026-04-21

### Added — CLI schema + doctor (PAI-93, PAI-85 epic step H)

- `paimos schema` — shows enums/transitions/conventions from the
  local cache; fetches transparently on first run. `--refresh`
  forces re-download and reports whether the server-side version
  moved. Cache file: `~/.paimos/schema-<instance>.json` (per-
  instance so multi-env setups don't clobber each other).
- `paimos doctor` — read-only preflight. Checks: config readable,
  `/api/health` reachable, API key valid (`/api/auth/me`), schema
  version current. Exit codes 0/1/2 (ok / warn / fail) per
  convention — CI-safe. `--json` emits the same result array for
  programmatic use.

## [1.3.1] — 2026-04-21

### Added — CLI write commands (PAI-91, PAI-85 epic step F)

- `paimos issue create --project PAI --type ticket --title "..." --description-file/--ac-file/--notes-file`
- `paimos issue update <ref> --status done --close-note-file note.md` —
  when the status is terminal (done/delivered/accepted/invoiced/cancelled),
  appends a **Close note** comment so the "why" is captured alongside
  the status change. Errors out if `--close-note` is passed without a
  terminal `--status`.
- `paimos issue ensure-status <ref> <status>` — idempotent: GETs current
  state, PUTs only if different; second run prints "already X" with
  exit 0. JSON mode reports `{changed: bool, previous, status}`.
- `paimos issue comment <ref> --body-file comment.md`.
- `paimos relation add <source> <type> <target>` — accepts all 7
  relation types (dogfoods PAI-89).

### Conventions

- **Multiline inputs are always file-first.** Inline `--foo "…"` is
  single-line by design. `--foo-file <path>` reads UTF-8 (or `-` for
  stdin). Mutually exclusive — combining inline + file errors out
  rather than silently preferring one.
- **`--dry-run`** on create/update prints the resolved request payload
  (method, path, body) as JSON to stdout and exits 0 without calling
  the API. Makes it safe to script without fear.
- Usage errors → stderr + exit 2. API errors → exit 1 (message in
  chosen format — pretty or `--json`). Nothing ever dumps HTML.

## [1.3.0] — 2026-04-21

### Added

- **`paimos` CLI — bootstrap** — PAI-90 (PAI-85 epic, E). New binary
  at `backend/cmd/paimos/`. Cobra + Viper-lite + `golang.org/x/term`
  (hidden API-key input) + `gopkg.in/yaml.v3` (config). Read-only
  surface for v1:
  - `paimos auth login` — interactive; prompts for URL + API key,
    verifies against `/api/auth/me`, writes `~/.paimos/config.yaml`
    (atomic temp+rename, mode 0600).
  - `paimos auth whoami` — shows active instance + user.
  - `paimos project list` — compact table or `--json`.
  - `paimos issue get <ref>` — ref is key or numeric id; pretty by
    default, `--json` pipes the server payload verbatim.
  - `paimos issue list --project PAI --status backlog --limit 20`.
  - `paimos issue children <ref>`.

  Global flags work on every subcommand: `--instance <name>` (multi-
  instance config), `--json` (machine output), `--config <path>`.
  Missing/invalid config → exit 2 with a pointer to `auth login`.
  API errors → exit 1, `--json` emits `{error, code}` (never HTML
  or unstructured dumps). `paimos` with no subcommand prints usage
  to stderr with exit 2.

### Note: CLI is bundled in the backend Go module (monorepo layout).
  Shared types with the server prevent schema drift. Distribution
  via `go install github.com/markus-barta/paimos/backend/cmd/paimos@latest`
  or a release binary — not via the PAIMOS Docker image.

## [1.2.8] — 2026-04-21

### Added

- **Three new relation types** — PAI-89 (PAI-85 epic, D):
  - `follows_from` (spin-off / carved-out ticket),
  - `blocks` (hard blocker, semantically distinct from `depends_on`),
  - `related` (loose "see also").

  Migration M67 extends the `issue_relations.type` CHECK constraint;
  existing rows unaffected. `GET /api/issues/{id}/relations` now tags
  every row with `direction: "outgoing" | "incoming"` so the UI can
  render inverse labels ("follows up on X" vs "followed up by Y")
  without a second stored row. Issue detail page picks up new form
  options + grouped rendering. `SchemaVersion` bumped to **1.1.0**;
  `/api/schema` `enums.relation` lists all seven types.

## [1.2.7] — 2026-04-21

### Added

- **Bulk issue endpoints** — PAI-88 (PAI-85 epic, C). Three new
  admin-only operations, all atomic under a single SQLite transaction
  with a 100-item hard cap (413 on exceed):
  - `POST /api/projects/{key}/issues/batch` — create N issues at once.
    Accepts project key or numeric id. Supports `parent_ref: "#N"`
    to point a child row at an earlier same-batch item, so the
    canonical "create an epic + all its children in one call" flow
    works without a round-trip-per-child.
  - `PATCH /api/issues` — update N issues at once. Body shape
    `[{ref: "PAI-83"|123, fields: {...}}, ...]`; any row failing
    validation rolls back the whole batch and returns per-row errors
    with `rolled_back: true`.
  - `GET /api/issues?keys=PAI-1,PAI-2,PAI-3` — pick list, response
    order matches request order, missing/inaccessible keys surface
    as `{ref, error: "not found"}` entries (never silently dropped).
- **Side-effect note** — bulk ops deliberately SKIP the auto-promote
  parent-epic / cascade-children / billing-timestamp logic that
  single-issue CreateIssue and UpdateIssue run. Bulk is mechanical;
  the CLI calls single endpoints when it wants the full lifecycle.

## [1.2.6] — 2026-04-21

### Added

- **`GET /api/schema` — self-describing discovery endpoint** — PAI-87
  (PAI-85 epic, B). Public, no auth required. Returns a versioned JSON
  payload with `enums` (status, priority, type, relation), recommended
  `transitions` (any→any still accepted server-side — these are hints
  for clients), `entities` (required/optional field lists per type,
  with `key_shape` for issues), and `conventions` (AC checkbox
  format, key case-sensitivity, multiline-input guidance). Strong
  ETag + `Cache-Control: public, max-age=300`; `X-Schema-Version`
  header mirrors the body's `version` string. The CLI (PAI-90+) and
  MCP (PAI-95) consume this before POSTing so typos like
  `status: "completed"` get caught client-side with a suggestion.
  Regression test hashes the payload — editing the schema without
  bumping `SchemaVersion` fails CI.

## [1.2.5] — 2026-04-21

### Added

- **API: accept issue keys everywhere** — PAI-86 (PAI-85 epic, A). Every
  `/api/issues/{id}/*` route now accepts either the numeric id or an
  issue key like `PAI-83` / `PMO26-639`. Keys resolve server-side via
  `(project.key, issue.issue_number)`; numeric ids keep working
  unchanged. Malformed refs return 400, key-shapes with no match 404.
  Soft-deleted issues still resolve so `restore` / `purge` can target
  them by key. New helper `auth.ResolveIssueRef` + `auth.IsIssueKey`;
  table-driven test in `keyresolve_test.go`.

## [1.2.4] — 2026-04-21

### Changed

- **Issue-status constants centralized + `IssueStatus` type completed**
  — PAI-44 (scope c). New `frontend/src/constants/status.ts` exports
  `STATUSES` (all 9 in canonical workflow order),
  `ACCRUALS_DEFAULT_STATUSES` (done / delivered / accepted / invoiced),
  and `ACCRUALS_EXTRA_STATUSES` (new / backlog / in-progress /
  cancelled — `qa` deliberately excluded). Four call sites migrated:
  `IntegrationsView.vue`, `ImportView.vue`, `AccrualsPrintView.vue`,
  `ProjectsView.vue` — the previously-duplicated literal arrays are
  gone.

### Fixed

- **`IssueStatus` type now reflects the full 9-status workflow**
  (`types.ts:73`). Previously omitted `qa` and `delivered`, which the
  backend already emits and the frontend handles at runtime — the
  type was silently wider than declared.

### Not changed (deliberately)

- Single-value equality checks like `if (s === 'done')` were left
  alone: TypeScript's `IssueStatus` union already gives compile-time
  safety there, so importing a const would be noise.
- `SprintBoardView`'s narrower "completed" arrays and the
  `useInlineEdit` / `IssueEditSidebar` "terminal" sets (which
  disagree on whether `delivered` counts as terminal) were not
  migrated. That's a semantic question for product review, not a
  centralization question. Flagged in the PAI-44 close note.
- `PT_FACTOR = 8` (`useTimeUnit.ts:38`) and the two `10 * time.Second`
  literals in `backend/handlers/integrations.go` — mentioned in
  PAI-44's description but not in its acceptance criteria; leaving
  to a separate ticket if ever warranted.

## [1.2.3] — 2026-04-21

### Changed

- **State-management pattern documented + localStorage keys
  centralized** — PAI-40. `docs/DEVELOPER_GUIDE.md` now names all
  three state tiers (Pinia store / module-scope composable singleton /
  component-local `ref`) with criteria for when each is appropriate,
  replacing the previous two-tier summary that didn't mention
  singleton composables despite their heavy use. New single source of
  truth `frontend/src/constants/storage.ts` exports every
  localStorage key the app touches — 22 static keys + 4 dynamic
  factory functions, migrated across 16 files. Legacy outliers
  (`sidebar-color-bg`, `sidebar-color-pattern`, `issue-display-type-*`,
  `paimos_time_unit`) keep their non-standard names to avoid wiping
  existing user preferences on upgrade; all documented in the module
  header. Pure refactor — no behavior change. Triple-ownership of
  type-color / table-row-color / sidebar-width state carved out to
  PAI-84 for separate treatment.

## [1.2.2] — 2026-04-21

### Fixed

- **"Session expired" banner stuck on after login** — PAI-83. Any 401
  from a non-auth endpoint flipped the global `sessionExpired` ref to
  `true`, but nothing ever cleared it. A user logging back in after a
  stale session saw the dashboard correctly populated — but the
  "Session expired. Your content may be out of date — please sign in
  again." banner persisted at the top. Fix: clear `sessionExpired` in
  `auth.setUser()` (which both password and TOTP login paths converge
  on) and in `auth.login()`. Added a regression test in
  `src/stores/auth.test.ts`.

## [1.2.1] — 2026-04-20

### Fixed

- **Issue-list scroll containment** — PAI-16. Two related scroll bugs
  in the issue list are gone:
  - In **tree view**, `AppFooter` no longer bleeds into the last rows.
    `<IssueTreeView>` now lives in a `.issue-tree-wrap` scroll container
    mirroring the flat table's `.issue-table-wrap` pattern; the
    `.tree-active` `overflow: visible` opt-out rules on
    `.issue-list-root` / `.issue-list-main` are dropped.
  - With the **side panel open in unpinned (floating) mode**, the list
    behind the panel stays scrollable via wheel / trackpad. Previously
    the transparent full-viewport `.sp-backdrop` intercepted wheel
    events and — being `position: fixed` — terminated the scroll chain
    at the viewport (`<html>` is `overflow: visible`). Now a
    `@wheel.passive` handler on the backdrop forwards the scroll to
    the element visually beneath it, preserving the existing
    click-to-close behaviour.

## [1.2.0] — 2026-04-20

### Added

- **Soft-delete (Trash) for issues** — PMO26-639. `DELETE /api/issues/{id}`
  now moves the issue to a Trash instead of hard-deleting. Child tasks
  cascade into the Trash too; `issue_relations` (groups / sprint /
  depends_on / impacts) stay intact so a later Restore re-attaches them
  automatically.
- `POST /api/issues/{id}/restore` — admin-only; clears `deleted_at` on
  one issue. Children stay in Trash — restore is deliberately explicit.
- `DELETE /api/issues/{id}/purge` — admin-only; hard-deletes a trashed
  issue (cascades comments, history, tags, time_entries, attachments,
  issue_relations). Two-step flow: must be in Trash first.
- `GET /api/issues/trash` — admin-only list of soft-deleted issues.
- **Settings → Trash** now lists deleted issues alongside deleted users
  and projects, with Restore and "Delete forever" buttons.
- `issues.deleted_at` (TEXT NULL) and `issues.deleted_by` (INTEGER NULL)
  columns with an `idx_issues_deleted_at` index (migration 66).
- 9 handler tests covering delete / restore / purge / cascade / relation
  survival / cross-project leak protection.

### Changed

- Every user-facing issue query (list, tree, recent, search, sprint,
  reports, aggregation, CSV export, cross-project list, distinct
  cost_unit / release, FTS, portal) now filters `deleted_at IS NULL`.
  Trashed issues only appear via the Trash endpoint.
- `GET /api/issues/{id}` and `PUT /api/issues/{id}` return `404` for
  trashed issues — restore first, then edit.
- Issue delete confirmation dialogs now say "Move to trash" and mention
  that child tasks are moved too and the action is recoverable from
  Settings → Trash.

### Notes

- Protection applies to **new** deletions only. Any issue hard-deleted
  before this release remains unrecoverable.

## [1.1.1] — 2026-04-20

This release bundles the per-project access-control feature with the
follow-on audit fixes and Docker/CI repairs. No `v1.1.0` tag was cut —
the feature landed on `main` and shipped together with the hotfixes
under `v1.1.1`.

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

- `/api/auth/login`, `/api/auth/me`, and `/api/auth/totp/verify` now return a
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

### Fixed

- 10 critical audit findings around access control enforcement
  (PAI-6..PAI-15) — see commit `899dd09`.
- Test suite green after audit fixes; migration test suite sped up.
- Dockerfile: copy `VERSION` and `docs/` into the SPA build stage so
  `__APP_VERSION__` and in-app docs links resolve in the built image.
- CI: sync `VERSION` from the git ref before the docker build so
  tagged images (`v1.1.1` → `1.1.1`) and main-branch images
  (`<base>-dev+<sha>`) have accurate version strings.

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
