<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, watch } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import AppIcon from '@/components/AppIcon.vue'
import AppFooter from '@/components/AppFooter.vue'
import UserAvatar from '@/components/UserAvatar.vue'
import IssueSidePanel from '@/components/IssueSidePanel.vue'
import { api, errMsg } from '@/api/client'
import type { Issue, Sprint, User } from '@/types'
import { useConfirm } from '@/composables/useConfirm'

// ── Data ─────────────────────────────────────────────────────────────────────
const route        = useRoute()
const { confirm }  = useConfirm()
const sprints      = ref<Sprint[]>([])
const allUsers     = ref<User[]>([])
const loading      = ref(true)
const error        = ref('')

// ── Sprint selection ─────────────────────────────────────────────────────────
const activeSprint = ref<Sprint | null>(null)
const boardIssues  = ref<Issue[]>([])   // tickets + tasks for selected sprint
const boardLoading = ref(false)

// All tickets assigned to the selected sprint
const boardTickets = computed<Issue[]>(() =>
  boardIssues.value.filter(i => i.type === 'ticket')
)

// Tasks indexed by parent ticket id
const tasksByTicket = computed<Map<number, Issue[]>>(() => {
  const map = new Map<number, Issue[]>()
  for (const i of boardIssues.value) {
    if (i.type === 'task' && i.parent_id != null) {
      if (!map.has(i.parent_id)) map.set(i.parent_id, [])
      map.get(i.parent_id)!.push(i)
    }
  }
  return map
})

// ── Expanded rows ────────────────────────────────────────────────────────────
const expandedTicketIds = ref<Set<number>>(new Set())
function toggleExpand(id: number) {
  const next = new Set(expandedTicketIds.value)
  if (next.has(id)) { next.delete(id) } else { next.add(id) }
  expandedTicketIds.value = next
}
function expandAll() {
  expandedTicketIds.value = new Set(boardTickets.value.map(t => t.id))
}
function collapseAll() {
  expandedTicketIds.value = new Set()
}

// ── Sprint selector adjacency ─────────────────────────────────────────────────
// Sprints sorted by start_date ascending
const sortedSprints = computed(() =>
  [...sprints.value].sort((a, b) => (a.start_date ?? '').localeCompare(b.start_date ?? ''))
)

const activeSprint2 = computed(() => {
  const today = new Date().toISOString().slice(0, 10)
  return sortedSprints.value.find(s => s.sprint_state === 'active')
    ?? sortedSprints.value.find(s => (s.start_date ?? '') <= today && (s.end_date ?? '') >= today)
    ?? [...sortedSprints.value].reverse().find(s => (s.start_date ?? '') <= today)
    ?? sortedSprints.value[0]
    ?? null
})

const selectedSprintIdx = computed(() =>
  activeSprint.value ? sortedSprints.value.findIndex(s => s.id === activeSprint.value!.id) : -1
)
const prevSprint = computed(() =>
  selectedSprintIdx.value > 0 ? sortedSprints.value[selectedSprintIdx.value - 1] : null
)
const nextSprint = computed(() =>
  selectedSprintIdx.value < sortedSprints.value.length - 1
    ? sortedSprints.value[selectedSprintIdx.value + 1]
    : null
)

async function selectSprint(sprint: Sprint) {
  activeSprint.value = sprint
  await loadBoard()
}

// ── Board load ────────────────────────────────────────────────────────────────
async function loadBoard() {
  if (!activeSprint.value) return
  boardLoading.value = true
  try {
    // Get all issues that have this sprint in sprint_ids — fetch via relation
    const members = await api.get<Issue[]>(`/issues/${activeSprint.value.id}/members?type=sprint`)
    // For each ticket, also fetch its tasks
    const tickets = members.filter(i => i.type === 'ticket')
    const taskArrays = await Promise.all(
      tickets.map(t => api.get<Issue[]>(`/issues/${t.id}/children`).catch(() => []))
    )
    const tasks = taskArrays.flat()
    boardIssues.value = [...members, ...tasks.filter(t => !members.find(m => m.id === t.id))]
      .filter(i => i.status !== 'new') // 'new' items not shown on sprint board
  } catch (e: unknown) {
    error.value = errMsg(e, 'Failed to load board.')
  } finally {
    boardLoading.value = false
  }
}

async function load() {
  loading.value = true
  error.value   = ''
  try {
    const [spr, users] = await Promise.all([
      api.get<Sprint[]>('/sprints'),
      api.get<User[]>('/users'),
    ])
    sprints.value  = spr
    allUsers.value = users
    // Prefer ?sprint=:id query param (from sidebar click or sprint row click),
    // fall back to active/nearest sprint.
    const qId = Number(route.query.sprint) || 0
    const fromQuery = qId ? spr.find(s => s.id === qId) : undefined
    activeSprint.value = fromQuery ?? activeSprint2.value
    await loadBoard()
  } catch (e: unknown) {
    error.value = errMsg(e, 'Failed to load sprint board.')
  } finally {
    loading.value = false
  }
}

// ── Move incomplete to next sprint ──────────────────────────────────────────
const showMoveConfirm = ref(false)
function onMoveKey(e: KeyboardEvent) {
  const tag = (e.target as HTMLElement)?.tagName
  if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return
  const k = e.key.toLowerCase()
  if (k === 'm' || k === 'enter') { moveIncomplete(); e.preventDefault() }
  else if (k === 'c' || k === 'escape') { showMoveConfirm.value = false; e.preventDefault() }
}
watch(showMoveConfirm, (v) => {
  if (v) window.addEventListener('keydown', onMoveKey)
  else   window.removeEventListener('keydown', onMoveKey)
})
const moveResult = ref<{ count: number } | null>(null)
const moving = ref(false)

const incompleteIssues = computed(() =>
  boardIssues.value.filter(i => !['done','accepted','invoiced','cancelled'].includes(i.status) && i.type === 'ticket')
)

async function moveIncomplete() {
  if (!activeSprint.value) return
  moving.value = true
  try {
    const res = await api.post<{ count: number; next_sprint_id: number }>(
      `/sprints/${activeSprint.value.id}/move-incomplete`, {}
    )
    moveResult.value = res
    showMoveConfirm.value = false
    await loadBoard()
  } catch (e: unknown) {
    alert(errMsg(e, 'Move failed.'))
  } finally {
    moving.value = false
  }
}

// ── Reporting tab ───────────────────────────────────────────────────────────
const activeTab = ref<'planning' | 'reporting'>('planning')

// Side panel for reporting row clicks
const reportPanelIssueId = ref<number | null>(null)
function openReportPanel(id: number) { reportPanelIssueId.value = id }
function closeReportPanel() { reportPanelIssueId.value = null }

// Locale-aware number formatting
const fmtNum = (v: number, decimals = 1) => v.toLocaleString(undefined, { minimumFractionDigits: decimals, maximumFractionDigits: decimals })
const fmtEur = (v: number) => v.toLocaleString(undefined, { style: 'currency', currency: 'EUR', minimumFractionDigits: 0, maximumFractionDigits: 0 })

function issueEur(i: Issue): number {
  const rh = i.rate_hourly ?? 0
  const rl = i.rate_lp ?? 0
  return (i.ar_hours ?? 0) * rh + (i.ar_lp ?? 0) * rl
}

const reportMetrics = computed(() => {
  if (!activeSprint.value) return null
  const all = boardIssues.value.filter(i => i.type === 'ticket')
  const completed = all.filter(i => ['done','accepted','invoiced'].includes(i.status))
  // All totals sum only completed/accepted items — consistent across all columns
  const estHours    = completed.reduce((s, i) => s + (i.estimate_hours ?? 0), 0)
  const estLp       = completed.reduce((s, i) => s + (i.estimate_lp ?? 0), 0)
  const arHours     = completed.reduce((s, i) => s + (i.ar_hours ?? 0), 0)
  const arLp        = completed.reduce((s, i) => s + (i.ar_lp ?? 0), 0)
  const arEur       = completed.reduce((s, i) => s + issueEur(i), 0)
  const bookedHours = completed.reduce((s, i) => s + (i.booked_hours ?? 0), 0)
  const completionPct = all.length ? Math.round((completed.length / all.length) * 100) : 0
  return {
    targetAR: activeSprint.value.target_ar,
    arHours, arLp, arEur, bookedHours,
    estHours, estLp,
    completionPct,
    completedCount: completed.length,
    totalCount: all.length,
  }
})

// Totals for ALL tickets (regardless of status)
const reportTotalsAll = computed(() => {
  const all = boardIssues.value.filter(i => i.type === 'ticket')
  return {
    estHours:    all.reduce((s, i) => s + (i.estimate_hours ?? 0), 0),
    estLp:       all.reduce((s, i) => s + (i.estimate_lp ?? 0), 0),
    bookedHours: all.reduce((s, i) => s + (i.booked_hours ?? 0), 0),
    arHours:     all.reduce((s, i) => s + (i.ar_hours ?? 0), 0),
    arLp:        all.reduce((s, i) => s + (i.ar_lp ?? 0), 0),
    arEur:       all.reduce((s, i) => s + issueEur(i), 0),
  }
})

// Breakdown table rows for reporting
const reportBreakdown = computed(() => {
  if (!activeSprint.value) return []
  return boardIssues.value
    .filter(i => i.type === 'ticket')
    .map(i => ({
      id: i.id,
      key: i.issue_key ?? '',
      title: i.title,
      status: i.status,
      estHours: i.estimate_hours ?? 0,
      estLp: i.estimate_lp ?? 0,
      arHours: i.ar_hours ?? 0,
      arLp: i.ar_lp ?? 0,
      eur: issueEur(i),
      bookedHours: i.booked_hours ?? 0,
      completed: ['done','accepted','invoiced'].includes(i.status),
      bookedOverEst: (i.estimate_hours ?? 0) > 0 && (i.booked_hours ?? 0) > (i.estimate_hours ?? 0),
    }))
})

onMounted(() => {
  load()
  document.addEventListener('click', closeAllPickers)
})
onUnmounted(() => document.removeEventListener('click', closeAllPickers))
function closeAllPickers() {
  assigneePickerIssueId.value = null
  statusPickerIssueId.value = null
}

// If ?sprint changes while the board is already mounted (e.g. sidebar click
// when already on /sprint-board), switch to that sprint without a full reload.
watch(() => route.query.sprint, async (newId) => {
  if (!newId || loading.value) return
  const id = Number(newId)
  const found = sortedSprints.value.find(s => s.id === id)
  if (found && found.id !== activeSprint.value?.id) {
    await selectSprint(found)
  }
})

// ── Kanban columns ────────────────────────────────────────────────────────────
const COLUMNS = [
  { key: 'backlog',     label: 'Backlog',     color: '#6b7280' },
  { key: 'in-progress', label: 'In Progress', color: '#d97706' },
  { key: 'done',        label: 'Done',        color: '#059669' },
  { key: 'cancelled',   label: 'Cancelled',   color: '#dc2626' },
] as const
type ColKey = typeof COLUMNS[number]['key']

function issuesInCol(issues: Issue[], col: ColKey): Issue[] {
  return issues.filter(i => normaliseStatus(i.status) === col)
}

// Map status string to board column (all values are now canonical after M32)
function normaliseStatus(s: string): ColKey {
  if (s === 'in-progress' || s === 'qa') return 'in-progress'
  if (s === 'done' || s === 'delivered') return 'done'
  if (s === 'cancelled')   return 'cancelled'
  return 'backlog'
}

// Canonical status values — written directly to API (match DB CHECK constraint)
function canonicalStatus(col: ColKey): string {
  return col  // column keys ARE the canonical status values
}

// ── Drag & drop ───────────────────────────────────────────────────────────────
const draggingId  = ref<number | null>(null)
const dragOverCol = ref<string | null>(null)
const dropTargetId = ref<number | null>(null)        // task hovered during drag (insert position inside a kanban column)
const ticketDropTargetId = ref<number | null>(null)  // ticket row hovered during drag (insert position in planning list)

function onDragStart(issue: Issue, e: DragEvent) {
  draggingId.value = issue.id
  if (e.dataTransfer) e.dataTransfer.effectAllowed = 'move'
}
function onDragOver(col: string) {
  dragOverCol.value = col
}
function onDragLeave() {
  dragOverCol.value = null
}
function onCardDragOver(taskId: number) {
  dropTargetId.value = taskId
}

// Move dragged item to be inserted before `beforeId` in boardIssues.
// If beforeId is null, append to end. No-op if dragging onto self.
function moveInBoardIssues(fromId: number, beforeId: number | null) {
  if (fromId === beforeId) return false
  const arr = boardIssues.value
  const fromIdx = arr.findIndex(i => i.id === fromId)
  if (fromIdx < 0) return false
  const [item] = arr.splice(fromIdx, 1)
  let toIdx = beforeId == null ? arr.length : arr.findIndex(i => i.id === beforeId)
  if (toIdx < 0) toIdx = arr.length
  arr.splice(toIdx, 0, item)
  return true
}

// Persist current boardIssues order as the sprint's rank order.
async function persistSprintOrder() {
  if (!activeSprint.value) return
  const ids = boardIssues.value
    .filter(i => i.sprint_ids?.includes(activeSprint.value!.id))
    .map(i => i.id)
  if (!ids.length) return
  try {
    await api.put(`/sprints/${activeSprint.value.id}/reorder`, { issue_ids: ids })
  } catch (e: unknown) {
    error.value = errMsg(e, 'Reorder failed.')
    await loadBoard()
  }
}

// Ticket row drag-drop in Planning list
function onTicketRowDragOver(ticketId: number) {
  const src = draggingId.value
  if (src == null) return
  const dragged = boardIssues.value.find(i => i.id === src)
  if (dragged?.type !== 'ticket') return
  ticketDropTargetId.value = ticketId
}
async function onTicketRowDrop(target: Issue) {
  const src = draggingId.value
  draggingId.value = null
  ticketDropTargetId.value = null
  if (src == null || src === target.id) return
  const dragged = boardIssues.value.find(i => i.id === src)
  if (dragged?.type !== 'ticket') return
  if (!moveInBoardIssues(src, target.id)) return
  await persistSprintOrder()
}

async function onDrop(targetCol: ColKey, parentTicket?: Issue) {
  dragOverCol.value = null
  const overTaskId = dropTargetId.value
  dropTargetId.value = null
  const id = draggingId.value
  draggingId.value  = null
  if (!id) return

  const issue = boardIssues.value.find(i => i.id === id)
  if (!issue) return
  const fromCol = normaliseStatus(issue.status)

  // Same column = reorder task within column
  if (fromCol === targetCol) {
    // Insert dragged task before the hovered card; if dropped on empty area, append after last task in this column
    let beforeId: number | null = overTaskId && overTaskId !== id ? overTaskId : null
    if (beforeId == null && parentTicket) {
      // Append: place after the last task in this column for this ticket
      const colTasks = issuesInCol(tasksByTicket.value.get(parentTicket.id) ?? [], targetCol)
      const last = colTasks[colTasks.length - 1]
      if (last && last.id !== id) {
        // Find next item after `last` in boardIssues to use as the insertion anchor
        const lastIdx = boardIssues.value.findIndex(i => i.id === last.id)
        const next = boardIssues.value[lastIdx + 1]
        beforeId = next?.id ?? null
      }
    }
    if (!moveInBoardIssues(id, beforeId)) return
    await persistSprintOrder()
    return
  }

  // ── State propagation rules ─────────────────────────────────
  // Ticket → Done: confirm if any tasks are not done
  if (issue.type === 'ticket' && targetCol === 'done') {
    const tasks = tasksByTicket.value.get(issue.id) ?? []
    const incomplete = tasks.filter(t => normaliseStatus(t.status) !== 'done')
    if (incomplete.length > 0) {
      if (!await confirm({ message: `${incomplete.length} task${incomplete.length !== 1 ? 's' : ''} not yet done. Mark ticket as Done anyway?`, confirmLabel: 'Mark Done' })) return
    }
  }

  // Apply status change
  await applyStatus(issue, targetCol)

  // Ticket → Cancelled: cascade to backlog/in-progress tasks
  if (issue.type === 'ticket' && targetCol === 'cancelled') {
    const tasks = tasksByTicket.value.get(issue.id) ?? []
    for (const task of tasks) {
      const col = normaliseStatus(task.status)
      if (col === 'backlog' || col === 'in-progress') {
        await applyStatus(task, 'cancelled')
      }
    }
  }

  // Task → In Progress: bubble parent ticket to In Progress if currently Backlog
  if (issue.type === 'task' && targetCol === 'in-progress' && parentTicket) {
    if (normaliseStatus(parentTicket.status) === 'backlog') {
      await applyStatus(parentTicket, 'in-progress')
    }
  }

  // Task → Done: check if all sibling tasks are now done → prompt for ticket
  if (issue.type === 'task' && targetCol === 'done' && parentTicket) {
    const tasks = tasksByTicket.value.get(parentTicket.id) ?? []
    const allDone = tasks.every(t => t.id === id ? true : normaliseStatus(t.status) === 'done')
    if (allDone && normaliseStatus(parentTicket.status) !== 'done') {
      if (await confirm({ message: 'All tasks are done. Mark ticket as Done?', confirmLabel: 'Mark Done' })) {
        await applyStatus(parentTicket, 'done')
      }
    }
  }
}

async function applyTicketStatus(ticket: Issue, col: ColKey) {
  if (normaliseStatus(ticket.status) === col) return
  // Same guards as drag: confirm if moving to Done with incomplete tasks
  if (col === 'done') {
    const tasks = tasksByTicket.value.get(ticket.id) ?? []
    const incomplete = tasks.filter(t => normaliseStatus(t.status) !== 'done')
    if (incomplete.length > 0) {
      if (!await confirm({ message: `${incomplete.length} task${incomplete.length !== 1 ? 's' : ''} not yet done. Mark ticket as Done anyway?`, confirmLabel: 'Mark Done' })) return
    }
  }
  await applyStatus(ticket, col)
  // Cascade cancelled
  if (col === 'cancelled') {
    const tasks = tasksByTicket.value.get(ticket.id) ?? []
    for (const task of tasks) {
      const tcol = normaliseStatus(task.status)
      if (tcol === 'backlog' || tcol === 'in-progress') await applyStatus(task, 'cancelled')
    }
  }
}

async function applyStatus(issue: Issue, col: ColKey) {
  const newStatus = canonicalStatus(col)
  try {
    const updated = await api.put<Issue>(`/issues/${issue.id}`, { status: newStatus })
    const idx = boardIssues.value.findIndex(i => i.id === issue.id)
    if (idx >= 0) boardIssues.value[idx] = updated
  } catch (e: unknown) {
    alert(`Failed to update ${issue.issue_key}: ${errMsg(e)}`)
  }
}

// ── Helpers ───────────────────────────────────────────────────────────────────
function userInitials(userId: number | null): string {
  if (!userId) return '?'
  const u = allUsers.value.find(u => u.id === userId)
  return u ? u.username.slice(0, 2).toUpperCase() : '?'
}

// ── Inline assignee picker ────────────────────────────────────────────────────
const assigneePickerIssueId = ref<number | null>(null)
const statusPickerIssueId   = ref<number | null>(null)

function toggleAssigneePicker(issueId: number, event: MouseEvent) {
  event.stopPropagation()
  event.preventDefault()
  statusPickerIssueId.value = null
  assigneePickerIssueId.value = assigneePickerIssueId.value === issueId ? null : issueId
}

function toggleStatusPicker(issueId: number, event: MouseEvent) {
  event.stopPropagation()
  event.preventDefault()
  assigneePickerIssueId.value = null
  statusPickerIssueId.value = statusPickerIssueId.value === issueId ? null : issueId
}

async function selectTicketStatus(ticket: Issue, col: ColKey) {
  statusPickerIssueId.value = null
  await applyTicketStatus(ticket, col)
}

async function assignUser(issueId: number, userId: number | null) {
  assigneePickerIssueId.value = null
  try {
    const updated = await api.put<Issue>(`/issues/${issueId}`, { assignee_id: userId })
    const idx = boardIssues.value.findIndex(i => i.id === issueId)
    if (idx >= 0) boardIssues.value[idx] = updated
  } catch (e: unknown) {
    alert(`Failed to assign: ${errMsg(e)}`)
  }
}

function userName(userId: number | null): string {
  if (!userId) return 'Unassigned'
  const u = allUsers.value.find(u => u.id === userId)
  return u?.username ?? '?'
}

function sprintLabel(s: Sprint): string {
  const state = s.sprint_state ? ` [${s.sprint_state}]` : ''
  if (s.start_date && s.end_date) return `${s.title}${state} (${s.start_date.slice(0,10)} – ${s.end_date.slice(0,10)})`
  return `${s.title}${state}`
}
</script>

<template>
  <div>
    <Teleport defer to="#app-header-left">
      <span class="ah-title">Sprint Board</span>
    </Teleport>

    <div v-if="loading" class="sb-loading">Loading…</div>
    <div v-else-if="error" class="sb-error">{{ error }}</div>

    <template v-else-if="!sortedSprints.length">
      <div class="sb-empty-state">
        <AppIcon name="layout-grid" :size="40" class="sb-empty-icon" />
        <h2 class="sb-empty-title">No sprints configured</h2>
        <p class="sb-empty-desc">Create sprints in Settings to start using the sprint board.</p>
        <RouterLink to="/settings?tab=sprints" class="btn btn-primary btn-sm">Go to Sprint Settings</RouterLink>
      </div>
    </template>

    <template v-else>
      <!-- Sprint selector bar -->
      <div class="sb-selector">
        <button class="sb-nav-btn" :disabled="!prevSprint" @click="prevSprint && selectSprint(prevSprint)" title="Previous sprint">
          <AppIcon name="chevron-left" :size="16" />
        </button>

        <div class="sb-sprint-info" v-if="activeSprint">
          <span class="sb-sprint-name">{{ activeSprint.title }}</span>
          <span v-if="activeSprint.sprint_state" :class="['sb-sprint-state', `sb-state--${activeSprint.sprint_state}`]">
            {{ activeSprint.sprint_state }}
          </span>
          <span v-if="activeSprint.start_date" class="sb-sprint-dates">
            {{ activeSprint.start_date.slice(0,10) }} – {{ activeSprint.end_date?.slice(0,10) ?? '?' }}
          </span>
        </div>

        <button class="sb-nav-btn" :disabled="!nextSprint" @click="nextSprint && selectSprint(nextSprint)" title="Next sprint">
          <AppIcon name="chevron-right" :size="16" />
        </button>

        <!-- Sprint quick-jump dropdown -->
        <select class="sb-sprint-select" :value="activeSprint?.id ?? ''" @change="(e) => { const s = sortedSprints.find(x => x.id === Number((e.target as HTMLSelectElement).value)); if (s) selectSprint(s) }">
          <option v-for="s in sortedSprints" :key="s.id" :value="s.id">{{ sprintLabel(s) }}</option>
        </select>

        <button v-if="nextSprint && incompleteIssues.length" class="btn btn-ghost btn-sm sb-move-btn" @click="showMoveConfirm = true" title="Move incomplete items to next sprint">
          <AppIcon name="arrow-right" :size="13" /> Move incomplete
        </button>
      </div>

      <!-- Tabs -->
      <div class="sb-tabs">
        <button :class="['sb-tab', { active: activeTab === 'planning' }]" @click="activeTab = 'planning'">Planning</button>
        <button :class="['sb-tab', { active: activeTab === 'reporting' }]" @click="activeTab = 'reporting'">Reporting</button>
        <span class="sb-tab-spacer" />
        <button v-if="activeTab === 'planning' && boardTickets.length" class="sb-tree-btn" @click="expandAll" title="Expand all">
          <AppIcon name="chevrons-down" :size="14" />
        </button>
        <button v-if="activeTab === 'planning' && boardTickets.length" class="sb-tree-btn" @click="collapseAll" title="Collapse all">
          <AppIcon name="chevrons-up" :size="14" />
        </button>
      </div>

      <!-- Move incomplete confirmation dialog -->
      <div v-if="showMoveConfirm" class="sb-move-dialog-backdrop" @click="showMoveConfirm = false">
        <div class="sb-move-dialog" @click.stop>
          <p style="font-size:14px;margin:0 0 .75rem"><strong>{{ incompleteIssues.length }}</strong> incomplete issue{{ incompleteIssues.length !== 1 ? 's' : '' }} will be moved to <strong>{{ nextSprint?.title }}</strong>:</p>
          <ul style="font-size:13px;margin:0 0 1rem;padding-left:1.25rem;max-height:200px;overflow-y:auto">
            <li v-for="i in incompleteIssues" :key="i.id">{{ i.issue_key }} — {{ i.title }}</li>
          </ul>
          <div style="display:flex;gap:.5rem;justify-content:flex-end">
            <button class="btn btn-ghost btn-sm" @click="showMoveConfirm = false"><u>C</u>ancel</button>
            <button class="btn btn-primary btn-sm" :disabled="moving" @click="moveIncomplete"><template v-if="moving">Moving…</template><template v-else><u>M</u>ove</template></button>
          </div>
        </div>
      </div>

      <div v-if="boardLoading" class="sb-loading">Loading board…</div>

      <!-- Reporting tab -->
      <template v-else-if="activeTab === 'reporting'">
        <div v-if="reportMetrics" class="sb-report">
          <div class="sb-report-cards">
            <div class="sb-report-card">
              <span class="sb-report-label"><AppIcon name="check-circle" :size="11" class="sb-card-icon" /> Completion</span>
              <span class="sb-report-value">{{ reportMetrics.completionPct }}%</span>
              <span class="sb-report-sub">{{ reportMetrics.completedCount }}/{{ reportMetrics.totalCount }} tickets</span>
            </div>
            <div class="sb-report-card">
              <span class="sb-report-label"><AppIcon name="crosshair" :size="11" class="sb-card-icon" /> Est H</span>
              <span class="sb-report-value">{{ fmtNum(reportMetrics.estHours) }}h</span>
            </div>
            <div class="sb-report-card">
              <span class="sb-report-label"><AppIcon name="crosshair" :size="11" class="sb-card-icon" /> Est LP</span>
              <span class="sb-report-value">{{ fmtNum(reportMetrics.estLp) }}</span>
            </div>
            <div class="sb-report-card">
              <span class="sb-report-label"><AppIcon name="clock" :size="11" class="sb-card-icon" /> Booked H</span>
              <span class="sb-report-value">{{ fmtNum(reportMetrics.bookedHours) }}h</span>
            </div>
            <div class="sb-report-card">
              <span class="sb-report-label"><AppIcon name="file-text" :size="11" class="sb-card-icon" /> AR H</span>
              <span class="sb-report-value">{{ fmtNum(reportMetrics.arHours) }}h</span>
            </div>
            <div class="sb-report-card">
              <span class="sb-report-label"><AppIcon name="file-text" :size="11" class="sb-card-icon" /> AR LP</span>
              <span class="sb-report-value">{{ fmtNum(reportMetrics.arLp) }}</span>
            </div>
            <div class="sb-report-card">
              <span class="sb-report-label"><AppIcon name="euro" :size="11" class="sb-card-icon" /> AR EUR</span>
              <span class="sb-report-value">{{ fmtEur(reportMetrics.arEur) }}</span>
            </div>
          </div>

          <!-- Breakdown table -->
          <div v-if="reportBreakdown.length" class="sb-breakdown-wrap">
          <table class="sb-breakdown">
            <thead>
              <tr>
                <th>Key</th>
                <th>Title</th>
                <th>Status</th>
                <th class="num">Est H</th>
                <th class="num">Est LP</th>
                <th class="num">Booked H</th>
                <th class="num">AR H</th>
                <th class="num">AR LP</th>
                <th class="num">AR EUR</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="row in reportBreakdown" :key="row.id" :class="{ 'sb-bd-completed': row.completed, 'sb-bd-active-panel': reportPanelIssueId === row.id }" class="sb-bd-row" @click="openReportPanel(row.id)">
                <td class="sb-bd-key">{{ row.key }}</td>
                <td class="sb-bd-title">{{ row.title }}</td>
                <td>
                  <span :class="['sb-bd-status', `sb-bd-status--${row.status}`]">{{ row.status }}</span>
                  <span v-if="boardIssues.find(i => i.id === row.id)?.tags?.some(t => t.system && t.name === 'At Risk')" class="sb-at-risk-badge" title="At Risk">⚠</span>
                </td>
                <td class="num">{{ row.estHours ? fmtNum(row.estHours) : '—' }}</td>
                <td class="num">{{ row.estLp ? fmtNum(row.estLp) : '—' }}</td>
                <td class="num">
                  <span v-if="row.bookedOverEst" class="sb-warn-bang" title="Booked exceeds estimate">!</span>
                  {{ row.bookedHours ? fmtNum(row.bookedHours) : '—' }}
                </td>
                <td class="num">{{ row.arHours ? fmtNum(row.arHours) : '—' }}</td>
                <td class="num">{{ row.arLp ? fmtNum(row.arLp) : '—' }}</td>
                <td class="num">{{ row.eur ? fmtEur(row.eur) : '—' }}</td>
              </tr>
            </tbody>
            <tfoot>
              <tr class="sb-foot-all">
                <td colspan="3"><strong>Total (all)</strong></td>
                <td class="num"><strong>{{ fmtNum(reportTotalsAll.estHours) }}</strong></td>
                <td class="num"><strong>{{ fmtNum(reportTotalsAll.estLp) }}</strong></td>
                <td class="num"><strong>{{ fmtNum(reportTotalsAll.bookedHours) }}</strong></td>
                <td class="num"><strong>{{ fmtNum(reportTotalsAll.arHours) }}</strong></td>
                <td class="num"><strong>{{ fmtNum(reportTotalsAll.arLp) }}</strong></td>
                <td class="num"><strong>{{ fmtEur(reportTotalsAll.arEur) }}</strong></td>
              </tr>
              <tr class="sb-foot-completed">
                <td colspan="3"><strong>Total (completed / accepted)</strong></td>
                <td class="num"><strong>{{ fmtNum(reportMetrics.estHours) }}</strong></td>
                <td class="num"><strong>{{ fmtNum(reportMetrics.estLp) }}</strong></td>
                <td class="num"><strong>{{ fmtNum(reportMetrics.bookedHours) }}</strong></td>
                <td class="num"><strong>{{ fmtNum(reportMetrics.arHours) }}</strong></td>
                <td class="num"><strong>{{ fmtNum(reportMetrics.arLp) }}</strong></td>
                <td class="num"><strong>{{ fmtEur(reportMetrics.arEur) }}</strong></td>
              </tr>
            </tfoot>
          </table>
          </div>
        </div>

        <!-- Side panel for reporting row clicks -->
        <IssueSidePanel
          v-if="reportPanelIssueId"
          :issue-id="reportPanelIssueId"
          :users="allUsers"
          :readonly="false"
          @close="closeReportPanel"
        />
      </template>

      <!-- Planning tab -->
      <template v-else-if="!boardTickets.length">
        <div class="sb-empty">
          No tickets assigned to this sprint yet.
          <span class="sb-empty-hint">Assign tickets in the issue detail view or via bulk change.</span>
        </div>
      </template>

      <!-- Board: list of tickets, each expandable into kanban -->
      <div v-else class="sb-board">
        <div v-for="ticket in boardTickets" :key="ticket.id" class="sb-ticket-block">

          <!-- Ticket row header -->
          <div
            class="sb-ticket-row"
            :class="[`sb-ticket--${normaliseStatus(ticket.status)}`, { 'sb-ticket-row--drop-above': ticketDropTargetId === ticket.id }]"
            :draggable="true"
            @dragstart="onDragStart(ticket, $event)"
            @dragover.prevent="onTicketRowDragOver(ticket.id)"
            @dragleave="ticketDropTargetId = null"
            @drop.prevent="onTicketRowDrop(ticket)"
          >
            <button class="sb-expand-btn" @click="toggleExpand(ticket.id)" :title="expandedTicketIds.has(ticket.id) ? 'Collapse' : 'Expand'">
              <AppIcon :name="expandedTicketIds.has(ticket.id) ? 'chevron-down' : 'chevron-right'" :size="14" />
            </button>
            <RouterLink :to="`/projects/${ticket.project_id}/issues/${ticket.id}`" class="sb-issue-key" @click.stop>
              {{ ticket.issue_key }}
            </RouterLink>
            <span class="sb-ticket-title">{{ ticket.title }}</span>
            <div class="sb-status-wrapper">
              <span
                class="sb-status-badge sb-status-badge--clickable"
                :class="`sb-status--${normaliseStatus(ticket.status)}`"
                @click.stop="toggleStatusPicker(ticket.id, $event)"
                title="Change status"
              >
                {{ normaliseStatus(ticket.status) === 'in-progress' ? 'In Progress' : (normaliseStatus(ticket.status).charAt(0).toUpperCase() + normaliseStatus(ticket.status).slice(1)) }}
                <AppIcon name="chevron-down" :size="9" />
              </span>
              <div v-if="statusPickerIssueId === ticket.id" class="sb-status-dropdown" @click.stop>
                <button
                  v-for="col in COLUMNS" :key="col.key"
                  class="sb-status-opt"
                  :class="{ 'sb-status-opt--active': normaliseStatus(ticket.status) === col.key }"
                  @click="selectTicketStatus(ticket, col.key)"
                >
                  <span class="sb-status-dot" :style="{ background: col.color }"></span>
                  {{ col.label }}
                </button>
              </div>
            </div>
            <span v-if="ticket.tags?.some(t => t.system && t.name === 'At Risk')" class="sb-at-risk-badge" title="At Risk: booked hours near estimate">⚠</span>
            <span class="sb-task-count" v-if="tasksByTicket.get(ticket.id)?.length">
              {{ tasksByTicket.get(ticket.id)!.length }} task{{ tasksByTicket.get(ticket.id)!.length !== 1 ? 's' : '' }}
            </span>
            <span class="sb-avatar sb-avatar--clickable" :class="{ 'sb-avatar--empty': !ticket.assignee_id }" :title="userName(ticket.assignee_id)" @click="toggleAssigneePicker(ticket.id, $event)">
              <UserAvatar :user="allUsers.find(u => u.id === ticket.assignee_id) ?? null" size="sm" :show-tooltip="false" />
            </span>
            <div v-if="assigneePickerIssueId === ticket.id" class="sb-assignee-dropdown" @click.stop>
              <button class="sb-assignee-opt" @click="assignUser(ticket.id, null)">
                <span class="sb-assignee-opt-name">Unassigned</span>
              </button>
              <button v-for="u in allUsers" :key="u.id" class="sb-assignee-opt" :class="{ 'sb-assignee-opt--active': ticket.assignee_id === u.id }" @click="assignUser(ticket.id, u.id)">
                <span class="sb-assignee-opt-initials">{{ u.username.slice(0,2).toUpperCase() }}</span>
                <span class="sb-assignee-opt-name">{{ u.username }}</span>
              </button>
            </div>
          </div>

          <!-- Kanban columns — visible when expanded -->
          <div v-if="expandedTicketIds.has(ticket.id)" class="sb-kanban">
            <div
              v-for="col in COLUMNS" :key="col.key"
              class="sb-col"
              :class="{ 'sb-col--dragover': dragOverCol === `${ticket.id}-${col.key}` }"
              @dragover.prevent="onDragOver(`${ticket.id}-${col.key}`)"
              @dragleave="onDragLeave"
              @drop.prevent="onDrop(col.key, ticket)"
            >
              <div class="sb-col-header" :style="{ borderColor: col.color }">
                <span class="sb-col-title" :style="{ color: col.color }">{{ col.label }}</span>
                <span class="sb-col-count">{{ issuesInCol(tasksByTicket.get(ticket.id) ?? [], col.key).length }}</span>
              </div>

              <!-- Task cards -->
              <div
                v-for="task in issuesInCol(tasksByTicket.get(ticket.id) ?? [], col.key)"
                :key="task.id"
                class="sb-card"
                :class="{ 'sb-card--dragging': draggingId === task.id, 'sb-card--drop-target': dropTargetId === task.id }"
                draggable="true"
                @dragstart="onDragStart(task, $event)"
                @dragover.prevent="onCardDragOver(task.id)"
              >
                <div class="sb-card-top">
                  <RouterLink :to="`/projects/${task.project_id}/issues/${task.id}`" class="sb-card-key">
                    {{ task.issue_key }}
                  </RouterLink>
                  <span class="sb-card-avatar sb-card-avatar--clickable" :class="{ 'sb-card-avatar--empty': !task.assignee_id }" :title="userName(task.assignee_id)" @click.stop="toggleAssigneePicker(task.id, $event)">
                    <UserAvatar :user="allUsers.find(u => u.id === task.assignee_id) ?? null" size="sm" :show-tooltip="false" />
                  </span>
                </div>
                <span class="sb-card-title">{{ task.title }}</span>
                <div v-if="assigneePickerIssueId === task.id" class="sb-assignee-dropdown sb-assignee-dropdown--card" @click.stop>
                  <button class="sb-assignee-opt" @click="assignUser(task.id, null)">
                    <span class="sb-assignee-opt-name">Unassigned</span>
                  </button>
                  <button v-for="u in allUsers" :key="u.id" class="sb-assignee-opt" :class="{ 'sb-assignee-opt--active': task.assignee_id === u.id }" @click="assignUser(task.id, u.id)">
                    <span class="sb-assignee-opt-initials">{{ u.username.slice(0,2).toUpperCase() }}</span>
                    <span class="sb-assignee-opt-name">{{ u.username }}</span>
                  </button>
                </div>
              </div>

              <div v-if="!issuesInCol(tasksByTicket.get(ticket.id) ?? [], col.key).length" class="sb-col-empty">
                Drop here
              </div>
            </div>

          </div>
        </div>
      </div>
    </template>

    <AppFooter />
  </div>
</template>

<style scoped>
.sb-loading, .sb-error {
  color: var(--text-muted); padding: 2rem 0; font-size: 13px;
}
.sb-error { color: #c0392b; }
.sb-empty-state {
  display: flex; flex-direction: column; align-items: center; justify-content: center;
  gap: .75rem; padding: 4rem 2rem; text-align: center;
}
.sb-empty-icon { color: var(--text-muted); opacity: .3; }
.sb-empty-title { font-size: 16px; font-weight: 700; color: var(--text); }
.sb-empty-desc { font-size: 13px; color: var(--text-muted); max-width: 300px; }

/* ── Sprint selector ─────────────────────────────────────────────────────── */
.sb-selector {
  display: flex; align-items: center; gap: .75rem;
  background: var(--bg-card); border: 1px solid var(--border);
  border-radius: 8px; padding: .65rem 1rem;
  margin-bottom: .5rem; box-shadow: var(--shadow);
}
.sb-move-btn { margin-left: auto; white-space: nowrap; }

/* Tabs */
.sb-tabs {
  display: flex; gap: 0; margin-bottom: 1rem;
  border-bottom: 2px solid var(--border);
}
.sb-tab {
  background: none; border: none; cursor: pointer;
  padding: .4rem .85rem; font-size: 13px; font-weight: 500;
  color: var(--text-muted); border-bottom: 2px solid transparent;
  margin-bottom: -2px; transition: color .15s, border-color .15s;
}
.sb-tab:hover { color: var(--text); }
.sb-tab.active { color: var(--bp-blue); border-bottom-color: var(--bp-blue); font-weight: 600; }
.sb-tab-spacer { flex: 1; }
.sb-tree-btn {
  background: none; border: none; cursor: pointer; padding: .3rem .4rem;
  color: var(--text-muted); border-radius: 4px; display: flex; align-items: center;
  transition: color .1s;
}
.sb-tree-btn:hover { color: var(--text); }

/* Move dialog */
.sb-move-dialog-backdrop {
  position: fixed; inset: 0; z-index: 9999;
  background: rgba(0,0,0,.35); display: flex; align-items: center; justify-content: center;
}
.sb-move-dialog {
  background: var(--bg-card); border: 1px solid var(--border); border-radius: 8px;
  padding: 1.25rem 1.5rem; min-width: 380px; max-width: 500px;
  box-shadow: 0 8px 32px rgba(0,0,0,.2);
}

/* Reporting */
.sb-report { padding: .5rem 0; }
.sb-report-cards {
  display: grid; grid-template-columns: repeat(auto-fill, minmax(160px, 1fr));
  gap: .75rem;
}
.sb-report-card {
  background: var(--bg-card); border: 1px solid var(--border);
  border-radius: 8px; padding: 1rem; display: flex; flex-direction: column; gap: .2rem;
}
.sb-report-label { font-size: 11px; font-weight: 600; text-transform: uppercase; letter-spacing: .04em; color: var(--text-muted); display: flex; align-items: center; gap: .3rem; }
.sb-card-icon { opacity: .4; flex-shrink: 0; }
.sb-report-value { font-size: 22px; font-weight: 700; color: var(--text); }
.sb-report-sub { font-size: 12px; color: var(--text-muted); }

/* Breakdown table */
.sb-breakdown-wrap {
  margin-top: 1.25rem;
  max-height: calc(100vh - 280px);
  overflow-y: auto;
  border: 1px solid var(--border);
  border-radius: var(--radius);
}
.sb-breakdown {
  width: 100%; border-collapse: collapse; font-size: 13px;
}
.sb-breakdown th {
  text-align: left; font-size: 11px; font-weight: 600; text-transform: uppercase;
  letter-spacing: .04em; color: var(--text-muted); padding: .4rem .5rem;
  border-bottom: 2px solid var(--border);
  position: sticky; top: 0; z-index: 1; background: var(--bg-card);
}
.sb-breakdown td {
  padding: .45rem .5rem; border-bottom: 1px solid var(--border); vertical-align: middle;
}
.sb-breakdown .num { text-align: right; font-variant-numeric: tabular-nums; }
.sb-breakdown tfoot td { border-top: 2px solid var(--border); border-bottom: none; padding-top: .55rem; position: sticky; bottom: 0; background: var(--bg-card); z-index: 1; }
.sb-foot-all td { color: var(--text-muted); }
.sb-foot-completed td { background: #f0fdf4 !important; }
.sb-bd-row { cursor: pointer; transition: background .1s; }
.sb-bd-row:hover { background: #f0f2f4; }
.sb-bd-active-panel { background: color-mix(in srgb, var(--bp-blue) 8%, var(--bg-card)); }
.sb-bd-key { font-family: monospace; font-weight: 700; color: var(--text-muted); white-space: nowrap; }
.sb-bd-title { max-width: 280px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.sb-bd-completed { }
.sb-bd-status {
  font-size: 11px; font-weight: 600; padding: .1rem .35rem; border-radius: 3px;
  white-space: nowrap;
}
.sb-bd-status--done, .sb-bd-status--accepted, .sb-bd-status--invoiced { background: #dcfce7; color: #166534; }
.sb-bd-status--in-progress, .sb-bd-status--qa { background: #fef3c7; color: #92400e; }
.sb-bd-status--backlog, .sb-bd-status--new { background: #f3f4f6; color: #6b7280; }
.sb-bd-status--cancelled { background: #fee2e2; color: #991b1b; }
.sb-at-risk-badge { color: #d97706; font-size: 12px; flex-shrink: 0; }
.sb-warn-bang { color: #d97706; font-weight: 800; margin-right: .2rem; }
.sb-nav-btn {
  background: none; border: 1px solid var(--border); border-radius: 6px;
  padding: .3rem .4rem; cursor: pointer; color: var(--text-muted);
  display: inline-flex; align-items: center;
  transition: border-color .1s, color .1s;
}
.sb-nav-btn:disabled { opacity: .3; cursor: not-allowed; }
.sb-nav-btn:not(:disabled):hover { border-color: var(--bp-blue); color: var(--bp-blue); }
.sb-sprint-info { display: flex; align-items: center; gap: .6rem; flex: 1; }
.sb-sprint-name { font-size: 15px; font-weight: 700; color: var(--text); }
.sb-sprint-state {
  font-size: 10px; font-weight: 700; text-transform: uppercase; letter-spacing: .05em;
  border-radius: 20px; padding: .1rem .45rem;
}
.sb-state--active   { background: #fff3e0; color: #b45309; }
.sb-state--planned  { background: #f3f4f6; color: #374151; }
.sb-state--complete { background: #dcfce7; color: #166534; }
.sb-state--archived { background: #e5e7eb; color: #6b7280; }
.sb-sprint-dates { font-size: 12px; color: var(--text-muted); }
.sb-no-sprint { font-size: 13px; color: var(--text-muted); flex: 1; }
.sb-sprint-select {
  border: 1px solid var(--border); border-radius: 6px;
  padding: .3rem .55rem; font-size: 12px; font-family: inherit;
  background: var(--bg); color: var(--text); outline: none; max-width: 260px;
}

/* ── Empty ───────────────────────────────────────────────────────────────── */
.sb-empty {
  display: flex; flex-direction: column; align-items: center; gap: .5rem;
  color: var(--text-muted); font-size: 13px; padding: 3rem 0; text-align: center;
}
.sb-empty-hint { font-size: 12px; opacity: .7; }

/* ── Board ───────────────────────────────────────────────────────────────── */
.sb-board { display: flex; flex-direction: column; gap: .75rem; }

/* Ticket block */
.sb-ticket-block {
  background: var(--bg-card); border: 1px solid var(--border);
  border-radius: 8px; box-shadow: var(--shadow);
}

/* Ticket header row */
.sb-ticket-row {
  display: flex; align-items: center; gap: .6rem;
  padding: .65rem 1rem; cursor: grab;
  border-left: 3px solid transparent;
  transition: background .1s; position: relative;
  border-radius: 7px 7px 0 0;
}
.sb-ticket-row:last-child { border-radius: 7px; }
.sb-ticket-row:hover { background: #f0f2f4; }
.sb-ticket-row--drop-above::before {
  content: ''; position: absolute; top: -2px; left: 0; right: 0;
  height: 3px; background: var(--color-primary, #2563eb); border-radius: 2px;
  pointer-events: none;
}
.sb-ticket--backlog     { border-left-color: #6b7280; }
.sb-ticket--in-progress { border-left-color: #d97706; }
.sb-ticket--done        { border-left-color: #059669; }
.sb-ticket--cancelled   { border-left-color: #dc2626; }

.sb-expand-btn {
  background: none; border: none; padding: .1rem; cursor: pointer;
  color: var(--text-muted); display: inline-flex; align-items: center;
  border-radius: 4px; flex-shrink: 0;
  transition: color .1s, background .1s;
}
.sb-expand-btn:hover { background: var(--border); color: var(--text); }
.sb-issue-key {
  font-size: 11px; font-weight: 700; font-family: monospace;
  color: var(--bp-blue); white-space: nowrap; flex-shrink: 0; text-decoration: none;
}
.sb-issue-key:hover { text-decoration: underline; }
.sb-ticket-title { font-size: 13px; font-weight: 500; color: var(--text); flex: 1; }
.sb-status-badge {
  font-size: 11px; font-weight: 600; padding: .1rem .5rem; border-radius: 20px;
  flex-shrink: 0; white-space: nowrap;
}
.sb-status--backlog     { background: #f3f4f6; color: #374151; }
.sb-status--in-progress { background: #fff3e0; color: #b45309; }
.sb-status--done        { background: #dcfce7; color: #166534; }
.sb-status--cancelled   { background: #fee2e2; color: #991b1b; }

.sb-avatar {
  width: 24px; height: 24px; border-radius: 50%;
  background: var(--bp-blue-pale); color: var(--bp-blue-dark);
  font-size: 10px; font-weight: 700;
  display: inline-flex; align-items: center; justify-content: center;
  flex-shrink: 0; position: relative;
}
.sb-avatar--clickable { cursor: pointer; transition: box-shadow .1s; }
.sb-avatar--clickable:hover { box-shadow: 0 0 0 2px var(--bp-blue); }
.sb-avatar--empty { background: var(--border); color: var(--text-muted); border: 1px dashed var(--text-muted); }
.sb-task-count { font-size: 11px; color: var(--text-muted); flex-shrink: 0; }

/* UserAvatar inside sb-avatar/sb-card-avatar: fill the parent circle */
.sb-avatar .ua,
.sb-card-avatar .ua { width: 100%; height: 100%; font-size: inherit; }

/* Assignee picker dropdown */
.sb-assignee-dropdown {
  position: absolute; top: calc(100% + 4px); right: 0; z-index: 50;
  background: var(--bg-card); border: 1px solid var(--border);
  border-radius: 8px; box-shadow: var(--shadow-md);
  width: 160px; max-height: 240px; overflow-y: auto;
  display: flex; flex-direction: column;
}
.sb-assignee-dropdown--card { right: auto; left: 0; }
.sb-assignee-opt {
  display: flex; align-items: center; gap: .4rem;
  padding: .35rem .6rem; font-size: 12px; font-family: inherit;
  background: none; border: none; cursor: pointer; color: var(--text);
  text-align: left; transition: background .1s;
}
.sb-assignee-opt:hover { background: #f0f2f4; }
.sb-assignee-opt--active { font-weight: 700; color: var(--bp-blue); }
.sb-assignee-opt-initials {
  width: 20px; height: 20px; border-radius: 50%;
  background: var(--bp-blue-pale); color: var(--bp-blue-dark);
  font-size: 9px; font-weight: 700; flex-shrink: 0;
  display: inline-flex; align-items: center; justify-content: center;
}
.sb-assignee-opt-name { flex: 1; }

/* ── Kanban ──────────────────────────────────────────────────────────────── */
.sb-kanban {
  display: flex; flex-direction: column; gap: 0;
  border-top: 1px solid var(--border);
  border-radius: 0 0 7px 7px; overflow: visible;
}

.sb-kanban > .sb-col:not(:last-child) { border-bottom: 1px solid var(--border); }

/* On wider screens, lay columns side-by-side */
@media (min-width: 900px) {
  .sb-kanban {
    flex-direction: row; align-items: stretch;
  }
  .sb-kanban > .sb-col { flex: 1; border-bottom: none !important; border-right: 1px solid var(--border); }
  .sb-kanban > .sb-col:last-child { border-right: none; }
}

.sb-col {
  padding: .65rem .85rem; min-height: 80px;
  transition: background .15s;
}
.sb-col--dragover { background: var(--bp-blue-pale); }

.sb-col-header {
  display: flex; align-items: center; justify-content: space-between;
  margin-bottom: .5rem; padding-bottom: .35rem;
  border-bottom: 2px solid;
}
.sb-col-title { font-size: 11px; font-weight: 700; text-transform: uppercase; letter-spacing: .06em; }
.sb-col-count {
  font-size: 11px; font-weight: 700; color: #fff;
  background: var(--text-muted); border-radius: 20px;
  min-width: 18px; height: 18px; padding: 0 5px;
  display: inline-flex; align-items: center; justify-content: center;
}
.sb-col-empty {
  font-size: 12px; color: var(--text-muted); opacity: .45;
  text-align: center; padding: .75rem 0; font-style: italic;
}

/* Task cards */
.sb-card {
  display: flex; flex-direction: column; gap: .25rem;
  background: var(--bg-card); border: 1px solid var(--border);
  border-radius: 6px; padding: .45rem .6rem;
  margin-bottom: .4rem; cursor: grab; box-shadow: var(--shadow);
  transition: box-shadow .1s, opacity .1s; position: relative;
}
.sb-card:hover { box-shadow: var(--shadow-md); }
.sb-card--dragging { opacity: .4; }
.sb-card--drop-target { border-top: 2px solid var(--bp-blue); margin-top: -2px; }
.sb-card { cursor: grab; }
.sb-card:active { cursor: grabbing; }
.sb-card-top { display: flex; align-items: center; justify-content: space-between; gap: .3rem; }
.sb-card-key {
  font-size: 10px; font-weight: 700; font-family: monospace;
  color: var(--bp-blue); flex-shrink: 0; text-decoration: none; white-space: nowrap;
}
.sb-card-key:hover { text-decoration: underline; }
.sb-card-title { font-size: 12px; color: var(--text); line-height: 1.4; }
.sb-card-avatar {
  width: 20px; height: 20px; border-radius: 50%;
  background: var(--bp-blue-pale); color: var(--bp-blue-dark);
  font-size: 9px; font-weight: 700; flex-shrink: 0;
  display: inline-flex; align-items: center; justify-content: center;
  position: relative;
}
.sb-card-avatar--clickable { cursor: pointer; transition: box-shadow .1s; }
.sb-card-avatar--clickable:hover { box-shadow: 0 0 0 2px var(--bp-blue); }
.sb-card-avatar--empty { background: var(--border); color: var(--text-muted); border: 1px dashed var(--text-muted); }

/* Status badge — clickable dropdown */
.sb-status-wrapper { position: relative; flex-shrink: 0; }
.sb-status-badge--clickable {
  cursor: pointer; user-select: none;
  display: inline-flex; align-items: center; gap: .2rem;
  transition: opacity .1s;
}
.sb-status-badge--clickable:hover { opacity: .8; }
.sb-status-dropdown {
  position: absolute; top: calc(100% + 4px); left: 0; z-index: 100;
  background: var(--bg-card); border: 1px solid var(--border);
  border-radius: 8px; box-shadow: var(--shadow-md);
  min-width: 140px; display: flex; flex-direction: column; overflow: hidden;
}
.sb-status-opt {
  display: flex; align-items: center; gap: .5rem;
  padding: .35rem .65rem; font-size: 12px; font-family: inherit;
  background: none; border: none; cursor: pointer; color: var(--text);
  text-align: left; transition: background .1s; white-space: nowrap;
}
.sb-status-opt:hover { background: #f0f2f4; }
.sb-status-opt--active { font-weight: 700; color: var(--bp-blue); }
.sb-status-dot { width: 8px; height: 8px; border-radius: 50%; flex-shrink: 0; }
</style>
