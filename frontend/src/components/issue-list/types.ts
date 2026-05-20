/*
 * PAI-468 / PAI-469: shared types for the IssueTable + IssueFilterBar
 * pair. Lives in its own file so both Vue SFCs and consumers (column
 * registries, vitest specs) can import the names without going through
 * a single component's default export.
 */

import type { VNode } from 'vue'

// IssueLike is the minimal shape the shared table expects — every
// consumer's row carries at least an id. Internal and portal views pass
// richer types (Issue, PortalIssue, AdminVisibilityIssue) via structural
// compatibility; the render callbacks cast internally as needed.
export interface IssueLike {
  id: number
}

export interface ColumnDef<T extends IssueLike = IssueLike> {
  key: string
  label: string
  width?: string
  sortable?: boolean
  /** Cell renderer — primitive (rendered as text) or a VNode (rendered
   *  via <component :is>). Use h(...) from your column registry when
   *  you need StatusDot, AppIcon, etc. */
  render: (issue: T) => string | number | VNode | null | undefined
}

export interface RowAction {
  key: string
  label: string
  variant?: 'primary' | 'ghost' | 'danger'
  disabled?: boolean
  onClick: () => void
}

export interface EmptyState {
  title: string
  subtitle?: string
  actionLabel?: string
  onAction?: () => void
}

// PAI-469: filter state shared between the internal IssueList and the
// portal PortalProjectView. Only the fields the shared filter bar
// supports — assignee/sprint/release/cost_unit/saved-view chrome live
// in the consumer.
export interface SharedFilterState {
  status: string[]
  type: string[]
  priority: string[]
  tagIds: number[]
  q: string
}

export type EnabledFilter =
  | 'status'
  | 'type'
  | 'priority'
  | 'tag'
  | 'q'

export interface TagOption {
  id: number
  name: string
  color?: string
}

export interface FilterOption {
  value: string
  label: string
}
