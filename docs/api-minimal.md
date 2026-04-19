# PAIMOS API — Quick Reference

Base URL: `https://paimos.com/api`  
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
GET    /issues/:id
PUT    /issues/:id                  partial update
DELETE /issues/:id                  admin only
GET    /issues/:id/comments
POST   /issues/:id/comments         {body}
DELETE /comments/:id
GET    /issues/:id/history
```

## Tags / Users / Search

```
GET    /tags
GET    /users
GET    /search?q=<term>             min 2 chars, prefix match
POST   /issues/:id/tags             {tag_id}
DELETE /issues/:id/tags/:tag_id
```

---

## Enums

| Field | Values |
|-------|--------|
| `type` | `epic` `ticket` `task` |
| `status` | `open` `in-progress` `done` `closed` |
| `priority` | `low` `medium` `high` |

Hierarchy: epic → ticket → task. Orphan tickets/tasks allowed. 422 on invalid parent.  
Issue key: `{PROJECT_KEY}-{n}` e.g. `ACME-1` — computed, not stored.  
Project 2 = PAIMOS's own backlog.

---

## Create backlog item

```bash
curl -s -H "Authorization: Bearer $KEY" \
  -X POST https://paimos.com/api/projects/2/issues \
  -H "Content-Type: application/json" \
  -d '{"title":"...","type":"ticket","status":"open","priority":"medium",
       "description":"...","acceptance_criteria":"- [ ] ..."}'
```
