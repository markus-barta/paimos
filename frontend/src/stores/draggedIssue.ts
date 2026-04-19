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
 * Global drag state — set by IssueList on dragstart, read by AppLayout sprint drop targets.
 * Also broadcasts post-drop updates so IssueList can refresh the affected row.
 */
import { defineStore } from 'pinia'
import { ref } from 'vue'
import type { Issue } from '@/types'

export const useDraggedIssue = defineStore('draggedIssue', () => {
  const draggedIssue = ref<Issue | null>(null)
  const updatedIssue = ref<Issue | null>(null)

  function setDragging(issue: Issue | null) { draggedIssue.value = issue }
  function notifyUpdated(issue: Issue) { updatedIssue.value = issue }

  return { draggedIssue, updatedIssue, setDragging, notifyUpdated }
})
