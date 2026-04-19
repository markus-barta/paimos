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
 * useSelection — Selection mode state and toggle functions.
 */
import { ref, computed } from 'vue'
import type { Ref, ComputedRef } from 'vue'
import type { Issue } from '@/types'

export function useSelection(filteredIssues: ComputedRef<Issue[]>) {
  const selectionMode = ref(false)
  const selectedIds   = ref<Set<number>>(new Set())

  function toggleSelectionMode() {
    selectionMode.value = !selectionMode.value
    if (selectionMode.value) {
      selectedIds.value = new Set(filteredIssues.value.map(i => i.id))
    } else {
      selectedIds.value = new Set()
    }
  }

  function toggleSelect(id: number) {
    const s = new Set(selectedIds.value)
    if (s.has(id)) s.delete(id); else s.add(id)
    selectedIds.value = s
  }

  function toggleSelectAll() {
    const all = filteredIssues.value.map(i => i.id)
    if (all.every(id => selectedIds.value.has(id))) {
      selectedIds.value = new Set()
    } else {
      selectedIds.value = new Set(all)
    }
  }

  const allSelected = computed(() =>
    filteredIssues.value.length > 0 &&
    filteredIssues.value.every(i => selectedIds.value.has(i.id))
  )

  return {
    selectionMode, selectedIds,
    toggleSelectionMode, toggleSelect, toggleSelectAll,
    allSelected,
  }
}
