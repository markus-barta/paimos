# IssueList v2

This note is the PAI-561 discovery/design checkpoint for the PAI-560
IssueList v2 epic. It follows the PAIMOS product posture from the PRD:
small self-hosted runtime, fast local workflows, one understandable list
engine, and no Jira-sized machinery.

## Current Ownership Map

- `backend/handlers/issues_list.go`
  - `ListAllIssues`: global `/api/issues` already owned search, count,
    pagination, and `ids_only`, but sort was implicit except for search
    ranking.
  - `ListIssues`: project `/api/projects/{id}/issues` returned a bare
    array, with project search and filters but no count/window envelope.
  - `appendGlobalIssueSearchFilter` / `issueSearchRankOrder`: global
    search matched issue fields, comments, and computed keys, then
    ranked exact key/title matches first.
- `backend/handlers/issues.go`
  - `applyIssueFilters`: common status/type/priority/cost-unit/release
    filters were server-owned. Negated tags/sprints/assignees,
    `parent_id`, project negation, and portal-visibility filtering were
    previously not part of the common server query.
- `backend/handlers/portal.go`
  - `PortalListIssues`: portal had visibility-safe fields and
    allowlisted sort/filter values, but returned a bare array and its
    search was narrower than the internal list.
- `frontend/src/views/IssuesView.vue`
  - Global list owned request sequencing, paging, freshness, and
    server search. It delegated filter and sort state to
    `IssueList.vue`, so sort did not previously round-trip to the
    server.
- `frontend/src/views/ProjectDetailView.vue`
  - Project list loaded all matching rows as a bare array through
    `services/projectDetail.ts`; freshness also expected a bare array.
- `frontend/src/components/IssueList.vue`
  - Owns filter state, saved views, visible columns, inline edit,
    selection, side panel, progressive render, and the table toolbar.
  - Emits `server-filter-change`; now also emits `server-sort-change`.
- `frontend/src/components/IssueTable.vue`
  - Owns sticky header, frozen key/actions columns, inline cell edit,
    column resize, and row interactions.
- `frontend/src/views/portal/PortalProjectView.vue`
  - Owns portal-safe filter URL state, KPI strip, tabs, and the
    new-request modal. The customer table and side-panel selection now
    render through `IssueList.vue` in `mode="customer"`; the old
    `components/issue-list` table/filter pair was removed in PAI-476.

## Known Failure Causes

- Project and global endpoints had different response contracts, so
  counts, windows, and freshness could not behave the same.
- Sorting was client-owned in `IssueList.vue`; with only the first
  server window loaded, sorting could reorder the window without
  requerying the full matching set.
- `Select all matching` called global `/api/issues?ids_only=1`; in a
  project view it relied on the caller remembering to add project scope.
- Several UI filters had local-only semantics, so totals and loaded
  rows could disagree when the full matching set was not loaded.
- Selection mode made the checkbox column appear before the frozen key
  column, while the key column still stuck at `left: 0`, causing overlap
  under horizontal scroll.

## v2 Contract

Internal list endpoints support the same explicit envelope:

```json
{
  "issues": [],
  "total": 0,
  "returned": 0,
  "offset": 0,
  "limit": 100,
  "has_more": false,
  "sort": "title",
  "order": "asc",
  "query": "abc",
  "revision": "...",
  "fingerprint": "...",
  "selection_fingerprint": "..."
}
```

- `GET /api/issues` returns the envelope by default.
- `GET /api/projects/{id}/issues?envelope=1` returns the envelope while
  preserving the legacy bare-array response unless `envelope=1` is set.
- `GET /api/portal/projects/{id}/issues?envelope=1` returns a portal-safe
  envelope with the same window/count shape.
- `limit` + `offset` are the loaded window. `returned` is the number of
  rows in this response, `has_more` reports whether another window
  exists, and `total` is the full matching set count. `limit=0` is the
  explicit "show all" mode and is preserved across compatible query
  transitions such as sorting. `ids_only=1` returns the full matching id
  set up to the existing bounded cap plus the same
  `selection_fingerprint`.
- `fingerprint` identifies the ordered query family for cache windows.
  It is stable across `offset` changes. `selection_fingerprint`
  identifies the all-matching result set for selection/bulk operations,
  independent from display sort.
- `sort` and `order` are allowlisted server inputs. Unknown columns or
  invalid order values return `400`.
- `q` searches the full matching set before windowing. Global/project
  search keeps the existing best-match ranking when no explicit sort is
  selected.

## Preserved, Shimmed, Deletable

- Preserved:
  - Existing `IssueList.vue` toolbar, saved views, inline edit,
    side-panel URL sync, column visibility, and progressive rendering.
  - Existing global `/api/issues` envelope shape for current callers.
  - Portal field minimization and visibility gate.
- Shimmed:
  - Project `/api/projects/{id}/issues` keeps the bare-array response
    for old callers; v2 callers opt in with `envelope=1`.
  - `services/projectDetail.ts` keeps `loadProjectIssues()` and adds
    `loadProjectIssuesEnvelope()` for v2 views.
- Deletable later:
  - Client-side duplicate filter/sort fallback inside `IssueList.vue`
    once all consumers use server envelopes and capability modes.
  - Portal project view's local search overlay once envelope search is
    proven with production portal data.
  - The project bare-array shim after one minor release and a changelog
    migration note.

## Verification Focus

- Backend: window/count/sort/search parity for global, project, and
  portal endpoints; ids-only project scope; filter negation.
- Frontend: sort emits a server query without resetting the loaded
  window intent; stale responses are ignored; project/global freshness
  applies envelopes.
- UX: horizontal scroll keeps checkbox, key, and actions columns
  readable; table viewport owns its scroll; side panels must not cover
  the header or frozen columns when pinned.
