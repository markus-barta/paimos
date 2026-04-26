# Undo Specification

`PAI-209` ships cross-mutation undo as a durable, request-correlated activity log with explicit conflict resolution. This is whole-mutation undo, not editor-text undo.

## Scope

- Own-mutation undo only
- Durable `mutation_log` audit rows
- Structured conflict responses
- Explicit resolution modal
- Undo + redo stacks
- Per-user stack depth control

Out of scope for this shipped slice:

- Cross-user undo
- Bulk multi-step rollback
- Text-editor keystroke undo
- A full mutation-class matrix for every entity family

## Conflict Patterns

Each pattern has one conservative default that avoids silently overwriting newer work.

| Pattern | Meaning | Conservative default |
|---|---|---|
| `field-changed-by-other` | A tracked field changed after the original mutation | `keep_theirs` |
| `parent-deleted` | Undo wants to restore a parent that is gone | `orphan` |
| `target-deleted` | Undo/redo wants to restore a relation target that is gone | `skip_relation` |
| `sprint-closed` | A sprint relation points to a closed sprint | `cancel` |
| `field-set-deleted` | Undo wants to restore a deleted enum-like value | `clear_field` |
| `bulk-children-modified` | Some children in a batch drifted | `revert_only_unmodified` |
| `time-entry-invoiced` | Entry is irreversible after invoicing | refuse with `423 Locked` |
| `permission-revoked` | User no longer has edit permission | refuse with `403 Forbidden` |

The current implementation fully ships:

- `field-changed-by-other`
- `parent-deleted`
- `target-deleted`

The remaining patterns are reserved by contract for later mutation-class expansion.

## API Contract

### Clean undo

```http
POST /api/undo/{logId}
200 OK
{
  "undone": true,
  "log_id": 42,
  "request_id": "req_..."
}
```

### Clean redo

```http
POST /api/redo/{logId}
200 OK
{
  "redone": true,
  "log_id": 42,
  "request_id": "req_..."
}
```

### Conflict

```http
POST /api/undo/{logId}
409 Conflict
{
  "status": "conflict",
  "log_id": 42,
  "request_id": "req_...",
  "mode": "undo",
  "mutation_type": "issue.update",
  "conflicts": [
    {
      "pattern": "field-changed-by-other",
      "field": "status",
      "their_value": "qa",
      "current_value": "qa",
      "target_value": "backlog",
      "options": [
        { "id": "overwrite", "label": "Use my target value", "default": true },
        { "id": "keep_theirs", "label": "Keep the newer value", "default": false }
      ]
    }
  ],
  "cascading_blockers": [
    {
      "pattern": "parent-deleted",
      "target_id": 42,
      "description": "Parent issue 42 no longer exists in active state.",
      "options": [
        { "id": "orphan", "label": "Make this issue top-level", "default": true },
        { "id": "cancel", "label": "Cancel", "default": false }
      ]
    }
  ]
}
```

### Resolution submission

```http
POST /api/undo/{logId}/resolve
Content-Type: application/json

{
  "field_choices": {
    "status": "overwrite"
  },
  "cascade_choices": {
    "parent-deleted": "orphan"
  }
}
```

Success:

```json
{
  "applied": true,
  "resolved": true,
  "log_id": 42,
  "request_id": "req_..."
}
```

### Other statuses

- `410 Gone`: mutation fell off the active stack or is no longer redoable
- `423 Locked`: irreversible mutation
- `403 Forbidden`: reserved for permission re-check failures

## Modal Mockup

```text
┌─ Undo needs your input ───────────────────────────────────────────────┐
│ issue.update                                                         │
│ Some fields changed since the original mutation.                     │
│ Nothing will be overwritten silently. Conservative options are       │
│ pre-selected.                                                        │
│                                                                      │
│ Field conflicts                                                      │
│  status                                                              │
│  Current: qa                                                         │
│  Target:  backlog                                                    │
│  ◉ Use my target value                                               │
│  ○ Keep the newer value                                              │
│                                                                      │
│ Cascade blockers                                                     │
│  Parent issue 42 no longer exists in active state.                   │
│  ◉ Make this issue top-level                                         │
│  ○ Cancel                                                            │
│                                                                      │
│                        [ Cancel ] [ Apply with selections ]          │
└──────────────────────────────────────────────────────────────────────┘
```

## State Flow

```text
record mutation
   |
   v
user clicks undo/redo
   |
   +--> active-stack check fails -----------> 410 Gone
   |
   +--> undoable=false ---------------------> 423 Locked
   |
   +--> current hash matches expected
   |        |
   |        +--> apply inverse/redo --------> 200 OK
   |
   +--> current hash drifted
            |
            +--> classify conflict ---------> 409 Conflict
            |                                  |
            |                                  +--> user cancels
            |                                  |
            |                                  +--> user resolves
            |                                             |
            |                                             +--> apply selection
            |                                             +--> 200 OK
            |
            +--> unsupported / forbidden ------> 423 / 403
```

## Configuration

### Runtime stack depth

- DB-backed `undo_stack_depth`
- Editable under `Settings -> Admin -> System`
- Bounds: `1..20`
- Default: `3`

### Retention

- `PAIMOS_RETENTION_DAYS_MUTATION_LOG`
- Default: `90`
- Controls row existence, not undoability

### GDPR

User erase scrubs:

- `mutation_log.user_id`
- `mutation_log.session_id`
- known display-name fields in stored snapshots

## Audit Invariant

- `before_state` and `after_state` hold entity-shaped field snapshots
- string fields are capped at `32 KiB`
- `inverse_op.body` is the reverse REST payload
- prompt bodies and model-response bodies are not persisted as dedicated audit payloads in `mutation_log`
