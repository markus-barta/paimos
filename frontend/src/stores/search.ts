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

import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { LS_SEARCH_LAST_QUERY as LS_KEY } from '@/constants/storage'

export type SearchScope = 'global' | 'project'

// Active search is URL/context driven. localStorage is only a recent-query
// memory now; it must not silently re-apply an old query as a live filter.
export const useSearchStore = defineStore('search', () => {
  const query = ref('')
  const lastQuery = ref(localStorage.getItem(LS_KEY) ?? '')
  const projectId = ref<number | null>(null)
  const projectKey = ref('')
  const scopeOverride = ref<SearchScope | null>(null)

  const hasProjectContext = computed(() => projectId.value !== null)
  const scope = computed<SearchScope>(() =>
    projectId.value !== null && scopeOverride.value !== 'global'
      ? 'project'
      : 'global',
  )

  function setQuery(q: string, opts: { remember?: boolean } = {}) {
    query.value = q
    if (opts.remember !== false && q) {
      lastQuery.value = q
      localStorage.setItem(LS_KEY, q)
    }
  }

  function clear() {
    query.value = ''
    localStorage.removeItem(LS_KEY)
  }

  function setProjectContext(id: number | null, key = '') {
    if (id !== projectId.value) scopeOverride.value = null
    projectId.value = id
    projectKey.value = key
  }

  function setProjectKey(key: string) {
    projectKey.value = key
  }

  function toggleScope() {
    if (projectId.value === null) {
      scopeOverride.value = null
      return
    }
    scopeOverride.value = scope.value === 'project' ? 'global' : null
  }

  return {
    query,
    lastQuery,
    projectId,
    projectKey,
    hasProjectContext,
    scope,
    clear,
    setProjectContext,
    setProjectKey,
    setQuery,
    toggleScope,
  }
})
