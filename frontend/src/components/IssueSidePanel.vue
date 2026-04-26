<script setup lang="ts">
import { ref, watch, computed, nextTick, onUnmounted } from 'vue'
import { useRouter } from 'vue-router'
import AppIcon from '@/components/AppIcon.vue'
import StatusDot from '@/components/StatusDot.vue'
import NumericInput from '@/components/NumericInput.vue'
import MetaSelect from '@/components/MetaSelect.vue'
import type { MetaOption } from '@/components/MetaSelect.vue'
import MarkdownToolbar from '@/components/MarkdownToolbar.vue'
import AutocompleteInput from '@/components/AutocompleteInput.vue'
import TagSelector from '@/components/TagSelector.vue'
import TagChip from '@/components/TagChip.vue'
import SprintChips from '@/components/issue/SprintChips.vue'
import AttachmentSidebar from '@/components/issue/AttachmentSidebar.vue'
import { api, errMsg } from '@/api/client'
import { attachmentsEnabled } from '@/api/instance'
import type { Issue, User, Tag, Sprint, TimeEntry, Attachment } from '@/types'
import { useAuthStore } from '@/stores/auth'
import { useTimerStore } from '@/stores/timer'
import { useIssueDisplay, STATUS_DOT_STYLE, STATUS_LABEL, PRIORITY_LABEL, PRIORITY_COLOR, TYPE_SVGS } from '@/composables/useIssueDisplay'
import { useMarkdown } from '@/composables/useMarkdown'
import { useTimeUnit } from '@/composables/useTimeUnit'
import { useDirtyGuard } from '@/composables/useDirtyGuard'
import { useSearchStore } from '@/stores/search'
import { highlightDom } from '@/composables/useHighlight'
import { formatDuration } from '@/composables/useDurationInput'
import { useConfirm } from '@/composables/useConfirm'
import { useIssueContext } from '@/composables/useIssueContext'
import { useAttachmentUploads } from '@/composables/useAttachmentUploads'
import {
  useSidePanelWidth,
  resetSidePanelWidth,
  SIDE_PANEL_DEFAULT_WIDTH,
  SIDE_PANEL_MIN_WIDTH,
  SIDE_PANEL_MAX_WIDTH_RATIO,
} from '@/composables/useSidePanelWidth'
// PAI-146: AI text optimization on multiline editors. Same composable
// + overlay singleton as the detail view; once mounted here, the side
// panel surfaces the AI action for description / acceptance / notes.
import AiActionMenu from '@/components/ai/AiActionMenu.vue'
import AiOptimizeOverlay from '@/components/ai/AiOptimizeOverlay.vue'
import AiOptimizeBanner from '@/components/ai/AiOptimizeBanner.vue'
import { useAiOptimize } from '@/composables/useAiOptimize'

const ctx = useIssueContext(true)

const props = defineProps<{
  issueId: number | null
  users?: User[]
  allTags?: Tag[]
  costUnits?: string[]
  releases?: string[]
  sprints?: Sprint[]
  issueIds?: number[]       // ordered list of visible issue IDs for prev/next
  startInEdit?: boolean
  pinned?: boolean
  readonly?: boolean         // no edit controls, fields rendered as text/markdown (portal mode)
}>()

// Prefer context, fall back to props for backward compatibility
const users    = computed(() => props.users?.length ? props.users : ctx.users.value)
const allTags  = computed(() => props.allTags?.length ? props.allTags : ctx.allTags.value)
const costUnits = computed(() => props.costUnits?.length ? props.costUnits : ctx.costUnits.value)
const releases  = computed(() => props.releases?.length ? props.releases : ctx.releases.value)
const sprints   = computed(() => props.sprints?.length ? props.sprints : ctx.sprints.value)

const emit = defineEmits<{
  close: []
  updated: [issue: Issue]
  deleted: [id: number]
  navigate: [id: number]
  'update:pinned': [pinned: boolean]
}>()

const router = useRouter()
const { confirm } = useConfirm()
const authStore = useAuthStore()
const timerStore = useTimerStore()
const { showTypeIcon } = useIssueDisplay()
const { formatHours, label: timeLabel, toggle: toggleTimeUnit, unit: timeUnit, toDisplay, toHours } = useTimeUnit()

const issue = ref<Issue | null>(null)
const loading = ref(false)
const editing = ref(false)
const saving = ref(false)
const saveError = ref('')
const mdMode = ref(authStore.user?.markdown_default ?? false)

// Full edit form
const form = ref({
  title: '', description: '', acceptance_criteria: '', notes: '',
  status: '', priority: '', type: '',
  assignee_id: '' as string,
  parent_id: '' as string,
  cost_unit: '', release: '',
  estimate_hours: null as number | null,
  estimate_lp: null as number | null,
  ar_hours: null as number | null,
  ar_lp: null as number | null,
  time_override: null as number | null,
})

const STATUS_OPTIONS: MetaOption[] = [
  { value: 'new', label: 'New' }, { value: 'backlog', label: 'Backlog' },
  { value: 'in-progress', label: 'In Progress' }, { value: 'qa', label: 'QA' },
  { value: 'done', label: 'Done' }, { value: 'delivered', label: 'Delivered' },
  { value: 'accepted', label: 'Accepted' },
  { value: 'invoiced', label: 'Invoiced' }, { value: 'cancelled', label: 'Cancelled' },
]
const PRIORITY_OPTIONS: MetaOption[] = [
  { value: 'low', label: 'Low' }, { value: 'medium', label: 'Medium' }, { value: 'high', label: 'High' },
]
const assigneeOptions = computed<MetaOption[]>(() => [
  { value: '', label: 'Unassigned' },
  ...users.value.filter(u => u.role !== 'external').map(u => ({ value: String(u.id), label: u.username })),
])

// Attachments — scoped to the currently-loaded issue.
const attachments = useAttachmentUploads({
  endpoint: () => issue.value ? `/issues/${issue.value.id}/attachments` : '/attachments',
})

async function loadAttachments() {
  if (!issue.value) { attachments.reset(); return }
  try {
    const list = await api.get<Attachment[]>(`/issues/${issue.value.id}/attachments`)
    attachments.seedExisting(list)
  } catch {
    attachments.reset()
  }
}

// Parent picker for the quick-edit form.
// Hierarchy: ticket → parent must be epic; task → parent must be ticket.
// Fetched lazily on edit, scoped to the currently-displayed issue so
// switching issues never shows stale candidates from the previous project.
const parentCandidates = ref<Issue[]>([])
const parentCandidatesForIssueId = ref<number | null>(null)
async function loadParentCandidates() {
  if (!issue.value) return
  if (parentCandidatesForIssueId.value === issue.value.id) return
  const t = issue.value.type
  if (t !== 'ticket' && t !== 'task') {
    parentCandidates.value = []
    parentCandidatesForIssueId.value = issue.value.id
    return
  }
  const parentType = t === 'ticket' ? 'epic' : 'ticket'
  const fetchingForId = issue.value.id
  try {
    const list = await api.get<Issue[]>(`/projects/${issue.value.project_id}/issues?type=${parentType}`)
    // Guard against races: ignore the result if the user has since switched issues.
    if (issue.value?.id !== fetchingForId) return
    parentCandidates.value = list
    parentCandidatesForIssueId.value = fetchingForId
  } catch {
    if (issue.value?.id !== fetchingForId) return
    parentCandidates.value = []
    parentCandidatesForIssueId.value = fetchingForId
  }
}
const parentOptions = computed<MetaOption[]>(() => {
  if (!issue.value) return []
  const opts: MetaOption[] = [{ value: '', label: '— None —' }]
  for (const p of parentCandidates.value) {
    const truncated = p.title.length > 40 ? p.title.slice(0, 40) + '...' : p.title
    opts.push({ value: String(p.id), label: `${p.issue_key} — ${truncated}` })
  }
  return opts
})
const showParentPicker = computed(() =>
  issue.value?.type === 'ticket' || issue.value?.type === 'task',
)

// Markdown rendering for view mode
const descRef = computed(() => issue.value?.description ?? '')
const acRef = computed(() => issue.value?.acceptance_criteria ?? '')
const notesRef = computed(() => issue.value?.notes ?? '')
const { html: descHtml } = useMarkdown(descRef, mdMode)
const { html: acHtml } = useMarkdown(acRef, mdMode)
const { html: notesHtml } = useMarkdown(notesRef, mdMode)

// PAI-146: AI optimization. The form's id matches issue.value.id when
// editing an existing issue (this panel never opens without one), so
// pass it through for context assembly.
const aiOptimize = useAiOptimize()
function onAiAccept(field: 'description' | 'acceptance_criteria' | 'notes') {
  return (text: string) => { form.value[field] = text }
}
const search = useSearchStore()

// DOM-based search highlighting — applied after v-html renders, walks text nodes only
const descEl = ref<HTMLElement | null>(null)
const acEl = ref<HTMLElement | null>(null)
const notesEl = ref<HTMLElement | null>(null)

watch([() => search.query, descHtml, acHtml, notesHtml], () => {
  nextTick(() => {
    if (descEl.value) highlightDom(descEl.value, search.query)
    if (acEl.value) highlightDom(acEl.value, search.query)
    if (notesEl.value) highlightDom(notesEl.value, search.query)
  })
})

// Prev/next navigation
const currentIdx = computed(() => {
  if (!props.issueIds || !props.issueId) return -1
  return props.issueIds.indexOf(props.issueId)
})
const canPrev = computed(() => currentIdx.value > 0)
const canNext = computed(() => props.issueIds ? currentIdx.value < props.issueIds.length - 1 : false)
function goPrev() { if (canPrev.value && props.issueIds) guardAction(() => emit('navigate', props.issueIds![currentIdx.value - 1])) }
function goNext() { if (canNext.value && props.issueIds) guardAction(() => emit('navigate', props.issueIds![currentIdx.value + 1])) }

// Tag management
const issueTagIds = computed(() => issue.value?.tags?.map(t => t.id) ?? [])
async function addTag(tagId: number) {
  if (!issue.value) return
  await api.post(`/issues/${issue.value.id}/tags`, { tag_id: tagId })
  issue.value = await api.get<Issue>(`/issues/${issue.value.id}`)
  emit('updated', issue.value)
}
async function removeTag(tagId: number) {
  if (!issue.value) return
  await api.delete(`/issues/${issue.value.id}/tags/${tagId}`)
  issue.value = await api.get<Issue>(`/issues/${issue.value.id}`)
  emit('updated', issue.value)
}

watch(() => props.issueId, async (id) => {
  if (!id) { issue.value = null; editing.value = false; attachments.reset(); return }
  loading.value = true
  try {
    issue.value = await api.get<Issue>(`/issues/${id}`)
    resetForm()
    editing.value = !props.readonly && !!props.startInEdit
    loadAttachments()
  } catch { issue.value = null }
  finally { loading.value = false }
}, { immediate: true })

function resetForm() {
  if (!issue.value) return
  const i = issue.value
  form.value = {
    title: i.title, description: i.description,
    acceptance_criteria: i.acceptance_criteria, notes: i.notes,
    status: i.status, priority: i.priority, type: i.type,
    assignee_id: i.assignee_id != null ? String(i.assignee_id) : '',
    parent_id: i.parent_id != null ? String(i.parent_id) : '',
    cost_unit: i.cost_unit, release: i.release,
    estimate_hours: i.estimate_hours, estimate_lp: i.estimate_lp,
    ar_hours: i.ar_hours, ar_lp: i.ar_lp,
    time_override: i.time_override,
  }
}

const savedSnapshot = ref('')
function startEdit() {
  resetForm()
  savedSnapshot.value = JSON.stringify(form.value)
  editing.value = true
  loadParentCandidates()
}
function cancelEdit() { editing.value = false; resetForm(); resetDirty() }

const currentSnapshot = computed(() => editing.value ? JSON.stringify(form.value) : '')
const { isDirty, guardAction, reset: resetDirty } = useDirtyGuard(currentSnapshot, savedSnapshot)

async function save() {
  if (!issue.value) return
  saving.value = true; saveError.value = ''
  try {
    const payload = {
      ...form.value,
      assignee_id: form.value.assignee_id ? Number(form.value.assignee_id) : null,
      parent_id: form.value.parent_id ? Number(form.value.parent_id) : null,
    }
    const updated = await api.put<Issue>(`/issues/${issue.value.id}`, payload)
    issue.value = updated
    editing.value = false
    savedSnapshot.value = ''  // reset dirty guard
    emit('updated', updated)
  } catch (e: unknown) { saveError.value = errMsg(e, 'Save failed.') }
  finally { saving.value = false }
}

function openFull() {
  if (!issue.value) return
  const editParam = editing.value ? '?edit=1' : ''
  guardAction(() => {
    router.push(`/projects/${issue.value!.project_id}/issues/${issue.value!.id}${editParam}`)
    emit('close')
  })
}

const cloning = ref(false)
async function cloneIssue() {
  if (!issue.value || cloning.value) return
  cloning.value = true
  try {
    const clone = await api.post<Issue>(`/issues/${issue.value.id}/clone`, {})
    // Navigate to the cloned issue in full view for editing
    router.push(`/projects/${clone.project_id}/issues/${clone.id}?edit=1`)
    emit('close')
  } catch (e: unknown) {
    saveError.value = errMsg(e, 'Clone failed.')
  } finally {
    cloning.value = false
  }
}

function togglePin() { emit('update:pinned', !props.pinned) }

// Forward wheel events from the transparent full-viewport backdrop to whatever
// scroll container sits beneath, so the list stays scrollable while the panel
// is open in unpinned mode. Without this, wheel events land on the backdrop
// (position: fixed), the scroll chain walks the containing-block chain to the
// viewport (overflow: visible), and nothing scrolls. PAI-16.
function onBackdropWheel(e: WheelEvent) {
  if (e.ctrlKey) return  // let browser zoom work
  const backdrop = e.currentTarget as HTMLElement
  // Temporarily disable backdrop's hit-testing so elementFromPoint returns the
  // element visually underneath it. Restored immediately, synchronously.
  const prevPE = backdrop.style.pointerEvents
  backdrop.style.pointerEvents = 'none'
  const hit = document.elementFromPoint(e.clientX, e.clientY) as HTMLElement | null
  backdrop.style.pointerEvents = prevPE
  if (!hit) return
  let el: HTMLElement | null = hit
  while (el && el !== document.body) {
    const cs = getComputedStyle(el)
    const scrollableY = (cs.overflowY === 'auto' || cs.overflowY === 'scroll') && el.scrollHeight > el.clientHeight
    const scrollableX = (cs.overflowX === 'auto' || cs.overflowX === 'scroll') && el.scrollWidth > el.clientWidth
    if (scrollableY && e.deltaY) { el.scrollTop += e.deltaY; return }
    if (scrollableX && e.deltaX) { el.scrollLeft += e.deltaX; return }
    el = el.parentElement
  }
}

// Type icon SVG lookup
const typeIcon = computed(() => {
  if (!issue.value) return ''
  return TYPE_SVGS[issue.value.type] ?? ''
})

// ── Resizable sidebar ────────────────────────────────────────────────────────
// `width` is the committed (persisted, layout-affecting) value shared with
// IssueList via useSidePanelWidth. During an active drag we use a local
// `draftWidth` for smooth visual feedback so the IssueList offset doesn't
// reflow on every mousemove — the new value lands in `width` only at drag-end.
const { width } = useSidePanelWidth()
const draftWidth = ref(width.value)
const resizing = ref(false)

watch(width, v => { if (!resizing.value) draftWidth.value = v })

function onResizeStart(e: MouseEvent) {
  e.preventDefault()
  resizing.value = true
  const startX = e.clientX
  const startW = draftWidth.value
  const maxW = Math.round(window.innerWidth * SIDE_PANEL_MAX_WIDTH_RATIO)

  function onMove(ev: MouseEvent) {
    const delta = startX - ev.clientX // moving left = wider
    draftWidth.value = Math.min(maxW, Math.max(SIDE_PANEL_MIN_WIDTH, startW + delta))
  }
  function onUp() {
    resizing.value = false
    width.value = draftWidth.value
    document.removeEventListener('mousemove', onMove)
    document.removeEventListener('mouseup', onUp)
  }
  document.addEventListener('mousemove', onMove)
  document.addEventListener('mouseup', onUp)
}

function resetWidth() {
  resetSidePanelWidth()
  draftWidth.value = SIDE_PANEL_DEFAULT_WIDTH
}

// ── Time entries (view mode) ────────────────────────────────────────────────
const timeEntries = ref<TimeEntry[]>([])
const showTimeEntries = ref(false)

const isTimerIssue = computed(() =>
  issue.value != null && timerStore.isRunning(issue.value.id)
)
const isTicketOrTask = computed(() =>
  issue.value?.type === 'ticket' || issue.value?.type === 'task'
)
const totalHours = computed(() =>
  timeEntries.value.reduce((sum, e) => sum + (e.hours ?? 0), 0)
)

watch(() => props.issueId, async () => {
  timeEntries.value = []
  if (props.issueId) {
    try {
      timeEntries.value = await api.get<TimeEntry[]>(`/issues/${props.issueId}/time-entries`)
    } catch { /* ignore */ }
  }
  // Auto-expand if there are entries or a running timer; collapse if empty
  showTimeEntries.value = timeEntries.value.length > 0 || (issue.value != null && timerStore.isRunning(issue.value.id))
})

async function toggleTimer() {
  if (!issue.value) return
  if (isTimerIssue.value) {
    const entry = timerStore.getRunningEntry(issue.value.id)
    if (entry) await timerStore.stop(entry.id)
  } else {
    await timerStore.start(issue.value.id)
  }
  // Reload entries after timer action
  if (issue.value) {
    try {
      timeEntries.value = await api.get<TimeEntry[]>(`/issues/${issue.value.id}/time-entries`)
    } catch { /* ignore */ }
  }
}

// Move to trash (soft-delete — recoverable from Settings → Trash)
async function deleteIssue() {
  if (!issue.value) return
  if (!await confirm({
    message: `Move ${issue.value.issue_key} "${issue.value.title}" to Trash? Any child tasks will be moved too. You can restore from Settings → Trash.`,
    confirmLabel: 'Move to trash',
    danger: true,
  })) return
  try {
    await api.delete(`/issues/${issue.value.id}`)
    emit('deleted', issue.value.id)
    emit('close')
  } catch (e: unknown) {
    /* error swallowed — panel stays open as feedback */
  }
}

// Sprint management
const sprintDropOpen = ref(false)

async function addSprint(sprintId: number) {
  if (!issue.value) return
  await api.post(`/issues/${issue.value.id}/relations`, { target_id: sprintId, type: 'sprint' })
  issue.value = { ...issue.value, sprint_ids: [...(issue.value.sprint_ids ?? []), sprintId] }
  emit('updated', issue.value)
  sprintDropOpen.value = false
}
async function removeSprint(sprintId: number) {
  if (!issue.value) return
  await api.delete(`/issues/${issue.value.id}/relations`, { target_id: sprintId, type: 'sprint' })
  issue.value = { ...issue.value, sprint_ids: (issue.value.sprint_ids ?? []).filter(id => id !== sprintId) }
  emit('updated', issue.value)
}

async function deleteTimeEntry(entry: TimeEntry) {
  const isOther = entry.user_id !== authStore.user?.id
  const msg = isOther
    ? `You are deleting ${entry.username}'s time entry. This cannot be undone.`
    : 'Delete this time entry?'
  if (!await confirm({ message: msg, confirmLabel: 'Delete', danger: true })) return
  await api.delete(`/time-entries/${entry.id}`)
  timeEntries.value = timeEntries.value.filter(e => e.id !== entry.id)
}
</script>

<template>
  <Transition name="sidepanel">
    <aside v-if="issueId" :class="['side-panel', { 'side-panel--pinned': pinned, 'side-panel--resizing': resizing }]"
      :style="{ width: draftWidth + 'px' }">
      <div v-if="!pinned" class="sp-backdrop"
           @click="guardAction(() => $emit('close'))"
           @wheel.passive="onBackdropWheel" />
      <div class="sp-resize-handle" @mousedown="onResizeStart" @dblclick="resetWidth" title="Drag to resize · double-click to reset" />
      <div class="sp-content">
        <!-- Header -->
        <div class="sp-header">
          <button class="sp-pin" :class="{ 'sp-pin--active': pinned }" @click="togglePin" :title="pinned ? 'Unpin sidebar' : 'Pin sidebar'">
            <AppIcon :name="pinned ? 'pin' : 'pin-off'" :size="14" />
          </button>
          <span v-if="issue" class="sp-key">
            <span v-if="typeIcon" class="sp-type-icon" v-html="typeIcon" />
            {{ issue.issue_key }}
          </span>
          <span class="sp-spacer" />
          <!-- PAI-179: issue-level AI menu (find parent, generate
               sub-tasks, estimate, detect duplicates). Only mounts
               when an issue is loaded. -->
          <AiActionMenu
            v-if="issue && !readonly"
            surface="issue"
            placement="issue"
            field=""
            field-label="Issue"
            :issue-id="issue.id"
            :text="() => issue?.title ?? ''"
            :on-accept="() => { /* issue actions don't rewrite text */ }"
          />
          <button v-if="issueIds && issueIds.length > 1" class="sp-action-btn" :disabled="!canPrev" @click="goPrev" title="Previous issue">
            <AppIcon name="chevron-up" :size="15" />
          </button>
          <button v-if="issueIds && issueIds.length > 1" class="sp-action-btn" :disabled="!canNext" @click="goNext" title="Next issue">
            <AppIcon name="chevron-down" :size="15" />
          </button>
          <button v-if="issue && !readonly && authStore.user?.role === 'admin'" class="sp-action-btn sp-action-btn--danger" @click="deleteIssue" title="Delete issue">
            <AppIcon name="trash-2" :size="15" />
          </button>
          <button v-if="issue && !readonly" class="sp-action-btn" @click="cloneIssue" :disabled="cloning" title="Clone issue">
            <AppIcon name="copy" :size="15" />
          </button>
          <button v-if="issue && !readonly" class="sp-action-btn" :class="{ 'sp-action-btn--disabled': editing }" :disabled="editing" @click="startEdit" title="Quick Edit">
            <AppIcon name="pencil" :size="15" />
          </button>
          <button v-if="issue" class="sp-action-btn" @click="openFull" title="Open full view">
            <AppIcon name="maximize-2" :size="15" />
          </button>
          <button class="sp-action-btn" @click="guardAction(() => $emit('close'))" title="Close">
            <AppIcon name="x" :size="15" />
          </button>
        </div>

        <div v-if="loading" class="sp-loading">Loading…</div>

        <!-- View mode -->
        <template v-else-if="issue && !editing">
          <h2 class="sp-title">{{ issue.title }}</h2>
          <div class="sp-meta">
            <span class="sp-meta-item">
              <StatusDot :status="issue.status" />
              {{ STATUS_LABEL[issue.status] ?? issue.status }}
            </span>
            <span class="sp-meta-item" :style="{ color: PRIORITY_COLOR[issue.priority] }">{{ PRIORITY_LABEL[issue.priority] ?? issue.priority }}</span>
            <span v-if="issue.assignee" class="sp-meta-item">{{ issue.assignee.username }}</span>
            <span v-else class="sp-meta-item sp-muted">Unassigned</span>
            <span v-if="issue.cost_unit" class="sp-meta-item sp-meta-item--dim">{{ issue.cost_unit }}</span>
            <span v-if="issue.release" class="sp-meta-item sp-meta-item--dim">{{ issue.release }}</span>
          </div>

          <!-- Tags -->
          <div v-if="issue.tags?.length" class="sp-tags">
            <TagChip v-for="t in issue.tags" :key="t.id" :tag="t" />
          </div>

          <!-- Sprints (click to edit) -->
          <div v-if="sprints?.length" class="sp-sprints sp-sprints--clickable" @click="!readonly && startEdit()">
            <AppIcon name="repeat" :size="12" class="sp-sprint-icon" />
            <SprintChips
              v-if="issue.sprint_ids?.length"
              :sprint-ids="issue.sprint_ids"
              :sprints="sprints"
              compact
            />
            <span v-else class="sp-muted">No sprints</span>
          </div>

          <!-- Estimate / AR — click to toggle h / PT -->
          <div v-if="issue.estimate_hours != null || issue.estimate_lp != null || issue.ar_hours != null || issue.ar_lp != null" class="sp-estimates">
            <span v-if="issue.estimate_hours != null" class="sp-est-item sp-est-item--toggle" @click="toggleTimeUnit" title="Toggle h / PT">Est. <span class="unit-toggle">{{ formatHours(issue.estimate_hours, 'detail') }}</span></span>
            <span v-if="issue.estimate_lp != null" class="sp-est-item">Est. LP {{ issue.estimate_lp }}</span>
            <span v-if="issue.ar_hours != null" class="sp-est-item sp-est-item--toggle" @click="toggleTimeUnit" title="Toggle h / PT">AR <span class="unit-toggle">{{ formatHours(issue.ar_hours, 'detail') }}</span></span>
            <span v-if="issue.ar_lp != null" class="sp-est-item">AR LP {{ issue.ar_lp }}</span>
          </div>

          <!-- Time tracking (view mode, ticket/task only) — before description -->
          <div v-if="!readonly" class="sp-time-section">
            <div class="sp-time-header" @click="showTimeEntries = !showTimeEntries">
              <span class="sp-time-title">
                <AppIcon name="clock" :size="12" class="sp-time-clock" />
                Time
                <span v-if="isTimerIssue && issue" class="sp-timer-badge">{{ timerStore.formattedElapsed(timerStore.getRunningEntry(issue.id)?.id ?? 0) }}</span>
                <span v-else-if="totalHours > 0" class="sp-time-badge">Total: {{ formatDuration(totalHours) }}</span>
              </span>
              <div class="sp-time-right">
                <button v-if="isTimerIssue" class="sp-te-action sp-te-action--stop" @click.stop="toggleTimer" title="Stop timer">
                  <AppIcon name="square" :size="10" /> Stop
                </button>
                <button v-else class="sp-te-action" @click.stop="toggleTimer" title="Start timer">
                  <AppIcon name="play" :size="10" /> Start
                </button>
                <AppIcon :name="showTimeEntries ? 'chevron-up' : 'chevron-down'" :size="12" />
              </div>
            </div>
            <div v-if="showTimeEntries && timeEntries.length" class="sp-time-entries">
              <div v-for="e in timeEntries" :key="e.id" class="sp-te-row">
                <span class="sp-te-date">{{ e.started_at.slice(0, 10) }}</span>
                <span class="sp-te-hours">
                  <template v-if="e.stopped_at">{{ formatDuration(e.hours) }}</template>
                  <AppIcon v-else name="clock" :size="11" class="sp-te-running-icon" />
                </span>
                <span class="sp-te-comment">{{ e.comment || '—' }}</span>
                <button v-if="authStore.user?.role === 'admin' || e.user_id === authStore.user?.id"
                  class="sp-te-del" @click="deleteTimeEntry(e)" title="Delete">
                  <AppIcon name="x" :size="10" />
                </button>
              </div>
            </div>
            <div v-else-if="showTimeEntries && !timeEntries.length" class="sp-muted" style="font-size:12px">No entries yet.</div>
          </div>

          <!-- Long text fields -->
          <div class="sp-body">
            <div class="sp-body-block" v-if="issue.description">
              <p class="sp-body-label">Description</p>
              <div ref="descEl" class="sp-body-text" :class="{ 'md-rendered': mdMode }" v-html="descHtml" />
            </div>
            <div class="sp-body-block" v-if="issue.acceptance_criteria">
              <p class="sp-body-label">Acceptance Criteria</p>
              <div ref="acEl" class="sp-body-text" :class="{ 'md-rendered': mdMode }" v-html="acHtml" />
            </div>
            <div class="sp-body-block" v-if="issue.notes">
              <p class="sp-body-label">Notes</p>
              <div ref="notesEl" class="sp-body-text" :class="{ 'md-rendered': mdMode }" v-html="notesHtml" />
            </div>
            <div v-if="!issue.description && !issue.acceptance_criteria && !issue.notes" class="sp-muted">No content.</div>
          </div>

          <MarkdownToolbar v-model="mdMode" :subtle="true" />

          <!-- Tag selector (view mode, not readonly) -->
          <div v-if="allTags && !readonly" class="sp-tag-selector">
            <TagSelector :all-tags="allTags" :selected-ids="issueTagIds" @add="addTag" @remove="removeTag" />
          </div>

          <!-- Attachments (view mode — read-only chip list, clickable thumbnails) -->
          <AttachmentSidebar
            v-if="attachments.jobs.value.length"
            class="sp-attach-sidebar"
            title="Attachments"
            :jobs="attachments.jobs.value"
            readonly
          />
        </template>

        <!-- Edit mode -->
        <template v-else-if="issue && editing">
          <div class="sp-form">
            <div class="field">
              <label>Title</label>
              <input v-model="form.title" type="text" />
            </div>
            <div class="sp-form-row">
              <div class="field" style="flex:1">
                <label>Status</label>
                <MetaSelect v-model="form.status" :options="STATUS_OPTIONS" />
              </div>
              <div class="field" style="flex:1">
                <label>Priority</label>
                <MetaSelect v-model="form.priority" :options="PRIORITY_OPTIONS" />
              </div>
            </div>
            <div class="field">
              <label>Assignee</label>
              <MetaSelect v-model="form.assignee_id" :options="assigneeOptions" />
            </div>
            <div class="field" v-if="showParentPicker">
              <label>Parent</label>
              <MetaSelect v-model="form.parent_id" :options="parentOptions" placeholder="— None —" searchable />
            </div>
            <div class="sp-form-row">
              <div class="field" style="flex:1" v-if="costUnits">
                <label>Cost Unit</label>
                <AutocompleteInput v-model="form.cost_unit" :suggestions="costUnits" placeholder="e.g. CU-1" />
              </div>
              <div class="field" style="flex:1" v-if="releases">
                <label>Release</label>
                <AutocompleteInput v-model="form.release" :suggestions="releases" placeholder="e.g. v1.0" />
              </div>
            </div>
            <!-- Sprint assignment -->
            <div v-if="sprints?.length" class="field">
              <label>Sprints</label>
              <div class="sp-sprint-edit">
                <SprintChips
                  v-if="issue?.sprint_ids?.length"
                  :sprint-ids="issue.sprint_ids"
                  :sprints="sprints"
                  removable
                  compact
                  @remove="removeSprint"
                />
                <div class="sp-sprint-add-wrap">
                  <button type="button" class="sp-sprint-add" @click="sprintDropOpen = !sprintDropOpen">+ Sprint</button>
                  <div v-if="sprintDropOpen" class="sp-sprint-dropdown">
                    <button v-for="s in sprints.filter(s => !(issue?.sprint_ids ?? []).includes(s.id))" :key="s.id"
                      type="button" class="sp-sprint-opt" @click="addSprint(s.id)">
                      {{ s.title }}
                    </button>
                    <div v-if="!sprints.filter(s => !(issue?.sprint_ids ?? []).includes(s.id)).length" class="sp-sprint-empty">All sprints assigned</div>
                  </div>
                </div>
              </div>
            </div>

            <div class="sp-form-row">
              <div class="field" style="flex:1">
                <label class="sp-label-toggle" @click="toggleTimeUnit">Est. <span class="unit-toggle">{{ timeLabel() }}</span></label>
                <NumericInput v-model="form.estimate_hours" />
              </div>
              <div class="field" style="flex:1">
                <label>Est. LP</label>
                <NumericInput v-model="form.estimate_lp" />
              </div>
            </div>
            <div class="sp-form-row">
              <div class="field" style="flex:1">
                <label class="sp-label-toggle" @click="toggleTimeUnit">AR <span class="unit-toggle">{{ timeLabel() }}</span></label>
                <NumericInput v-model="form.ar_hours" />
              </div>
              <div class="field" style="flex:1">
                <label>AR LP</label>
                <NumericInput v-model="form.ar_lp" />
              </div>
            </div>
            <!-- Time tracking in edit mode -->
            <div v-if="!readonly" class="sp-time-section sp-time-section--edit">
              <div class="sp-time-header" @click="showTimeEntries = !showTimeEntries">
                <span class="sp-time-title">
                  <AppIcon name="clock" :size="12" class="sp-time-clock" />
                  Time
                  <span v-if="isTimerIssue && issue" class="sp-timer-badge">{{ timerStore.formattedElapsed(timerStore.getRunningEntry(issue.id)?.id ?? 0) }}</span>
                  <span v-else-if="totalHours > 0" class="sp-time-badge">{{ formatDuration(totalHours) }}</span>
                </span>
                <div class="sp-time-right">
                  <button v-if="isTimerIssue" class="sp-te-action sp-te-action--stop" @click.stop="toggleTimer" title="Stop timer">
                    <AppIcon name="square" :size="10" /> Stop
                  </button>
                  <button v-else class="sp-te-action" @click.stop="toggleTimer" title="Start timer">
                    <AppIcon name="play" :size="10" /> Start
                  </button>
                  <AppIcon :name="showTimeEntries ? 'chevron-up' : 'chevron-down'" :size="12" />
                </div>
              </div>
              <div v-if="showTimeEntries && timeEntries.length" class="sp-time-entries">
                <div v-for="e in timeEntries" :key="e.id" class="sp-te-row">
                  <span class="sp-te-date">{{ e.started_at.slice(0, 10) }}</span>
                  <span class="sp-te-hours">
                    <template v-if="e.stopped_at">{{ formatDuration(e.hours) }}</template>
                    <AppIcon v-else name="clock" :size="11" class="sp-te-running-icon" />
                  </span>
                  <span class="sp-te-comment">{{ e.comment || '—' }}</span>
                  <button v-if="authStore.user?.role === 'admin' || e.user_id === authStore.user?.id"
                    class="sp-te-del" @click="deleteTimeEntry(e)" title="Delete">
                    <AppIcon name="x" :size="10" />
                  </button>
                </div>
              </div>
            </div>

            <AiOptimizeBanner />
            <div class="field">
              <div class="field-label-row">
                <label>Description</label>
                <AiActionMenu surface="issue"
                  field="description"
                  field-label="Description"
                  :issue-id="issue?.id ?? 0"
                  :text="() => form.description"
                  :on-accept="onAiAccept('description')"
                />
              </div>
              <textarea v-model="form.description" rows="5" />
            </div>
            <div class="field">
              <div class="field-label-row">
                <label>Acceptance Criteria</label>
                <AiActionMenu surface="issue"
                  field="acceptance_criteria"
                  field-label="Acceptance Criteria"
                  :issue-id="issue?.id ?? 0"
                  :text="() => form.acceptance_criteria"
                  :on-accept="onAiAccept('acceptance_criteria')"
                />
              </div>
              <textarea v-model="form.acceptance_criteria" rows="4" />
            </div>
            <div class="field">
              <div class="field-label-row">
                <label>Notes</label>
                <AiActionMenu surface="issue"
                  field="notes"
                  field-label="Notes"
                  :issue-id="issue?.id ?? 0"
                  :text="() => form.notes"
                  :on-accept="onAiAccept('notes')"
                />
              </div>
              <textarea v-model="form.notes" rows="3" />
            </div>
            <!-- Attachments (edit mode — drop, upload, remove) -->
            <AttachmentSidebar
              v-if="attachmentsEnabled"
              class="sp-attach-sidebar sp-attach-sidebar--edit"
              title="Attachments"
              :jobs="attachments.jobs.value"
              @add-files="(files) => attachments.addFiles(files)"
              @remove="(job) => attachments.removeJob(job)"
              @retry="(job) => attachments.retryJob(job)"
            />
            <div v-if="saveError" class="form-error">{{ saveError }}</div>
            <div class="sp-form-actions">
              <button class="btn btn-ghost btn-sm" @click="cancelEdit">Cancel</button>
              <button
                class="btn btn-primary btn-sm"
                :disabled="saving || attachments.hasInFlight.value"
                @click="save"
              >
                {{ saving
                  ? 'Saving…'
                  : attachments.hasInFlight.value
                    ? `Uploading ${attachments.inFlightCount.value}…`
                    : 'Save' }}
              </button>
            </div>
          </div>
        </template>
      </div>
    </aside>
  </Transition>

  <!-- PAI-146: AI optimize preview overlay. Single mount per panel
       instance; the composable is a singleton so opening from a
       textarea here uses the same slot as the detail view. -->
  <AiOptimizeOverlay
    v-if="aiOptimize.overlay.visible"
    :original="aiOptimize.overlay.original"
    :optimized="aiOptimize.overlay.optimized"
    :field-label="aiOptimize.overlay.fieldLabel"
    :model-name="aiOptimize.overlay.modelName"
    :retrying="aiOptimize.overlay.retrying"
    @accept="aiOptimize.accept()"
    @reject="aiOptimize.reject()"
    @retry="aiOptimize.retry()"
  />
</template>

<style scoped>
/* PAI-146: per-field label row holds the label + the AI optimize
   button on the right. Mirrors the IssueDetailView treatment. */
.field-label-row {
  display: flex; align-items: center; justify-content: space-between;
  gap: .5rem;
}
.field-label-row > label { margin-bottom: 0; }

.side-panel {
  position: fixed; top: 0; right: 0; bottom: 0;
  z-index: 200;
}
.sp-backdrop {
  position: fixed; top: 0; left: 0; bottom: 0; right: 0;
  z-index: -1;
}
.side-panel--pinned {
  position: fixed; top: 0; right: 0; bottom: 0; left: auto;
  z-index: 150; min-width: 300px;
}
.side-panel--resizing { user-select: none; }
.sp-resize-handle {
  position: absolute; top: 0; left: -3px; bottom: 0; width: 6px;
  cursor: col-resize; z-index: 5;
}
.sp-resize-handle::after {
  content: ''; position: absolute; top: 50%; left: 2px; width: 2px; height: 32px;
  transform: translateY(-50%); border-radius: 1px;
  background: var(--border); opacity: 0; transition: opacity .15s;
}
.sp-resize-handle:hover::after, .side-panel--resizing .sp-resize-handle::after { opacity: 1; background: var(--bp-blue); }
.sp-content {
  width: 100%; max-width: 90vw; height: 100%;
  background: var(--bg-card); border-left: 1px solid var(--border);
  box-shadow: -4px 0 24px rgba(0,0,0,.1);
  overflow-y: auto; padding: 1.25rem 1.5rem;
  display: flex; flex-direction: column; gap: .75rem;
}
.side-panel--pinned .sp-content {
  max-width: none; box-shadow: none;
  border-left: 2px solid var(--border); padding-left: 1.25rem;
}
.sp-header {
  display: flex; align-items: center; gap: .5rem;
  border-bottom: 1px solid var(--border); padding-bottom: .75rem;
  flex-shrink: 0;
}
.sp-pin {
  background: none; border: 1px solid transparent; cursor: pointer; padding: 3px;
  color: var(--text-muted); border-radius: 4px; display: flex; align-items: center;
}
.sp-pin:hover { background: var(--bg); color: var(--text); }
.sp-pin--active { color: var(--bp-blue); border-color: var(--bp-blue); background: var(--bp-blue-pale); }
.sp-key {
  font-size: 13px; font-weight: 700; color: var(--bp-blue-dark);
  background: var(--bp-blue-pale); padding: .15rem .5rem; border-radius: 4px;
  display: inline-flex; align-items: center; gap: .35rem;
}
.sp-type-icon { display: inline-flex; align-items: center; }
.sp-type-icon :deep(svg) { width: 14px; height: 14px; }
/* Nav buttons now use sp-action-btn style (right side of header) */
.sp-spacer { flex: 1; }
.sp-action-btn {
  background: none; border: none; cursor: pointer; padding: 5px;
  color: var(--text-muted); border-radius: 50%; display: flex; align-items: center;
  transition: background .15s, color .15s;
}
.sp-action-btn:hover:not(:disabled) { background: var(--bg); color: var(--text); }
.sp-action-btn:disabled { opacity: .3; cursor: default; }
.sp-action-btn--danger { color: #dc2626; }
.sp-action-btn--danger:hover:not(:disabled) { background: #fef2f2; color: #dc2626; }
.sp-loading { color: var(--text-muted); font-size: 13px; padding: 2rem 0; }
.sp-title { font-size: 18px; font-weight: 600; margin: 0; line-height: 1.3; }
.sp-meta { display: flex; flex-wrap: wrap; gap: .5rem; font-size: 13px; }
.sp-meta-item { display: inline-flex; align-items: center; gap: .3rem; }
.sp-meta-item--dim { color: var(--text-muted); }
/* Status dot now rendered by StatusDot.vue component */
.sp-muted { color: var(--text-muted); font-style: italic; font-size: 13px; }
.sp-tags { display: flex; flex-wrap: wrap; gap: .3rem; }
.sp-estimates {
  display: flex; flex-wrap: wrap; gap: .75rem; font-size: 12px; color: var(--text-muted);
  padding: .4rem 0; border-top: 1px solid var(--border); border-bottom: 1px solid var(--border);
}
.sp-est-item { white-space: nowrap; }
.sp-est-item--toggle { cursor: pointer; }
.sp-est-item--toggle:hover .unit-toggle { filter: brightness(.85); }
.sp-body { display: flex; flex-direction: column; gap: .75rem; flex: 1; min-height: 0; overflow-y: auto; }
.sp-body-block {}
.sp-body-label { font-size: 11px; font-weight: 700; text-transform: uppercase; letter-spacing: .04em; color: var(--text-muted); margin: 0 0 .25rem; }
.sp-body-text { font-size: 13px; line-height: 1.55; color: var(--text); white-space: pre-wrap; }
/* Markdown styles now in global .md-rendered class (App.vue) */
.sp-tag-selector { margin-top: auto; padding-top: .5rem; border-top: 1px solid var(--border); }
.sp-attach-sidebar {
  /* Reset the inline-sidebar border/margin when mounted in the flow of the panel. */
  margin: .5rem -1.5rem 0;
  border-left: none;
  border-top: 1px solid var(--border);
  max-width: none;
  padding: .75rem 1.5rem;
}
.sp-attach-sidebar--edit { margin-top: .25rem; }
.sp-form { display: flex; flex-direction: column; gap: .65rem; overflow-y: auto; flex: 1; }
.sp-form .field { display: flex; flex-direction: column; gap: .3rem; }
.sp-form .field label { font-size: 12px; font-weight: 600; color: var(--text-muted); text-transform: uppercase; letter-spacing: .05em; }
.sp-form textarea { resize: vertical; max-width: 100%; }
.sp-form-row { display: flex; gap: .75rem; }
.sp-form-actions { display: flex; gap: .5rem; justify-content: flex-end; margin-top: .5rem; position: sticky; bottom: 0; background: var(--bg-card); padding: .5rem 0; }
.sp-label-toggle { cursor: pointer; }
.unit-toggle { color: var(--bp-blue); font-weight: 600; text-decoration: underline; text-decoration-style: dotted; }

/* Timer button */
.sp-timer-active {
  color: #22c55e !important;
  background: rgba(34, 197, 94, .1) !important;
}
.sp-timer-active:hover { background: rgba(34, 197, 94, .2) !important; }

/* Sprints — chip styling lives in SprintChips.vue */
.sp-sprints { display: flex; flex-wrap: wrap; gap: .3rem; align-items: center; }
.sp-sprints--clickable { cursor: pointer; padding: .25rem .4rem; border-radius: var(--radius); transition: background .12s; }
.sp-sprints--clickable:hover { background: var(--bg-hover, rgba(0,0,0,.04)); }
.sp-sprint-icon { color: var(--text-muted); flex-shrink: 0; }
.sp-sprint-edit { display: flex; flex-wrap: wrap; gap: .3rem; align-items: center; }
.sp-sprint-add-wrap { position: relative; }
.sp-sprint-add {
  background: none; border: 1px dashed var(--border); border-radius: 20px;
  padding: .1rem .5rem; font-size: 11px; color: var(--text-muted); cursor: pointer;
  font-family: inherit;
}
.sp-sprint-add:hover { border-color: var(--bp-blue); color: var(--bp-blue); }
.sp-sprint-dropdown {
  position: absolute; top: calc(100% + 4px); left: 0; z-index: 300;
  background: var(--bg-card); border: 1px solid var(--border); border-radius: 6px;
  box-shadow: 0 4px 16px rgba(0,0,0,.12); min-width: 180px; max-height: 200px; overflow-y: auto;
}
.sp-sprint-opt {
  display: block; width: 100%; text-align: left; background: none; border: none;
  padding: .4rem .65rem; font-size: 12px; cursor: pointer; font-family: inherit; color: var(--text);
}
.sp-sprint-opt:hover { background: var(--surface-2); }
.sp-sprint-empty { padding: .4rem .65rem; font-size: 12px; color: var(--text-muted); }

/* Time section */
.sp-time-section { border-top: 1px solid var(--border); padding: .65rem 1.5rem; margin: 0 -1.5rem; }
.sp-time-section--edit { margin: 0; padding: .5rem 0; border-top: 1px solid var(--border); border-bottom: 1px solid var(--border); }
.sp-action-label { font-size: 11px; font-weight: 600; }
.sp-time-header {
  display: flex; align-items: center; justify-content: space-between;
  cursor: pointer; padding: .2rem 0; font-size: 11px; font-weight: 700;
  text-transform: uppercase; letter-spacing: .04em; color: var(--text-muted);
}
.sp-time-title { display: flex; align-items: center; gap: .4rem; }
.sp-time-right { display: flex; align-items: center; gap: .4rem; }
.sp-time-clock { color: var(--text-muted); }
.sp-te-action {
  background: none; border: none; cursor: pointer;
  display: inline-flex; align-items: center; gap: .2rem;
  font-size: 10px; font-weight: 600; color: var(--text-muted);
  padding: .15rem .4rem; border-radius: 4px; font-family: inherit;
  transition: color .1s, background .1s;
}
.sp-te-action:hover { color: var(--bp-green, #16a34a); background: color-mix(in srgb, var(--bp-green) 8%, transparent); }
.sp-te-action--stop { color: var(--bp-green, #16a34a); }
.sp-timer-badge {
  font-size: 10px; font-weight: 600; padding: .1rem .4rem; border-radius: 8px;
  background: rgba(34, 197, 94, .15); color: #22c55e;
  animation: timer-pulse 2s ease-in-out infinite;
}
@keyframes timer-pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: .6; }
}
.sp-time-badge {
  font-size: 10px; font-weight: 600; padding: .1rem .4rem; border-radius: 8px;
  background: var(--bp-blue-pale); color: var(--bp-blue);
}
.sp-time-entries { display: flex; flex-direction: column; gap: 1px; margin-top: .35rem; }
.sp-te-row {
  display: flex; align-items: center; gap: .4rem; font-size: 11px;
  padding: .2rem .25rem; border-radius: 3px;
}
.sp-te-row:hover { background: var(--bg); }
.sp-te-date { color: var(--text-muted); white-space: nowrap; min-width: 65px; }
.sp-te-hours { font-weight: 600; white-space: nowrap; min-width: 40px; display: inline-flex; align-items: center; }
.sp-te-running-icon { color: var(--bp-green, #16a34a); }
.sp-te-comment { color: var(--text-muted); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; flex: 1; min-width: 0; }
.sp-te-del {
  background: none; border: none; cursor: pointer; padding: 2px;
  color: var(--text-muted); border-radius: 3px; display: flex; opacity: 0;
  transition: opacity .1s;
}
.sp-te-row:hover .sp-te-del { opacity: 1; }
.sp-te-del:hover { color: var(--danger); background: var(--bg); }

/* Slide transition */
.sidepanel-enter-active, .sidepanel-leave-active { transition: opacity .2s, transform .2s; }
.sidepanel-enter-active .sp-content, .sidepanel-leave-active .sp-content { transition: transform .2s; }
.sidepanel-enter-from { opacity: 0; }
.sidepanel-enter-from .sp-content { transform: translateX(100%); }
.sidepanel-leave-to { opacity: 0; }
.sidepanel-leave-to .sp-content { transform: translateX(100%); }
</style>
