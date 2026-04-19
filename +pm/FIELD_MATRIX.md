# Issue Field Matrix

> Source of truth for which fields appear per issue type — in view, edit, and column selector.
> Edit this file, then implement. ✅ = shown/available · — = not applicable

## Hierarchy

```
Project
  └── Group (epic / cost_unit)
        └── Ticket  ←  Sprint
              ├── Time-Entry
              └── Task
```

---

## Field Matrix

| Field               | epic | cost_unit | release | sprint | ticket | task |
| ------------------- | :--: | :-------: | :-----: | :----: | :----: | :--: |
| title               |  ✅  |    ✅     |   ✅    |   ✅   |   ✅   |  ✅  |
| status              |  ✅  |    ✅     |   ✅    |   ✅   |   ✅   |  ✅  |
| priority            |  ✅  |    ✅     |   ✅    |   ✅   |   ✅   |  ✅  |
| assignee            |  ✅  |    ✅     |   ✅    |   ✅   |   ✅   |  ✅  |
| description         |  ✅  |    ✅     |   ✅    |   ✅   |   ✅   |  ✅  |
| acceptance_criteria |  ✅  |    ✅     |    —    |    —   |   ✅   |   —  |
| notes               |  ✅  |    ✅     |   ✅    |   ✅   |   ✅   |  ✅  |
| tags                |  ✅  |    ✅     |   ✅    |   ✅   |   ✅   |  ✅  |
| parent_id           |   —  |    ✅     |   ✅    |   ✅   |   ✅   |  ✅  |
| cost_unit (ref)     |  ✅  |     —     |    —    |    —   |   ✅   |  ✅  |
| release (ref)       |  ✅  |     —     |    —    |    —   |   ✅   |  ✅  |
| depends_on          |  ✅  |     —     |    —    |    —   |   ✅   |  ✅  |
| impacts             |  ✅  |     —     |    —    |    —   |   ✅   |  ✅  |
| billing_type        |  ✅  |    ✅     |    —    |    —   |    —   |   —  |
| total_budget        |  ✅  |    ✅     |    —    |    —   |    —   |   —  |
| rate_hourly         |  ✅  |    ✅     |    —    |    —   |    —   |   —  |
| rate_package        |  ✅  |    ✅     |    —    |    —   |    —   |   —  |
| start_date          |   —  |     —     |   ✅    |   ✅   |    —   |   —  |
| end_date            |   —  |     —     |   ✅    |   ✅   |    —   |   —  |
| group_state         |   —  |     —     |   ✅    |    —   |    —   |   —  |
| sprint_state        |   —  |     —     |    —    |   ✅   |    —   |   —  |
| jira_id             |  ✅  |    ✅     |    —    |   ✅   |   ✅   |   —  |
| jira_version        |   —  |     —     |   ✅    |    —   |    —   |   —  |
| jira_text           |   —  |     —     |    —    |   ✅   |    —   |   —  |

---

## Column Selector

All v2 columns hidden by default (opt-in via column selector).

| Column key    | Label        | Default  |
| ------------- | ------------ | :------: |
| key           | Key          | visible (pinned) |
| type          | Type         | visible  |
| title         | Title        | visible (pinned) |
| status        | Status       | visible  |
| priority      | Priority     | visible  |
| cost_unit     | Cost Unit    | visible  |
| release       | Release      | visible  |
| assignee      | Assignee     | visible  |
| tags          | Tags         | visible  |
| billing_type  | Billing      | hidden   |
| total_budget  | Budget       | hidden   |
| rate_hourly   | Rate/h       | hidden   |
| rate_package  | Rate pkg     | hidden   |
| start_date    | Start        | hidden   |
| end_date      | End          | hidden   |
| group_state   | Group State  | hidden   |
| sprint_state  | Sprint State | hidden   |
| jira_id       | Jira ID      | hidden   |
| jira_version  | Jira Version | hidden   |
| jira_text     | Jira Text    | hidden   |

---

## Notes

- `epic` = "Group" in the data model diagram
- `cost_unit` = sibling group type, shares billing fields with epic
- `ticket` = story / change request / bug — central work item
- `task` = smallest leaf; no billing, no jira, no dates
- `release` and `sprint` are container/planning types — no billing fields
- `parent_id` hidden for `epic` (top of hierarchy, no parent)
- `acceptance_criteria` only for types with deliverable scope (epic, cost_unit, ticket)
- New columns are all hidden by default — user opts in via column selector
