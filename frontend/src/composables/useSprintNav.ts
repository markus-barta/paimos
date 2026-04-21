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
 * useSprintNav — Sprint navigator state, navigation functions, persistence.
 */
import { ref, computed, watch, nextTick } from 'vue'
import type { Ref } from 'vue'
import type { Sprint } from '@/types'
import { lsSprintNavKey } from '@/constants/storage'

export interface UseSprintNavOptions {
  projectId: Ref<number | undefined>
  allSprints: () => Sprint[]
  showArchivedSprints: Ref<boolean>
}

export function useSprintNav(opts: UseSprintNavOptions) {
  const toolbarSprintIds = ref<number[]>([])
  const sprintNavOpen = ref(false)
  const sprintNavSearch = ref('')
  const snSelectorEl = ref<HTMLElement | null>(null)
  const snSearchEl = ref<HTMLElement | null>(null)

  const orderedSprints = computed(() =>
    [...opts.allSprints()].filter(s => !s.archived || opts.showArchivedSprints.value)
      .sort((a, b) => (a.start_date ?? '').localeCompare(b.start_date ?? ''))
  )

  const currentSprintId = computed(() => {
    const today = new Date().toISOString().slice(0, 10)
    return orderedSprints.value.find(s => s.start_date && s.end_date && s.start_date <= today && today <= s.end_date)?.id ?? null
  })

  const navAnchor = computed(() => {
    if (!toolbarSprintIds.value.length) return null
    return orderedSprints.value.find(s => toolbarSprintIds.value.includes(s.id)) ?? null
  })

  const activeRange = computed(() => {
    if (!navAnchor.value) return 0
    const idx = orderedSprints.value.findIndex(s => s.id === navAnchor.value!.id)
    if (idx < 0) return 0
    for (let n = 1; n <= 3; n++) {
      const expected = orderedSprints.value.slice(idx, idx + n).map(s => s.id)
      if (expected.length === n && toolbarSprintIds.value.length === n &&
          expected.every(id => toolbarSprintIds.value.includes(id))) return n
    }
    return 0
  })

  const navLabel = computed(() => {
    if (!toolbarSprintIds.value.length) return 'All'
    if (toolbarSprintIds.value.length === 1) {
      const s = orderedSprints.value.find(s => s.id === toolbarSprintIds.value[0])
      return s?.title ?? 'Sprint'
    }
    return `${toolbarSprintIds.value.length} sprints`
  })

  const canPrev = computed(() => {
    if (!toolbarSprintIds.value.length) return !!currentSprintId.value
    const anchor = navAnchor.value
    if (!anchor) return false
    const idx = orderedSprints.value.findIndex(s => s.id === anchor.id)
    return idx > 0
  })

  const canNext = computed(() => {
    if (!toolbarSprintIds.value.length) return !!currentSprintId.value
    const anchor = navAnchor.value
    if (!anchor) return false
    const idx = orderedSprints.value.findIndex(s => s.id === anchor.id)
    return idx < orderedSprints.value.length - 1
  })

  function navPrev() {
    if (!toolbarSprintIds.value.length) {
      if (currentSprintId.value) toolbarSprintIds.value = [currentSprintId.value]
      return
    }
    const anchor = navAnchor.value
    if (!anchor) return
    const idx = orderedSprints.value.findIndex(s => s.id === anchor.id)
    if (idx > 0) toolbarSprintIds.value = [orderedSprints.value[idx - 1].id]
  }

  function navNext() {
    if (!toolbarSprintIds.value.length) {
      if (currentSprintId.value) toolbarSprintIds.value = [currentSprintId.value]
      return
    }
    const anchor = navAnchor.value
    if (!anchor) return
    const idx = orderedSprints.value.findIndex(s => s.id === anchor.id)
    if (idx < orderedSprints.value.length - 1) toolbarSprintIds.value = [orderedSprints.value[idx + 1].id]
  }

  function navRange(n: number) {
    let anchor = navAnchor.value
    if (!anchor) {
      const cid = currentSprintId.value
      if (!cid) return
      anchor = orderedSprints.value.find(s => s.id === cid) ?? null
      if (!anchor) return
    }
    const idx = orderedSprints.value.findIndex(s => s.id === anchor!.id)
    toolbarSprintIds.value = orderedSprints.value.slice(idx, idx + n).map(s => s.id)
  }

  function navClear() {
    toolbarSprintIds.value = []
  }

  function toggleNavSprint(id: number) {
    const idx = toolbarSprintIds.value.indexOf(id)
    if (idx >= 0) {
      toolbarSprintIds.value = toolbarSprintIds.value.filter(x => x !== id)
    } else {
      toolbarSprintIds.value = [...toolbarSprintIds.value, id]
    }
  }

  // Persistence
  const sprintNavStorageKey = computed(() => lsSprintNavKey(opts.projectId.value ?? 'global'))

  function loadSprintNav() {
    try {
      const raw = localStorage.getItem(sprintNavStorageKey.value)
      if (raw) toolbarSprintIds.value = JSON.parse(raw)
    } catch { /* ignore */ }
  }

  function saveSprintNav() {
    localStorage.setItem(sprintNavStorageKey.value, JSON.stringify(toolbarSprintIds.value))
  }

  watch(toolbarSprintIds, saveSprintNav, { deep: true })

  // Dropdown style
  const snDropdownStyle = computed(() => {
    if (!snSelectorEl.value) return {} as Record<string, string>
    const rect = snSelectorEl.value.getBoundingClientRect()
    return {
      position: 'fixed' as const,
      top: `${rect.bottom + 4}px`,
      left: `${rect.left}px`,
      zIndex: 10000,
    }
  })

  const filteredNavSprints = computed(() => {
    const q = sprintNavSearch.value.toLowerCase()
    return orderedSprints.value.filter(s => !q || s.title.toLowerCase().includes(q))
  })

  // Auto-focus search and close on outside click
  watch(sprintNavOpen, (v) => {
    if (v) nextTick(() => (snSearchEl.value as HTMLElement | null)?.focus())
    else sprintNavSearch.value = ''
  })

  return {
    toolbarSprintIds, sprintNavOpen, sprintNavSearch,
    snSelectorEl, snSearchEl,
    orderedSprints, currentSprintId, navAnchor, activeRange, navLabel,
    canPrev, canNext,
    navPrev, navNext, navRange, navClear, toggleNavSprint,
    loadSprintNav, saveSprintNav,
    snDropdownStyle, filteredNavSprints,
  }
}
