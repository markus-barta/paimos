<script setup lang="ts">
import AppIcon from '@/components/AppIcon.vue'
import TagChip from '@/components/TagChip.vue'
import IssueRowActions from '@/components/IssueRowActions.vue'
import StatusDot from '@/components/StatusDot.vue'
import type { Issue } from '@/types'
import {
  useIssueDisplay,
  TYPE_SVGS,
  STATUS_LABEL,
  PRIORITY_ICON, PRIORITY_COLOR, PRIORITY_LABEL,
} from '@/composables/useIssueDisplay'

type TreeIssue = Issue & { children: (Issue & { children: Issue[] })[] }

const props = defineProps<{
  issueTree: TreeIssue[]
  selectionMode: boolean
  selectedIds: Set<number>
  treeExpanded: Set<number>
  isAdmin: boolean
  sidePanelIssueId: number | null
}>()

const emit = defineEmits<{
  'toggle-tree-node': [id: number]
  'toggle-tree-select': [issue: Issue]
  'toggle-select': [id: number]
  'navigate-to': [issue: Issue]
  'copy-key': [key: string, event?: MouseEvent]
  'open-create': [issue: Issue]
  'open-side-panel': [issue: Issue, edit: boolean]
  'delete-row': [issue: Issue]
}>()

const { showTypeIcon, showTypeText } = useIssueDisplay()

function typeLabel(type: string): string {
  return type.charAt(0).toUpperCase() + type.slice(1)
}
</script>

<template>
  <div class="tree-wrap">
    <div v-for="epic in issueTree" :key="epic.id" class="tree-epic">
      <div :class="['tree-row tree-row-epic clickable', { 'row-selected': selectionMode && selectedIds.has(epic.id), 'row-active-panel': sidePanelIssueId === epic.id }]" @click="selectionMode ? emit('toggle-tree-select', epic) : emit('navigate-to', epic)">
        <button class="tree-toggle" @click.stop="emit('toggle-tree-node', epic.id)" :title="treeExpanded.has(epic.id) ? 'Collapse' : 'Expand'">
          <AppIcon :name="treeExpanded.has(epic.id) ? 'chevron-down' : 'chevron-right'" :size="12" />
        </button>
        <input v-if="selectionMode" type="checkbox" class="tree-check" :checked="selectedIds.has(epic.id)" @click.stop="emit('toggle-tree-select', epic)" />
        <span class="issue-key-copy" @click.stop="emit('copy-key', epic.issue_key, $event)" title="Copy issue key">{{ epic.issue_key }}<AppIcon name="clipboard" :size="11" class="copy-ghost-icon" /></span>
        <span :class="`issue-type issue-type--${epic.type}`">
          <span v-if="showTypeIcon" v-html="TYPE_SVGS[epic.type] ?? ''"></span>
          <span v-if="showTypeText" class="type-label-text">{{ typeLabel(epic.type) }}</span>
        </span>
        <span class="issue-link">{{ epic.title }}</span>
        <span class="issue-status"><StatusDot :status="epic.status" />{{ STATUS_LABEL[epic.status] }}</span>
        <span class="issue-priority" :style="{ color: PRIORITY_COLOR[epic.priority] }"><AppIcon :name="PRIORITY_ICON[epic.priority]" :size="12" :stroke-width="2.5" class="issue-priority-arrow" />{{ PRIORITY_LABEL[epic.priority] }}</span>
        <span v-if="epic.assignee" class="tree-assignee">{{ epic.assignee.username }}</span>
        <template v-if="epic.tags?.length"><TagChip v-for="t in epic.tags.slice(0,3)" :key="t.id" :tag="t" /></template>
        <span v-if="epic.cost_unit" class="meta-pill">{{ epic.cost_unit }}</span>
        <span v-if="epic.release" class="meta-pill release-pill">{{ epic.release }}</span>
        <span v-if="(epic.type === 'epic' || epic.type === 'cost_unit') && epic.total_budget != null" class="meta-pill budget-pill">{{ epic.total_budget.toLocaleString() }}</span>
        <span v-if="epic.type === 'release' && epic.group_state" :class="['v2-tree-badge', `v2-tree--${epic.group_state}`]">{{ epic.group_state }}</span>
        <span v-if="epic.type === 'sprint' && epic.sprint_state" :class="['v2-tree-badge', `v2-tree--${epic.sprint_state}`]">{{ epic.sprint_state }}</span>
        <div class="tree-actions" @click.stop>
          <IssueRowActions :can-have-children="true" :issue-id="epic.id" :issue-type="epic.type" :booked-hours="epic.booked_hours" :is-admin="isAdmin" @add-child="emit('open-create', epic)" @edit="emit('open-side-panel', epic, true)" @view="emit('open-side-panel', epic, false)" @copy="emit('copy-key', epic.issue_key)" @delete="emit('delete-row', epic)" />
        </div>
      </div>
      <template v-if="treeExpanded.has(epic.id)">
      <template v-for="ticket in epic.children" :key="ticket.id">
        <div :class="['tree-row tree-row-ticket clickable', { 'row-selected': selectionMode && selectedIds.has(ticket.id), 'row-active-panel': sidePanelIssueId === ticket.id }]" @click="selectionMode ? emit('toggle-tree-select', ticket) : emit('navigate-to', ticket)">
          <span class="tree-indent"></span>
          <button v-if="ticket.children?.length" class="tree-toggle" @click.stop="emit('toggle-tree-node', ticket.id)">
            <AppIcon :name="treeExpanded.has(ticket.id) ? 'chevron-down' : 'chevron-right'" :size="11" />
          </button>
          <span v-else class="tree-toggle-spacer"></span>
          <input v-if="selectionMode" type="checkbox" class="tree-check" :checked="selectedIds.has(ticket.id)" @click.stop="emit('toggle-tree-select', ticket)" />
          <span class="issue-key-copy" @click.stop="emit('copy-key', ticket.issue_key, $event)" title="Copy issue key">{{ ticket.issue_key }}<AppIcon name="clipboard" :size="11" class="copy-ghost-icon" /></span>
          <span :class="`issue-type issue-type--${ticket.type}`">
            <span v-if="showTypeIcon" v-html="TYPE_SVGS[ticket.type] ?? ''"></span>
            <span v-if="showTypeText" class="type-label-text">{{ typeLabel(ticket.type) }}</span>
          </span>
          <span class="issue-link">{{ ticket.title }}</span>
          <span class="issue-status"><StatusDot :status="ticket.status" />{{ STATUS_LABEL[ticket.status] }}</span>
          <span class="issue-priority" :style="{ color: PRIORITY_COLOR[ticket.priority] }"><AppIcon :name="PRIORITY_ICON[ticket.priority]" :size="12" :stroke-width="2.5" class="issue-priority-arrow" />{{ PRIORITY_LABEL[ticket.priority] }}</span>
          <span v-if="ticket.assignee" class="tree-assignee">{{ ticket.assignee.username }}</span>
          <template v-if="ticket.tags?.length"><TagChip v-for="t in ticket.tags.slice(0,3)" :key="t.id" :tag="t" /></template>
          <span v-if="ticket.cost_unit" class="meta-pill">{{ ticket.cost_unit }}</span>
          <span v-if="ticket.release" class="meta-pill release-pill">{{ ticket.release }}</span>
          <div class="tree-actions" @click.stop>
            <IssueRowActions :can-have-children="true" :issue-id="ticket.id" :issue-type="ticket.type" :booked-hours="ticket.booked_hours" :is-admin="isAdmin" @add-child="emit('open-create', ticket)" @edit="emit('open-side-panel', ticket, true)" @view="emit('open-side-panel', ticket, false)" @copy="emit('copy-key', ticket.issue_key)" @delete="emit('delete-row', ticket)" />
          </div>
        </div>
        <template v-if="treeExpanded.has(ticket.id)">
        <div v-for="task in ticket.children" :key="task.id"
             :class="['tree-row tree-row-task clickable', { 'row-selected': selectionMode && selectedIds.has(task.id), 'row-active-panel': sidePanelIssueId === task.id }]"
             @click="selectionMode ? emit('toggle-select', task.id) : emit('navigate-to', task)">
          <span class="tree-indent"></span>
          <span class="tree-indent"></span>
          <span class="tree-toggle-spacer"></span>
          <input v-if="selectionMode" type="checkbox" class="tree-check" :checked="selectedIds.has(task.id)" @click.stop="emit('toggle-select', task.id)" />
          <span class="issue-key-copy" @click.stop="emit('copy-key', task.issue_key, $event)" title="Copy issue key">{{ task.issue_key }}<AppIcon name="clipboard" :size="11" class="copy-ghost-icon" /></span>
          <span :class="`issue-type issue-type--${task.type}`">
            <span v-if="showTypeIcon" v-html="TYPE_SVGS[task.type] ?? ''"></span>
            <span v-if="showTypeText" class="type-label-text">{{ typeLabel(task.type) }}</span>
          </span>
          <span class="issue-link">{{ task.title }}</span>
          <span class="issue-status"><StatusDot :status="task.status" />{{ STATUS_LABEL[task.status] }}</span>
          <span class="issue-priority" :style="{ color: PRIORITY_COLOR[task.priority] }"><AppIcon :name="PRIORITY_ICON[task.priority]" :size="12" :stroke-width="2.5" class="issue-priority-arrow" />{{ PRIORITY_LABEL[task.priority] }}</span>
          <span v-if="task.assignee" class="tree-assignee">{{ task.assignee.username }}</span>
          <template v-if="task.tags?.length"><TagChip v-for="t in task.tags.slice(0,3)" :key="t.id" :tag="t" /></template>
          <span v-if="task.cost_unit" class="meta-pill">{{ task.cost_unit }}</span>
          <span v-if="task.release" class="meta-pill release-pill">{{ task.release }}</span>
          <div class="tree-actions" @click.stop>
            <IssueRowActions :can-have-children="false" :issue-id="task.id" :issue-type="task.type" :booked-hours="task.booked_hours" :is-admin="isAdmin" @edit="emit('open-side-panel', task, true)" @view="emit('open-side-panel', task, false)" @copy="emit('copy-key', task.issue_key)" @delete="emit('delete-row', task)" />
          </div>
        </div>
        </template>
      </template>
      </template>
    </div>
  </div>
</template>

<style scoped>
.tree-wrap { display: flex; flex-direction: column; gap: .5rem; margin-top: 1.25rem; }
.tree-epic { background: var(--bg-card); border: 1px solid var(--border); border-radius: 8px; overflow: hidden; box-shadow: var(--shadow); }
.tree-row { display: flex; align-items: center; gap: .5rem; padding: .65rem 1rem; border-bottom: 1px solid var(--border); flex-wrap: nowrap; min-width: 0; }
.tree-row:last-child { border-bottom: none; }
.tree-row-epic   { background: #f8f6fd; }
.tree-row-ticket { background: var(--bg-card); }
.tree-row-task   { background: #fafcfa; }
.tree-row.clickable { cursor: pointer; }
.tree-row.clickable:hover { background: #f0f2f4; }
.tree-row.row-selected.clickable:hover { background: var(--bp-blue-pale); }
.tree-indent { width: 20px; flex-shrink: 0; }
.tree-toggle { background: none; border: none; cursor: pointer; padding: 2px; color: var(--text-muted); border-radius: 3px; display: flex; align-items: center; flex-shrink: 0; transition: color .1s, background .1s; }
.tree-toggle:hover { color: var(--text); background: rgba(0,0,0,.05); }
.tree-toggle-spacer { width: 16px; flex-shrink: 0; }
.tree-check { width: 15px; height: 15px; padding: 0; border: revert; border-radius: revert; background: revert; margin: 0; cursor: pointer; flex-shrink: 0; accent-color: var(--bp-blue); }
.tree-assignee { font-size: 11px; color: var(--text-muted); white-space: nowrap; max-width: 80px; overflow: hidden; text-overflow: ellipsis; flex-shrink: 0; }
.tree-actions { margin-left: auto; display: flex; gap: .2rem; flex-shrink: 0; }

.row-selected { background: var(--bp-blue-pale) !important; }
.row-active-panel { background: color-mix(in srgb, var(--bp-blue) 8%, var(--bg-card)); box-shadow: inset 3px 0 0 var(--bp-blue); }

.issue-key-copy { font-size: 11px; font-weight: 700; letter-spacing: .04em; font-family: monospace; color: var(--text-muted); white-space: nowrap; flex-shrink: 0; cursor: pointer; position: relative; display: inline-flex; align-items: center; gap: 3px; }
.issue-key-copy:hover { color: var(--text); }
.copy-ghost-icon { opacity: 0; color: var(--text-muted); transition: opacity .1s; position: absolute; left: calc(100% + 2px); top: 50%; transform: translateY(-50%); pointer-events: none; }
.issue-key-copy:hover .copy-ghost-icon { opacity: .6; }

.type-label-text { font-size: 12px; }
.issue-link { font-weight: 500; color: var(--text); }
.clickable:hover .issue-link { color: var(--bp-blue); }

.meta-pill { font-size: 11px; padding: .1rem .45rem; border-radius: 20px; background: #e0eeff; color: #1e4a8a; }
.release-pill { background: #ede9fe; color: #5b21b6; }
.budget-pill  { background: #dcfce7; color: #166534; font-weight: 700; }
.v2-tree-badge { font-size: 10px; font-weight: 700; text-transform: uppercase; letter-spacing: .04em; padding: .1rem .45rem; border-radius: 20px; }
.v2-tree--unreleased { background: #e0eeff; color: #1e4a8a; }
.v2-tree--released   { background: #dcfce7; color: #166534; }
.v2-tree--planned    { background: #f3f4f6; color: #374151; }
.v2-tree--active     { background: #fff3e0; color: #b45309; }
.v2-tree--complete   { background: #dcfce7; color: #166534; }
.v2-tree--archived   { background: #e5e7eb; color: #6b7280; }
</style>
