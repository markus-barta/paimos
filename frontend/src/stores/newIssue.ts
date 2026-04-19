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
 * Global "open new issue modal" signal.
 * AppLayout fires requestCreate() with route-derived context.
 * IssueList / views watch pendingCreate and open their modal.
 */
import { defineStore } from 'pinia'
import { ref } from 'vue'

export interface NewIssueContext {
  type?: string          // pre-fill type
  parentId?: number      // pre-fill parent_id
  projectId?: number     // pre-fill project (for views that support it)
}

export const useNewIssueStore = defineStore('newIssue', () => {
  const trigger = ref(0)
  const context = ref<NewIssueContext>({})

  function requestCreate(ctx: NewIssueContext = {}) {
    context.value = ctx
    trigger.value += 1
  }

  return { trigger, context, requestCreate }
})
