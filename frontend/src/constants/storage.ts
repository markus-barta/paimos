/*
 * PAIMOS — Your Professional & Personal AI Project OS
 * Copyright (C) 2026 Markus Barta <markus@barta.com>
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as
 * published by the Free Software Foundation, version 3.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
 * GNU Affero General Public License for more details.
 *
 * You should have received a copy of the GNU Affero General Public
 * License along with this program. If not, see <https://www.gnu.org/licenses/>.
 */

// PAI-40 — localStorage key catalog.
//
// Single source of truth for every browser-side storage key the app
// touches. Before adding a new key: pick a name shaped as
// `paimos:<domain>:<thing>` (lowercase, colon-separated) and add it
// here, then import from this module — do not inline the string at
// the callsite.
//
// Five historical outliers keep non-standard names because renaming
// would silently wipe existing user preferences on upgrade:
//   - `sidebar-color-bg`, `sidebar-color-pattern` — no `paimos:` prefix
//   - `issue-display-type-icon`, `issue-display-type-text` — no `paimos:` prefix
//   - `paimos_time_unit` — underscore instead of colon

// ── Static keys ─────────────────────────────────────────────────────

// Sidebar layout
export const LS_SIDEBAR_COLLAPSED = 'paimos:sidebar:collapsed'
export const LS_SIDEBAR_PINNED    = 'paimos:sidebar:pinned'
export const LS_SIDEBAR_WIDTH     = 'paimos:sidebar:width'

// Sidebar colors (legacy names)
export const LS_SIDEBAR_BG_COLOR      = 'sidebar-color-bg'
export const LS_SIDEBAR_PATTERN_COLOR = 'sidebar-color-pattern'

// Branding
export const LS_BRANDING_FILE = 'paimos:branding-file'

// Issue type colors
export const LS_TYPE_COLOR_EPIC   = 'paimos:type-color-epic'
export const LS_TYPE_COLOR_TICKET = 'paimos:type-color-ticket'
export const LS_TYPE_COLOR_TASK   = 'paimos:type-color-task'

// Table row appearance
export const LS_TABLE_ROW_BORDERS      = 'paimos:table-row-borders'
export const LS_TABLE_ROW_STRIPES      = 'paimos:table-row-stripes'
export const LS_TABLE_ROW_BORDER_COLOR = 'paimos:table-row-border-color'
export const LS_TABLE_ROW_STRIPE_COLOR = 'paimos:table-row-stripe-color'

// Accruals report accent
export const LS_ACCRUALS_ACCENT = 'paimos:accruals-accent'

// Issue list
export const LS_EPIC_DISPLAY_MODE = 'paimos:epic-display-mode'

// Issue display toggles (legacy names)
export const LS_ISSUE_DISPLAY_TYPE_ICON = 'issue-display-type-icon'
export const LS_ISSUE_DISPLAY_TYPE_TEXT = 'issue-display-type-text'

// Time entries panel
export const LS_TIME_ENTRIES_EXPANDED = 'paimos:te-expanded'

// Views
export const LS_VIEWS_MRU = 'paimos:views:mru'

// Search
export const LS_SEARCH_LAST_QUERY = 'paimos:search:lastQuery'

// Timer panel
export const LS_TIMER_PANEL_OPEN = 'paimos:timer-panel:open'

// Time unit (legacy name)
export const LS_TIME_UNIT = 'paimos_time_unit'

// ── Dynamic key factories ───────────────────────────────────────────

/** Column visibility per list scope (e.g. "global", "project:42"). */
export const lsColumnsKey = (scope: string) => `paimos:columns:${scope}`

/** Issue-list filter state per project. `null`/`undefined` → "global". */
export const lsFiltersKey = (projectId: number | string | null | undefined) =>
  `paimos:filters:${projectId ?? 'global'}`

/** Last-selected view per (user, scope). `undefined` userId → 0. */
export const lsLastViewKey = (userId: number | undefined, scope: string) =>
  `paimos:views:last:${userId ?? 0}:${scope}`

/** Toolbar sprint selection per project (or "global"). */
export const lsSprintNavKey = (id: number | string) =>
  `paimos:sprint-nav:${id}`
