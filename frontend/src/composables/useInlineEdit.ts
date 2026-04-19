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
 * useInlineEdit — Inline cell editing state + sprint picker logic.
 */
import { ref, computed } from 'vue'
import type { Ref } from 'vue'
import type { Issue, Sprint } from '@/types'
import type { MetaOption } from '@/components/MetaSelect.vue'
import {
  STATUS_DOT_STYLE, STATUS_LABEL,
  PRIORITY_ICON, PRIORITY_COLOR, PRIORITY_LABEL,
} from '@/composables/useIssueDisplay'
import type { User } from '@/types'
import { api, errMsg } from '@/api/client'

export type EditableField =
  | 'status' | 'priority' | 'cost_unit' | 'release' | 'assignee_id' | 'sprint'
  | 'title' | 'estimate_hours' | 'estimate_lp'

/** Text/numeric fields that auto-save on blur. */
const AUTOSAVE_TEXT_FIELDS: ReadonlySet<EditableField> = new Set([
  'cost_unit', 'release', 'title', 'estimate_hours', 'estimate_lp',
])
const NUMERIC_FIELDS: ReadonlySet<EditableField> = new Set([
  'estimate_hours', 'estimate_lp',
])

export const INLINE_STATUS_OPTIONS: MetaOption[] = [
  { value: 'new',         label: STATUS_LABEL.new,            dotColor: STATUS_DOT_STYLE.new.color,            dotOutline: STATUS_DOT_STYLE.new.outline },
  { value: 'backlog',     label: STATUS_LABEL.backlog,        dotColor: STATUS_DOT_STYLE.backlog.color,        dotOutline: STATUS_DOT_STYLE.backlog.outline },
  { value: 'in-progress', label: STATUS_LABEL['in-progress'], dotColor: STATUS_DOT_STYLE['in-progress'].color, dotOutline: STATUS_DOT_STYLE['in-progress'].outline },
  { value: 'qa',          label: STATUS_LABEL.qa,             dotColor: STATUS_DOT_STYLE.qa.color,             dotOutline: STATUS_DOT_STYLE.qa.outline },
  { value: 'done',        label: STATUS_LABEL.done,           dotColor: STATUS_DOT_STYLE.done.color,           dotOutline: STATUS_DOT_STYLE.done.outline },
  { value: 'delivered',   label: STATUS_LABEL.delivered,      dotColor: STATUS_DOT_STYLE.delivered.color,      dotOutline: STATUS_DOT_STYLE.delivered.outline },
  { value: 'accepted',    label: STATUS_LABEL.accepted,       dotColor: STATUS_DOT_STYLE.accepted.color,       dotOutline: STATUS_DOT_STYLE.accepted.outline },
  { value: 'invoiced',    label: STATUS_LABEL.invoiced,       dotColor: STATUS_DOT_STYLE.invoiced.color,       dotOutline: STATUS_DOT_STYLE.invoiced.outline },
  { value: 'cancelled',   label: STATUS_LABEL.cancelled,      dotColor: STATUS_DOT_STYLE.cancelled.color,      dotOutline: STATUS_DOT_STYLE.cancelled.outline },
]

export const INLINE_PRIORITY_OPTIONS: MetaOption[] = [
  { value: 'high',   label: PRIORITY_LABEL.high,   arrow: PRIORITY_ICON.high,   arrowColor: PRIORITY_COLOR.high   },
  { value: 'medium', label: PRIORITY_LABEL.medium, arrow: PRIORITY_ICON.medium, arrowColor: PRIORITY_COLOR.medium },
  { value: 'low',    label: PRIORITY_LABEL.low,    arrow: PRIORITY_ICON.low,    arrowColor: PRIORITY_COLOR.low    },
]

export interface UseInlineEditOptions {
  issues: Ref<Issue[]>
  users: Ref<User[]>
  sprints?: Ref<Sprint[] | undefined>
  loadedSprints: Ref<Sprint[]>
  childrenOf: (parentId: number) => Issue[]
  emit: {
    updated: (issue: Issue) => void
  }
}

export function useInlineEdit(opts: UseInlineEditOptions) {
  const editingCell  = ref<{ issueId: number; field: EditableField } | null>(null)
  const cellEditValue = ref('')

  // Epic cascade confirmation dialog state
  const cascadeDialogOpen = ref(false)
  const cascadePendingIssue = ref<Issue | null>(null)
  const cascadePendingStatus = ref('')
  const cascadeChildCount = ref(0)

  function countNonTerminalDescendants(parentId: number): number {
    const terminal = new Set(['done', 'delivered', 'accepted', 'invoiced', 'cancelled'])
    const children = opts.childrenOf(parentId)
    let count = 0
    for (const ch of children) {
      if (!terminal.has(ch.status)) count++
      count += countNonTerminalDescendants(ch.id)
    }
    return count
  }

  function openCell(issue: Issue, field: EditableField, e: MouseEvent) {
    e.stopPropagation()
    editingCell.value  = { issueId: issue.id, field }
    cellEditValue.value = field === 'assignee_id'
      ? (issue.assignee_id !== null ? String(issue.assignee_id) : '')
      : (String((issue as any)[field] ?? ''))
  }

  function closeCell(autoSave = false) {
    if (autoSave && editingCell.value) {
      const { issueId, field } = editingCell.value
      if (AUTOSAVE_TEXT_FIELDS.has(field)) {
        const issue = opts.issues.value.find(i => i.id === issueId)
        if (issue && cellEditValue.value !== String((issue as any)[field] ?? '')) {
          saveCellEdit(issue, field, cellEditValue.value)
          return
        }
      }
    }
    editingCell.value = null
  }

  async function saveCellEdit(issue: Issue, field: EditableField, value: string) {
    const payload: Record<string, unknown> = {}
    if (field === 'assignee_id') {
      payload.assignee_id = value === '' ? null : Number(value)
    } else if (NUMERIC_FIELDS.has(field)) {
      const trimmed = value.trim()
      payload[field] = trimmed === '' ? null : Number(trimmed)
      if (trimmed !== '' && Number.isNaN(payload[field] as number)) {
        // Invalid number — bail without saving, close cell.
        closeCell(false)
        return
      }
    } else {
      payload[field] = value
    }
    closeCell(false)

    // Epic cascade dialog for accepted/invoiced
    if (field === 'status' && issue.type === 'epic' && (value === 'accepted' || value === 'invoiced')) {
      const affected = countNonTerminalDescendants(issue.id)
      if (affected > 0) {
        cascadePendingIssue.value = issue
        cascadePendingStatus.value = value
        cascadeChildCount.value = affected
        cascadeDialogOpen.value = true
        return
      }
    }

    try {
      const updated = await api.put<Issue>(`/issues/${issue.id}`, payload)
      opts.emit.updated(updated)
    } catch (e: unknown) {
      /* error swallowed — row keeps previous state */
    }
  }

  async function cascadeConfirm(cascade: boolean) {
    const issue = cascadePendingIssue.value
    const status = cascadePendingStatus.value
    cascadeDialogOpen.value = false
    if (!issue) return
    try {
      const updated = await api.put<Issue>(`/issues/${issue.id}`, {
        status,
        cascade_children: cascade,
      })
      opts.emit.updated(updated)
    } catch (e: unknown) {
      /* error swallowed — row keeps previous state */
    }
  }

  // Sprint picker
  const sprintPickerSearch = ref('')
  const sprintPickerPos = ref<Record<string, string>>({})
  const sprintPickerRef = ref<HTMLElement | null>(null)

  function allSprints(): Sprint[] {
    return opts.sprints?.value?.length ? opts.sprints.value : opts.loadedSprints.value
  }

  function sprintPickerFiltered(issue: Issue): Sprint[] {
    const q = sprintPickerSearch.value.toLowerCase()
    return allSprints().filter(s => !q || s.title.toLowerCase().includes(q))
  }

  function openSprintPicker(issue: Issue, e: MouseEvent) {
    sprintPickerSearch.value = ''
    const rect = (e.currentTarget as HTMLElement).getBoundingClientRect()
    sprintPickerPos.value = {
      position: 'fixed',
      top: rect.bottom + 4 + 'px',
      left: rect.left + 'px',
      zIndex: '9000',
    }
    openCell(issue, 'sprint', e)
  }

  async function toggleSprint(issue: Issue, sprintId: number) {
    const assigned = (issue.sprint_ids ?? []).includes(sprintId)
    try {
      if (assigned) {
        await api.delete(`/issues/${issue.id}/relations`, { target_id: sprintId, type: 'sprint' })
      } else {
        await api.post(`/issues/${issue.id}/relations`, { target_id: sprintId, type: 'sprint' })
      }
      const updated = await api.get<Issue>(`/issues/${issue.id}`)
      opts.emit.updated(updated)
    } catch (e: unknown) {
      /* error swallowed — row keeps previous state */
    }
  }

  function inlineAssigneeOptions(): MetaOption[] {
    return [
      { value: '', label: 'Unassigned' },
      ...opts.users.value.filter(u => u.role !== 'external').map(u => ({ value: String(u.id), label: u.username })),
    ]
  }

  function onGlobalMousedownCell(e: MouseEvent) {
    if (!editingCell.value) return
    const target = e.target as Element
    if (target.closest('.inline-edit-cell') || target.closest('.meta-select-dropdown--teleported') || target.closest('.sprint-picker--teleported')) return
    closeCell(true)
  }

  return {
    editingCell, cellEditValue,
    openCell, closeCell, saveCellEdit,
    cascadeDialogOpen, cascadePendingIssue, cascadePendingStatus, cascadeChildCount,
    cascadeConfirm, countNonTerminalDescendants,
    sprintPickerSearch, sprintPickerPos, sprintPickerRef,
    allSprints, sprintPickerFiltered, openSprintPicker, toggleSprint,
    inlineAssigneeOptions, onGlobalMousedownCell,
    INLINE_STATUS_OPTIONS, INLINE_PRIORITY_OPTIONS,
  }
}
