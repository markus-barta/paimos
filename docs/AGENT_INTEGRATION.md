# Agent integration

How AI agents participate in PAIMOS — authentication, workflows,
and best practices for treating an agent as a first-class user.

---

## Introduction

PAIMOS is a self-hosted project management system for engineering teams
and solo developers. Epics → tickets → tasks, sprints, time tracking,
attachments, search — all behind a single Go binary and a JSON API.

Agent integration matters because it is the reason PAIMOS exists in its
current shape. Agents can do everything humans can — create and read
issues, leave comments, update status, log time, search, manage sprints.
They do not interact through a second-class "automation" surface.
They use the same REST API as the web SPA, authenticated the same way,
gated by the same role and project-membership rules.

If you are building an agent that ships software, PAIMOS is the place
it logs its work.

---

## Authentication

Agents authenticate with API keys. A key is bound to a user account;
the agent inherits that user's role and project memberships.

- Keys are created via `POST /api/auth/api-keys` and returned **once**
  in the response. They are never retrievable again.
- Keys are prefixed (default `paimos_`) and stored only as a sha256
  hash. Losing the key means creating a new one.
- Every authenticated request sends the key as a bearer token:
  `Authorization: Bearer <key>`.

### Creating a key

The `/api/auth/api-keys` endpoint itself requires an authenticated
session. Log in once from a browser or with `POST /api/auth/login`,
then create a key for the agent to use going forward.

```bash
# 1. Log in (captures a session cookie)
curl -s -c cookies.txt -H "Content-Type: application/json" \
  -X POST https://paimos.example.com/api/auth/login \
  -d '{"username":"ci-bot","password":"<password>"}'

# 2. Mint an API key
curl -s -b cookies.txt -H "Content-Type: application/json" \
  -X POST https://paimos.example.com/api/auth/api-keys \
  -d '{"name":"build-agent"}'
# → { "id": 7, "name": "build-agent", "key_prefix": "paimos_1a2b3c4",
#     "key": "paimos_<64-hex-chars>"  ← store this now, you can't get it later
#   }

# 3. Use the key on every subsequent request
export KEY='paimos_<64-hex-chars>'
curl -s -H "Authorization: Bearer $KEY" https://paimos.example.com/api/auth/me
```

Revoke a key with `DELETE /api/auth/api-keys/{id}`.

### Response headers worth watching

Every authenticated response (key or cookie) carries:

- `X-Permissions-Epoch` — per-user counter (PAI-320). Bumped on
  role / membership / status change. Track the value seen at first
  request; if it changes, capability decisions cached client-side
  are stale and should be re-derived.

Cookie-authenticated responses (i.e. browser SPA, not API keys)
additionally carry `X-Session-Expires-At` (RFC3339) for the unified
expiry-modal flow (PAI-322).

### Agent attribution headers (PAI-324 / PAI-325 / PAI-354)

Two request headers tag every mutation with the agent and session that
caused it. Both are optional — missing headers persist as NULL — but
strongly recommended for any non-human caller:

| Header | Persists to | Notes |
|---|---|---|
| `X-Paimos-Agent-Name` | `issue_history.agent_name`, `mutation_log.agent_name` | Free-text label (≤ 64 chars). Convention: kebab-case role name (`ops`, `dev`, `sec-review`). |
| `X-Paimos-Session-Id` | `issue_history.session_id`, `mutation_log.session_id` | UUIDv7 minted by `paimos session start`. Shared across every call within one "session" so the undo / activity feeds group correctly. |

The `paimos` CLI forwards both headers automatically when
`PAIMOS_AGENT_NAME` / `PAIMOS_SESSION_ID` env vars are set — see
`AGENT_INTERFACE.md` §4a for the `paimos session start` flow. Hand-
rolled HTTP clients should set them explicitly.

---

## Core workflows for agents

### 1. Reading project state

```bash
# List all projects the agent's user account can see
curl -s -H "Authorization: Bearer $KEY" https://paimos.example.com/api/projects

# Get a project's issues with filters
curl -s -H "Authorization: Bearer $KEY" \
  "https://paimos.example.com/api/projects/2/issues?status=backlog&priority=high"

# Get the full hierarchy (epics → tickets → tasks) for a project
curl -s -H "Authorization: Bearer $KEY" \
  https://paimos.example.com/api/projects/2/issues/tree

# Place a ticket under an epic. Hierarchy (epic⊃ticket, ticket⊃task) is the
# `parent` relation edge — the single source of truth (one parent per child).
# Equivalent to setting parent_id on issue create/update.
curl -s -X POST -H "Authorization: Bearer $KEY" -H "Content-Type: application/json" \
  -d '{"target_id": 123, "type": "parent"}' \
  https://paimos.example.com/api/issues/100/relations   # 100=epic, 123=ticket

# Search across all accessible projects
curl -s -H "Authorization: Bearer $KEY" \
  "https://paimos.example.com/api/search?q=authentication+bug"
```

Search also accepts issue keys — `?q=PAI-42` will find that specific
issue, and partial keys (`PAI-4`) prefix-match.

### 1a. Reading project context for coding agents

PAIMOS now has a dedicated project-context layer for code-aware agents.
Use it when an issue needs repository locations, canonical commands, or
structured environment facts instead of just prose.

```bash
# List linked repos for a project
curl -s -H "Authorization: Bearer $KEY" \
  https://paimos.example.com/api/projects/2/repos

# Read unified project knowledge
curl -s -H "Authorization: Bearer $KEY" \
  https://paimos.example.com/api/projects/2/knowledge

# Fetch the canonical agent artifact
curl -s -H "Authorization: Bearer $KEY" \
  https://paimos.example.com/api/projects/2/agents/codex.json

# Retrieve mixed context hits for a question
curl -s -X POST -H "Authorization: Bearer $KEY" -H "Content-Type: application/json" \
  https://paimos.example.com/api/projects/2/retrieve \
  -d '{"q":"where is the auth middleware and how do I run tests?","k":8}'

# Inspect anchors for one issue
curl -s -H "Authorization: Bearer $KEY" \
  https://paimos.example.com/api/issues/PAI-42/anchors
```

Project context is no longer a manifest blob (PAI-358). Use the
first-class surfaces instead:

- `repos` — linked repos or subtrees relevant to the project
- `knowledge` — memories, runbooks, guidelines, external systems, related projects
- `agents/{name}.json` — canonical agent artifact including commands, rules, and inventories
- `anchors` — issue-to-file anchors uploaded by repo-side tooling
- `graph` / `graph/blast-radius` — typed relationships and impact views
- `retrieve` — mixed context search across issues, anchors, knowledge, graph neighbours, and repo context

Anchors are uploaded to `/api/projects/:id/anchors` by a repo-side tool
that maps issue keys to file/line locations. Each anchor carries repo
revision and schema metadata so deep links and provenance stay explicit.

`/api/projects/:id/retrieve` now fuses project-scoped lexical hits from
issue text plus a dedicated context index for anchors and derived
symbols, then blends in local semantic vector matches and appends
graph-neighbor expansion. Vector indexing is asynchronous: retrieve
queues a project refresh and uses already-indexed vectors, so cold
projects can return lexical-only on the first call. The response includes
retrieval metadata so clients can see the fusion strategy, stage counts,
`embedding_indexing`, `embedding_model`, `embedding_provider`,
`vector_index`, and `freshness`. In v3.10.3 the local embedding model is
`local-semantic-v2`; vectors are ranked through SQLite via
`sqlite-scalar-cosine`.

For agents running inside a checked-out repo, `paimos serve` adds a local
read-only context broker:

```bash
paimos serve --project PAI --repo-root . --addr 127.0.0.1:8765
```

It combines the authenticated project context surface with bounded local
repo search/read/symbol tools:

- `GET /context/repo` — branch, HEAD, dirty counts, `AGENTS.md`, anchor index
- `POST /context/search` — fixed-string ripgrep search with bounded hits
- `POST /context/read` — bounded line-range file read with path escape checks and redaction
- `POST /context/symbols` — regex fallback for common declarations (`lsp_available: false`)
- `POST /context/retrieve` — remote `/retrieve` plus local search/symbol hits
- `POST /context/pack` — issue/query context bundle with an approximate token budget

MCP clients can launch the same broker over stdio:

```bash
paimos serve --project PAI --repo-root . --mcp-stdio
```

The broker does not accept writes. HTTP mode is loopback-only unless the
operator explicitly passes `--unsafe-allow-remote`.

Blast-radius queries are available at
`GET /api/projects/:id/graph/blast-radius?issue=PAI-79&depth=3` for the
"what else is affected if I change this?" agent flow.

### 2. Creating and updating issues

```bash
# Create a ticket in project 2
curl -s -X POST -H "Authorization: Bearer $KEY" -H "Content-Type: application/json" \
  https://paimos.example.com/api/projects/2/issues \
  -d '{"title":"Fix login timeout","type":"ticket","status":"backlog","priority":"high",
       "description":"Session expires after 5 minutes instead of 24 hours",
       "acceptance_criteria":"- [ ] Session lasts 24h\n- [ ] No regression on TOTP flow"}'

# Update issue status (PUT is partial — only send what changes)
curl -s -X PUT -H "Authorization: Bearer $KEY" -H "Content-Type: application/json" \
  https://paimos.example.com/api/issues/42 \
  -d '{"status":"done"}'

# Look up an issue by key
curl -s -H "Authorization: Bearer $KEY" \
  "https://paimos.example.com/api/search?q=PAI-42"
```

### 3. Comments and collaboration

Comments are the natural place for agents to post build reports,
review notes, and anything a human teammate would drop into a ticket.

```bash
# Markdown is rendered in the web UI
curl -s -X POST -H "Authorization: Bearer $KEY" -H "Content-Type: application/json" \
  https://paimos.example.com/api/issues/42/comments \
  -d '{"body":"## Build Report\n\nAll tests pass\n- Backend: 42 tests, 0 failures\n- Frontend: typecheck clean"}'
```

### 4. Time tracking

Agents should log time against the issues they work on so humans can
see the cost and cadence of agent-driven work alongside their own.

```bash
# Log time spent on an issue
curl -s -X POST -H "Authorization: Bearer $KEY" -H "Content-Type: application/json" \
  https://paimos.example.com/api/issues/42/time-entries \
  -d '{"minutes":30,"description":"Implemented fix and wrote tests"}'
```

### 5. Sprint management

```bash
# List active sprints (add ?include_archived=true for historical)
curl -s -H "Authorization: Bearer $KEY" https://paimos.example.com/api/sprints
```

---

## Access model

An agent does not have a separate identity class. It authenticates
**as a user account** via an API key, and whatever that user can see
and do, the agent can see and do.

PAIMOS uses two orthogonal layers:

1. **Role** (`admin` / `member` / `external`) — gates admin-only
   actions (project CRUD, user CRUD, some delete paths).
2. **Per-project access level** (`none` / `viewer` / `editor`) — gates
   read and write on individual projects and their issues.

- **Admin** agents bypass per-project checks — effectively editor on
  every project, plus admin-only surface.
- **Member** agents default to `editor` on every non-deleted project
  (seeded at user creation); individual projects can be downgraded to
  `viewer` or `none`.
- **External** agents default to `none` and must be granted `viewer`
  or `editor` per project explicitly; portal endpoints still apply.

404 is returned for projects/issues the agent can't view (no
existence oracle); 403 when a viewer tries to edit.

**Recommendation**: create a dedicated user account for each agent
(e.g. `ci-bot`, `triage-agent`, `release-agent`) with the minimum role
it needs. Do not share keys across agents — revoking a compromised
key should never disrupt an unrelated workflow.

---

## "Implement this" — UI-triggered local runs (PAI-605)

PAIMOS can hand a ticket to a coding agent **on a developer's own workstation**.
A button in the web UI creates a *run*; the developer's local runner picks it up
over the existing SSE channel, executes (Claude Code by default), and reports the
result back onto the ticket. This is a separate **execution** surface from the
render-only adapter protocol (`paimos skill render`) — the adapter turns a
canonical agent artifact into a harness file; the runner *executes* work.

**Default posture: opt-in, repo-scoped, confirm-gated, and report-back only — no
auto-deploy.** Deploy stays a manual step until the deploy-gating phase.

### Run lifecycle

```
queued → running → tests_passed | tests_failed → deployed
                                                ↘ failed | cancelled
```

The runner itself sets `running` / `tests_passed` / `failed` / `deployed`;
`cancelled` is the decline-the-prompt off-ramp before `running`, and
`tests_failed` is only ever set by the spawned agent reporting its own result.
Terminal statuses (`deployed` / `failed` / `cancelled`) are enforced
server-side — a run can't be moved back out of one.

### Endpoints

```bash
# Create a run (the "Implement this" button). Project-editor gated.
# Optional body: { "device_id": "<target runner>", "deploy_target": "ppm" }
curl -X POST -H "Authorization: Bearer $KEY" \
  "$BASE/api/issues/PAI-265/implement"

# List a ticket's runs (issue-access gated) — the UI's run-status card.
curl -H "Authorization: Bearer $KEY" "$BASE/api/issues/PAI-265/runs"

# Fetch / update a single run (requester or admin). The runner PATCHes the
# status transitions and the structured report.
curl -H "Authorization: Bearer $KEY" "$BASE/api/runs/42"
curl -X PATCH -H "Authorization: Bearer $KEY" \
  -d '{"status":"deployed","version":"4.6.0","deploy_target":"ppm",
       "tests_summary":"42 passed"}' \
  "$BASE/api/runs/42"

# Online, implement-capable runners for a project (the device picker).
curl -H "Authorization: Bearer $KEY" "$BASE/api/projects/2/runners"
```

`POST …/implement` publishes an **`implement_requested`** SSE event on the
project's `…/agents/events` stream — the run id rides in the event's `rev`, the
issue key in `name`.

### The runner

```bash
# On the developer's workstation, in the repo checkout:
paimos run-agent watch --project PAI --repo-root .
#   subscribes advertising implement-capability (?implement=1), and on an
#   implement_requested event: claims the run, spawns `claude` (override with
#   --exec) in --repo-root, then reports tests_passed / failed.
#   It reconnects on a dropped stream, processes one job at a time, and
#   periodically catches up on queued runs it missed; prompts before each run
#   unless --yes. Two runners never double-execute the same run (atomic claim).
```

`--exec` runs through a shell (`sh -c`), so quoting, pipes, and chaining work,
e.g. `--exec "claude --print 'do the ticket' && npm test"`.

`--attach-logs` (OFF by default) captures the job's combined output and attaches
it to the ticket as a log, stamping `log_attachment_id`. It is opt-in because
agent output can contain secrets, and a ticket attachment is visible to every
project member — only enable it for repos/tickets where that's acceptable.

The run lifecycle is enforced server-side: status changes must follow a legal
edge (e.g. a run can't jump straight to `deployed`), and a terminal run
(`deployed`/`failed`/`cancelled`) is immutable.

Enabling deploy is **triple-gated** and off by default — it runs only when all
three hold: `--allow-deploy` AND `--deploy-exec "<cmd>"` AND the run carries a
`deploy_target`. Even then it asks for a separate deploy confirmation unless
`--yes-deploy` is also passed:

```bash
paimos run-agent watch --project PAI --yes \
  --allow-deploy --deploy-exec "just deploy-ppm" --yes-deploy
#   after a successful run with a deploy_target, runs the deploy command,
#   captures the version from ./VERSION, and marks the run `deployed`.
```

The spawned command sees `PAIMOS_RUN_ID` and `PAIMOS_ISSUE_KEY` in its
environment, so the agent can PATCH richer progress itself (e.g. capture the
version, advance to `deployed`). On any transition into a terminal status the
server auto-posts a summary comment on the ticket — attributed to the reporting
user — so the human-readable trail always matches the structured run record.

---

## Best practices for agent implementors

1. **Search before creating.** Run `GET /api/search?q=...` first so
   your agent does not create duplicate issues for the same symptom.
2. **Reference issue keys in comments.** Write `PAI-42` style keys in
   prose so cross-linking from another issue picks them up. The web UI
   autolinks them.
3. **Follow the status lifecycle.**
   Typical flow: `new → backlog → in-progress → qa → done → delivered
   → accepted → invoiced`. `cancelled` is a terminal off-ramp at any
   point. Avoid jumping straight to `done` (skips QA) or setting
   `accepted`/`invoiced` programmatically — those are usually human
   decisions. See `docs/DATA_MODEL.md` for the full enum.
4. **Partial updates.** `PUT /api/issues/{id}` is partial. Send only
   the fields you want to change; everything else is preserved.
5. **Be reasonable about rate.** There is no hard rate limit on API
   key traffic, but stay under ~10 req/s. Batch work where you can,
   and respect 5xx with exponential backoff.
6. **Handle errors.**
   - `401` — missing or invalid key
   - `403` — authenticated but not authorised (e.g. member trying to
     delete, or an edit on a view-only portal issue)
   - `404` — issue/project does not exist **or** your user has no
     access to it (the two are deliberately indistinguishable)
   - `422` — validation error (bad enum, missing required field,
     invalid parent for the hierarchy)

---

## Full API reference

- **Compact API reference**: [`api-minimal.md`](api-minimal.md) — every
  route the web SPA uses, in one page.
- **Permissions and role matrix**: see the *Access model* section above
  and the per-project `project_members` / `access_audit` model in
  [`DATA_MODEL.md`](DATA_MODEL.md) and
  [`DEVELOPER_GUIDE.md`](DEVELOPER_GUIDE.md) §4a. Admin-gated routes
  are marked with `auth.RequireAdmin`; per-project view/edit gates
  live in `backend/auth/middleware_project.go`.

---

## Example: wiring PAIMOS into an agent skill

Drop something like this into your agent's tool manifest or skill
description so it knows how to reach a PAIMOS instance:

```markdown
## Tool: PAIMOS

- Base URL: https://your-paimos-instance.example.com/api
- Auth: `Authorization: Bearer <api-key>` (key minted by a human)
- Create issue:  `POST /projects/{id}/issues`
- Update issue:  `PUT  /issues/{id}`  (partial)
- Comment:       `POST /issues/{id}/comments`
- Log time:      `POST /issues/{id}/time-entries`
- Search:        `GET  /search?q=...`
- Project ctx:   `GET  /projects/{id}/repos`
- Knowledge:     `GET  /projects/{id}/knowledge`
- Retrieve:      `POST /projects/{id}/retrieve`
- Anchors:       `GET  /issues/{id}/anchors`

Before creating a new issue, always search for the title first.
Post a build report as a comment on the issue you just finished.
Use markdown freely — the web UI renders it.
```

That is the whole integration surface. An agent that can `curl` can
collaborate.
