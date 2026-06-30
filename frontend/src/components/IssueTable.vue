<script setup lang="ts">
import { ref, onUnmounted, type CSSProperties } from 'vue'
import { RouterLink } from 'vue-router'
import { highlight } from '@/composables/useHighlight'
import AppIcon from '@/components/AppIcon.vue'
import AutocompleteInput from '@/components/AutocompleteInput.vue'
import TagChip from '@/components/TagChip.vue'
import IssueRowActions from '@/components/IssueRowActions.vue'
import AIWorkStatusBadge from '@/components/issue/AIWorkStatusBadge.vue'
import StatusDot from '@/components/StatusDot.vue'
import MetaSelect from '@/components/MetaSelect.vue'
import UserAvatar from '@/components/UserAvatar.vue'
import IssueStatusSelect from '@/components/issue/IssueStatusSelect.vue'
import IssueAssigneeSelect from '@/components/issue/IssueAssigneeSelect.vue'
import type { Issue, Sprint, User } from '@/types'
import {
  useIssueDisplay,
  TYPE_SVGS,
  STATUS_LABEL,
  PRIORITY_ICON, PRIORITY_COLOR, PRIORITY_LABEL,
} from '@/composables/useIssueDisplay'
import { INLINE_PRIORITY_OPTIONS } from '@/composables/useInlineEdit'
import type { EditableField } from '@/composables/useInlineEdit'
import { useTimeUnit } from '@/composables/useTimeUnit'
import type { FormatContext } from '@/composables/useTimeUnit'
import { useIssueContext } from '@/composables/useIssueContext'
import { clampColumnWidth, defaultColumnWidth, type ColumnWidths } from '@/composables/useColumnWidths'
import { formatDecimalFlex } from '@/composables/useNumberFormat'

const { users, sprints, costUnits, releases } = useIssueContext()

const props = defineProps<{
  issues: Issue[]
  allIssues: Issue[]
  loadedSprints: Sprint[]
  compact: boolean
  selectionMode: boolean
  selectedIds: Set<number>
  allSelected: boolean
  isAdmin: boolean
  projectId?: number
  // Sort
  sortResult: {
    thProps: (key: string) => Record<string, unknown>
    sortIndicator: (key: string) => string
  }
  // Column visibility
  isVisible: (key: string) => boolean
  columnWidths: ColumnWidths
  // Inline edit state
  editingCell: { issueId: number; field: string } | null
  cellEditValue: string
  // Sprint picker
  sprintPickerSearch: string
  sprintPickerPos: Record<string, string>
  sprintPickerFiltered: (issue: Issue) => Sprint[]
  sprintPickerRef: HTMLElement | null
  // Sprint groups
  sprintGroupHeads: Map<number, Sprint>
  backlogHeadId: number | null
  issueSprintGroup: Map<number, number | 'backlog'>
  dragOverSprintId: number | 'backlog' | null
  // Expand
  isGroupExpandView: boolean
  expandedGroupIds: Set<number>
  childrenOf: (id: number) => Issue[]
  // Table appearance
  showBorders: boolean
  showStripes: boolean
  actionsCollapsed: boolean
  // Side panel
  sidePanelIssueId: number | null
  // Display
  searchQuery: string
  // Epic display
  epicDisplayMode: 'key' | 'title' | 'abbreviated'
  // Format hours
  formatHours: (hours: number | null | undefined, context?: FormatContext) => string
  timeLabel: () => string
}>()

const emit = defineEmits<{
  'toggle-select': [id: number]
  'toggle-select-all': []
  'open-cell': [issue: Issue, field: EditableField, event: MouseEvent]
  'close-cell': [save: boolean]
  'save-cell-edit': [issue: Issue, field: EditableField, value: string]
  'open-sprint-picker': [issue: Issue, event: MouseEvent]
  'toggle-sprint': [issue: Issue, sprintId: number]
  'copy-key': [key: string, event?: MouseEvent]
  'navigate-to': [issue: Issue]
  'open-create': [issue: Issue]
  'open-side-panel': [issue: Issue, edit: boolean]
  'delete-row': [issue: Issue]
  'set-dragging': [issue: Issue]
  'drag-end': []
  'section-drag-over': [event: DragEvent, groupId: number | 'backlog']
  'section-drag-leave': [event: DragEvent, groupId: number | 'backlog']
  'section-drop': [event: DragEvent, groupId: number | 'backlog']
  'toggle-group-expand': [id: number]
  'toggle-time-unit': []
  'resize-column': [key: string, width: number]
  'reset-column-width': [key: string]
  'update:cell-edit-value': [value: string]
  'update:sprint-picker-search': [value: string]
}>()

const { showTypeIcon, showTypeText } = useIssueDisplay()

// PAI-466: tiny helper for the always-visible CUSTOMERPORTAL marker
// rendered in the type cell. Lives here (not in useIssueDisplay) so it
// stays close to its only caller; the chip styling lives in TagChip.vue.
function hasCustomerPortal(issue: Issue): boolean {
  return (issue.tags ?? []).some((t) => t.name === 'CUSTOMERPORTAL')
}

const BILLING_LABEL: Record<string, string> = {
  time_and_material: 'Time & Material',
  fixed_price:       'Fixed Price',
}

function typeLabel(type: string): string {
  // title-case each underscore-separated word: cost_unit → "Cost Unit"
  return type
    .split('_')
    .map((w) => w.charAt(0).toUpperCase() + w.slice(1))
    .join(' ')
}

// ── Epic display ──────────────────────────────────────────────────────────
const EPIC_COLORS: Record<string, { bg: string; fg: string }> = {
  red:    { bg: '#fee2e2', fg: '#991b1b' },
  orange: { bg: '#fff7ed', fg: '#9a3412' },
  yellow: { bg: '#fef9c3', fg: '#854d0e' },
  green:  { bg: '#dcfce7', fg: '#166534' },
  teal:   { bg: '#ccfbf1', fg: '#115e59' },
  blue:   { bg: '#dbeafe', fg: '#1e40af' },
  indigo: { bg: '#e0e7ff', fg: '#3730a3' },
  purple: { bg: '#f3e8ff', fg: '#6b21a8' },
  pink:   { bg: '#fce7f3', fg: '#9d174d' },
  gray:   { bg: '#f3f4f6', fg: '#374151' },
}

function epicLabel(epic: Issue): string {
  switch (props.epicDisplayMode) {
    case 'key': return epic.issue_key
    case 'title': return epic.title
    case 'abbreviated': return epic.title.length > 5 ? epic.title.slice(0, 5) + '...' : epic.title
  }
}

function resolveEpic(issue: Issue): Issue | null {
  if (!issue.parent_id) return null
  const parent = props.allIssues.find(p => p.id === issue.parent_id)
  if (!parent) return null
  if (parent.type === 'epic') return parent
  if (parent.parent_id) {
    const grandparent = props.allIssues.find(p => p.id === parent.parent_id)
    if (grandparent?.type === 'epic') return grandparent
  }
  return null
}

function epicBadgeStyle(epic: Issue): Record<string, string> {
  if (!epic.color || !EPIC_COLORS[epic.color]) return {}
  const c = EPIC_COLORS[epic.color]
  return { background: c.bg, color: c.fg }
}

function onRowClick(i: Issue) {
  if (props.selectionMode) {
    emit('toggle-select', i.id)
  } else if (props.isGroupExpandView) {
    emit('toggle-group-expand', i.id)
  } else {
    emit('navigate-to', i)
  }
}

function isEditing(issue: Issue, field: EditableField): boolean {
  return props.editingCell?.issueId === issue.id && props.editingCell?.field === field
}

function assigneeUser(issue: Issue): { username: string; avatar_path?: string; first_name?: string; last_name?: string; email?: string; nickname?: string } | null {
  if (issue.assignee_id !== null) {
    const user = users.value.find(u => u.id === issue.assignee_id)
    if (user) return user
  }
  return issue.assignee
}

function assigneeLabel(issue: Issue): string {
  return assigneeUser(issue)?.username ?? 'Unassigned'
}

function colStyle(key: string): CSSProperties {
  const width = props.columnWidths[key]
  if (!width) return {}
  const px = `${width}px`
  return { width: px, minWidth: px, maxWidth: px }
}

const resizingColumnKey = ref<string | null>(null)
let resizeState: { key: string; startX: number; startWidth: number } | null = null
const RESIZE_HIT_PX = 12

function isResizeHit(event: PointerEvent | MouseEvent): boolean {
  const th = event.currentTarget as HTMLElement | null
  if (!th) return false
  const rect = th.getBoundingClientRect()
  return event.clientX >= rect.right - RESIZE_HIT_PX && event.clientX <= rect.right + RESIZE_HIT_PX
}

function headerProps(key: string, sortable = false): Record<string, unknown> {
  const base: Record<string, unknown> = sortable ? props.sortResult.thProps(key) : {}
  return {
    ...base,
    class: [base.class, 'resizable-th', resizingColumnKey.value === key ? 'resizable-th--active' : ''],
    style: colStyle(key),
    'data-col-key': key,
    onPointerdown: (event: PointerEvent) => onColumnHeaderPointerDown(key, event),
    onDblclick: (event: MouseEvent) => onColumnHeaderDoubleClick(key, event),
  }
}

function onColumnHeaderPointerDown(key: string, event: PointerEvent) {
  if (event.button !== 0 || !isResizeHit(event)) return
  event.preventDefault()
  event.stopPropagation()
  const th = event.currentTarget as HTMLElement
  const currentWidth = props.columnWidths[key] ?? th.getBoundingClientRect().width ?? defaultColumnWidth(key)
  resizeState = { key, startX: event.clientX, startWidth: currentWidth }
  resizingColumnKey.value = key
  document.body.classList.add('is-column-resizing')
  window.addEventListener('pointermove', onColumnResizePointerMove)
  window.addEventListener('pointerup', stopColumnResize, { once: true })
}

function onColumnResizePointerMove(event: PointerEvent) {
  if (!resizeState) return
  const width = clampColumnWidth(resizeState.key, resizeState.startWidth + event.clientX - resizeState.startX)
  emit('resize-column', resizeState.key, width)
}

function stopColumnResize() {
  resizeState = null
  resizingColumnKey.value = null
  document.body.classList.remove('is-column-resizing')
  window.removeEventListener('pointermove', onColumnResizePointerMove)
  window.removeEventListener('pointerup', stopColumnResize)
}

function onColumnHeaderDoubleClick(key: string, event: MouseEvent) {
  if (!isResizeHit(event)) return
  event.preventDefault()
  event.stopPropagation()
  emit('reset-column-width', key)
}

onUnmounted(stopColumnResize)
</script>

<template>
  <table class="issue-table" :class="{ 'issue-table--selection-mode': selectionMode }">
    <colgroup v-if="!compact">
      <col v-if="selectionMode" class="sel-col" />
      <col v-if="isVisible('key')" :style="colStyle('key')" />
      <col v-if="isVisible('type')" :style="colStyle('type')" />
      <col v-if="isVisible('title')" :style="colStyle('title')" />
      <col v-if="isVisible('status')" :style="colStyle('status')" />
      <col v-if="isVisible('priority')" :style="colStyle('priority')" />
      <col v-if="isVisible('cost_unit')" :style="colStyle('cost_unit')" />
      <col v-if="isVisible('release')" :style="colStyle('release')" />
      <col v-if="isVisible('assignee')" :style="colStyle('assignee')" />
      <col v-if="isVisible('tags')" :style="colStyle('tags')" />
      <col v-if="isVisible('epic')" :style="colStyle('epic')" />
      <col v-if="isVisible('sprint')" :style="colStyle('sprint')" />
      <col v-if="isVisible('billing_type')" :style="colStyle('billing_type')" />
      <col v-if="isVisible('total_budget')" :style="colStyle('total_budget')" />
      <col v-if="isVisible('rate_hourly')" :style="colStyle('rate_hourly')" />
      <col v-if="isVisible('rate_lp')" :style="colStyle('rate_lp')" />
      <col v-if="isVisible('estimate_hours')" :style="colStyle('estimate_hours')" />
      <col v-if="isVisible('estimate_lp')" :style="colStyle('estimate_lp')" />
      <col v-if="isVisible('ar_hours')" :style="colStyle('ar_hours')" />
      <col v-if="isVisible('ar_lp')" :style="colStyle('ar_lp')" />
      <col v-if="isVisible('start_date')" :style="colStyle('start_date')" />
      <col v-if="isVisible('end_date')" :style="colStyle('end_date')" />
      <col v-if="isVisible('group_state')" :style="colStyle('group_state')" />
      <col v-if="isVisible('sprint_state')" :style="colStyle('sprint_state')" />
      <col v-if="isVisible('jira_id')" :style="colStyle('jira_id')" />
      <col v-if="isVisible('jira_version')" :style="colStyle('jira_version')" />
      <col v-if="isVisible('jira_text')" :style="colStyle('jira_text')" />
      <col v-if="isVisible('booked_hours')" :style="colStyle('booked_hours')" />
      <col v-if="isVisible('report_summary')" :style="colStyle('report_summary')" />
      <col v-if="isVisible('ai_status')" :style="colStyle('ai_status')" />
      <col :style="colStyle('actions')" />
    </colgroup>
    <thead v-if="!compact">
      <tr>
        <th v-if="selectionMode" class="sel-th">
          <input type="checkbox" class="sel-cb" :checked="allSelected" @change="emit('toggle-select-all')" title="Select all" />
        </th>
        <th v-if="isVisible('key')" v-bind="headerProps('key', true)" class="col-key">Key <span class="sort-ind"><AppIcon :name="sortResult.sortIndicator('key')" :size="11" /></span></th>
        <th v-if="isVisible('type')" v-bind="headerProps('type', true)">Type <span class="sort-ind"><AppIcon :name="sortResult.sortIndicator('type')" :size="11" /></span></th>
        <th v-if="isVisible('title')" v-bind="headerProps('title', true)">Title <span class="sort-ind"><AppIcon :name="sortResult.sortIndicator('title')" :size="11" /></span></th>
        <th v-if="isVisible('status')" v-bind="headerProps('status', true)">Status <span class="sort-ind"><AppIcon :name="sortResult.sortIndicator('status')" :size="11" /></span></th>
        <th v-if="isVisible('priority')" v-bind="headerProps('priority', true)">Priority <span class="sort-ind"><AppIcon :name="sortResult.sortIndicator('priority')" :size="11" /></span></th>
        <th v-if="isVisible('cost_unit')" v-bind="headerProps('cost_unit', true)">Cost Unit <span class="sort-ind"><AppIcon :name="sortResult.sortIndicator('cost_unit')" :size="11" /></span></th>
        <th v-if="isVisible('release')" v-bind="headerProps('release', true)">Release <span class="sort-ind"><AppIcon :name="sortResult.sortIndicator('release')" :size="11" /></span></th>
        <th v-if="isVisible('assignee')" v-bind="headerProps('assignee', true)">Assignee <span class="sort-ind"><AppIcon :name="sortResult.sortIndicator('assignee')" :size="11" /></span></th>
        <th v-if="isVisible('tags')" v-bind="headerProps('tags')" class="tags-th">Tags</th>
        <th v-if="isVisible('epic')" v-bind="headerProps('epic')" class="tags-th">Epic</th>
        <th v-if="isVisible('sprint')" v-bind="headerProps('sprint')" class="tags-th">Sprint</th>
        <th v-if="isVisible('billing_type')" v-bind="headerProps('billing_type', true)">Billing <span class="sort-ind"><AppIcon :name="sortResult.sortIndicator('billing_type')" :size="11" /></span></th>
        <th v-if="isVisible('total_budget')" v-bind="headerProps('total_budget', true)">Budget <span class="sort-ind"><AppIcon :name="sortResult.sortIndicator('total_budget')" :size="11" /></span></th>
        <th v-if="isVisible('rate_hourly')" v-bind="headerProps('rate_hourly', true)">Rate/h <span class="sort-ind"><AppIcon :name="sortResult.sortIndicator('rate_hourly')" :size="11" /></span></th>
        <th v-if="isVisible('rate_lp')" v-bind="headerProps('rate_lp', true)">Rate LP <span class="sort-ind"><AppIcon :name="sortResult.sortIndicator('rate_lp')" :size="11" /></span></th>
        <th v-if="isVisible('estimate_hours')" v-bind="headerProps('estimate_hours', true)" class="th-toggle" @click.stop="emit('toggle-time-unit')">Est. <span class="unit-toggle">{{ timeLabel() }}</span> <span class="sort-ind"><AppIcon :name="sortResult.sortIndicator('estimate_hours')" :size="11" /></span></th>
        <th v-if="isVisible('estimate_lp')" v-bind="headerProps('estimate_lp', true)">Est. LP <span class="sort-ind"><AppIcon :name="sortResult.sortIndicator('estimate_lp')" :size="11" /></span></th>
        <th v-if="isVisible('ar_hours')" v-bind="headerProps('ar_hours', true)" class="th-toggle" @click.stop="emit('toggle-time-unit')">AR <span class="unit-toggle">{{ timeLabel() }}</span> <span class="sort-ind"><AppIcon :name="sortResult.sortIndicator('ar_hours')" :size="11" /></span></th>
        <th v-if="isVisible('ar_lp')" v-bind="headerProps('ar_lp', true)">AR LP <span class="sort-ind"><AppIcon :name="sortResult.sortIndicator('ar_lp')" :size="11" /></span></th>
        <th v-if="isVisible('start_date')" v-bind="headerProps('start_date', true)">Start <span class="sort-ind"><AppIcon :name="sortResult.sortIndicator('start_date')" :size="11" /></span></th>
        <th v-if="isVisible('end_date')" v-bind="headerProps('end_date', true)">End <span class="sort-ind"><AppIcon :name="sortResult.sortIndicator('end_date')" :size="11" /></span></th>
        <th v-if="isVisible('group_state')" v-bind="headerProps('group_state', true)">Group State <span class="sort-ind"><AppIcon :name="sortResult.sortIndicator('group_state')" :size="11" /></span></th>
        <th v-if="isVisible('sprint_state')" v-bind="headerProps('sprint_state', true)">Sprint State <span class="sort-ind"><AppIcon :name="sortResult.sortIndicator('sprint_state')" :size="11" /></span></th>
        <th v-if="isVisible('jira_id')" v-bind="headerProps('jira_id', true)">Jira ID <span class="sort-ind"><AppIcon :name="sortResult.sortIndicator('jira_id')" :size="11" /></span></th>
        <th v-if="isVisible('jira_version')" v-bind="headerProps('jira_version', true)">Jira Version <span class="sort-ind"><AppIcon :name="sortResult.sortIndicator('jira_version')" :size="11" /></span></th>
        <th v-if="isVisible('jira_text')" v-bind="headerProps('jira_text', true)">Jira Text <span class="sort-ind"><AppIcon :name="sortResult.sortIndicator('jira_text')" :size="11" /></span></th>
        <th v-if="isVisible('booked_hours')" v-bind="headerProps('booked_hours', true)">Booked <span class="sort-ind"><AppIcon :name="sortResult.sortIndicator('booked_hours')" :size="11" /></span></th>
        <th v-if="isVisible('report_summary')" v-bind="headerProps('report_summary', true)">Report summary <span class="sort-ind"><AppIcon :name="sortResult.sortIndicator('report_summary')" :size="11" /></span></th>
        <th v-if="isVisible('ai_status')" v-bind="headerProps('ai_status', true)" class="col-ai-status">AI <span class="sort-ind"><AppIcon :name="sortResult.sortIndicator('ai_status')" :size="11" /></span></th>
        <th v-bind="headerProps('actions')" class="col-actions">Actions</th>
      </tr>
    </thead>
    <tbody>
      <template v-for="(i, rowIdx) in issues" :key="i.id">
      <tr v-if="sprintGroupHeads.has(i.id)" :key="`sep-${sprintGroupHeads.get(i.id)!.id}`"
          class="sprint-separator-row"
          :class="{ 'sprint-section-dragover': dragOverSprintId === sprintGroupHeads.get(i.id)!.id }"
          :data-sprint-group="sprintGroupHeads.get(i.id)!.id"
          @dragover.prevent="emit('section-drag-over', $event, sprintGroupHeads.get(i.id)!.id)"
          @dragleave="emit('section-drag-leave', $event, sprintGroupHeads.get(i.id)!.id)"
          @drop.prevent="emit('section-drop', $event, sprintGroupHeads.get(i.id)!.id)">
        <td :colspan="100">
          <span class="sprint-sep-label">{{ sprintGroupHeads.get(i.id)!.title }}
            <span v-if="sprintGroupHeads.get(i.id)!.start_date" class="sprint-sep-dates">{{ sprintGroupHeads.get(i.id)!.start_date }} – {{ sprintGroupHeads.get(i.id)!.end_date }}</span>
          </span>
        </td>
      </tr>
      <tr v-if="backlogHeadId === i.id" :key="'sep-backlog'"
          class="sprint-separator-row"
          :class="{ 'sprint-section-dragover': dragOverSprintId === 'backlog' }"
          :data-sprint-group="'backlog'"
          @dragover.prevent="emit('section-drag-over', $event, 'backlog')"
          @dragleave="emit('section-drag-leave', $event, 'backlog')"
          @drop.prevent="emit('section-drop', $event, 'backlog')">
        <td :colspan="100">
          <span class="sprint-sep-label">Backlog (no sprint)</span>
        </td>
      </tr>
      <tr
          class="issue-row clickable"
          :class="[
            rowIdx % 2 === 1 ? 'row-even' : 'row-odd',
            {
              'row-selected': selectionMode && selectedIds.has(i.id),
              'row-expanded': isGroupExpandView && expandedGroupIds.has(i.id),
              'row-active-panel': sidePanelIssueId === i.id,
              'sprint-section-dragover': dragOverSprintId !== null && issueSprintGroup.get(i.id) === dragOverSprintId,
            },
          ]"
          :data-issue-id="i.id"
          draggable="true"
          @dragstart="emit('set-dragging', i)"
          @dragend="emit('drag-end')"
          @dragover.prevent="issueSprintGroup.has(i.id) && emit('section-drag-over', $event, issueSprintGroup.get(i.id)!)"
          @dragleave="issueSprintGroup.has(i.id) && emit('section-drag-leave', $event, issueSprintGroup.get(i.id)!)"
          @drop.prevent="issueSprintGroup.has(i.id) && emit('section-drop', $event, issueSprintGroup.get(i.id)!)"
          @click="onRowClick(i)">
        <td v-if="selectionMode" class="sel-td" @click.stop="emit('toggle-select', i.id)">
          <input type="checkbox" class="sel-cb" :checked="selectedIds.has(i.id)" @change="emit('toggle-select', i.id)" @click.stop />
        </td>
        <td v-if="!compact && isVisible('key')" class="key-cell col-key">
          <span class="issue-key-copy" @click="emit('copy-key', i.issue_key, $event)" title="Copy issue key">
            <span v-html="highlight(i.issue_key, searchQuery)" />
            <AppIcon name="clipboard" :size="11" class="copy-ghost-icon" />
          </span>
        </td>
        <td v-if="compact" class="key-cell col-key">
          <span class="issue-key-copy" @click="emit('copy-key', i.issue_key, $event)" title="Copy issue key">
            <span v-html="highlight(i.issue_key, searchQuery)" />
            <AppIcon name="clipboard" :size="11" class="copy-ghost-icon" />
          </span>
        </td>
        <td v-if="!compact && isVisible('type')">
          <span :class="`issue-type issue-type--${i.type}`">
            <span v-if="showTypeIcon" v-html="TYPE_SVGS[i.type] ?? ''"></span>
            <span v-if="showTypeText" class="type-label-text">{{ typeLabel(i.type) }}</span>
            <!-- PAI-466: always-visible CUSTOMERPORTAL marker. Lives in
                 the type cell (never collapsed) so the visibility
                 signal survives even when the tags column is hidden. -->
            <span
              v-if="hasCustomerPortal(i)"
              class="customerportal-marker"
              title="issue is shown in customer portal"
            >
              <AppIcon name="eye" :size="11" />
            </span>
          </span>
        </td>
        <td v-if="compact">
          <span :class="`issue-type issue-type--${i.type}`">
            <span v-if="showTypeIcon" v-html="TYPE_SVGS[i.type] ?? ''"></span>
            <span v-if="showTypeText" class="type-label-text">{{ typeLabel(i.type) }}</span>
            <span
              v-if="hasCustomerPortal(i)"
              class="customerportal-marker"
              title="issue is shown in customer portal"
            >
              <AppIcon name="eye" :size="11" />
            </span>
          </span>
        </td>
        <td v-if="!compact && isVisible('title')" class="issue-title-cell inline-edit-cell">
          <input
            v-if="editingCell?.issueId === i.id && editingCell?.field === 'title'"
            ref="titleInputRef"
            class="title-inline-input"
            :value="cellEditValue"
            @input="emit('update:cell-edit-value', ($event.target as HTMLInputElement).value)"
            @keydown.enter.stop.prevent="emit('save-cell-edit', i, 'title', cellEditValue)"
            @keydown.escape.stop="emit('close-cell', false)"
            @blur="emit('close-cell', true)"
          />
          <span
            v-else
            class="issue-link"
            @dblclick.stop.prevent="emit('open-cell', i, 'title', $event)"
            title="Double-click to rename"
            v-html="highlight(i.title, searchQuery)"
          />
        </td>
         <td v-if="compact" class="issue-title-cell">
           <span class="issue-link" v-html="highlight(i.title, searchQuery)" />
         </td>
        <td v-if="!compact && isVisible('status')" class="inline-edit-cell" :style="colStyle('status')">
          <div v-if="isEditing(i, 'status')" class="inline-control inline-control--status">
            <IssueStatusSelect
              :model-value="i.status"
              size="sm"
              open-on-mount
              @update:model-value="v => emit('save-cell-edit', i, 'status', v)"
            />
          </div>
          <button
            v-else
            type="button"
            class="inline-read-value inline-read-value--status"
            title="Change status"
            @click.stop="emit('open-cell', i, 'status', $event)"
          >
            <span class="issue-status">
              <StatusDot :status="i.status" />
              {{ STATUS_LABEL[i.status] ?? i.status }}
            </span>
            <AppIcon name="pencil" :size="11" class="inline-edit-ghost" />
          </button>
        </td>
        <td v-if="compact">
          <span class="issue-status">
            <StatusDot :status="i.status" />
            {{ STATUS_LABEL[i.status] }}
          </span>
        </td>
        <td v-if="!compact && isVisible('priority')" class="inline-edit-cell" :style="colStyle('priority')">
          <div v-if="isEditing(i, 'priority')" class="inline-control inline-control--priority">
            <MetaSelect
              :model-value="i.priority"
              :options="INLINE_PRIORITY_OPTIONS"
              size="sm"
              open-on-mount
              @update:model-value="v => emit('save-cell-edit', i, 'priority', v)"
            />
          </div>
          <button
            v-else
            type="button"
            class="inline-read-value inline-read-value--priority"
            title="Change priority"
            @click.stop="emit('open-cell', i, 'priority', $event)"
          >
            <span class="issue-priority" :style="{ color: PRIORITY_COLOR[i.priority] }">
              <AppIcon :name="PRIORITY_ICON[i.priority]" :size="12" :stroke-width="2.5" class="issue-priority-arrow" />
              {{ PRIORITY_LABEL[i.priority] }}
            </span>
            <AppIcon name="pencil" :size="11" class="inline-edit-ghost" />
          </button>
        </td>
        <td v-if="compact">
          <span class="issue-priority" :style="{ color: PRIORITY_COLOR[i.priority] }">
            <AppIcon :name="PRIORITY_ICON[i.priority]" :size="12" :stroke-width="2.5" class="issue-priority-arrow" />
            {{ PRIORITY_LABEL[i.priority] }}
          </span>
        </td>
        <td v-if="!compact && isVisible('cost_unit')" class="meta-cell inline-edit-cell">
          <AutocompleteInput
            v-if="editingCell?.issueId === i.id && editingCell?.field === 'cost_unit'"
            :model-value="cellEditValue"
            :suggestions="costUnits"
            placeholder="Cost unit..."
            @update:model-value="v => emit('update:cell-edit-value', v)"
            @keydown.enter.stop="emit('save-cell-edit', i, 'cost_unit', cellEditValue)"
            @keydown.escape.stop="emit('close-cell', false)"
          />
          <span v-else class="clickable-cell" @click.stop="emit('open-cell', i, 'cost_unit', $event)">{{ i.cost_unit?.label || '—' }}</span>
        </td>
        <td v-if="!compact && isVisible('release')" class="meta-cell inline-edit-cell">
          <select
            v-if="editingCell?.issueId === i.id && editingCell?.field === 'release'"
            class="meta-inline-select"
            :value="cellEditValue"
            @change="emit('save-cell-edit', i, 'release', ($event.target as HTMLSelectElement).value)"
            @keydown.escape.stop="emit('close-cell', false)"
          >
            <option value="">— None —</option>
            <option v-for="r in releases" :key="r" :value="r">{{ r }}</option>
          </select>
          <span v-else class="clickable-cell" @click.stop="emit('open-cell', i, 'release', $event)">{{ i.release?.label || '—' }}</span>
        </td>
        <td v-if="!compact && isVisible('assignee')" class="meta-cell inline-edit-cell" :style="colStyle('assignee')">
          <div v-if="isEditing(i, 'assignee_id')" class="inline-control inline-control--assignee">
            <IssueAssigneeSelect
              :model-value="i.assignee_id !== null ? String(i.assignee_id) : ''"
              :users="users"
              :fallback-user="i.assignee"
              size="sm"
              open-on-mount
              @update:model-value="v => emit('save-cell-edit', i, 'assignee_id', v)"
            />
          </div>
          <button
            v-else
            type="button"
            class="inline-read-value inline-read-value--assignee"
            title="Change assignee"
            @click.stop="emit('open-cell', i, 'assignee_id', $event)"
          >
            <UserAvatar v-if="assigneeUser(i)" :user="assigneeUser(i)" size="sm" />
            <span class="inline-read-label">{{ assigneeLabel(i) }}</span>
            <AppIcon name="pencil" :size="11" class="inline-edit-ghost" />
          </button>
        </td>
        <td v-if="!compact && isVisible('tags')" class="tags-cell">
          <div v-if="i.tags?.length" class="row-tags">
            <TagChip v-for="t in i.tags" :key="t.id" :tag="t" />
          </div>
          <span v-else class="meta-cell">—</span>
        </td>
        <td v-if="!compact && isVisible('epic')" class="tags-cell">
          <span v-if="resolveEpic(i)" class="epic-wrap">
            <RouterLink :to="`/projects/${projectId}/issues/${resolveEpic(i)!.id}`" class="epic-badge" :style="epicBadgeStyle(resolveEpic(i)!)" @click.stop>{{ epicLabel(resolveEpic(i)!) }}</RouterLink>
          </span>
          <span v-else class="meta-cell">—</span>
        </td>
        <td v-if="!compact && isVisible('sprint')" class="meta-cell inline-edit-cell">
          <div v-if="editingCell?.issueId === i.id && editingCell?.field === 'sprint'" class="sprint-picker-wrap">
            <Teleport to="body">
              <div class="sprint-picker sprint-picker--teleported" :style="sprintPickerPos">
                <input :value="sprintPickerSearch" @input="emit('update:sprint-picker-search', ($event.target as HTMLInputElement).value)" class="sprint-picker-search" placeholder="Search sprints..." autocomplete="off" @keydown.escape.stop="emit('close-cell', false)" />
                <div class="sprint-picker-list">
                  <button
                    v-for="s in sprintPickerFiltered(i)" :key="s.id"
                    class="sprint-picker-opt"
                    type="button"
                    @click.stop="emit('toggle-sprint', i, s.id)"
                  >
                    <span class="sprint-picker-check">{{ (i.sprint_ids ?? []).includes(s.id) ? '✓' : '' }}</span>
                    <span class="sprint-picker-title">{{ s.title }}</span>
                  </button>
                  <div v-if="sprintPickerFiltered(i).length === 0" class="sprint-picker-empty">No sprints found</div>
                </div>
              </div>
            </Teleport>
          </div>
          <span v-else class="clickable-cell" @click.stop="emit('open-sprint-picker', i, $event)">
            <template v-if="i.sprint_ids?.length">
              <span v-for="sid in i.sprint_ids" :key="sid" class="sprint-link-inline">
                {{ sprints?.find(s => s.id === sid)?.title ?? loadedSprints.find(s => s.id === sid)?.title ?? `#${sid}` }}
              </span>
            </template>
            <template v-else>—</template>
          </span>
        </td>
        <td v-if="!compact && isVisible('billing_type')" class="meta-cell">{{ i.billing_type ? BILLING_LABEL[i.billing_type] ?? i.billing_type : '—' }}</td>
        <td v-if="!compact && isVisible('total_budget')" class="meta-cell">{{ i.total_budget != null ? formatDecimalFlex(i.total_budget, 2) : '—' }}</td>
        <td v-if="!compact && isVisible('rate_hourly')" class="meta-cell">{{ i.rate_hourly != null ? i.rate_hourly : '—' }}</td>
        <td v-if="!compact && isVisible('rate_lp')" class="meta-cell">{{ i.rate_lp != null ? i.rate_lp : '—' }}</td>
        <td v-if="!compact && isVisible('estimate_hours')" class="meta-cell inline-edit-cell">
          <input
            v-if="editingCell?.issueId === i.id && editingCell?.field === 'estimate_hours'"
            class="numeric-inline-input"
            type="text"
            :value="cellEditValue"
            @input="emit('update:cell-edit-value', ($event.target as HTMLInputElement).value)"
            @keydown.enter.stop.prevent="emit('save-cell-edit', i, 'estimate_hours', cellEditValue)"
            @keydown.escape.stop="emit('close-cell', false)"
            @blur="emit('close-cell', true)"
          />
          <span v-else class="clickable-cell" @click.stop="emit('open-cell', i, 'estimate_hours', $event)">
            {{ i.estimate_hours != null ? formatHours(i.estimate_hours, 'table') : '—' }}
          </span>
        </td>
        <td v-if="!compact && isVisible('estimate_lp')" class="meta-cell inline-edit-cell">
          <input
            v-if="editingCell?.issueId === i.id && editingCell?.field === 'estimate_lp'"
            class="numeric-inline-input"
            type="text"
            :value="cellEditValue"
            @input="emit('update:cell-edit-value', ($event.target as HTMLInputElement).value)"
            @keydown.enter.stop.prevent="emit('save-cell-edit', i, 'estimate_lp', cellEditValue)"
            @keydown.escape.stop="emit('close-cell', false)"
            @blur="emit('close-cell', true)"
          />
          <span v-else class="clickable-cell" @click.stop="emit('open-cell', i, 'estimate_lp', $event)">
            {{ i.estimate_lp != null ? i.estimate_lp : '—' }}
          </span>
        </td>
        <td v-if="!compact && isVisible('ar_hours')" class="meta-cell">{{ i.ar_hours != null ? formatHours(i.ar_hours, 'table') : '—' }}</td>
        <td v-if="!compact && isVisible('ar_lp')" class="meta-cell">{{ i.ar_lp != null ? i.ar_lp : '—' }}</td>
        <td v-if="!compact && isVisible('start_date')" class="meta-cell">{{ i.start_date || '—' }}</td>
        <td v-if="!compact && isVisible('end_date')" class="meta-cell">{{ i.end_date || '—' }}</td>
        <td v-if="!compact && isVisible('group_state')" class="meta-cell">{{ i.group_state || '—' }}</td>
        <td v-if="!compact && isVisible('sprint_state')" class="meta-cell">{{ i.sprint_state || '—' }}</td>
        <td v-if="!compact && isVisible('jira_id')" class="meta-cell">{{ i.jira_id || '—' }}</td>
        <td v-if="!compact && isVisible('jira_version')" class="meta-cell">{{ i.jira_version || '—' }}</td>
        <td v-if="!compact && isVisible('jira_text')" class="meta-cell">{{ i.jira_text || '—' }}</td>
        <td v-if="!compact && isVisible('booked_hours')" class="meta-cell booked-cell">{{ i.booked_hours > 0 ? formatHours(i.booked_hours, 'table') : '—' }}</td>
        <td v-if="!compact && isVisible('report_summary')" class="meta-cell report-summary-cell" :title="i.report_summary || ''">{{ i.report_summary || '—' }}</td>
        <td v-if="!compact && isVisible('ai_status')" class="meta-cell ai-status-cell" @click.stop>
          <AIWorkStatusBadge
            v-if="i.ai_work_status"
            :run="i.ai_work_status"
            @open="emit('open-side-panel', i, false)"
          />
          <span v-else class="ai-status-empty">—</span>
        </td>
        <td class="col-actions" @click.stop>
          <IssueRowActions :can-have-children="true" :compact="compact" :collapsed="actionsCollapsed" :issue-id="i.id" :issue-type="i.type" :booked-hours="i.booked_hours" :is-admin="isAdmin" :ai-work-status="i.ai_work_status" @add-child="emit('open-create', i)" @edit="emit('open-side-panel', i, true)" @view="emit('open-side-panel', i, false)" @copy="emit('copy-key', i.issue_key)" @delete="emit('delete-row', i)" />
        </td>
      </tr>
      <!-- Expand panel for group types -->
      <tr v-if="isGroupExpandView && expandedGroupIds.has(i.id)" :key="`expand-${i.id}`" class="expand-panel-row">
        <td :colspan="100" class="expand-panel-cell">
          <div v-if="childrenOf(i.id).length === 0" class="expand-empty">No child issues.</div>
          <div v-else class="expand-children">
            <div
              v-for="child in childrenOf(i.id)"
              :key="child.id"
              class="expand-child-row"
              @click.stop="emit('navigate-to', child)"
            >
              <span class="expand-child-key">{{ child.issue_key }}</span>
              <span class="expand-child-title">{{ child.title }}</span>
              <span class="expand-child-status"><span :class="`badge badge-${child.status}`">{{ child.status }}</span></span>
              <span class="expand-child-assignee">{{ users.find(u => u.id === child.assignee_id)?.username ?? '—' }}</span>
            </div>
          </div>
        </td>
      </tr>
      </template>
    </tbody>
  </table>

</template>

<style scoped>
.issue-table { width: 100%; border-collapse: separate; border-spacing: 0; min-width: max-content; }

.issue-table thead tr th:first-child { border-top-left-radius: 7px; }
.issue-table thead tr th:last-child  { border-top-right-radius: 7px; }
.issue-table tbody tr:last-child td:first-child { border-bottom-left-radius: 7px; }
.issue-table tbody tr:last-child td:last-child  { border-bottom-right-radius: 7px; }

.issue-table thead th { padding: .6rem .85rem; text-align: left; font-size: 11px; font-weight: 700; text-transform: uppercase; letter-spacing: .06em; color: var(--text-muted); background: var(--bg); border-bottom: 1px solid var(--border); position: sticky; top: 0; z-index: 10; box-sizing: border-box; }
.issue-table tbody tr { border-bottom: none; }
.issue-table td { padding: .6rem .85rem; font-size: 13px; vertical-align: middle; box-sizing: border-box; }

.clickable { cursor: pointer; }
.issue-table tbody tr.clickable:hover { background: #f0f2f4; }
.inline-edit-cell { cursor: default; position: relative; overflow: visible; }
.inline-control { display: inline-flex; align-items: center; min-height: 28px; max-width: 100%; }
.inline-control :deep(.meta-select-trigger) { min-width: 112px; }
.inline-control--assignee :deep(.meta-select-trigger) { min-width: 138px; }
.inline-read-value {
  display: inline-flex;
  align-items: center;
  gap: .35rem;
  min-height: 28px;
  max-width: 100%;
  padding: .1rem .25rem;
  border: 1px solid transparent;
  border-radius: 4px;
  background: transparent;
  color: inherit;
  font: inherit;
  text-align: left;
  white-space: nowrap;
  cursor: pointer;
}
.inline-read-value:hover,
.inline-read-value:focus-visible {
  background: color-mix(in srgb, var(--bp-blue) 8%, transparent);
  border-color: color-mix(in srgb, var(--bp-blue) 22%, transparent);
  outline: none;
}
.inline-read-label {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
}
.inline-edit-ghost {
  flex-shrink: 0;
  color: var(--bp-blue);
  opacity: 0;
  transition: opacity .12s;
}
.inline-read-value:hover .inline-edit-ghost,
.inline-read-value:focus-visible .inline-edit-ghost {
  opacity: .58;
}
.clickable-cell { cursor: cell; border-radius: 3px; padding: .1rem .25rem; position: relative; }
.clickable-cell:hover { background: color-mix(in srgb, var(--bp-blue) 8%, transparent); outline: 1px solid color-mix(in srgb, var(--bp-blue) 25%, transparent); outline-offset: -1px; }
.clickable-cell::before { content: '✎'; position: absolute; left: -14px; top: 50%; transform: translateY(-50%); font-size: 11px; color: var(--bp-blue); opacity: 0; transition: opacity .15s; pointer-events: none; }
.clickable-cell:hover::before { opacity: .6; }

.sel-th, .sel-td {
  width: 36px;
  min-width: 36px;
  max-width: 36px;
  text-align: center;
  padding: 0 .5rem !important;
  position: sticky;
  left: 0;
  background: var(--bg-card);
  z-index: 12;
}
.issue-table thead .sel-th { background: var(--bg); z-index: 13; }
.issue-table tbody tr.row-active-panel .sel-td { background: color-mix(in srgb, var(--bp-blue) 8%, var(--bg-card)); }
.issue-table tbody tr.row-selected .sel-td { background: var(--bp-blue-pale); }
.issue-table tbody tr:hover .sel-td { background: #f0f2f4; }
.issue-table tbody tr.row-selected:hover .sel-td { background: var(--bp-blue-pale); }
.sel-cb { width: 15px; height: 15px; padding: 0; border: revert; border-radius: revert; background: revert; cursor: pointer; accent-color: var(--bp-blue); }
.row-selected { background: var(--bp-blue-pale) !important; }
.row-active-panel { background: color-mix(in srgb, var(--bp-blue) 8%, var(--bg-card)); box-shadow: inset 3px 0 0 var(--bp-blue); }

.sortable-th { cursor: pointer; user-select: none; white-space: nowrap; }
.sortable-th:hover { color: var(--text); background: var(--border) !important; }
.sortable-th.sort-active { color: var(--bp-blue-dark) !important; }
.sort-ind { display: inline-block; margin-left: .25rem; font-size: 10px; opacity: .55; vertical-align: middle; }
.sortable-th.sort-active .sort-ind { opacity: 1; }
.resizable-th { position: sticky; overflow: visible; }
.resizable-th::after {
  content: '';
  position: absolute;
  top: 0;
  right: -5px;
  bottom: 0;
  width: 10px;
  cursor: col-resize;
  z-index: 2;
}
.resizable-th:hover::before,
.resizable-th--active::before {
  content: '';
  position: absolute;
  top: .35rem;
  right: 0;
  bottom: .35rem;
  width: 2px;
  border-radius: 999px;
  background: color-mix(in srgb, var(--bp-blue) 55%, transparent);
}
:global(body.is-column-resizing) {
  cursor: col-resize;
  user-select: none;
}

.col-key { position: sticky; left: 0; z-index: 11; }
.issue-table--selection-mode .col-key { left: 36px; }
.issue-table thead .col-key { background: var(--bg); z-index: 12; }
.issue-table tbody .col-key { background: var(--bg-card); }
.issue-table tbody tr.row-active-panel .col-key { background: color-mix(in srgb, var(--bp-blue) 8%, var(--bg-card)); }
.issue-table tbody tr.row-selected .col-key { background: var(--bp-blue-pale); }
.issue-table tbody tr:hover .col-key { background: #f0f2f4; }
.issue-table tbody tr.row-selected:hover .col-key { background: var(--bp-blue-pale); }
.key-cell { white-space: nowrap; }
.issue-title-cell { min-width: 220px; }

:deep(.search-highlight) { background: #fef08a; color: inherit; border-radius: 2px; padding: 0 1px; font-style: normal; }
.issue-link { font-weight: 500; color: var(--text); }
.clickable:hover .issue-link { color: var(--bp-blue); }

/* Inline edit inputs — title (row) and estimate cells (tabular) */
.title-inline-input {
  width: 100%;
  font: inherit;
  font-size: 13px;
  font-weight: 500;
  padding: .2rem .4rem;
  border: 1px solid var(--bp-blue);
  border-radius: 4px;
  background: var(--bg-card);
  color: var(--text);
  outline: none;
}
.numeric-inline-input {
  width: 72px;
  font: inherit;
  font-size: 12px;
  padding: .2rem .35rem;
  border: 1px solid var(--bp-blue);
  border-radius: 4px;
  background: var(--bg-card);
  color: var(--text);
  outline: none;
  text-align: right;
  font-variant-numeric: tabular-nums;
}
.meta-inline-select {
  min-width: 140px;
  max-width: 240px;
  font: inherit;
  font-size: 12px;
  padding: .2rem .35rem;
  border: 1px solid var(--bp-blue);
  border-radius: 4px;
  background: var(--bg-card);
  color: var(--text);
  outline: none;
}
.meta-cell { color: var(--text-muted); white-space: nowrap; font-size: 12px; }
.booked-cell { color: var(--bp-green, #16a34a); font-weight: 600; }
.report-summary-cell { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; max-width: 0; color: var(--text); }
.col-ai-status,
.ai-status-cell {
  text-align: left;
  white-space: nowrap;
}
.ai-status-empty {
  color: var(--text-muted);
}

.tags-th { white-space: nowrap; }
.tags-cell { vertical-align: middle; }
.tags-cell .row-tags { display: flex; flex-wrap: wrap; gap: .2rem; }

/* PAI-466: always-visible CUSTOMERPORTAL eye glyph that lives in the
   type cell. Subtle so it doesn't overpower the row, but immediately
   readable as a "yes, the customer sees this" affordance. */
.customerportal-marker {
  display: inline-flex;
  align-items: center;
  margin-left: .35rem;
  color: var(--brand, #2563eb);
  opacity: .8;
}
.customerportal-marker:hover { opacity: 1; }
.sprint-link-inline { display: inline-flex; align-items: center; gap: 3px; font-size: 12px; color: var(--text); }
.sprint-link-inline + .sprint-link-inline { margin-left: .3rem; }

.epic-wrap { display: inline-flex; align-items: center; gap: 3px; }
.epic-badge { display: inline-flex; align-items: center; background: #f3e8ff; color: #6b21a8; border-radius: 20px; font-size: 11px; font-weight: 600; padding: .1rem .5rem; white-space: nowrap; text-decoration: none; max-width: 220px; overflow: hidden; text-overflow: ellipsis; transition: box-shadow .1s; }
.epic-badge:hover { box-shadow: 0 0 0 2px rgba(107,33,168,.25); }

.issue-key-copy { font-size: 11px; font-weight: 700; letter-spacing: .04em; font-family: monospace; color: var(--text-muted); white-space: nowrap; flex-shrink: 0; cursor: pointer; position: relative; display: inline-flex; align-items: center; gap: 3px; }
.issue-key-copy:hover { color: var(--text); }
.copy-ghost-icon { opacity: 0; color: var(--text-muted); transition: opacity .1s; position: absolute; left: calc(100% + 2px); top: 50%; transform: translateY(-50%); pointer-events: none; }
.issue-key-copy:hover .copy-ghost-icon { opacity: .6; }

.type-label-text { font-size: 12px; }

.meta-cell-assignee { display: inline-flex; align-items: center; gap: .4rem; }

.col-actions { position: sticky; right: 0; text-align: center; white-space: nowrap; padding-left: 1rem; padding-right: 1rem; }
.issue-table thead .col-actions { z-index: 12; background: var(--bg); }
.issue-table tbody .col-actions { z-index: 11; background: var(--bg-card); }
.issue-table tbody tr.row-active-panel .col-actions { background: color-mix(in srgb, var(--bp-blue) 8%, var(--bg-card)); }
.issue-table tbody tr.row-selected .col-actions { background: var(--bp-blue-pale); }
.issue-table tbody tr:hover .col-actions { background: #f0f2f4; }
.issue-table tbody tr.row-selected:hover .col-actions { background: var(--bp-blue-pale); }

.th-toggle { cursor: pointer; }
.unit-toggle { color: var(--bp-blue); font-weight: 600; text-decoration: underline; text-decoration-style: dotted; }


.row-expanded > td:first-child { border-left: 2px solid var(--bp-blue); }
.expand-panel-row { background: var(--surface-2); }
.expand-panel-cell { padding: 0 !important; border-bottom: 1px solid var(--border); }
.expand-empty { font-size: 12px; color: var(--text-muted); padding: .5rem 1rem; font-style: italic; }
.expand-children { display: flex; flex-direction: column; }
.expand-child-row { display: flex; align-items: center; gap: 1rem; padding: .4rem 1rem .4rem 2rem; border-bottom: 1px solid var(--border-subtle, var(--border)); font-size: 12px; cursor: pointer; transition: background .1s; }
.expand-child-row:last-child { border-bottom: none; }
.expand-child-row:hover { background: var(--bg-card); }
.expand-child-key { font-family: monospace; font-size: 11px; color: var(--text-muted); white-space: nowrap; min-width: 80px; }
.expand-child-title { flex: 1; color: var(--text); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.expand-child-status { flex-shrink: 0; }
.expand-child-assignee { font-size: 11px; color: var(--text-muted); flex-shrink: 0; min-width: 60px; }
</style>
