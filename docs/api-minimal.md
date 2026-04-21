# PAIMOS API — Quick Reference

Base URL: `https://paimos.example.com/api`  
Auth: `Authorization: Bearer <key>`  
Format: JSON in, JSON out.

---

## Auth

```
GET    /auth/me
POST   /auth/login                  {username, password}
POST   /auth/api-keys               {name} → {key} (shown once)
GET    /auth/api-keys
DELETE /auth/api-keys/:id
```

## Projects

```
GET    /projects                    ?status=active|archived
POST   /projects                    {name, key, description}
GET    /projects/:id
PUT    /projects/:id                partial update
DELETE /projects/:id                admin only
```

## Issues

```
GET    /projects/:id/issues         ?status= &priority= &type= &assignee_id=
POST   /projects/:id/issues         {title, type, status, priority, description, acceptance_criteria}
GET    /projects/:id/issues/tree    epic → ticket → task hierarchy
GET    /issues                      cross-project list (or pick-list when ?keys=PAI-1,PAI-2)
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
POST   /issues/:id/comments         {body}
DELETE /comments/:id
```

## Issue relations

```
GET    /issues/:id/relations
POST   /issues/:id/relations        {target_id, type}   type: groups|sprint|depends_on|impacts
DELETE /issues/:id/relations        {target_id, type} — admin only
GET    /issues/:id/members          list by relation
```

## Time entries

```
GET    /issues/:id/time-entries
POST   /issues/:id/time-entries     {started_at, stopped_at?, override?, comment?}
PUT    /time-entries/:id            partial update
DELETE /time-entries/:id
GET    /time-entries/running        active timers for current user
GET    /time-entries/recent         recent entries for quick re-entry
```

## Attachments

```
GET    /issues/:id/attachments
POST   /issues/:id/attachments      multipart upload — links immediately
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
POST   /users                       admin only
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
POST   /tags                        admin only
PUT    /tags/:id                    admin only
DELETE /tags/:id                    admin only
POST   /issues/:id/tags             {tag_id}
DELETE /issues/:id/tags/:tag_id
POST   /projects/:id/tags           {tag_id}
DELETE /projects/:id/tags/:tag_id
GET    /system-tag-rules
PUT    /system-tag-rules            admin only
```

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
```

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
GET    /projects/:id/reports/lieferbericht/pdf   PDF delivery report
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
| issue-relation `type` | `groups` `sprint` `depends_on` `impacts` `follows_from` `blocks` `related` |

Hierarchy: ticket → task via `parent_id` (strict 1:1). Group-level types
(epic / cost_unit / release) link to tickets via `issue_relations` (M:N).
Orphan tickets/tasks allowed. 422 on invalid parent.  
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
