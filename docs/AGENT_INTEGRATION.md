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

# Search across all accessible projects
curl -s -H "Authorization: Bearer $KEY" \
  "https://paimos.example.com/api/search?q=authentication+bug"
```

Search also accepts issue keys — `?q=PAI-42` will find that specific
issue, and partial keys (`PAI-4`) prefix-match.

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

- **Admin** agents see every project and can create/delete projects,
  users, tags, and sprints.
- **Member** agents see only the projects their user account is
  assigned to, and cannot perform admin-only actions (project CRUD,
  user CRUD, some delete paths, etc.).
- **External** agents use the portal-only route surface (read-only
  issues + accept/reject).

**Recommendation**: create a dedicated user account for each agent
(e.g. `ci-bot`, `triage-agent`, `release-agent`) with the minimum role
it needs. Do not share keys across agents — revoking a compromised
key should never disrupt an unrelated workflow.

---

## Best practices for agent implementors

1. **Search before creating.** Run `GET /api/search?q=...` first so
   your agent does not create duplicate issues for the same symptom.
2. **Reference issue keys in comments.** Write `PAI-42` style keys in
   prose so cross-linking from another issue picks them up. The web UI
   autolinks them.
3. **Follow the status lifecycle.**
   `backlog → open → in-progress → done`. Avoid jumping straight to
   `closed` — that bypasses the review/accept stage humans expect.
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
  and the `role` / `user_project_access` tables in
  [`DATA_MODEL.md`](DATA_MODEL.md). Admin-gated routes are marked with
  `auth.RequireAdmin` in `backend/main.go`.

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

Before creating a new issue, always search for the title first.
Post a build report as a comment on the issue you just finished.
Use markdown freely — the web UI renders it.
```

That is the whole integration surface. An agent that can `curl` can
collaborate.
