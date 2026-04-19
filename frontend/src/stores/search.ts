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
import { ref } from 'vue'

const LS_KEY = 'paimos:search:lastQuery'

// Search state is URL-driven (/issues?q=...).
// This store tracks the input value so AppLayout can bind v-model,
// and persists the last query to localStorage so it survives navigation.
export const useSearchStore = defineStore('search', () => {
  // Initialise from localStorage so the sidebar input is pre-populated on load.
  const query = ref(localStorage.getItem(LS_KEY) ?? '')

  function setQuery(q: string) {
    query.value = q
    if (q) localStorage.setItem(LS_KEY, q)
    // deliberately do NOT removeItem on empty — only clear() does that.
    // This prevents navigation away from /issues from wiping the last query.
  }

  function clear() {
    query.value = ''
    localStorage.removeItem(LS_KEY)
  }

  return { query, clear, setQuery }
})
