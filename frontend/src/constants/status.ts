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

// PAI-44 — status constant catalog.
//
// Use these for iteration / array-valued cases (select options, filter
// presets, report defaults). For single-value equality checks like
// `if (s === 'done')`, the `IssueStatus` union in `types.ts` already
// gives compile-time safety — no need to import a const there.
//
// Not centralized here (deliberately):
//   - `SprintBoardView`'s "completed" arrays (`['done','accepted','invoiced']`
//     and `['done','accepted','invoiced','cancelled']`) — narrower semantics
//     (missing `delivered`) that may or may not be intentional. Left inline
//     pending a product review.
//   - `useInlineEdit.ts` vs `IssueEditSidebar.vue` "terminal" sets disagree
//     on whether `delivered` counts as terminal — same caveat.
//   - `useIssueFilter.ts` `KNOWN_STATUSES` — the URL-parser's tolerant
//     set, different concept.

import type { IssueStatus } from '@/types'

/** All issue statuses, in canonical workflow order. */
export const STATUSES: readonly IssueStatus[] = [
  'new',
  'backlog',
  'in-progress',
  'qa',
  'done',
  'delivered',
  'accepted',
  'invoiced',
  'cancelled',
] as const

/** Statuses the accruals report counts as completed work by default. */
export const ACCRUALS_DEFAULT_STATUSES: readonly IssueStatus[] = [
  'done',
  'delivered',
  'accepted',
  'invoiced',
] as const

/**
 * Statuses the accruals report exposes as opt-in extras alongside the
 * defaults. Deliberately excludes `qa` — in-flight QA work is never
 * treated as accruable.
 */
export const ACCRUALS_EXTRA_STATUSES: readonly IssueStatus[] = [
  'new',
  'backlog',
  'in-progress',
  'cancelled',
] as const
