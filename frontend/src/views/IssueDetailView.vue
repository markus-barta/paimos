<script setup lang="ts">
import { ref, computed, onMounted, watch, nextTick } from 'vue'
import { useRoute, useRouter, RouterLink, onBeforeRouteLeave } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import AppFooter from '@/components/AppFooter.vue'
import IssueList from '@/components/IssueList.vue'
import AppIcon from '@/components/AppIcon.vue'
import StatusDot from '@/components/StatusDot.vue'
import { useDirtyGuard } from '@/composables/useDirtyGuard'
import { useConfirm } from '@/composables/useConfirm'
import { useMarkdown } from '@/composables/useMarkdown'
import { useTimeUnit } from '@/composables/useTimeUnit'
import { api, errMsg } from '@/api/client'
import { attachmentsEnabled } from '@/api/instance'
import { useNewIssueStore } from '@/stores/newIssue'
import { provideIssueContext } from '@/composables/useIssueContext'
import type { Issue, Tag, Project, Sprint, User, Attachment } from '@/types'
import {
  useIssueDisplay,
  TYPE_SVGS,
  STATUS_LABEL,
  PRIORITY_ICON, PRIORITY_COLOR, PRIORITY_LABEL,
} from '@/composables/useIssueDisplay'
import { vAutoGrow } from '@/directives/autoGrow'
import AiOptimizeButton from '@/components/ai/AiOptimizeButton.vue'
import AiOptimizeOverlay from '@/components/ai/AiOptimizeOverlay.vue'
import { useAiOptimize } from '@/composables/useAiOptimize'

// Sub-components
import IssueTimeEntries from '@/components/issue/IssueTimeEntries.vue'
import IssueHistory from '@/components/issue/IssueHistory.vue'
import IssueRelations from '@/components/issue/IssueRelations.vue'
import IssueAttachments from '@/components/issue/IssueAttachments.vue'
import IssueComments from '@/components/issue/IssueComments.vue'
import IssueGroupMembers from '@/components/issue/IssueGroupMembers.vue'
import IssueMetaGrid from '@/components/issue/IssueMetaGrid.vue'
import IssueEditSidebar from '@/components/issue/IssueEditSidebar.vue'
import IssueBillingSummary from '@/components/issue/IssueBillingSummary.vue'
import IssueCompleteEpicModal from '@/components/issue/IssueCompleteEpicModal.vue'

const route     = useRoute()
const router    = useRouter()
const { confirm } = useConfirm()

const issueId   = ref(Number(route.params.issueId))
const projectId = ref(Number(route.params.id))

const issue          = ref<Issue | null>(null)
const project        = ref<Project | null>(null)
const parentIssue    = ref<Issue | null>(null)
const children       = ref<Issue[]>([])
const projectIssues  = ref<Issue[]>([])
const users          = ref<User[]>([])
const allTags        = ref<Tag[]>([])
const allSprints     = ref<Sprint[]>([])
const costUnits      = ref<string[]>([])
const releases       = ref<string[]>([])

provideIssueContext({ users, allTags, costUnits, releases, projects: ref([]), sprints: allSprints })

const loading        = ref(true)
const editing        = ref(false)
const saving         = ref(false)
const saveError      = ref('')

const issueTagIds = computed(() => issue.value?.tags?.map(t => t.id) ?? [])

const form = ref({
  title: '', description: '', acceptance_criteria: '', notes: '',
  type: '', status: '', priority: '', cost_unit: '', release: '',
  parent_id: null as number | null, assignee_id: null as number | null,
  billing_type:  null as string | null,
  total_budget:  null as number | null,
  rate_hourly:   null as number | null,
  rate_lp:  null as number | null,
  estimate_hours: null as number | null,
  estimate_lp:    null as number | null,
  ar_hours:       null as number | null,
  ar_lp:          null as number | null,
  time_override:  null as number | null,
  start_date:    null as string | null,
  end_date:      null as string | null,
  group_state:   null as string | null,
  sprint_state:  null as string | null,
  jira_id:       null as string | null,
  jira_version:  null as string | null,
  jira_text:     null as string | null,
  color:         null as string | null,
})

// Sub-component refs
const timeEntriesRef   = ref<InstanceType<typeof IssueTimeEntries> | null>(null)
const historyRef       = ref<InstanceType<typeof IssueHistory> | null>(null)
const relationsRef     = ref<InstanceType<typeof IssueRelations> | null>(null)
const attachmentsRef   = ref<InstanceType<typeof IssueAttachments> | null>(null)
const commentsRef      = ref<InstanceType<typeof IssueComments> | null>(null)
const groupMembersRef  = ref<InstanceType<typeof IssueGroupMembers> | null>(null)

// Reload when route issueId changes
watch(() => route.params.issueId, (newId) => {
  issueId.value = Number(newId)
  projectId.value = Number(route.params.id)
  load()
})

async function load() {
  loading.value = true
  editing.value = false
  saveError.value = ''
  const [i, u, cu, rel, ch, tags, projIssues, proj, sprints] = await Promise.all([
    api.get<Issue>(`/issues/${issueId.value}`),
    api.get<User[]>('/users'),
    api.get<string[]>(`/projects/${projectId.value}/cost-units`).catch(() => []),
    api.get<string[]>(`/projects/${projectId.value}/releases`).catch(() => []),
    api.get<Issue[]>(`/issues/${issueId.value}/children`).catch(() => []),
    api.get<Tag[]>('/tags'),
    api.get<Issue[]>(`/projects/${projectId.value}/issues?fields=list`).catch(() => []),
    api.get<Project>(`/projects/${projectId.value}`).catch(() => null),
    api.get<Sprint[]>('/sprints').catch(() => []),
  ])
  issue.value         = i
  project.value       = proj
  users.value         = u
  costUnits.value     = cu
  releases.value      = rel
  children.value      = ch
  allTags.value       = tags
  allSprints.value    = sprints
  projectIssues.value = projIssues
  parentIssue.value = i.parent_id
    ? await api.get<Issue>(`/issues/${i.parent_id}`).catch(() => null)
    : null
  resetForm()
  loading.value = false
  // Sub-components load their own data
  nextTick(() => {
    commentsRef.value?.load()
    relationsRef.value?.load()
    timeEntriesRef.value?.load()
    groupMembersRef.value?.load()
    attachmentsRef.value?.load()
    loadAggregation()
  })
}

onMounted(async () => {
  await load()
  initMdModes()
  if (route.query.edit === '1') {
    editing.value = true
    router.replace({ query: { ...route.query, edit: undefined } })
  }
})

function resetForm() {
  if (!issue.value) return
  form.value = {
    title:               issue.value.title,
    description:         issue.value.description,
    acceptance_criteria: issue.value.acceptance_criteria,
    notes:               issue.value.notes,
    type:                issue.value.type,
    status:              issue.value.status,
    priority:            issue.value.priority,
    cost_unit:           issue.value.cost_unit,
    release:             issue.value.release,
    parent_id:           issue.value.parent_id,
    assignee_id:         issue.value.assignee_id,
    billing_type:  issue.value.billing_type  ?? null,
    total_budget:  issue.value.total_budget  ?? null,
    rate_hourly:   issue.value.rate_hourly   ?? null,
    rate_lp:        issue.value.rate_lp        ?? null,
    estimate_hours: issue.value.estimate_hours ?? null,
    estimate_lp:    issue.value.estimate_lp    ?? null,
    ar_hours:       issue.value.ar_hours       ?? null,
    ar_lp:          issue.value.ar_lp          ?? null,
    time_override:  issue.value.time_override  ?? null,
    start_date:    issue.value.start_date    ?? null,
    end_date:      issue.value.end_date      ?? null,
    group_state:   issue.value.group_state   ?? null,
    sprint_state:  issue.value.sprint_state  ?? null,
    jira_id:       issue.value.jira_id       ?? null,
    jira_version:  issue.value.jira_version  ?? null,
    jira_text:     issue.value.jira_text     ?? null,
    color:         issue.value.color         ?? null,
  }
}

// Dirty guard for unsaved changes
const detailSavedSnapshot = ref('')
const detailCurrentSnapshot = computed(() => editing.value ? JSON.stringify(form.value) : '')
const { isDirty: isDetailDirty, reset: resetDetailDirty } = useDirtyGuard(detailCurrentSnapshot, detailSavedSnapshot)

onBeforeRouteLeave(async () => {
  if (pendingInlineUploads.value > 0) {
    return await confirm({
      message: `An attachment upload is still in progress (${pendingInlineUploads.value}). Leave anyway? Pending placeholders will be lost.`,
      confirmLabel: 'Leave', danger: true,
    })
  }
  if (isDetailDirty.value) {
    return await confirm({ message: 'You have unsaved changes. Discard and leave?', confirmLabel: 'Discard', danger: true })
  }
})

function enterEditMode() {
  resetForm()
  editing.value = true
  nextTick(() => { detailSavedSnapshot.value = JSON.stringify(form.value) })
}

async function save() {
  if (pendingInlineUploads.value > 0) {
    saveError.value = `Please wait — ${pendingInlineUploads.value} attachment upload${pendingInlineUploads.value > 1 ? 's' : ''} still in progress.`
    return
  }
  saveError.value = ''
  saving.value = true
  try {
    issue.value = await api.put<Issue>(`/issues/${issueId.value}`, form.value)
    parentIssue.value = issue.value.parent_id
      ? await api.get<Issue>(`/issues/${issue.value.parent_id}`).catch(() => null)
      : null
    editing.value = false
    resetDetailDirty()
    const cu = issue.value.cost_unit?.trim()
    if (cu && !costUnits.value.includes(cu))
      costUnits.value = [...costUnits.value, cu].sort((a, b) => a.localeCompare(b))
    const rel = issue.value.release?.trim()
    if (rel && !releases.value.includes(rel))
      releases.value = [...releases.value, rel].sort((a, b) => a.localeCompare(b))
  } catch (e: unknown) {
    saveError.value = errMsg(e, 'Save failed.')
  } finally {
    saving.value = false
  }
}

async function deleteIssue() {
  if (saving.value) return
  if (!await confirm({ message: `Delete ${issue.value?.issue_key} "${issue.value?.title}"?`, confirmLabel: 'Delete', danger: true })) return
  saving.value = true
  try {
    await api.delete(`/issues/${issueId.value}`)
    router.push(`/projects/${projectId.value}`)
  } finally {
    saving.value = false
  }
}

// ── Clone ────────────────────────────────────────────────────────────────────
const cloning = ref(false)
async function cloneIssue() {
  if (cloning.value) return
  cloning.value = true
  try {
    const clone = await api.post<Issue>(`/issues/${issueId.value}/clone`, {})
    router.push(`/projects/${projectId.value}/issues/${clone.id}?edit=1`)
  } catch (e: unknown) {
    alert(errMsg(e, 'Clone failed.'))
  } finally {
    cloning.value = false
  }
}

// ── Complete Epic ────────────────────────────────────────────────────────────
const completeEpicRef = ref<InstanceType<typeof IssueCompleteEpicModal> | null>(null)

function onEpicCompleted(updated: Issue, ch: Issue[]) {
  issue.value = updated
  children.value = ch
}

// ── Inline file paste/drop (ACME-1 / 581 / 583 / 584 / 585) ──────────────
const pendingAttachmentIds = ref<number[]>([])
const descDragOver = ref(false)
const acDragOver   = ref(false)
let pendingUploadSeq = 0

function clearAllDragOver() {
  descDragOver.value = false
  acDragOver.value   = false
}

type InlineField = 'description' | 'acceptance_criteria'
type UploadStatus = 'pending' | 'done' | 'failed'

interface UploadJob {
  seq: number
  field: InlineField
  filename: string
  file: File
  isImage: boolean
  progress: number
  status: UploadStatus
  error?: string
  insertAt: number
}

// Sidecar upload state — NOT mixed into the textarea. The textarea stays clean;
// the markdown link is only inserted when the upload resolves successfully.
const uploadJobs = ref<UploadJob[]>([])

const pendingInlineUploads = computed(
  () => uploadJobs.value.filter(j => j.status === 'pending').length,
)
const avgUploadProgress = computed(() => {
  const active = uploadJobs.value.filter(j => j.status === 'pending')
  if (!active.length) return 0
  return Math.round(active.reduce((s, j) => s + j.progress, 0) / active.length)
})
function jobsFor(field: InlineField): UploadJob[] {
  return uploadJobs.value.filter(j => j.field === field)
}

// Escape characters that would break a markdown link's text segment.
function escapeLinkText(name: string): string {
  return name.replace(/[\[\]]/g, (m) => '\\' + m).replace(/[\r\n]+/g, ' ')
}

function startUpload(job: UploadJob) {
  job.status = 'pending'
  job.progress = 0
  job.error = undefined

  const fd = new FormData()
  fd.append('file', job.file)

  const endpoint = issue.value?.id
    ? `/issues/${issueId.value}/attachments`
    : '/attachments'

  api.upload<Attachment>(endpoint, fd, (pct) => { job.progress = pct })
    .then((a) => {
      const url = `/api/attachments/${a.id}`
      const safeName = escapeLinkText(a.filename)
      const snippet  = job.isImage ? `![${safeName}](${url})` : `[${safeName}](${url})`

      // Insert at the saved cursor position, clamped to current text length.
      // Prefix a newline if we're not already on a fresh line, so successive
      // drops don't smash into each other or into existing prose.
      const text = form.value[job.field]
      const pos  = Math.min(Math.max(job.insertAt, 0), text.length)
      const needsLeadingNL  = pos > 0 && text[pos - 1] !== '\n'
      const needsTrailingNL = pos < text.length && text[pos] !== '\n'
      const inserted = (needsLeadingNL ? '\n' : '') + snippet + (needsTrailingNL ? '\n' : '')
      form.value[job.field] = text.slice(0, pos) + inserted + text.slice(pos)

      if (issue.value?.id) {
        attachmentsRef.value?.load()
      } else {
        pendingAttachmentIds.value.push(a.id)
      }

      job.status = 'done'
      job.progress = 100
      // Auto-dismiss success chips so the row doesn't pile up.
      setTimeout(() => {
        uploadJobs.value = uploadJobs.value.filter(j => j !== job)
      }, 1500)
    })
    .catch((err: unknown) => {
      job.status = 'failed'
      job.error  = errMsg(err, 'upload failed')
    })
}

function uploadInlineFiles(files: FileList | File[], modelField: InlineField, insertAt: number) {
  const list = Array.from(files)
  if (!list.length) return

  const newJobs: UploadJob[] = list.map((file) => ({
    seq: ++pendingUploadSeq,
    field: modelField,
    filename: file.name,
    file,
    isImage: file.type.startsWith('image/'),
    progress: 0,
    status: 'pending',
    insertAt,
  }))

  uploadJobs.value.push(...newJobs)
  for (const job of newJobs) startUpload(job)
}

function retryUpload(job: UploadJob) {
  startUpload(job)
}

function dismissJob(job: UploadJob) {
  uploadJobs.value = uploadJobs.value.filter(j => j !== job)
}

function onTextareaPaste(e: ClipboardEvent, textareaRef: HTMLTextAreaElement, modelField: InlineField) {
  const files = e.clipboardData?.files
  if (!files || !files.length) return
  if (!attachmentsEnabled.value) return  // storage not configured — let paste fall through untouched
  // Don't hijack text paste — only intercept when the clipboard actually carries files.
  e.preventDefault()
  const start = textareaRef.selectionStart
  uploadInlineFiles(files, modelField, start)
}

function onTextareaDrop(e: DragEvent, textareaRef: HTMLTextAreaElement, modelField: InlineField) {
  if (!e.dataTransfer?.files?.length) return
  e.preventDefault()
  if (!attachmentsEnabled.value) return  // storage not configured — drop is swallowed silently
  const start = textareaRef.selectionStart ?? form.value[modelField].length
  uploadInlineFiles(e.dataTransfer.files, modelField, start)
}

async function addTag(tagId: number) {
  await api.post(`/issues/${issueId.value}/tags`, { tag_id: tagId })
  const tag = allTags.value.find(t => t.id === tagId)
  if (tag && issue.value) issue.value = { ...issue.value, tags: [...(issue.value.tags ?? []), tag] }
}

async function removeTag(tagId: number) {
  if (!await confirm({ message: 'Remove this tag?', confirmLabel: 'Remove' })) return
  await api.delete(`/issues/${issueId.value}/tags/${tagId}`)
  if (issue.value) issue.value = { ...issue.value, tags: (issue.value.tags ?? []).filter(t => t.id !== tagId) }
}

// ── Sprint assignment ────────────────────────────────────────────────────────
const sprintSearchQuery   = ref('')
const sprintDropdownOpen  = ref(false)
const sprintSearchRef     = ref<HTMLInputElement | null>(null)
const sprintWrapperRef    = ref<HTMLElement | null>(null)
const sprintDropdownPos   = ref({ top: 0, left: 0 })

function onSprintOutsideClick(e: MouseEvent) {
  const target = e.target as Node
  if (sprintWrapperRef.value && !sprintWrapperRef.value.contains(target)) {
    const dd = document.querySelector('.sprint-dropdown--teleported')
    if (dd && dd.contains(target)) return
    sprintDropdownOpen.value = false
  }
}
watch(sprintDropdownOpen, (open) => {
  if (open) document.addEventListener('mousedown', onSprintOutsideClick)
  else      document.removeEventListener('mousedown', onSprintOutsideClick)
})

const assignedSprints = computed(() =>
  allSprints.value.filter(s => issue.value?.sprint_ids?.includes(s.id))
)

const availableSprintsFiltered = computed(() => {
  const assigned = issue.value?.sprint_ids ?? []
  const q = sprintSearchQuery.value.toLowerCase()
  return allSprints.value
    .filter(s => !assigned.includes(s.id))
    .filter(s => !q || s.title.toLowerCase().includes(q))
    .slice(0, 20)
})

function toggleSprintDropdown() {
  sprintDropdownOpen.value = !sprintDropdownOpen.value
  if (sprintDropdownOpen.value) {
    nextTick(() => {
      if (sprintWrapperRef.value) {
        const rect = sprintWrapperRef.value.getBoundingClientRect()
        sprintDropdownPos.value = { top: rect.bottom + 4, left: rect.left }
      }
      sprintSearchRef.value?.focus()
    })
  }
}

async function assignSprint(sprint: Sprint) {
  if (!issue.value) return
  await api.post(`/issues/${issueId.value}/relations`, { target_id: sprint.id, type: 'sprint' })
  issue.value = { ...issue.value, sprint_ids: [...(issue.value.sprint_ids ?? []), sprint.id] }
  sprintDropdownOpen.value = false
  sprintSearchQuery.value  = ''
}

async function removeSprint(sprintId: number) {
  if (!issue.value) return
  if (!await confirm({ message: 'Remove sprint assignment?', confirmLabel: 'Remove' })) return
  await api.delete(`/issues/${issueId.value}/relations`, { target_id: sprintId, type: 'sprint' })
  issue.value = { ...issue.value, sprint_ids: (issue.value.sprint_ids ?? []).filter(id => id !== sprintId) }
}

// IssueList ref
const childIssueListRef = ref<InstanceType<typeof IssueList> | null>(null)

const newIssueStore = useNewIssueStore()
watch(() => newIssueStore.trigger, () => {
  const ctx = newIssueStore.context
  if (ctx.projectId !== undefined && ctx.projectId !== projectId.value) return
  if (ctx.parentId !== undefined && ctx.parentId !== issueId.value) return
  if (issue.value && childLabel(issue.value.type) && childIssueListRef.value) {
    childIssueListRef.value.openCreate()
    return
  }
})

function onChildCreated(child: Issue) { children.value.push(child) }
function onChildUpdated(child: Issue) {
  const idx = children.value.findIndex(c => c.id === child.id)
  if (idx >= 0) children.value[idx] = child
}
function onChildDeleted(id: number) { children.value = children.value.filter(c => c.id !== id) }

const { showTypeIcon, showTypeText } = useIssueDisplay()
const authStore = useAuthStore()
// Per-project edit flag for the current user. Consumed by templates to
// hide edit affordances when the caller only has viewer access.
const canEditThisProject = computed(() => {
  const pid = issue.value?.project_id ?? projectId.value
  return authStore.canEdit(pid)
})
const validParents = computed(() => {
  const currentId = issue.value?.id
  const t = form.value.type
  if (t === 'epic') return []
  if (t === 'ticket') return projectIssues.value.filter(i => i.type === 'epic' && i.id !== currentId)
  if (t === 'task')   return projectIssues.value.filter(i => i.type === 'ticket' && i.id !== currentId)
  return projectIssues.value.filter(i => i.type === 'epic' && i.id !== currentId)
})

const typeChangeWarning = computed(() => {
  if (!issue.value || form.value.type === issue.value.type) return ''
  if (children.value.length > 0)
    return `This issue has ${children.value.length} child issue${children.value.length > 1 ? 's' : ''} — changing its type may break the hierarchy.`
  return ''
})

const childLabel = (type: string) =>
  type === 'epic' ? 'Tickets' : type === 'ticket' ? 'Tasks' : null

// ── Markdown / monospace preferences ─────────────────────────────────────────
const mdMode = ref(false)
function initMdModes() { mdMode.value = authStore.user?.markdown_default ?? false }
const isMonospace = computed(() => authStore.user?.monospace_fields ?? false)

const descriptionRef = computed(() => issue.value?.description ?? '')
const acRef          = computed(() => issue.value?.acceptance_criteria ?? '')
const notesRef       = computed(() => issue.value?.notes ?? '')
const { html: descHtml  } = useMarkdown(descriptionRef, mdMode)
const { html: acHtml    } = useMarkdown(acRef,          mdMode)
const { html: notesHtml } = useMarkdown(notesRef,       mdMode)

// PAI-146: AI text optimization. The composable manages availability,
// in-flight state, and the overlay slot; we just provide the per-field
// onAccept callback that writes the rewrite back into the form.
const aiOptimize = useAiOptimize()
function onOptimizeAccept(field: 'description' | 'acceptance_criteria' | 'notes') {
  return (text: string) => {
    form.value[field] = text
  }
}

// ── h/PT toggle + EUR calculations ───────────────────────────────────────────
const { unit: timeUnit, toggle: toggleTimeUnit, formatHours, label: timeLabel } = useTimeUnit()

const linkedBillingType = computed(() => {
  const i = issue.value
  if (!i || !i.cost_unit) return null
  if (i.type === 'cost_unit' || i.type === 'epic') return i.billing_type || null
  const cu = projectIssues.value.find(p => p.type === 'cost_unit' && p.title === i.cost_unit)
  return cu?.billing_type || null
})

function fmtDateTime(s: string): string {
  if (!s) return '—'
  const d = new Date(s.endsWith('Z') ? s : s + 'Z')
  return isNaN(d.getTime()) ? s : d.toLocaleString(undefined, { year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
}

// ── Aggregation (cost_unit / epic) ──────────────────────────────────────────
interface Aggregation {
  member_count: number
  estimate_hours: number | null; estimate_lp: number | null; estimate_eur: number | null
  ar_hours: number | null; ar_lp: number | null; ar_eur: number | null
  actual_hours: number | null; actual_internal_cost: number | null; margin_eur: number | null
}
const aggregation = ref<Aggregation | null>(null)
const aggLoading  = ref(false)
const isCostUnitOrEpic = computed(() => issue.value?.type === 'cost_unit' || issue.value?.type === 'epic')

async function loadAggregation() {
  if (!issueId.value || !isCostUnitOrEpic.value) return
  aggLoading.value = true
  try { aggregation.value = await api.get<Aggregation>(`/issues/${issueId.value}/aggregation`) }
  catch { aggregation.value = null }
  finally { aggLoading.value = false }
}

const BILLING_LABEL: Record<string, string> = { time_and_material: 'Time & Material', fixed_price: 'Fixed Price', mixed: 'Mixed' }

// ── History overlay ──────────────────────────────────────────────────────────
const historyOpen = ref(false)
async function openHistory() {
  historyOpen.value = true
  historyRef.value?.load()
}


async function cancelEdit() {
  if (pendingInlineUploads.value > 0) {
    const ok = await confirm({
      message: `An attachment upload is still in progress (${pendingInlineUploads.value}). Cancel anyway? Pending placeholders will be lost.`,
      confirmLabel: 'Cancel edit', danger: true,
    })
    if (!ok) return
  }
  editing.value = false
  resetForm()
  saveError.value = ''
  resetDetailDirty()
}
</script>

<template>
    <div v-if="loading" class="loading">Loading…</div>
    <template v-else-if="issue">

      <!-- Breadcrumb -->
      <Teleport defer to="#app-header-left">
        <RouterLink :to="`/projects/${projectId}`" class="ah-back">
          <AppIcon name="arrow-left" :size="13" />
          {{ project?.key ?? '…' }} Issues
        </RouterLink>
        <template v-if="parentIssue">
          <span class="ah-sep">/</span>
          <RouterLink :to="`/projects/${projectId}/issues/${parentIssue.id}`" class="ah-crumb">
            {{ parentIssue.issue_key }}
          </RouterLink>
        </template>
        <span class="ah-sep">/</span>
        <span class="ah-crumb ah-crumb--current">{{ issue.issue_key }}</span>
      </Teleport>

      <div class="issue-card">
        <!-- Header -->
        <div class="issue-header">
          <div class="issue-header-left">
            <div class="issue-subheader">
              <span class="issue-key-text">{{ issue.issue_key }}</span>
              <span class="subheader-sep">·</span>
              <span :class="`issue-type issue-type--${issue.type}`">
                <span v-if="showTypeIcon" v-html="TYPE_SVGS[issue.type] ?? ''"></span>
                <span v-if="showTypeText" class="type-label-text">{{ issue.type.charAt(0).toUpperCase() + issue.type.slice(1) }}</span>
              </span>
              <span class="subheader-sep">·</span>
              <span class="issue-status">
                <StatusDot :status="issue.status" />
                {{ STATUS_LABEL[issue.status] }}
              </span>
              <template v-if="issue.type !== 'sprint'">
                <span class="subheader-sep">·</span>
                <span class="issue-priority" :style="{ color: PRIORITY_COLOR[issue.priority] }">
                  <AppIcon :name="PRIORITY_ICON[issue.priority]" :size="11" :stroke-width="2.5" />
                  {{ PRIORITY_LABEL[issue.priority] }}
                </span>
              </template>
            </div>
            <h1 v-if="!editing" class="issue-title">{{ issue.title }}</h1>
            <input v-else v-model="form.title" class="title-input" type="text" />
          </div>

          <div class="issue-header-actions">
            <template v-if="!editing">
              <button v-if="authStore.user?.role === 'admin'" class="btn btn-danger" @click="deleteIssue">Delete</button>
              <button
                v-if="issue.type === 'epic' && issue.status !== 'done' && issue.status !== 'cancelled'"
                class="btn btn-ghost"
                @click="completeEpicRef?.show()"
              >Mark as Done</button>
              <button class="btn btn-ghost" :disabled="cloning" @click="cloneIssue">
                <AppIcon name="copy" :size="13" /> Clone
              </button>
              <button class="btn btn-ghost" @click="enterEditMode">Edit</button>
              <button class="btn btn-ghost" @click="router.push(`/projects/${projectId}`)">
                <AppIcon name="x" :size="13" /> Close
              </button>
            </template>
            <template v-else>
              <button v-if="authStore.user?.role === 'admin'" class="btn btn-danger" @click="deleteIssue">Delete</button>
              <button class="btn btn-ghost" :disabled="cloning" @click="cloneIssue">
                <AppIcon name="copy" :size="13" /> Clone
              </button>
              <button class="btn btn-ghost" @click="cancelEdit">Cancel</button>
              <button
                class="btn btn-primary"
                :class="{ 'btn--uploading': pendingInlineUploads > 0 }"
                :style="pendingInlineUploads > 0 ? `--upload-progress:${avgUploadProgress}%` : undefined"
                @click="save"
                :disabled="saving || pendingInlineUploads > 0"
              >
                {{ pendingInlineUploads > 0 ? `Uploading ${pendingInlineUploads}…` : (saving ? 'Saving…' : 'Save') }}
              </button>
            </template>
          </div>
        </div>

        <!-- Meta (view mode) -->
        <div class="meta-section">
          <IssueMetaGrid
            v-if="!editing"
            :issue="issue"
            :parent-issue="parentIssue"
            :project-id="projectId"
            :assigned-sprints="assignedSprints"
            :all-sprints="allSprints"
            :billing-label="BILLING_LABEL"
            :linked-billing-type="linkedBillingType"
            :time-label="timeLabel"
            :format-hours="formatHours"
            :toggle-time-unit="toggleTimeUnit"
            v-model:md-mode="mdMode"
            @remove-sprint="removeSprint"
            @toggle-sprint-dropdown="toggleSprintDropdown"
          >
            <template #sprint-dropdown>
              <Teleport to="body">
                <div v-if="sprintDropdownOpen && !editing" class="sprint-dropdown sprint-dropdown--teleported" :style="{ top: sprintDropdownPos.top + 'px', left: sprintDropdownPos.left + 'px' }">
                  <input
                    ref="sprintSearchRef"
                    v-model="sprintSearchQuery"
                    class="sprint-search"
                    placeholder="Search sprints…"
                    autocomplete="off"
                    @keydown.escape="sprintDropdownOpen = false"
                  />
                  <div class="sprint-list">
                    <div v-if="!availableSprintsFiltered.length" class="sprint-empty">No sprints found</div>
                    <button
                      v-for="s in availableSprintsFiltered" :key="s.id"
                      class="sprint-opt"
                      type="button"
                      @click="assignSprint(s)"
                    >
                      <span class="sprint-opt-title">{{ s.title }}</span>
                      <span v-if="s.sprint_state" :class="['sprint-opt-state', `sprint-opt-state--${s.sprint_state}`]">{{ s.sprint_state }}</span>
                      <span v-if="s.start_date" class="sprint-opt-dates">{{ s.start_date.slice(0,10) }}</span>
                    </button>
                  </div>
                </div>
              </Teleport>
            </template>
          </IssueMetaGrid>
        </div>

        <!-- Billing Summary -->
        <IssueBillingSummary
          v-if="isCostUnitOrEpic && !editing && aggregation"
          :aggregation="aggregation"
          :time-label="timeLabel"
          :format-hours="formatHours"
          :toggle-time-unit="toggleTimeUnit"
        />

        <!-- Time Entries -->
        <IssueTimeEntries ref="timeEntriesRef" :issue-id="issueId" />

        <!-- Body (view mode) -->
        <div class="body-section" v-if="!editing">
          <div class="body-block">
            <p class="body-label">Description</p>
            <div v-if="issue.description"
              :class="['body-text', { 'body-text--mono': isMonospace, 'md-rendered': mdMode }]"
              v-html="descHtml"
            />
            <span v-else class="body-empty">—</span>
          </div>
          <div class="body-block" v-if="['epic','cost_unit','ticket'].includes(issue.type)">
            <p class="body-label">Acceptance Criteria</p>
            <div v-if="issue.acceptance_criteria"
              :class="['body-text', { 'body-text--mono': isMonospace, 'md-rendered': mdMode }]"
              v-html="acHtml"
            />
            <span v-else class="body-empty">—</span>
          </div>
          <div class="body-block">
            <p class="body-label">Notes</p>
            <div v-if="issue.notes"
              :class="['body-text', { 'body-text--mono': isMonospace, 'md-rendered': mdMode }]"
              v-html="notesHtml"
            />
            <span v-else class="body-empty">—</span>
          </div>
          <div v-if="!issue.description && !issue.notes && !(issue.acceptance_criteria && ['epic','cost_unit','ticket'].includes(issue.type))" class="body-empty">
            No description or notes.
          </div>
        </div>

        <!-- Edit layout -->
        <div v-else class="edit-layout">
          <div class="edit-content">
            <!-- PAI-146: surface AI optimize failures inline so the user
                 knows why the spinner stopped without a successful overlay.
                 One banner for the whole edit pane — a single optimize
                 call is in flight at a time. -->
            <div v-if="aiOptimize.lastError" class="ai-error-banner">
              <span>AI optimization failed: {{ aiOptimize.lastError }}</span>
              <button type="button" class="ai-error-banner-x" @click="aiOptimize.clearError()">×</button>
            </div>
            <div class="field">
              <div class="field-label-row">
                <label>Description</label>
                <AiOptimizeButton
                  field="description"
                  field-label="Description"
                  :issue-id="issueId"
                  :text="() => form.description"
                  :on-accept="onOptimizeAccept('description')"
                />
              </div>
              <div v-if="jobsFor('description').length" class="upload-chips">
                <div
                  v-for="job in jobsFor('description')"
                  :key="job.seq"
                  class="upload-chip"
                  :class="[`upload-chip--${job.status}`]"
                >
                  <AppIcon
                    :name="job.status === 'failed' ? 'alert-circle' : job.status === 'done' ? 'check' : (job.isImage ? 'image' : 'paperclip')"
                    :size="13"
                  />
                  <span class="upload-chip__name" :title="job.filename">{{ job.filename }}</span>
                  <template v-if="job.status === 'pending'">
                    <div class="upload-chip__bar">
                      <div class="upload-chip__bar-fill" :style="{ width: job.progress + '%' }"></div>
                    </div>
                    <span class="upload-chip__pct">{{ job.progress }}%</span>
                  </template>
                  <span v-else-if="job.status === 'failed'" class="upload-chip__error" :title="job.error">{{ job.error }}</span>
                  <button v-if="job.status === 'failed'" class="upload-chip__btn" @click="retryUpload(job)" title="Retry upload" type="button">
                    <AppIcon name="refresh-cw" :size="12" />
                  </button>
                  <button v-if="job.status !== 'done'" class="upload-chip__btn" @click="dismissJob(job)" title="Dismiss" type="button">
                    <AppIcon name="x" :size="12" />
                  </button>
                </div>
              </div>
              <div class="textarea-drop-wrap" @dragenter.prevent="attachmentsEnabled ? (descDragOver = true) : null" @dragleave.self="descDragOver = false">
                <textarea
                  ref="descTextarea"
                  v-auto-grow
                  v-model="form.description" rows="4"
                  :class="{ 'textarea--mono': isMonospace }"
                  placeholder="What needs to be done?"
                  @paste="(e: ClipboardEvent) => onTextareaPaste(e, $refs.descTextarea as HTMLTextAreaElement, 'description')"
                  @dragover.prevent
                  @drop.prevent="clearAllDragOver(); onTextareaDrop($event as DragEvent, $refs.descTextarea as HTMLTextAreaElement, 'description')"
                ></textarea>
                <div v-if="descDragOver && attachmentsEnabled" class="textarea-drop-overlay">
                  <AppIcon name="upload" :size="20" /> Drop files here
                </div>
              </div>
            </div>
            <div class="field" v-if="['epic','cost_unit','ticket'].includes(form.type)">
              <div class="field-label-row">
                <label>Acceptance Criteria</label>
                <AiOptimizeButton
                  field="acceptance_criteria"
                  field-label="Acceptance Criteria"
                  :issue-id="issueId"
                  :text="() => form.acceptance_criteria"
                  :on-accept="onOptimizeAccept('acceptance_criteria')"
                />
              </div>
              <div v-if="jobsFor('acceptance_criteria').length" class="upload-chips">
                <div
                  v-for="job in jobsFor('acceptance_criteria')"
                  :key="job.seq"
                  class="upload-chip"
                  :class="[`upload-chip--${job.status}`]"
                >
                  <AppIcon
                    :name="job.status === 'failed' ? 'alert-circle' : job.status === 'done' ? 'check' : (job.isImage ? 'image' : 'paperclip')"
                    :size="13"
                  />
                  <span class="upload-chip__name" :title="job.filename">{{ job.filename }}</span>
                  <template v-if="job.status === 'pending'">
                    <div class="upload-chip__bar">
                      <div class="upload-chip__bar-fill" :style="{ width: job.progress + '%' }"></div>
                    </div>
                    <span class="upload-chip__pct">{{ job.progress }}%</span>
                  </template>
                  <span v-else-if="job.status === 'failed'" class="upload-chip__error" :title="job.error">{{ job.error }}</span>
                  <button v-if="job.status === 'failed'" class="upload-chip__btn" @click="retryUpload(job)" title="Retry upload" type="button">
                    <AppIcon name="refresh-cw" :size="12" />
                  </button>
                  <button v-if="job.status !== 'done'" class="upload-chip__btn" @click="dismissJob(job)" title="Dismiss" type="button">
                    <AppIcon name="x" :size="12" />
                  </button>
                </div>
              </div>
              <div class="textarea-drop-wrap" @dragenter.prevent="attachmentsEnabled ? (acDragOver = true) : null" @dragleave.self="acDragOver = false">
                <textarea
                  ref="acTextarea"
                  v-auto-grow
                  v-model="form.acceptance_criteria" rows="4"
                  :class="{ 'textarea--mono': isMonospace }"
                  placeholder="When is this done?"
                  @paste="(e: ClipboardEvent) => onTextareaPaste(e, $refs.acTextarea as HTMLTextAreaElement, 'acceptance_criteria')"
                  @dragover.prevent
                  @drop.prevent="clearAllDragOver(); onTextareaDrop($event as DragEvent, $refs.acTextarea as HTMLTextAreaElement, 'acceptance_criteria')"
                ></textarea>
                <div v-if="acDragOver && attachmentsEnabled" class="textarea-drop-overlay">
                  <AppIcon name="upload" :size="20" /> Drop files here
                </div>
              </div>
            </div>
            <div class="field">
              <div class="field-label-row">
                <label>Notes</label>
                <AiOptimizeButton
                  field="notes"
                  field-label="Notes"
                  :issue-id="issueId"
                  :text="() => form.notes"
                  :on-accept="onOptimizeAccept('notes')"
                />
              </div>
              <textarea
                v-auto-grow
                v-model="form.notes" rows="3"
                :class="{ 'textarea--mono': isMonospace }"
                placeholder="Additional context, links, etc."
              ></textarea>
            </div>
          </div>

          <IssueEditSidebar
            :form="form"
            :issue-type="issue.type"
            :cost-units="costUnits"
            :releases="releases"
            :all-tags="allTags"
            :issue-tag-ids="issueTagIds"
            :valid-parents="validParents"
            :users="users"
            :assigned-sprints="assignedSprints"
            :type-change-warning="typeChangeWarning"
            :linked-billing-type="linkedBillingType"
            :time-unit="timeUnit"
            :time-label="timeLabel"
            :toggle-time-unit="toggleTimeUnit"
            :saving="saving || pendingInlineUploads > 0"
            :save-error="saveError"
            @save="save"
            @cancel="cancelEdit"
            @add-tag="addTag"
            @remove-tag="removeTag"
            @remove-sprint="removeSprint"
            @toggle-sprint-dropdown="toggleSprintDropdown"
          >
            <template #sprint-dropdown>
              <Teleport to="body">
                <div v-if="sprintDropdownOpen && editing" class="sprint-dropdown sprint-dropdown--teleported" :style="{ top: sprintDropdownPos.top + 'px', left: sprintDropdownPos.left + 'px' }">
                  <input
                    ref="sprintSearchRef"
                    v-model="sprintSearchQuery"
                    class="sprint-search"
                    placeholder="Search sprints…"
                    autocomplete="off"
                    @keydown.escape="sprintDropdownOpen = false"
                  />
                  <div class="sprint-list">
                    <div v-if="!availableSprintsFiltered.length" class="sprint-empty">No sprints found</div>
                    <button
                      v-for="s in availableSprintsFiltered" :key="s.id"
                      class="sprint-opt"
                      type="button"
                      @click="assignSprint(s)"
                    >
                      <span class="sprint-opt-title">{{ s.title }}</span>
                      <span v-if="s.sprint_state" :class="['sprint-opt-state', `sprint-opt-state--${s.sprint_state}`]">{{ s.sprint_state }}</span>
                      <span v-if="s.start_date" class="sprint-opt-dates">{{ s.start_date.slice(0,10) }}</span>
                    </button>
                  </div>
                </div>
              </Teleport>
            </template>
          </IssueEditSidebar>
        </div>
      </div>

      <!-- Children section -->
      <div v-if="childLabel(issue.type)" class="children-section">
        <IssueList
          ref="childIssueListRef"
          :project-id="projectId"
          :issues="children"
          :project-all-issues="projectIssues"
          :initial-type="issue.type === 'epic' ? 'ticket' : 'task'"
          :default-parent-id="issueId"
          compact
          :title="childLabel(issue.type)!"
          @created="onChildCreated"
          @updated="onChildUpdated"
          @deleted="onChildDeleted"
          @cost-unit-added="(v: string) => { if (!costUnits.includes(v)) costUnits = [...costUnits, v].sort((a,b)=>a.localeCompare(b)) }"
          @release-added="(v: string) => { if (!releases.includes(v)) releases = [...releases, v].sort((a,b)=>a.localeCompare(b)) }"
        />
      </div>

      <!-- Group / Sprint panel -->
      <IssueGroupMembers
        ref="groupMembersRef"
        :issue-id="issueId"
        :issue-type="issue.type"
        :project-id="projectId"
      />

      <!-- Issue Relations -->
      <IssueRelations
        v-if="issue.type === 'ticket' || issue.type === 'task' || issue.type === 'epic'"
        ref="relationsRef"
        :issue-id="issueId"
        :project-id="projectId"
        :project-issues="projectIssues"
      />

      <!-- Attachments -->
      <IssueAttachments ref="attachmentsRef" :issue-id="issueId" />

      <!-- Comments -->
      <IssueComments
        ref="commentsRef"
        :issue-id="issueId"
        :md-mode="mdMode"
        :is-monospace="isMonospace"
      />

    <!-- Footer -->
    <footer class="issue-footer">
      <span class="issue-footer-item">
        Last edited <strong>{{ fmtDateTime(issue.updated_at) }}</strong>
        <template v-if="issue.last_changed_by_name"> by {{ issue.last_changed_by_name }}</template>
      </span>
      <span class="issue-footer-sep">·</span>
      <span class="issue-footer-item">
        Created <strong>{{ fmtDateTime(issue.created_at) }}</strong>
        <template v-if="issue.created_by_name"> by {{ issue.created_by_name }}</template>
      </span>
      <template v-if="issue.assignee?.username">
        <span class="issue-footer-sep">·</span>
        <span class="issue-footer-item">Assigned to <strong>{{ issue.assignee.username }}</strong></span>
      </template>
      <span class="issue-footer-spacer"></span>
      <button class="history-btn" @click="openHistory">
        <AppIcon name="history" :size="13" />
        History
      </button>
    </footer>

    </template>
    <AppFooter v-else />

    <!-- History overlay -->
    <IssueHistory ref="historyRef" :issue-id="issueId" :open="historyOpen" @close="historyOpen = false" />

  <IssueCompleteEpicModal
    ref="completeEpicRef"
    :issue-id="issueId"
    :issue-key="issue?.issue_key ?? ''"
    :children="children"
    @completed="onEpicCompleted"
  />

  <!-- PAI-146: AI optimize preview overlay. Mounted once for the page;
       the composable is a singleton so all three field buttons share
       this slot. v-if (not v-show) so the diff DP is only computed
       when the overlay is actually open. -->
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
.loading { color: var(--text-muted); padding: 2rem 0; }

/* PAI-147: per-field label row holds the label + the AI optimize
   button on the right. Existing field labels were a bare <label>; the
   wrapper keeps that semantic but lets the button share the row. */
.field-label-row {
  display: flex; align-items: center; justify-content: space-between;
  gap: .5rem;
  margin-bottom: .25rem;
}
.field-label-row > label { margin-bottom: 0; }

/* PAI-146: inline error banner for failed optimize calls. Sits at the
   top of the edit pane so the user sees it whichever field they hit. */
.ai-error-banner {
  display: flex; justify-content: space-between; align-items: center;
  gap: .5rem;
  background: #fef2f2; color: #b91c1c;
  border: 1px solid #fecaca; border-radius: var(--radius);
  padding: .45rem .75rem;
  font-size: 13px;
  margin-bottom: .75rem;
}
.ai-error-banner-x {
  background: none; border: none; color: #b91c1c;
  cursor: pointer; font-size: 16px; line-height: 1; padding: 0 .25rem;
}
.ai-error-banner-x:hover { color: #7f1d1d; }

.issue-card {
  background: var(--bg-card); border: 1px solid var(--border);
  border-radius: 8px; box-shadow: var(--shadow); overflow: visible;
}

.issue-header {
  display: flex; align-items: flex-start; justify-content: space-between;
  gap: 1rem; padding: 1.25rem 1.5rem; border-bottom: 1px solid var(--border);
}
.issue-header-left { display: flex; flex-direction: column; gap: .4rem; flex: 1; min-width: 0; }
.issue-subheader { display: flex; align-items: center; gap: .45rem; flex-wrap: wrap; }
.issue-key-text {
  font-size: 12px; font-weight: 700; letter-spacing: .05em;
  font-family: 'DM Mono', monospace; color: var(--text-muted);
  white-space: nowrap; flex-shrink: 0;
}
.subheader-sep { font-size: 11px; color: var(--border); user-select: none; }
.type-label-text { font-size: 12px; }
.issue-title { font-size: 18px; font-weight: 700; color: var(--text); line-height: 1.3; }
.title-input { font-size: 16px; font-weight: 600; flex: 1; min-width: 200px; }
.issue-header-actions { display: flex; align-items: center; gap: .4rem; flex-shrink: 0; padding-top: .1rem; }

.meta-section { padding: .9rem 1.5rem; border-bottom: 1px solid var(--border); background: var(--bg); }

/* Sprint dropdown (teleported to body, not scoped) */
.sprint-dropdown {
  position: absolute; top: calc(100% + 4px); left: 0; z-index: 300;
  background: var(--bg-card); border: 1px solid var(--border);
  border-radius: 8px; box-shadow: var(--shadow-md);
  width: 220px; display: flex; flex-direction: column;
}
.sprint-dropdown--teleported { position: fixed; z-index: 9000; }
.sprint-search {
  border: none; border-bottom: 1px solid var(--border);
  padding: .5rem .75rem; font-size: 13px; font-family: inherit;
  outline: none; background: transparent; color: var(--text);
  border-radius: 8px 8px 0 0;
}
.sprint-list { max-height: 220px; overflow-y: auto; }
.sprint-empty { padding: .65rem .75rem; font-size: 13px; color: var(--text-muted); }
.sprint-opt {
  display: flex; align-items: center; justify-content: space-between;
  width: 100%; padding: .45rem .75rem; font-size: 13px;
  background: none; border: none; cursor: pointer; font-family: inherit;
  color: var(--text); text-align: left; transition: background .1s;
}
.sprint-opt:hover { background: #f0f2f4; }
.sprint-opt-title { font-weight: 500; }
.sprint-opt-dates { font-size: 11px; color: var(--text-muted); }
.sprint-opt-state {
  font-size: 9px; font-weight: 700; text-transform: uppercase; letter-spacing: .04em;
  border-radius: 3px; padding: 0 .25rem;
}
.sprint-opt-state--active   { background: #fff3e0; color: #b45309; }
.sprint-opt-state--planned  { background: #f3f4f6; color: #6b7280; }
.sprint-opt-state--complete { background: #dcfce7; color: #166534; }
.sprint-opt-state--archived { background: #e5e7eb; color: #6b7280; }

/* Body (view mode) */
.body-section { padding: 1.5rem; display: flex; flex-direction: column; gap: 1.25rem; }
.body-label { font-size: 11px; font-weight: 700; text-transform: uppercase; letter-spacing: .06em; color: var(--text-muted); margin-bottom: .4rem; }
.body-text  { font-size: 14px; color: var(--text); line-height: 1.7; white-space: pre-wrap; }
.body-empty { font-size: 13px; color: var(--text-muted); font-style: italic; }
.body-text--mono { font-family: 'DM Mono', 'Menlo', monospace; font-size: 13px; }

/* Edit layout */
.edit-layout {
  display: grid;
  grid-template-columns: 1fr 280px;
  gap: 0;
  min-height: 0;
}
.edit-content {
  padding: 1.5rem;
  display: flex; flex-direction: column; gap: 1rem;
  border-right: 1px solid var(--border);
}

.children-section { margin-top: 1.5rem; }

/* Issue footer */
.issue-footer {
  display: flex; align-items: center; gap: .5rem; flex-wrap: wrap;
  padding: 1.25rem 0 .5rem; margin-top: 2rem;
  border-top: 1px solid var(--border);
  font-size: 12px; color: var(--text-muted);
}
.issue-footer strong { color: var(--text); font-weight: 600; }
.issue-footer-sep { color: var(--border); }
.issue-footer-item { display: flex; align-items: center; gap: .3rem; }
.issue-footer-spacer { flex: 1; }
.history-btn {
  display: inline-flex; align-items: center; gap: .35rem;
  font-size: 11px; font-weight: 600; color: var(--text-muted);
  background: none; border: 1px solid var(--border);
  border-radius: var(--radius); padding: .25rem .6rem;
  cursor: pointer; font-family: inherit; transition: color .12s, border-color .12s;
}
.history-btn:hover { color: var(--bp-blue); border-color: var(--bp-blue); }

.field { display: flex; flex-direction: column; gap: .35rem; }
.field label { font-size: 11px; font-weight: 700; color: var(--text-muted); text-transform: uppercase; letter-spacing: .06em; }
.form-error { font-size: 13px; color: #c0392b; background: #fde8e8; padding: .5rem .75rem; border-radius: var(--radius); }
textarea { resize: vertical; min-height: 80px; }
.textarea--mono { font-family: 'DM Mono', 'Menlo', monospace !important; font-size: 13px; }

/* Textarea drop overlay */
.textarea-drop-wrap { position: relative; }
.textarea-drop-overlay {
  position: absolute; inset: 0; z-index: 5;
  display: flex; align-items: center; justify-content: center; gap: .5rem;
  background: rgba(46,109,164,.08);
  border: 2px dashed var(--bp-blue);
  border-radius: var(--radius);
  color: var(--bp-blue);
  font-size: 13px; font-weight: 600;
  pointer-events: none;
  animation: drop-overlay-in 140ms cubic-bezier(.2,.7,.2,1);
}
@keyframes drop-overlay-in {
  from { opacity: 0; transform: scale(.985); }
  to   { opacity: 1; transform: scale(1); }
}

/* Inline upload chips */
.upload-chips {
  display: flex;
  flex-direction: column;
  gap: .3rem;
  margin-bottom: .15rem;
}
.upload-chip {
  display: flex;
  align-items: center;
  gap: .55rem;
  padding: .38rem .55rem;
  background: var(--bg-card, #fff);
  border: 1px solid var(--border);
  border-radius: calc(var(--radius) - 2px);
  font-size: 12px;
  color: var(--text);
  font-weight: 500;
  line-height: 1;
  animation: upload-chip-in 180ms cubic-bezier(.2,.7,.2,1);
}
@keyframes upload-chip-in {
  from { opacity: 0; transform: translateY(-3px); }
  to   { opacity: 1; transform: translateY(0); }
}
.upload-chip--pending {
  border-color: rgba(46,109,164,.28);
  background: linear-gradient(180deg, rgba(46,109,164,.05), rgba(46,109,164,.02));
}
.upload-chip--done {
  border-color: rgba(30,132,73,.35);
  background: rgba(30,132,73,.06);
  color: #1e7a3a;
  transition: opacity .5s ease;
  animation: upload-chip-out-delayed 1.5s forwards;
}
@keyframes upload-chip-out-delayed {
  0%, 70% { opacity: 1; }
  100%    { opacity: 0; transform: translateY(-2px); }
}
.upload-chip--failed {
  border-color: rgba(192,57,43,.4);
  background: #fdeeec;
  color: #a02b1c;
}
.upload-chip__name {
  flex: 0 1 auto;
  min-width: 0;
  max-width: 260px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  font-variant-numeric: tabular-nums;
}
.upload-chip__bar {
  flex: 1 1 auto;
  min-width: 60px;
  max-width: 220px;
  height: 4px;
  background: rgba(46,109,164,.12);
  border-radius: 999px;
  overflow: hidden;
  position: relative;
}
.upload-chip__bar-fill {
  position: absolute; top: 0; bottom: 0; left: 0;
  background: var(--bp-blue, #2e6da4);
  border-radius: 999px;
  width: 0;
  transition: width 140ms linear;
}
.upload-chip__bar-fill::after {
  content: '';
  position: absolute; inset: 0;
  background: linear-gradient(90deg, transparent, rgba(255,255,255,.6), transparent);
  animation: upload-chip-shimmer 1.1s linear infinite;
}
@keyframes upload-chip-shimmer {
  0%   { transform: translateX(-100%); }
  100% { transform: translateX(100%); }
}
.upload-chip__pct {
  font-size: 11px;
  color: var(--text-muted);
  font-variant-numeric: tabular-nums;
  min-width: 32px;
  text-align: right;
}
.upload-chip__error {
  flex: 1 1 auto;
  min-width: 0;
  font-size: 11px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  color: #a02b1c;
}
.upload-chip__btn {
  display: inline-flex; align-items: center; justify-content: center;
  background: transparent;
  border: none;
  color: inherit;
  opacity: .6;
  cursor: pointer;
  padding: 2px;
  border-radius: 4px;
  transition: opacity .12s, background .12s;
}
.upload-chip__btn:hover { opacity: 1; background: rgba(0,0,0,.06); }
.upload-chip--failed .upload-chip__btn:hover { background: rgba(160,43,28,.1); }

/* Primary save button — progress bar along bottom edge while uploading */
.btn--uploading {
  position: relative;
  overflow: hidden;
}
.btn--uploading::after {
  content: '';
  position: absolute;
  left: 0;
  bottom: 0;
  height: 2px;
  width: var(--upload-progress, 0%);
  background: rgba(255,255,255,.85);
  transition: width 140ms linear;
  border-radius: 0 0 var(--radius) var(--radius);
}
</style>
