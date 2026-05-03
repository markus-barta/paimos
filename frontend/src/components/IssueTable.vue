<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink } from 'vue-router'
import { highlight } from '@/composables/useHighlight'
import AppIcon from '@/components/AppIcon.vue'
import AutocompleteInput from '@/components/AutocompleteInput.vue'
import TagChip from '@/components/TagChip.vue'
import IssueRowActions from '@/components/IssueRowActions.vue'
import StatusDot from '@/components/StatusDot.vue'
import UserAvatar from '@/components/UserAvatar.vue'
import MetaSelect from '@/components/MetaSelect.vue'
import type { MetaOption } from '@/components/MetaSelect.vue'
import type { Issue, Sprint, User } from '@/types'
import {
  useIssueDisplay,
  TYPE_SVGS,
  STATUS_LABEL,
  PRIORITY_ICON, PRIORITY_COLOR, PRIORITY_LABEL,
} from '@/composables/useIssueDisplay'
import { INLINE_STATUS_OPTIONS, INLINE_PRIORITY_OPTIONS } from '@/composables/useInlineEdit'
import type { EditableField } from '@/composables/useInlineEdit'
import { useTimeUnit } from '@/composables/useTimeUnit'
import type { FormatContext } from '@/composables/useTimeUnit'
import { useSearchStore } from '@/stores/search'
import { useIssueContext } from '@/composables/useIssueContext'

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
  // Inline assignee options
  inlineAssigneeOptions: () => MetaOption[]
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
  'update:cell-edit-value': [value: string]
  'update:sprint-picker-search': [value: string]
}>()

const { showTypeIcon, showTypeText } = useIssueDisplay()

const BILLING_LABEL: Record<string, string> = {
  time_and_material: 'Time & Material',
  fixed_price:       'Fixed Price',
}

function typeLabel(type: string): string {
  return type.charAt(0).toUpperCase() + type.slice(1)
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
</script>

<template>
  <table class="issue-table">
    <thead v-if="!compact">
      <tr>
        <th v-if="selectionMode" class="sel-th">
          <input type="checkbox" class="sel-cb" :checked="allSelected" @change="emit('toggle-select-all')" title="Select all" />
        </th>
        <th v-if="isVisible('key')" v-bind="sortResult.thProps('key')" class="col-key">Key <span class="sort-ind"><AppIcon :name="sortResult.sortIndicator('key')" :size="11" /></span></th>
        <th v-if="isVisible('type')" v-bind="sortResult.thProps('type')">Type <span class="sort-ind"><AppIcon :name="sortResult.sortIndicator('type')" :size="11" /></span></th>
        <th v-if="isVisible('title')" v-bind="sortResult.thProps('title')">Title <span class="sort-ind"><AppIcon :name="sortResult.sortIndicator('title')" :size="11" /></span></th>
        <th v-if="isVisible('status')" v-bind="sortResult.thProps('status')">Status <span class="sort-ind"><AppIcon :name="sortResult.sortIndicator('status')" :size="11" /></span></th>
        <th v-if="isVisible('priority')" v-bind="sortResult.thProps('priority')">Priority <span class="sort-ind"><AppIcon :name="sortResult.sortIndicator('priority')" :size="11" /></span></th>
        <th v-if="isVisible('cost_unit')" v-bind="sortResult.thProps('cost_unit')">Cost Unit <span class="sort-ind"><AppIcon :name="sortResult.sortIndicator('cost_unit')" :size="11" /></span></th>
        <th v-if="isVisible('release')" v-bind="sortResult.thProps('release')">Release <span class="sort-ind"><AppIcon :name="sortResult.sortIndicator('release')" :size="11" /></span></th>
        <th v-if="isVisible('assignee')" v-bind="sortResult.thProps('assignee')">Assignee <span class="sort-ind"><AppIcon :name="sortResult.sortIndicator('assignee')" :size="11" /></span></th>
        <th v-if="isVisible('tags')" class="tags-th">Tags</th>
        <th v-if="isVisible('epic')" class="tags-th">Epic</th>
        <th v-if="isVisible('sprint')" class="tags-th">Sprint</th>
        <th v-if="isVisible('billing_type')" v-bind="sortResult.thProps('billing_type')">Billing <span class="sort-ind"><AppIcon :name="sortResult.sortIndicator('billing_type')" :size="11" /></span></th>
        <th v-if="isVisible('total_budget')" v-bind="sortResult.thProps('total_budget')">Budget <span class="sort-ind"><AppIcon :name="sortResult.sortIndicator('total_budget')" :size="11" /></span></th>
        <th v-if="isVisible('rate_hourly')" v-bind="sortResult.thProps('rate_hourly')">Rate/h <span class="sort-ind"><AppIcon :name="sortResult.sortIndicator('rate_hourly')" :size="11" /></span></th>
        <th v-if="isVisible('rate_lp')" v-bind="sortResult.thProps('rate_lp')">Rate LP <span class="sort-ind"><AppIcon :name="sortResult.sortIndicator('rate_lp')" :size="11" /></span></th>
        <th v-if="isVisible('estimate_hours')" v-bind="sortResult.thProps('estimate_hours')" class="th-toggle" @click.stop="emit('toggle-time-unit')">Est. <span class="unit-toggle">{{ timeLabel() }}</span> <span class="sort-ind"><AppIcon :name="sortResult.sortIndicator('estimate_hours')" :size="11" /></span></th>
        <th v-if="isVisible('estimate_lp')" v-bind="sortResult.thProps('estimate_lp')">Est. LP <span class="sort-ind"><AppIcon :name="sortResult.sortIndicator('estimate_lp')" :size="11" /></span></th>
        <th v-if="isVisible('ar_hours')" v-bind="sortResult.thProps('ar_hours')" class="th-toggle" @click.stop="emit('toggle-time-unit')">AR <span class="unit-toggle">{{ timeLabel() }}</span> <span class="sort-ind"><AppIcon :name="sortResult.sortIndicator('ar_hours')" :size="11" /></span></th>
        <th v-if="isVisible('ar_lp')" v-bind="sortResult.thProps('ar_lp')">AR LP <span class="sort-ind"><AppIcon :name="sortResult.sortIndicator('ar_lp')" :size="11" /></span></th>
        <th v-if="isVisible('start_date')" v-bind="sortResult.thProps('start_date')">Start <span class="sort-ind"><AppIcon :name="sortResult.sortIndicator('start_date')" :size="11" /></span></th>
        <th v-if="isVisible('end_date')" v-bind="sortResult.thProps('end_date')">End <span class="sort-ind"><AppIcon :name="sortResult.sortIndicator('end_date')" :size="11" /></span></th>
        <th v-if="isVisible('group_state')" v-bind="sortResult.thProps('group_state')">Group State <span class="sort-ind"><AppIcon :name="sortResult.sortIndicator('group_state')" :size="11" /></span></th>
        <th v-if="isVisible('sprint_state')" v-bind="sortResult.thProps('sprint_state')">Sprint State <span class="sort-ind"><AppIcon :name="sortResult.sortIndicator('sprint_state')" :size="11" /></span></th>
        <th v-if="isVisible('jira_id')" v-bind="sortResult.thProps('jira_id')">Jira ID <span class="sort-ind"><AppIcon :name="sortResult.sortIndicator('jira_id')" :size="11" /></span></th>
        <th v-if="isVisible('jira_version')" v-bind="sortResult.thProps('jira_version')">Jira Version <span class="sort-ind"><AppIcon :name="sortResult.sortIndicator('jira_version')" :size="11" /></span></th>
        <th v-if="isVisible('jira_text')" v-bind="sortResult.thProps('jira_text')">Jira Text <span class="sort-ind"><AppIcon :name="sortResult.sortIndicator('jira_text')" :size="11" /></span></th>
        <th v-if="isVisible('booked_hours')" v-bind="sortResult.thProps('booked_hours')">Booked <span class="sort-ind"><AppIcon :name="sortResult.sortIndicator('booked_hours')" :size="11" /></span></th>
        <th class="col-actions">Actions</th>
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
          </span>
        </td>
        <td v-if="compact">
          <span :class="`issue-type issue-type--${i.type}`">
            <span v-if="showTypeIcon" v-html="TYPE_SVGS[i.type] ?? ''"></span>
            <span v-if="showTypeText" class="type-label-text">{{ typeLabel(i.type) }}</span>
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
        <td v-if="!compact && isVisible('status')" class="inline-edit-cell">
          <MetaSelect
            v-if="editingCell?.issueId === i.id && editingCell?.field === 'status'"
            :model-value="i.status"
            :options="INLINE_STATUS_OPTIONS"
            @update:model-value="v => emit('save-cell-edit', i, 'status', v)"
          />
          <span v-else class="issue-status clickable-cell" @click.stop="emit('open-cell', i, 'status', $event)">
            <StatusDot :status="i.status" />
            {{ STATUS_LABEL[i.status] }}
          </span>
        </td>
        <td v-if="compact">
          <span class="issue-status">
            <StatusDot :status="i.status" />
            {{ STATUS_LABEL[i.status] }}
          </span>
        </td>
        <td v-if="!compact && isVisible('priority')" class="inline-edit-cell">
          <MetaSelect
            v-if="editingCell?.issueId === i.id && editingCell?.field === 'priority'"
            :model-value="i.priority"
            :options="INLINE_PRIORITY_OPTIONS"
            @update:model-value="v => emit('save-cell-edit', i, 'priority', v)"
          />
          <span v-else class="issue-priority clickable-cell" :style="{ color: PRIORITY_COLOR[i.priority] }" @click.stop="emit('open-cell', i, 'priority', $event)">
            <AppIcon :name="PRIORITY_ICON[i.priority]" :size="12" :stroke-width="2.5" class="issue-priority-arrow" />
            {{ PRIORITY_LABEL[i.priority] }}
          </span>
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
          <span v-else class="clickable-cell" @click.stop="emit('open-cell', i, 'cost_unit', $event)">{{ i.cost_unit || '—' }}</span>
        </td>
        <td v-if="!compact && isVisible('release')" class="meta-cell inline-edit-cell">
          <AutocompleteInput
            v-if="editingCell?.issueId === i.id && editingCell?.field === 'release'"
            :model-value="cellEditValue"
            :suggestions="releases"
            placeholder="Release..."
            @update:model-value="v => emit('update:cell-edit-value', v)"
            @keydown.enter.stop="emit('save-cell-edit', i, 'release', cellEditValue)"
            @keydown.escape.stop="emit('close-cell', false)"
          />
          <span v-else class="clickable-cell" @click.stop="emit('open-cell', i, 'release', $event)">{{ i.release || '—' }}</span>
        </td>
        <td v-if="!compact && isVisible('assignee')" class="meta-cell inline-edit-cell">
          <MetaSelect
            v-if="editingCell?.issueId === i.id && editingCell?.field === 'assignee_id'"
            :model-value="i.assignee_id !== null ? String(i.assignee_id) : ''"
            :options="inlineAssigneeOptions()"
            @update:model-value="v => emit('save-cell-edit', i, 'assignee_id', v)"
          />
          <span v-else class="meta-cell-assignee clickable-cell" @click.stop="emit('open-cell', i, 'assignee_id', $event)">
            <UserAvatar v-if="i.assignee" :user="users.find(u => u.id === i.assignee_id) ?? i.assignee" size="sm" :show-tooltip="true" />
            <span>{{ i.assignee?.username ?? '—' }}</span>
          </span>
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
        <td v-if="!compact && isVisible('total_budget')" class="meta-cell">{{ i.total_budget != null ? i.total_budget.toLocaleString() : '—' }}</td>
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
        <td class="col-actions" @click.stop>
          <IssueRowActions :can-have-children="true" :compact="compact" :collapsed="actionsCollapsed" :issue-id="i.id" :issue-type="i.type" :booked-hours="i.booked_hours" :is-admin="isAdmin" @add-child="emit('open-create', i)" @edit="emit('open-side-panel', i, true)" @view="emit('open-side-panel', i, false)" @copy="emit('copy-key', i.issue_key)" @delete="emit('delete-row', i)" />
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

.issue-table thead th { padding: .6rem .85rem; text-align: left; font-size: 11px; font-weight: 700; text-transform: uppercase; letter-spacing: .06em; color: var(--text-muted); background: var(--bg); border-bottom: 1px solid var(--border); position: sticky; top: 0; z-index: 10; }
.issue-table tbody tr { border-bottom: none; }
.issue-table td { padding: .6rem .85rem; font-size: 13px; vertical-align: middle; }

.clickable { cursor: pointer; }
.issue-table tbody tr.clickable:hover { background: #f0f2f4; }
.inline-edit-cell { cursor: default; position: relative; overflow: visible; }
.clickable-cell { cursor: cell; border-radius: 3px; padding: .1rem .25rem; position: relative; }
.clickable-cell:hover { background: color-mix(in srgb, var(--bp-blue) 8%, transparent); outline: 1px solid color-mix(in srgb, var(--bp-blue) 25%, transparent); outline-offset: -1px; }
.clickable-cell::before { content: '✎'; position: absolute; left: -14px; top: 50%; transform: translateY(-50%); font-size: 11px; color: var(--bp-blue); opacity: 0; transition: opacity .15s; pointer-events: none; }
.clickable-cell:hover::before { opacity: .6; }

.sel-th, .sel-td { width: 36px; text-align: center; padding: 0 .5rem !important; }
.sel-cb { width: 15px; height: 15px; padding: 0; border: revert; border-radius: revert; background: revert; cursor: pointer; accent-color: var(--bp-blue); }
.row-selected { background: var(--bp-blue-pale) !important; }
.row-active-panel { background: color-mix(in srgb, var(--bp-blue) 8%, var(--bg-card)); box-shadow: inset 3px 0 0 var(--bp-blue); }

.sortable-th { cursor: pointer; user-select: none; white-space: nowrap; }
.sortable-th:hover { color: var(--text); background: var(--border) !important; }
.sortable-th.sort-active { color: var(--bp-blue-dark) !important; }
.sort-ind { display: inline-block; margin-left: .25rem; font-size: 10px; opacity: .55; vertical-align: middle; }
.sortable-th.sort-active .sort-ind { opacity: 1; }

.col-key { position: sticky; left: 0; z-index: 11; }
.issue-table thead .col-key { background: var(--bg); z-index: 12; }
.issue-table tbody .col-key { background: var(--bg-card); }
.issue-table tbody tr.row-active-panel .col-key { background: color-mix(in srgb, var(--bp-blue) 8%, var(--bg-card)); }
.issue-table tbody tr.row-selected .col-key { background: var(--bp-blue-pale); }
.issue-table tbody tr:hover .col-key { background: #f0f2f4; }
.issue-table tbody tr.row-selected:hover .col-key { background: var(--bp-blue-pale); }
.key-cell { white-space: nowrap; }
.issue-title-cell { min-width: 250px; max-width: 260px; }

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
.meta-cell { color: var(--text-muted); white-space: nowrap; font-size: 12px; }
.booked-cell { color: var(--bp-green, #16a34a); font-weight: 600; }

.tags-th { white-space: nowrap; }
.tags-cell { vertical-align: middle; }
.tags-cell .row-tags { display: flex; flex-wrap: wrap; gap: .2rem; }
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
