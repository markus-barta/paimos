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

import { ref } from 'vue'
import { storeToRefs } from 'pinia'
import { api } from '@/api/client'
import { useDraggedIssue } from '@/stores/draggedIssue'
import type { Sprint, Issue } from '@/types'

// Shared state — singleton across all callers
const sidebarSprints  = ref<Sprint[]>([])
const sprintDragOver  = ref<number | null>(null)
const sprintAssigning = ref<number | null>(null)

export function useSidebarSprints() {
  const dragStore = useDraggedIssue()
  const { draggedIssue } = storeToRefs(dragStore)
  const { setDragging, notifyUpdated } = dragStore

  async function loadSidebarSprints() {
    try {
      const all = await api.get<Sprint[]>('/sprints')
      const today = new Date().toISOString().slice(0, 10)
      const sorted = [...all].sort((a, b) => (a.start_date ?? '').localeCompare(b.start_date ?? ''))
      const currentIdx = sorted.findIndex(s =>
        s.start_date && s.end_date && s.start_date <= today && today <= s.end_date
      )
      if (currentIdx >= 0) {
        sidebarSprints.value = sorted.slice(currentIdx, currentIdx + 4)
      } else {
        const nextIdx = sorted.findIndex(s => (s.start_date ?? '') > today)
        sidebarSprints.value = nextIdx >= 0 ? sorted.slice(nextIdx, nextIdx + 4) : sorted.slice(0, 4)
      }
    } catch { /* silent */ }
  }

  function isCurrentSprint(s: Sprint): boolean {
    const today = new Date().toISOString().slice(0, 10)
    return !!(s.start_date && s.end_date && s.start_date <= today && today <= s.end_date)
  }

  async function dropOnSprint(sprint: Sprint) {
    const issue = draggedIssue.value
    sprintDragOver.value = null
    if (!issue) return
    sprintAssigning.value = sprint.id
    try {
      const existingSprintIds = issue.sprint_ids ?? []
      for (const sid of existingSprintIds) {
        if (sid !== sprint.id) await api.delete(`/issues/${issue.id}/relations`, { target_id: sid, type: 'sprint' })
      }
      if (!existingSprintIds.includes(sprint.id)) {
        await api.post(`/issues/${issue.id}/relations`, { target_id: sprint.id, type: 'sprint' })
      }
      const updated = await api.get<Issue>(`/issues/${issue.id}`)
      notifyUpdated(updated)
    } catch { /* silent */ }
    finally { sprintAssigning.value = null; setDragging(null) }
  }

  return {
    sidebarSprints,
    sprintDragOver,
    sprintAssigning,
    loadSidebarSprints,
    isCurrentSprint,
    dropOnSprint,
  }
}
