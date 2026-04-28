# Project Management (+pm)

Tickets, backlog, and task tracking for paimos product work live in
**PAIMOS itself** — that is the single source of truth.

> **Instance:** <https://pm.barta.cm> (the `ppm` instance)
> **Project:** PAIMOS — key `PAI`, id `6`

This directory exists for **long-form product framing only**:

```
+pm/
├── README.md       # This file
├── PRD.md          # Product Requirements Document (vision)
└── FIELD_MATRIX.md # Field/feature matrix
```

The previous `backlog/` and `done/` markdown workflow is **deprecated**.
Past `done/` items have been migrated into PAIMOS as `done` tickets;
new work is filed directly in PAIMOS.

---

## Filing new work

Open the project in the UI:

    https://pm.barta.cm/projects/6

Or use the API (Bearer token from `~/Secrets/ppm/PPMAPIKEY.env`):

    curl -A "<your-ua>" \
         -H "Authorization: Bearer $PPMAPIKEY" \
         -H "Content-Type: application/json" \
         -d @body.json \
         https://pm.barta.cm/api/projects/6/issues

Issue schema (subset):

| field                 | values                                              |
| --------------------- | --------------------------------------------------- |
| `type`                | `ticket`, `epic`, `task`                            |
| `status`              | `new`, `backlog`, `accepted`, `done`, `cancelled`   |
| `priority`            | `low`, `medium`, `high`                             |
| `title`               | string                                              |
| `description`         | markdown                                            |
| `acceptance_criteria` | markdown checklist                                  |
| `parent_id`           | int (epic/parent linkage), nullable                 |

References to a ticket use its key, e.g. `PAI-188`, with the canonical
URL `https://pm.barta.cm/projects/6/issues/PAI-188`.

---

## Long-form docs in this directory

- **`PRD.md`** — product vision and requirement framing. Long-form
  context that would clutter individual tickets. Update via PR; link
  from tickets that depend on it.
- **`FIELD_MATRIX.md`** — feature/field matrix.

If a doc here grows enough that someone would actually file a ticket
against *it* (e.g. "rewrite section X"), the ticket goes in PAIMOS
and links back to the doc — not the other way around.
