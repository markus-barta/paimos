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
 * useSort — generic column-sort composable for any table.
 *
 * Usage:
 *   const { sorted, sortKey, sortDir, sortBy, thProps } = useSort(items, colDefs)
 *
 * colDefs maps column keys to a SortDef describing how to compare values.
 * sortBy(key) toggles asc/desc on same key, resets to asc on new key.
 * thProps(key) returns { onClick, class, 'aria-sort' } to spread onto <th>.
 * sortIndicator(key) returns '↕' | '↑' | '↓' for display.
 */

import { ref, computed } from 'vue'
import type { Ref, ComputedRef } from 'vue'

export type SortDir = 'asc' | 'desc'

export type SortType =
  | 'string'
  | 'number'
  | 'date'
  | { order: string[] }   // custom enum order — first = highest rank

export interface SortDef<T> {
  /** Extract the value to sort by from a row */
  value: (row: T) => string | number | null | undefined
  type: SortType
}

export type ColDefs<T> = Record<string, SortDef<T>>

function makeComparator<T>(def: SortDef<T>, dir: SortDir): (a: T, b: T) => number {
  const sign = dir === 'asc' ? 1 : -1
  return (a, b) => {
    const av = def.value(a)
    const bv = def.value(b)

    // nulls / empty always last regardless of direction
    const aEmpty = av === null || av === undefined || av === ''
    const bEmpty = bv === null || bv === undefined || bv === ''
    if (aEmpty && bEmpty) return 0
    if (aEmpty) return 1
    if (bEmpty) return -1

    if (typeof def.type === 'object' && 'order' in def.type) {
      const order = def.type.order
      const ai = order.indexOf(String(av))
      const bi = order.indexOf(String(bv))
      const ar = ai === -1 ? order.length : ai
      const br = bi === -1 ? order.length : bi
      return sign * (ar - br)
    }

    if (def.type === 'number') {
      return sign * ((av as number) - (bv as number))
    }

    // string + date — lexicographic (ISO dates sort correctly as strings)
    return sign * String(av).localeCompare(String(bv))
  }
}

export function useSort<T>(
  items: Ref<T[]> | ComputedRef<T[]>,
  cols: ColDefs<T>,
) {
  const sortKey = ref<string>('')
  const sortDir = ref<SortDir>('asc')

  function sortBy(key: string) {
    if (!cols[key]) return
    if (sortKey.value === key) {
      sortDir.value = sortDir.value === 'asc' ? 'desc' : 'asc'
    } else {
      sortKey.value = key
      sortDir.value = 'asc'
    }
  }

  const sorted = computed<T[]>(() => {
    if (!sortKey.value || !cols[sortKey.value]) return items.value
    return [...items.value].sort(makeComparator(cols[sortKey.value], sortDir.value))
  })

  /** Lucide icon name for the current sort state of a column */
  function sortIndicator(key: string): 'arrow-up-down' | 'arrow-up' | 'arrow-down' {
    if (sortKey.value !== key) return 'arrow-up-down'
    return sortDir.value === 'asc' ? 'arrow-up' : 'arrow-down'
  }

  function thProps(key: string) {
    const active = sortKey.value === key
    return {
      onClick: () => sortBy(key),
      class: ['sortable-th', active ? 'sort-active' : ''],
      'aria-sort': (active ? (sortDir.value === 'asc' ? 'ascending' : 'descending') : 'none') as 'ascending' | 'descending' | 'none',
      'data-sort-key': key,
    }
  }

  /** Sort an arbitrary array with the current key/dir — for tree-level sorting */
  function sortArray(arr: T[]): T[] {
    if (!sortKey.value || !cols[sortKey.value]) return arr
    return [...arr].sort(makeComparator(cols[sortKey.value], sortDir.value))
  }

  return { sorted, sortKey, sortDir, sortBy, sortIndicator, thProps, sortArray }
}
