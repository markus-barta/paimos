# PAIMOS API — Quick Reference

Base URL: `https://paimos.example.com/api`  
Auth: `Authorization: Bearer <key>`  
Format: JSON in, JSON out.

---

## Auth

```
GET    /auth/me
POST   /auth/login                  {username, password}
POST   /auth/password               {current_password, new_password}
                                      — invalidates all *other* sessions; clears must_change_password
POST   /auth/impersonation/start    {user_id} — super_admin only
POST   /auth/impersonation/end      {} — exits active impersonation
POST   /auth/api-keys               {name} → {key} (shown once)
GET    /auth/api-keys
DELETE /auth/api-keys/:id
```

Every authenticated response carries two session-state response
headers (PAI-320 / PAI-322):

- `X-Session-Expires-At` — RFC3339 absolute expiry of the current
  session. Sessions slide on each request up to a 90-day absolute
  cap; the SPA uses this to render the unified expiry modal.
- `X-Permissions-Epoch` — per-user counter bumped on role /
  membership / status change. Clients compare against the value at
  login; a mismatch means cached capability decisions are stale.

Newly created users with `must_change_password=true` (the default)
get `403 {"error":"must_change_password"}` on every endpoint except
`/auth/login`, `/auth/me`, `/auth/logout`, and `/auth/password`
until they POST a new password.

## Projects

```
GET    /projects                    ?status=active|archived
POST   /projects                    {name, key, description}
GET    /projects/:id
PUT    /projects/:id                partial update
DELETE /projects/:id                admin only
GET    /projects/:id/repos
POST   /projects/:id/repos          {url, default_branch, label, sort_order}
PUT    /projects/:id/repos/:repoId  partial update
DELETE /projects/:id/repos/:repoId
# PAI-358 (v3.0): /projects/:id/manifest endpoints removed; legacy
# taxonomy fully replaced by the PAI-338 knowledge plane.
POST   /projects/:id/anchors        {repo_id, schema_version, repo_revision, generated_at, anchors}
GET    /projects/:id/graph          ?root=issue:42&depth=2
GET    /projects/:id/graph/blast-radius ?issue=PAI-79&depth=3
POST   /projects/:id/retrieve       {q, k}
```

## Issues

```
GET    /projects/:id/issues         ?status= &priority= &type= &assignee_id=
POST   /projects/:id/issues         {title, type, status, priority, description, acceptance_criteria, notes, report_summary}
GET    /projects/:id/issues/tree    epic → ticket → task hierarchy
GET    /issues                      cross-project list; ?q= ranks best match, then recency
GET    /issues?keys=PAI-1,PAI-2     pick list, order preserved, missing → {ref, error} entries
POST   /issues                      orphan (no project) issue
POST   /projects/:key/issues/batch  admin — atomic create-many (parent_ref:"#N" cross-refs)
PATCH  /issues                      admin — atomic update-many [{ref, fields: {...}}, ...]
GET    /issues/recent               dashboard feed
GET    /issues/:id
PUT    /issues/:id                  partial update
DELETE /issues/:id                  admin only — moves to Trash (soft-delete; cascades to child tasks)
POST   /issues/:id/restore          admin only — clears deleted_at
DELETE /issues/:id/purge            admin only — hard delete (must be in Trash first)
GET    /issues/trash                admin only — list soft-deleted issues
PATCH  /issues/:id/archive          {archived: bool} — admin only
POST   /issues/:id/clone            {...field map}
POST   /issues/:id/complete-epic    bulk-transition children
GET    /issues/:id/aggregation      rollup stats
GET    /issues/:id/children
GET    /issues/:id/history          audit trail (who/when/diff)
GET    /issues/:id/comments
POST   /issues/:id/comments         {body, visibility?: 'internal'|'external'}   default internal (PAI-475)
PATCH  /comments/:id                {visibility: 'internal'|'external'}          author or admin
DELETE /comments/:id
GET    /issues/:id/anchors
```

## Issue relations

```
GET    /issues/:id/relations
POST   /issues/:id/relations        {target_id, type}   type: parent|groups|sprint|depends_on|impacts|...
DELETE /issues/:id/relations        {target_id, type} — admin only
GET    /issues/:id/members          list by relation (type=parent for an epic's tickets)
```

`type=parent` is the issue hierarchy (epic⊃ticket, ticket⊃task) — source=parent,
target=child, one parent per child. To put a ticket under an epic:
`POST /issues/{epic}/relations {target_id: ticket, type: parent}`. Legacy
`type=groups` with an epic source is auto-translated to `parent`. A second parent
for a child is rejected (409) — reparent via `parent_id` on issue update instead.

## Time entries

```
GET    /issues/:id/time-entries
POST   /issues/:id/time-entries     {started_at, stopped_at?, override?, comment?, user_id?}
                                      — super-admin only: user_id = create on behalf of another user
GET    /time-entries/:id
PUT    /time-entries/:id            partial update — super-admin can edit any user's entry
DELETE /time-entries/:id            super-admin can delete any user's entry
GET    /time-entries/running        active timers for current user
GET    /time-entries/recent         recent entries for quick re-entry
GET    /time-entries/today-summary  ?from=<RFC3339>&to=<RFC3339> (both required)
                                      — sum of the current user's stopped entries in [from, to)
                                      → {total_hours, count} (PAI-495)
```

Cross-user writes are stamped in `mutation_log` and emit a
`super_admin_act` audit line (PAI-335).

## Attachments

```
GET    /issues/:id/attachments
POST   /issues/:id/attachments      multipart upload — links immediately
GET    /attachments/:id/meta        metadata only
GET    /attachments/:id             fetch file bytes
DELETE /attachments/:id             admin only
POST   /attachments                 multipart — upload pending (not yet linked)
PATCH  /attachments/link            {issue_id, attachment_ids} — batch link pending
```

## Sprints

```
GET    /sprints                     ?include_archived=true
GET    /sprints/years               distinct years
GET    /sprints/:year               sprints for one year
POST   /sprints/batch               {...template} — admin only
PUT    /sprints/:id                 partial — admin only
POST   /sprints/:id/move-incomplete admin only — bump unfinished to next sprint
PUT    /sprints/:id/reorder         {member_order}
```

## Users

```
GET    /users
POST   /users                       admin only — accepts must_change_password (default true)
PUT    /users/:id                   admin only
POST   /users/:id/disable           admin only
DELETE /users/:id                   admin only
POST   /users/:id/reset-totp        admin only
```

## User memberships (project access)

```
GET    /users/:id/memberships                     admin — effective per-project level for every project
PUT    /users/:id/memberships/:projectId          admin — upsert grant {level: "none"|"viewer"|"editor"}
DELETE /users/:id/memberships/:projectId          admin — revert to role default

GET    /users/:id/projects                        admin — legacy portal-grant list (kept for compat)
POST   /users/:id/projects         {project_id}   admin — legacy grant (viewer-equivalent)
DELETE /users/:id/projects/:projectId             admin — legacy revoke

GET    /users/me/recent-projects                  self
POST   /users/me/recent-projects   {project_id}   self — record a visit
```

## Permissions & access audit

```
GET    /permissions/matrix                        any logged-in user — capability × level matrix for UI
GET    /access-audit                              admin — grant/update/revoke trail
```

## Tags

```
GET    /tags
POST   /tags                        admin only — {name, color?, description?}
PUT    /tags/:id                    admin only
DELETE /tags/:id                    admin only
POST   /issues/:id/tags             {tag_id}
DELETE /issues/:id/tags/:tag_id
GET    /projects/:id/tags
POST   /projects/:id/tags           {tag_id}
DELETE /projects/:id/tags/:tag_id
GET    /system-tag-rules
PUT    /system-tag-rules            admin only
```

`color` is constrained to a fixed 12-value palette (paired
background+foreground rendered by the SPA). Hex codes and arbitrary
CSS color names are rejected with `400 invalid color`. The canonical
list, in display order, is:

```
gray, slate, blue, indigo, purple, pink,
red, orange, yellow, green, teal, cyan
```

The same list is discoverable at `GET /api/schema` →
`enums.tag_colors` since schema version `1.2.2` — clients should
prefer the schema over hard-coding the values.

## Views

```
GET    /views
POST   /views                       {name, filters, columns}
PUT    /views/:id                   partial
DELETE /views/:id
PATCH  /views/order                 admin only
POST   /views/:id/pin
DELETE /views/:id/pin
```

## Search

```
GET    /search?q=<term>             min 2 chars; also matches issue keys (prefix)
                                      optional: project=<key-or-id>, type=<issue-type>, limit=N, offset=N
                                      legacy project scope also works: scope=project&project_id=<id>
```

## Agent Context

`/projects/:id/repos`, `/projects/:id/anchors`,
`/projects/:id/graph`, `/projects/:id/retrieve`, and `/issues/:id/anchors`
form the project-context layer for agents.

## Agents & inventories (PAI-326 / PAI-329 / PAI-331)

Each project declares the agents that work it plus shared inventories
(environments, deploy recipes) those agents inherit. Reads are
project-view-gated; writes are admin-only.

```
GET    /projects/:id/agents
POST   /projects/:id/agents            { name, description?, slash_command_name?, lane_tags?, metadata?, body?, bootstrap_steps?, non_negotiable_rules? }
PUT    /projects/:id/agents/:name      partial update
DELETE /projects/:id/agents/:name
GET    /projects/:id/agents/:name.json canonical agent artifact (inlines repos + environments + deploy_recipes)
GET    /projects/:id/agents/:name.md   markdown rendering for CLI / skill render
GET    /projects/:id/agents/:name.rev  plain-text rev hash for cheap-poll fallback
GET    /projects/:id/agents/events     SSE stream — auto-watch sync (PAI-331)
```

Project inventories — small CRUD trios shared by every agent in the project:

```
GET    /projects/:id/environments
POST   /projects/:id/environments      { name, url?, host_alias?, host_ip?, sort_order? }
PUT    /projects/:id/environments/:envId
DELETE /projects/:id/environments/:envId

GET    /projects/:id/deploy-recipes
POST   /projects/:id/deploy-recipes    { name, command?, summary?, sort_order? }
PUT    /projects/:id/deploy-recipes/:recipeId
DELETE /projects/:id/deploy-recipes/:recipeId
```

`/projects/:id/repos` (existing) is the third inventory; all three are
inlined into the canonical agent artifact at render time.

## Knowledge

Knowledge entries (memory, runbook, external_system, related_project,
guideline) live as issues with a discriminator on `issues.type` and
are addressed through one unified surface (PAI-394):

```
GET    /projects/:id/knowledge                          list all types
GET    /projects/:id/knowledge?type=<seg>               filtered list
GET    /projects/:id/knowledge/<type>/<slug>            single entry
GET    /projects/:id/knowledge/<type>/<slug>.rev        cheap-poll rev hash
POST   /projects/:id/knowledge                          { type, slug, title, body?, status?, metadata? }
PUT    /projects/:id/knowledge/<type>/<slug>            full replacement (PATCH → 405)
DELETE /projects/:id/knowledge/<type>/<slug>            soft-delete
```

`<type>` (and the `?type=` value) is the kebab-singular URL segment:
`memory`, `runbook`, `guideline`, `external-system`, `related-project`.
Request bodies accept either the URL segment or the SQL discriminator
(`external_system`) in their `type` field.

Memory-specific subroutes — slugs `references`, `stale`, `proposed`
are reserved server-side so they can't shadow a real entry:

```
POST /projects/:id/knowledge/memory/references          { ids: [...] }    bump decay counter
GET  /projects/:id/knowledge/memory/stale[?days=N]      decay candidates
GET  /projects/:id/knowledge/memory/proposed/stale[?days=N]   aged drafts
```

Cross-scope memory (out-of-project ownership) — user-scoped and
instance-scoped memory live alongside project knowledge but on their
own resources:

```
GET    /users/me/memory                   list this user's memory entries
POST   /users/me/memory                   { slug, title, body?, status?, metadata? }
GET    /users/me/memory/:slug
PUT    /users/me/memory/:slug             full replacement
DELETE /users/me/memory/:slug

GET    /instance/memory                   instance-wide memory (read = any user)
GET    /instance/memory/:slug
POST   /memory/:slug/promote              { from: 'project'|'user', to: 'user'|'instance', source_project_id? }
```

Issue-level surfaces that lean on the knowledge plane:

```
GET    /issues/:id/applicable-memories    PAI-342 — memories that match this issue's surface
GET    /issues/:id/lesson-capture-prompt  PAI-343 — prefilled prompt for closing-as-lesson UX
```

The discoverable schema at `GET /api/schema` exposes the registered
type set under `enums.knowledge_types` and the full surface under
the top-level `knowledge` block.

- `repos` declares the mirrored/source repositories a project uses.
- `anchors` ingests machine-generated issue-to-file locations per repo.
- anchors may include derived `symbol` metadata for the nearest enclosing
  function / method / class / type when the repo-side scanner can parse it.
- `graph` exposes typed entity relations (issues, repos, anchors, project).
- `graph/blast-radius` answers "what else is affected if this changes?" in a grouped-by-type shape.
- `retrieve` returns mixed context hits from issue text, anchors, knowledge
  entries, canonical agent/project inventories, derived symbols, and
  graph-neighbor expansion. It uses project-scoped lexical search plus
  deterministic local vector scoring and reciprocal-rank fusion across issue
  and context documents. Response shape includes `hits`, `strategy`, and
  `meta`.

There is no project manifest blob after PAI-358. Agents should compose
context from `repos`, `knowledge`, `anchors`, `graph`, `retrieve`, and
`agents/{name}.json`.

## Auto-watch sync (PAI-331)

Per-(user, device, project) opt-in for the agent-events SSE stream.
Default OFF — a fresh `(device, project)` tuple does not receive
pushes. Toggling OFF invalidates the device's active SSE connection
server-side.

```
GET    /auth/auto-watch                                this user's subscriptions
PUT    /auth/auto-watch/:deviceID/:projectID           { enabled: bool }
DELETE /auth/auto-watch/:deviceID/:projectID           explicit unsubscribe
```

PAI-341 (knowledge-plane sync) reuses the same `(user, device, project)`
table verbatim; one subscription covers all kinds for that triple.

## Adapter registry (PAI-332)

```
GET    /registry/adapters                              all adapters paimos can hand off to
```

Returns the merged in-tree + `$PAIMOS_ADAPTER_PATH`-discovered adapter
list, with `name`, `source` (`builtin` or `PAIMOS_ADAPTER_PATH`),
`harness`, and the rendering capabilities each one exposes.
Env-discovered adapters override in-tree adapters with the same name.

## Schema (self-describing discovery)

```
GET    /schema                      public — enums, transitions, entity shapes
```

Returns `{version, enums, transitions, entities, conventions}`. No auth
required. Cacheable: strong ETag + `Cache-Control: public, max-age=300`.
Version bumps whenever any enum, transition, field, or convention changes.
The CLI and MCP use this endpoint to validate user input before POSTing
so agents catch typos (e.g. `status: "completed"`) client-side.

## Session audit (opt-in)

```
GET    /sessions/{id}/activity      admin — mutations tagged with X-PAIMOS-Session-Id
                                   ?cursor=<id>&limit=100 (keyset pagination)
```

Off by default in v1. Enable with `PAIMOS_AUDIT_SESSIONS=true` env var
on the backend. When on, every mutation request (POST/PUT/PATCH/DELETE)
is recorded with the caller's `X-PAIMOS-Session-Id` header. Missing
header → row with `session_id = null` (non-fatal). The `paimos` CLI
auto-generates a UUIDv7 per invocation; `PAIMOS_SESSION_ID` env var
overrides so multi-step scripts can share a session.

## Reports / audit

```
GET    /projects/:id/acceptance-log              timeline of accept/reject decisions
GET    /projects/:id/acceptance-report           full report
GET    /projects/:id/reports/lieferbericht       JSON delivery report
GET    /projects/:id/reports/lieferbericht/pdf   PDF delivery report  ?text_source=tech|report (PAI-418)
GET    /projects/:id/reports/projektbericht/pdf  alias of lieferbericht/pdf
GET    /projektberichte/:code/pdf                snapshot-by-code PDF (portal default text_source=report)
GET    /reports/accruals                         admin only — per-user time rollup
```

## Project metadata

```
GET    /projects/suggest-key                     key suggester
GET    /projects/:id/cost-units
GET    /projects/:id/releases
GET    /cost-units                               cross-project distinct values
GET    /releases                                 cross-project distinct values
GET    /projects/:id/export/csv                  admin only
POST   /projects/:id/import/csv/preflight        admin only
POST   /projects/:id/import/csv                  admin only
POST   /import/csv/preflight                     admin only — global
POST   /import/csv                               admin only — global
```

---

## Enums

| Field | Values |
|-------|--------|
| `type` | `epic` `cost_unit` `release` `sprint` `ticket` `task` |
| `status` | `new` `backlog` `in-progress` `qa` `done` `delivered` `accepted` `invoiced` `cancelled` |
| `priority` | `low` `medium` `high` |
| issue-relation `type` | `parent` `groups` `sprint` `depends_on` `impacts` `follows_from` `blocks` `related` |

Hierarchy (epic⊃ticket, ticket⊃task) is the `parent` relation edge — the single
source of truth (source=parent, target=child, at most one parent per child). Set
it via `parent_id` on issue create/update OR a `type=parent` relation. `parent_id`
is kept in sync and still returned, but reads come from the edge. `groups` is
cost_unit/release container membership (M:N, orthogonal axis). Orphan
tickets/tasks allowed. 422 on invalid parent.  
Issue key: `{PROJECT_KEY}-{n}` e.g. `PAI-1` — computed, not stored.  
Project numeric IDs are assigned per-deployment in creation order — always `GET /projects` and match on `key` or `name` before POSTing. Do not hard-code project IDs from examples.

**Issue `{id}` accepts keys too.** Every `/issues/{id}/*` route resolves either
a numeric id (`462`) or an issue key (`PAI-83`, `PMO26-639`). Keys match
case-sensitively against `project.key` + `issue_number`. Malformed
references return 400; key-shaped refs with no matching row return 404.
Soft-deleted issues still resolve so `POST /issues/:id/restore` and
`DELETE /issues/:id/purge` work with keys.

---

## Create backlog item

```bash
# Resolve project id first — never hard-code from examples.
PID=$(curl -s -H "Authorization: Bearer $KEY" \
  https://paimos.example.com/api/projects \
  | jq '.[] | select(.key=="PAI") | .id')

curl -s -H "Authorization: Bearer $KEY" \
  -X POST "https://paimos.example.com/api/projects/$PID/issues" \
  -H "Content-Type: application/json" \
  -d '{"title":"...","type":"ticket","status":"backlog","priority":"medium",
       "description":"...","acceptance_criteria":"- [ ] ..."}'
```
