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

/**
 * useColumnConfig — persisted column visibility for IssueList flat table.
 *
 * `key` and `title` columns are always visible (pinned). All others are toggleable.
 * v2 columns (billing_type, total_budget, rate_hourly, rate_lp, start_date,
 * end_date, group_state, sprint_state, jira_id, jira_version, jira_text) are
 * hidden by default — user opts in via column selector.
 * Persisted to localStorage keyed by scope (project id or 'global').
 *
 * Accepts a reactive scope so the config reloads when the user navigates
 * between projects in the same SPA session without a full remount.
 */

import { ref, computed, watch, isRef } from 'vue'
import type { Ref } from 'vue'

export interface ColumnDef {
  key:    string
  label:  string
  pinned: boolean   // true = always shown, not toggleable
}

export const ALL_COLUMNS: ColumnDef[] = [
  { key: 'key',          label: 'Key',          pinned: true  },
  { key: 'type',         label: 'Type',         pinned: false },
  { key: 'title',        label: 'Title',        pinned: true  },
  { key: 'status',       label: 'Status',       pinned: false },
  { key: 'priority',     label: 'Priority',     pinned: false },
  { key: 'cost_unit',    label: 'Cost Unit',    pinned: false },
  { key: 'release',      label: 'Release',      pinned: false },
  { key: 'assignee',     label: 'Assignee',     pinned: false },
  { key: 'tags',         label: 'Tags',         pinned: false },
  { key: 'epic',         label: 'Epic',         pinned: false },
  { key: 'sprint',       label: 'Sprint',       pinned: false },
  // v2 — hidden by default
  { key: 'billing_type', label: 'Billing',      pinned: false },
  { key: 'total_budget', label: 'Budget',       pinned: false },
  { key: 'rate_hourly',  label: 'Rate/h',       pinned: false },
  { key: 'rate_lp',       label: 'Rate LP',      pinned: false },
  { key: 'estimate_hours', label: 'Est. h',     pinned: false },
  { key: 'estimate_lp',   label: 'Est. LP',     pinned: false },
  { key: 'ar_hours',      label: 'AR h',        pinned: false },
  { key: 'ar_lp',         label: 'AR LP',       pinned: false },
  { key: 'start_date',   label: 'Start',        pinned: false },
  { key: 'end_date',     label: 'End',          pinned: false },
  { key: 'group_state',  label: 'Group State',  pinned: false },
  { key: 'sprint_state', label: 'Sprint State', pinned: false },
  { key: 'jira_id',      label: 'Jira ID',      pinned: false },
  { key: 'jira_version', label: 'Jira Version', pinned: false },
  { key: 'jira_text',    label: 'Jira Text',    pinned: false },
  { key: 'booked_hours', label: 'Booked',       pinned: false },
  { key: 'actions',      label: 'Actions',      pinned: true  },
]

// O(1) lookup map — keyed by column key
const COL_MAP = new Map(ALL_COLUMNS.map(c => [c.key, c]))

// Columns hidden by default — v2 fields opt-in
const DEFAULT_HIDDEN = new Set<string>([
  'epic',
  'billing_type', 'total_budget', 'rate_hourly', 'rate_lp',
  'estimate_hours', 'estimate_lp', 'ar_hours', 'ar_lp',
  'start_date', 'end_date', 'group_state', 'sprint_state',
  'jira_id', 'jira_version', 'jira_text',
  'booked_hours',
])

const LS_KEY = (scope: string) => `paimos:columns:${scope}`

export function useColumnConfig(scope: string | Ref<string>) {
  const hidden = ref<Set<string>>(new Set())

  function resolveScope(): string {
    return isRef(scope) ? scope.value : scope
  }

  function load() {
    try {
      const raw = localStorage.getItem(LS_KEY(resolveScope()))
      if (!raw) { hidden.value = new Set(DEFAULT_HIDDEN); return }
      const arr: string[] = JSON.parse(raw)
      hidden.value = new Set(arr)
    } catch {
      hidden.value = new Set(DEFAULT_HIDDEN)
    }
  }

  function save() {
    localStorage.setItem(LS_KEY(resolveScope()), JSON.stringify([...hidden.value]))
  }

  // Reload when scope changes (user navigates between projects)
  if (isRef(scope)) {
    watch(scope, load)
  }

  load()

  // Watch for any replacement of the hidden ref (toggle/reset replace the whole Set)
  watch(() => [...hidden.value], save)

  const visibleKeys = computed(() =>
    ALL_COLUMNS
      .filter(c => c.pinned || !hidden.value.has(c.key))
      .map(c => c.key)
  )

  function isVisible(key: string): boolean {
    const col = COL_MAP.get(key)
    if (!col) return false   // unknown key → not visible (fail closed)
    return col.pinned || !hidden.value.has(key)
  }

  function toggle(key: string) {
    const col = COL_MAP.get(key)
    if (!col || col.pinned) return
    const next = new Set(hidden.value)
    if (next.has(key)) {
      next.delete(key)
    } else {
      next.add(key)
    }
    hidden.value = next
  }

  function reset() {
    hidden.value = new Set(DEFAULT_HIDDEN)
  }

  // Re-read from localStorage — used when external code (e.g. applyView) has
  // written a new value to the key and wants the composable to pick it up.
  function forceReload() {
    load()
  }

  // Directly set hidden columns from a JSON array string (e.g. from a saved view).
  // Bypasses localStorage round-trip — clean, no side effects.
  function setFromJSON(json: string) {
    try {
      const arr: string[] = JSON.parse(json)
      hidden.value = new Set(arr)
    } catch {
      hidden.value = new Set(DEFAULT_HIDDEN)
    }
  }

  // Serialise current hidden set to JSON — used when snapshotting a view.
  function toJSON(): string {
    return JSON.stringify([...hidden.value].sort())
  }

  return { ALL_COLUMNS, visibleKeys, isVisible, toggle, reset, forceReload, setFromJSON, toJSON }
}
