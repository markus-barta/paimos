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
 * useIssueSelection — PAI-565 (selection across lazy-loaded result sets).
 *
 * Distinguishes three intents that v1 conflated:
 *   - explicit ids   — the user ticked specific rows.
 *   - all-matching    — every row matching the *current query*, with an
 *                       exclusion set; bound to the query fingerprint so a
 *                       bulk action can never run against a different
 *                       filter/sort/search set than the user saw.
 *   - none.
 *
 * Loading more rows never changes an explicit selection; in all-matching the
 * newly loaded rows are already considered selected (minus exclusions), which
 * is the whole point. A query change invalidates an all-matching selection.
 */
import { ref, computed, watch, readonly } from 'vue'

export type SelectionMode = 'none' | 'explicit' | 'all-matching'

export interface SelectionResolved {
  mode: SelectionMode
  /** explicit mode: the chosen ids. */
  ids?: number[]
  /** all-matching mode: the bound query fingerprint. */
  fingerprint?: string
  /** all-matching mode: ids to exclude from the matching set. */
  exclude?: number[]
  /** how many issues the action will affect. */
  count: number
}

export interface UseIssueSelectionDeps {
  /** current query fingerprint (reactive getter). */
  fingerprint: () => string
  /** ids currently loaded in the window (reactive getter). */
  loadedIds: () => number[]
  /** total matching count for the current query (reactive getter). */
  total: () => number
}

export function useIssueSelection(deps: UseIssueSelectionDeps) {
  const mode = ref<SelectionMode>('none')
  const boundFingerprint = ref('')
  const explicit = new Set<number>()
  const exclusions = new Set<number>()
  const rev = ref(0) // selection-change counter to drive reactivity
  const bump = () => { rev.value++ }

  function clear() {
    mode.value = 'none'
    explicit.clear()
    exclusions.clear()
    boundFingerprint.value = ''
    bump()
  }

  // A query change can't keep an all-matching selection — the matching set is
  // now different. Explicit selections are intentionally preserved.
  watch(
    () => deps.fingerprint(),
    (fp) => {
      if (mode.value === 'all-matching' && fp !== boundFingerprint.value) clear()
    },
  )

  function toggle(id: number) {
    if (mode.value === 'all-matching') {
      if (exclusions.has(id)) exclusions.delete(id)
      else exclusions.add(id)
      bump()
      return
    }
    if (explicit.has(id)) explicit.delete(id)
    else explicit.add(id)
    mode.value = explicit.size > 0 ? 'explicit' : 'none'
    bump()
  }

  /** Select exactly the currently-loaded rows (header checkbox). */
  function selectAllLoaded() {
    explicit.clear()
    for (const id of deps.loadedIds()) explicit.add(id)
    exclusions.clear()
    mode.value = explicit.size > 0 ? 'explicit' : 'none'
    bump()
  }

  /** Select every row matching the current query, bound to its fingerprint. */
  function selectAllMatching() {
    mode.value = 'all-matching'
    boundFingerprint.value = deps.fingerprint()
    explicit.clear()
    exclusions.clear()
    bump()
  }

  function setExplicit(ids: number[]) {
    explicit.clear()
    for (const id of ids) explicit.add(id)
    exclusions.clear()
    mode.value = explicit.size > 0 ? 'explicit' : 'none'
    bump()
  }

  function isSelected(id: number): boolean {
    void rev.value
    if (mode.value === 'all-matching') return !exclusions.has(id)
    if (mode.value === 'explicit') return explicit.has(id)
    return false
  }

  /** How many issues the current selection represents (across the full set). */
  const effectiveCount = computed(() => {
    void rev.value
    if (mode.value === 'explicit') return explicit.size
    if (mode.value === 'all-matching') return Math.max(0, deps.total() - exclusions.size)
    return 0
  })

  /** How many of the currently-loaded rows are selected (for the checkbox UI). */
  const loadedSelectedCount = computed(() => {
    void rev.value
    return deps.loadedIds().filter((id) => isSelected(id)).length
  })

  const active = computed(() => mode.value !== 'none')

  /** True when an all-matching selection no longer matches the live query. */
  function isStale(currentFingerprint?: string): boolean {
    if (mode.value !== 'all-matching') return false
    return boundFingerprint.value !== (currentFingerprint ?? deps.fingerprint())
  }

  /** Resolve to a server-executable description of the selection. */
  function resolve(): SelectionResolved {
    if (mode.value === 'explicit') {
      return { mode: 'explicit', ids: [...explicit], count: explicit.size }
    }
    if (mode.value === 'all-matching') {
      return {
        mode: 'all-matching',
        fingerprint: boundFingerprint.value,
        exclude: [...exclusions],
        count: effectiveCount.value,
      }
    }
    return { mode: 'none', count: 0 }
  }

  return {
    mode: readonly(mode),
    boundFingerprint: readonly(boundFingerprint),
    active,
    effectiveCount,
    loadedSelectedCount,
    isSelected,
    toggle,
    setExplicit,
    selectAllLoaded,
    selectAllMatching,
    clear,
    isStale,
    resolve,
  }
}
