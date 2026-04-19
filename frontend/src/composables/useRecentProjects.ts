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

import { ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { api } from '@/api/client'
import type { Project } from '@/types'

// Shared state — singleton across all callers
const recentProjects = ref<Project[]>([])

export function useRecentProjects() {
  const route = useRoute()

  async function loadRecentProjects() {
    try {
      recentProjects.value = await api.get<Project[]>('/users/me/recent-projects')
    } catch { /* silent */ }
  }

  /** Record project visit from ANY /projects/:id/... route */
  let lastRecordedProjectId = ''
  function startVisitTracking() {
    watch(() => route.path, async (path) => {
      const m = path.match(/^\/projects\/(\d+)/)
      if (!m) return
      const id = m[1]
      if (id === lastRecordedProjectId) return
      lastRecordedProjectId = id
      try {
        await api.post('/users/me/recent-projects', { project_id: Number(id) })
        await loadRecentProjects()
      } catch { /* silent */ }
    }, { immediate: true })
  }

  return {
    recentProjects,
    loadRecentProjects,
    startVisitTracking,
  }
}
