<script setup lang="ts">
import LoadingText from "@/components/LoadingText.vue";
import { ref, onMounted, onUnmounted, computed, nextTick, watch } from 'vue'
import { useRoute, useRouter, RouterLink } from 'vue-router'

import AppModal from '@/components/AppModal.vue'
import IssueList from '@/components/IssueList.vue'
import TagChip from '@/components/TagChip.vue'
import TagSelector from '@/components/TagSelector.vue'
import MetaSelect from '@/components/MetaSelect.vue'
import type { MetaOption } from '@/components/MetaSelect.vue'
import ImportCollisionModal from '@/components/ImportCollisionModal.vue'
import type { PreflightResult, CollisionStrategy } from '@/components/ImportCollisionModal.vue'
import { errMsg } from '@/api/client'
import { MAX_IMAGE_SIZE } from '@/utils/constants'
import { useAuthStore } from '@/stores/auth'
import { useSearchStore } from '@/stores/search'
import { useIssueRefreshPromptStore } from '@/stores/issueRefreshPrompt'
import AppIcon from '@/components/AppIcon.vue'
import { useConfirm } from '@/composables/useConfirm'
import { provideIssueContext } from '@/composables/useIssueContext'
import { useFreshness } from '@/composables/useFreshness'
import {
  buildProjectUpdatePayload,
  emptyProjectEditForm,
  inheritedProjectRateHint,
  projectToEditForm,
} from '@/config/projectDetailEdit'
import { buildProjectPurgePayload, emptyProjectPurgeForm } from '@/config/projectPurge'
import type { Tag, Issue, Project, User, SavedView, Sprint, Customer } from '@/types'
import { buildProjectDisplayTabs } from '@/config/projectDefaultViews'
import {
  addProjectTag as addProjectTagRequest,
  buildProjectIssuesUrl,
  buildProjectCsvExportUrl,
  deleteProjectDetail,
  deleteProjectLogo,
  executeProjectTimeEntryPurge,
  loadProjectDetailData,
  loadProjectIssues,
  loadProjectPurgeUsers,
  preflightProjectCsvImport,
  previewProjectTimeEntryPurge,
  refreshProjectViews,
  removeProjectTag as removeProjectTagRequest,
  runProjectCsvImport,
  saveProjectDetail,
  setProjectStatus,
  uploadProjectLogo,
} from '@/services/projectDetail'
import DocumentsSection from '@/components/customer/DocumentsSection.vue'
import CooperationSection from '@/components/customer/CooperationSection.vue'
import ProjectContextSection from '@/components/project/ProjectContextSection.vue'
// PAI-146 expansion: AI optimize on the project description.
// project_description is its own field name (not aliased to
// "description") so the prompt reminder fits a stakeholder audience.
import AiActionMenu from '@/components/ai/AiActionMenu.vue'
import AiSurfaceFeedback from '@/components/ai/AiSurfaceFeedback.vue'
import {
  notifySidePanelOpened,
  onOtherSidePanelOpened,
} from '@/composables/useSidePanelExclusion'

const { confirm } = useConfirm()
const PROJECT_STATUS_OPTIONS: MetaOption[] = [
  { value: 'active',   label: 'Active'   },
  { value: 'archived', label: 'Archived' },
  { value: 'deleted',  label: 'Deleted'  },
]

const route     = useRoute()
const router    = useRouter()
const auth      = useAuthStore()
const search    = useSearchStore()
const issueRefreshPrompt = useIssueRefreshPromptStore()
const isAdmin   = computed(() => auth.user?.role === 'admin')
const projectId = computed(() => Number(route.params.id))
// Whether the current user can edit inside this project — admins and
// project editors pass, viewers fall through to read-only rendering.
const canEditProject = computed(() => auth.canEdit(projectId.value))
const initialPanelIssueId = computed(() => {
  const p = route.query.panel
  return p ? Number(p) : undefined
})

const project   = ref<Project | null>(null)
const issues    = ref<Issue[]>([])
const users     = ref<User[]>([])
const allTags   = ref<Tag[]>([])
const costUnits = ref<string[]>([])
const releases  = ref<string[]>([])
const sprints   = ref<Sprint[]>([])
// PAI-58/59. Loaded once on mount; powers the customer assignment
// dropdown in the edit modal and the inherited-rate hints.
const customers = ref<Customer[]>([])

// PAI-145. Documents and Cooperation panes don't belong inline in
// the project body — they were always-visible and shouted empty-state
// at the user. They now live in a slide-from-right aux panel
// (ProjectAuxPanel) toggled from buttons in the IssueList toolbar
// (next to Tree/Flat). Independent toggles, not tabs — but at most one
// aux panel is shown at a time because they share the right-edge slot
// with IssueSidePanel.
// PAI-178: 'context' joins docs/cooperation as a third aux panel.
// Project context (repos + manifest) used to render full-width
// above the issue tabs which crowded the page even on projects
// that don't use anchors. Now it's behind a toggle that lives in
// the same toolbar cluster as Docs / Coop.
type ProjectWorkspace = 'docs' | 'cooperation' | 'context' | null
const workspacePanel = ref<ProjectWorkspace>(null)
const docCount = ref(0)
const cooperationPopulated = ref(false)
const contextSummary = ref({ repoCount: 0, hasManifest: false, populated: false })

provideIssueContext({ users, allTags, costUnits, releases, projects: ref([]), sprints })

const loading       = ref(true)
const issueListRef  = ref<InstanceType<typeof IssueList> | null>(null)
const exporting     = ref(false)
const projectIssueQuery = computed(() => search.scope === 'project' ? search.query : '')
const issueListPath = computed(() => buildProjectIssuesUrl(projectId.value, projectIssueQuery.value))
const trimmedProjectIssueQuery = computed(() => projectIssueQuery.value.trim())
let projectLoadRequestSeq = 0
let projectIssueRequestSeq = 0
let projectSearchTimer: ReturnType<typeof setTimeout> | null = null

// PAI-246: header ⋯ menu (Export / Import / Edit project).
// PAI-265: panel is teleported to <body> with position:fixed because the
// teleport-target #app-header-right has `overflow:hidden` (load-bearing for
// header truncation), which clips an absolute-positioned panel inside it.
const overflowOpen = ref(false)
const overflowTriggerRef = ref<HTMLElement | null>(null)
const overflowPanelRef = ref<HTMLElement | null>(null)
const overflowPanelStyle = ref<{ top: string; right: string }>({ top: '0px', right: '0px' })
function recomputeOverflowPosition() {
  const el = overflowTriggerRef.value
  if (!el) return
  const r = el.getBoundingClientRect()
  overflowPanelStyle.value = {
    top: `${r.bottom + 6}px`,
    right: `${window.innerWidth - r.right}px`,
  }
}
function closeOverflow() { overflowOpen.value = false }
function toggleOverflow() {
  overflowOpen.value = !overflowOpen.value
  if (overflowOpen.value) void nextTick(recomputeOverflowPosition)
}
function onOverflowOutsideClick(e: MouseEvent) {
  if (!overflowOpen.value) return
  const target = e.target as Node
  const inTrigger = overflowTriggerRef.value?.contains(target) ?? false
  const inPanel = overflowPanelRef.value?.contains(target) ?? false
  if (!inTrigger && !inPanel) closeOverflow()
}
function onOverflowKey(e: KeyboardEvent) {
  if (e.key === 'Escape' && overflowOpen.value) closeOverflow()
}
function onMenuExport() { closeOverflow(); void exportCSV() }
function onMenuImport() { closeOverflow(); triggerImport() }
function onMenuEdit() { closeOverflow(); openEdit() }

// ── Admin-default views as tabs ───────────────────────────────────────────────

// Synthetic fallback views used when no admin-default views exist in the DB.
// Negative IDs ensure they never collide with real DB rows.
const allViews    = ref<SavedView[]>([])
const activeTabId = ref<number | null>(null)

// Tabs: admin-default (not hidden, or user-pinned) + pinned personal views
const displayTabs = computed(() => buildProjectDisplayTabs(allViews.value))

async function selectTab(view: SavedView) {
  const isReclick = activeTabId.value === view.id
  activeTabId.value = view.id
  // Re-fetch issues on tab switch or re-click
  if (await replaceProjectIssues(projectIssueQuery.value)) {
    nextTick(() => issueListRef.value?.applyView(view))
  }
}

function onViewApplied(viewId: number) {
  activeTabId.value = viewId
}

async function refreshViews() {
  try { allViews.value = await refreshProjectViews() } catch { /* ignore */ }
}

const contextStatusLabel = computed(() =>
  contextSummary.value.populated ? 'Configured' : 'Not set up',
)

function resetWorkspaceState() {
  workspacePanel.value = null
  docCount.value = 0
  cooperationPopulated.value = false
  contextSummary.value = { repoCount: 0, hasManifest: false, populated: false }
}

function toggleWorkspace(panel: Exclude<ProjectWorkspace, null>) {
  workspacePanel.value = workspacePanel.value === panel ? null : panel
  if (workspacePanel.value !== null) notifySidePanelOpened('aux')
}

let unbindAuxPanelExclusion: (() => void) | null = null
onMounted(() => {
  unbindAuxPanelExclusion = onOtherSidePanelOpened('aux', () => {
    workspacePanel.value = null
  })
  document.addEventListener('mousedown', onOverflowOutsideClick)
  document.addEventListener('keydown', onOverflowKey)
  // PAI-265: keep teleported panel anchored to trigger across viewport changes.
  // Capture phase catches scrolls inside nested scrollers (.main-content) too.
  window.addEventListener('resize', recomputeOverflowPosition)
  window.addEventListener('scroll', recomputeOverflowPosition, true)
})
onUnmounted(() => {
  unbindAuxPanelExclusion?.()
  unbindAuxPanelExclusion = null
  document.removeEventListener('mousedown', onOverflowOutsideClick)
  document.removeEventListener('keydown', onOverflowKey)
  window.removeEventListener('resize', recomputeOverflowPosition)
  window.removeEventListener('scroll', recomputeOverflowPosition, true)
  if (projectSearchTimer) clearTimeout(projectSearchTimer)
  issueRefreshPrompt.clear(refreshProjectIssueListFromHeader)
})

function updateContextSummary(payload: { repoCount: number; hasManifest: boolean; populated: boolean }) {
  contextSummary.value = payload
}

const workspaceSummary = computed(() => ({
  docs: docCount.value > 0 ? `${docCount.value} repo file${docCount.value === 1 ? '' : 's'}` : 'No docs yet',
  cooperation: cooperationPopulated.value ? 'Profile configured' : 'Not set up',
  context: contextSummary.value.populated
    ? `${contextSummary.value.repoCount} repo${contextSummary.value.repoCount === 1 ? '' : 's'} · ${contextSummary.value.hasManifest ? 'manifest set' : 'manifest empty'}`
    : 'Not set up',
}))

// Edit project
const showEdit  = ref(false)
const editForm  = ref(emptyProjectEditForm())
const editError = ref('')
const saving    = ref(false)

// PAI-146 expansion: AI optimize composable + onAccept handler for
// the project description. The edit modal is admin-gated, so the
// button only appears for users who already have edit rights here.
function onProjectDescriptionAccept(text: string) {
  editForm.value.description = text
}
async function applyProjectAiResult(info: any) {
  if (info.action === 'ui_generation') {
    const spec = String(info.body?.spec_markdown ?? '')
    editForm.value.description = info.intent === 'replace-description'
      ? spec
      : [editForm.value.description, spec].filter(Boolean).join('\n\n')
    return
  }
  if (info.action === 'suggest_enhancement') {
    const lines = (info.selection ?? []).map((idx: number) => info.body?.suggestions?.[idx]).filter(Boolean).map((it: any) => `- ${it.title}: ${it.body}`)
    editForm.value.description = [editForm.value.description, lines.join('\n')].filter(Boolean).join('\n\n')
    return
  }
  if (info.action === 'spec_out') {
    const lines = (info.selection ?? []).map((idx: number) => info.body?.items?.[idx]?.text).filter(Boolean).map((text: string) => `- ${text}`)
    editForm.value.description = [editForm.value.description, lines.join('\n')].filter(Boolean).join('\n\n')
  }
}

// Logo upload
const logoInputRef   = ref<HTMLInputElement | null>(null)
const logoUploading  = ref(false)
const logoError      = ref('')

async function uploadLogo(e: Event) {
  const file = (e.target as HTMLInputElement).files?.[0]
  if (!file) return
  if (file.size > MAX_IMAGE_SIZE) { logoError.value = 'Image must be smaller than 3 MB.'; return }
  logoError.value = ''
  logoUploading.value = true
  try {
    project.value = await uploadProjectLogo(projectId.value, file)
  } catch (ex: unknown) {
    logoError.value = errMsg(ex, 'Upload failed.')
  } finally {
    logoUploading.value = false
    if (logoInputRef.value) logoInputRef.value.value = ''
  }
}

async function deleteLogo() {
  logoError.value = ''
  try {
    project.value = await deleteProjectLogo(projectId.value)
  } catch (ex: unknown) {
    logoError.value = errMsg(ex, 'Failed to remove logo.')
  }
}

const projectTagIds = computed(() => project.value?.tags.map(t => t.id) ?? [])

function openEdit() {
  if (!project.value) return
  editForm.value = projectToEditForm(project.value)
  editError.value = ''
  showEdit.value = true
}

async function saveProject() {
  editError.value = ''
  if (!editForm.value.name.trim()) { editError.value = 'Name required.'; return }
  saving.value = true
  try {
    const payload = buildProjectUpdatePayload(editForm.value, project.value?.customer_id ?? null)
    project.value = await saveProjectDetail(projectId.value, payload)
    showEdit.value = false
  } catch (e: unknown) {
    editError.value = errMsg(e)
  } finally {
    saving.value = false
  }
}

// PAI-59. Effective rate display in the edit modal: when the project
// rate is null and a customer is selected with a rate, show the
// inherited value as a hint under the input.
function inheritedRateLabel(kind: 'hourly' | 'lp'): string {
  return inheritedProjectRateHint(editForm.value, customers.value, kind)
}

// ── Archive / Delete ──────────────────────────────────────────────────────────
const archiving        = ref(false)
const showDeleteConfirm = ref(false)
const deletingProject  = ref(false)

async function toggleArchive() {
  if (!project.value) return
  archiving.value = true
  try {
    const newStatus = project.value.status === 'active' ? 'archived' : 'active'
    project.value = await setProjectStatus(projectId.value, newStatus)
    editForm.value.status = project.value.status
  } finally {
    archiving.value = false
  }
}

async function deleteProject() {
  deletingProject.value = true
  try {
    await deleteProjectDetail(projectId.value)
    showEdit.value = false
    showDeleteConfirm.value = false
    router.push('/projects')
  } catch (e: unknown) {
    editError.value = errMsg(e, 'Delete failed.')
  } finally {
    deletingProject.value = false
  }
}

// ── Purge time entries ───────────────────────────────────────────────────────
const showPurge      = ref(false)
const purgeLoading   = ref(false)
const purgeBusy      = ref(false)
const purgeConfirmKey = ref('')
const purgeError     = ref('')
const purgeForm      = ref(emptyProjectPurgeForm())
const purgePreview   = ref<{ count: number; total_hours: number } | null>(null)
const purgeSuccess   = ref<{ count: number; total_hours: number } | null>(null)
const purgeUsers     = ref<{ id: number; username: string }[]>([])

async function openPurge() {
  showEdit.value = false
  purgeForm.value = emptyProjectPurgeForm()
  purgePreview.value = null
  purgeSuccess.value = null
  purgeConfirmKey.value = ''
  purgeError.value = ''
  showPurge.value = true
  try {
    purgeUsers.value = await loadProjectPurgeUsers(projectId.value)
  } catch { /* ignore */ }
}

function closePurge() {
  showPurge.value = false
}

async function previewPurge() {
  purgeLoading.value = true
  purgePreview.value = null
  purgeSuccess.value = null
  purgeError.value = ''
  purgeConfirmKey.value = ''
  try {
    purgePreview.value = await previewProjectTimeEntryPurge(projectId.value, buildProjectPurgePayload(purgeForm.value))
  } catch (e: unknown) {
    purgeError.value = errMsg(e, 'Preview failed')
  } finally {
    purgeLoading.value = false
  }
}

async function executePurge() {
  purgeBusy.value = true
  purgeError.value = ''
  try {
    const payload = { ...buildProjectPurgePayload(purgeForm.value), confirmation_key: purgeConfirmKey.value }
    purgeSuccess.value = await executeProjectTimeEntryPurge(projectId.value, payload)
    purgePreview.value = null
    purgeConfirmKey.value = ''
  } catch (e: unknown) {
    purgeError.value = errMsg(e, 'Purge failed')
  } finally {
    purgeBusy.value = false
  }
}

async function addProjectTag(tagId: number) {
  await addProjectTagRequest(projectId.value, tagId)
  const tag = allTags.value.find(t => t.id === tagId)
  if (tag && project.value) project.value.tags = [...project.value.tags, tag]
}

async function removeProjectTag(tagId: number) {
  if (!await confirm({ message: 'Remove this tag from the project?', confirmLabel: 'Remove' })) return
  await removeProjectTagRequest(projectId.value, tagId)
  if (project.value) project.value.tags = project.value.tags.filter(t => t.id !== tagId)
}

// ── CSV Export ────────────────────────────────────────────────────────────────
const exportError = ref('')

async function exportCSV() {
  if (exporting.value) return   // guard against double-click
  exporting.value = true
  exportError.value = ''
  try {
    const il = issueListRef.value
    const selectedIds = il?.selectionMode && il.selectedIds.size > 0 ? [...il.selectedIds] : []
    const url = buildProjectCsvExportUrl(projectId.value, selectedIds)
    const resp = await fetch(url, { credentials: 'include' })
    if (resp.status === 401) { exportError.value = 'Session expired — please reload and log in again.'; return }
    if (!resp.ok) { exportError.value = `Export failed (${resp.status}).`; return }
    const blob = await resp.blob()
    if (blob.size === 0) { exportError.value = 'Export returned an empty file.'; return }
    const cd = resp.headers.get('Content-Disposition') ?? ''
    const match = cd.match(/filename="([^"]+)"/)
    const filename = match ? match[1] : `${project.value?.key ?? 'export'}.csv`
    const objUrl = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = objUrl
    a.download = filename
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
    URL.revokeObjectURL(objUrl)
  } catch (e: unknown) {
    exportError.value = `Export failed: ${errMsg(e, 'network error')}`
  } finally {
    exporting.value = false
  }
}

// ── CSV Import (with preflight + collision modal) ─────────────────────────────
const importResult     = ref<{ imported: number; updated: number; skipped: number; errors: string[] } | null>(null)
const importError      = ref('')
const importing        = ref(false)
const importInputRef   = ref<HTMLInputElement | null>(null)
const importPreflight  = ref<PreflightResult | null>(null)
const showImportModal  = ref(false)
const pendingImportFile = ref<File | null>(null)

function triggerImport() {
  importResult.value = null
  importError.value  = ''
  importInputRef.value?.click()
}

async function onImportFile(e: Event) {
  const file = (e.target as HTMLInputElement).files?.[0]
  if (!file) return
  pendingImportFile.value = file
  importing.value = true
  importError.value = ''
  importResult.value = null
  try {
    importPreflight.value = await preflightProjectCsvImport(projectId.value, file)
    // If no collisions, import directly without showing modal
    if (importPreflight.value.collision_count === 0) {
      await doImport('insert', '')
    } else {
      showImportModal.value = true
    }
  } catch (ex: unknown) {
    importError.value = errMsg(ex, 'Preflight failed.')
  } finally {
    importing.value = false
    if (importInputRef.value) importInputRef.value.value = ''
  }
}

async function onImportConfirm(strategy: CollisionStrategy, _projectName: string) {
  showImportModal.value = false
  await doImport(strategy, '')
}

async function doImport(strategy: CollisionStrategy, _projectName: string) {
  if (!pendingImportFile.value) return
  importing.value = true
  try {
    importResult.value = await runProjectCsvImport(projectId.value, pendingImportFile.value, strategy)
    await load()
  } catch (ex: unknown) {
    importError.value = errMsg(ex, 'Import failed.')
  } finally {
    importing.value = false
    pendingImportFile.value = null
  }
}

async function load() {
  const request = ++projectLoadRequestSeq
  loading.value = true
  resetWorkspaceState()
  const loadQuery = projectIssueQuery.value
  const data = await loadProjectDetailData(projectId.value, loadQuery)
  if (request !== projectLoadRequestSeq) return
  project.value = data.project
  if (loadQuery === projectIssueQuery.value) {
    issues.value = data.issues
    await issueFreshness.prime(data.issues)
  } else {
    await replaceProjectIssues(projectIssueQuery.value)
  }
  users.value = data.users
  costUnits.value = data.costUnits
  releases.value = data.releases
  allTags.value = data.allTags
  allViews.value = data.allViews
  customers.value = data.customers
  // activeTabId is set by IssueList's view-applied emit (MRU-based)
  loading.value   = false
}

onMounted(() => {
  load()
})

// Re-fetch when navigating project→project (Vue Router reuses the component)
watch(() => route.params.id, (newId, oldId) => {
  if (newId && newId !== oldId) {
    resetWorkspaceState()
    activeTabId.value = null
    load()
  }
})

async function replaceProjectIssues(q: string): Promise<boolean> {
  const request = ++projectIssueRequestSeq
  const nextIssues = await loadProjectIssues(projectId.value, q)
  if (request !== projectIssueRequestSeq) return false
  issues.value = nextIssues
  await issueFreshness.prime(nextIssues)
  return true
}

// Re-fetch issues when search query changes (search-as-filter overlay).
watch(trimmedProjectIssueQuery, (q) => {
  if (projectSearchTimer) clearTimeout(projectSearchTimer)
  projectSearchTimer = setTimeout(() => {
    void replaceProjectIssues(q)
  }, 150)
})

function onCreated(issue: Issue) {
  issues.value.push(issue)
  if (issue.cost_unit && !costUnits.value.includes(issue.cost_unit))
    costUnits.value = [...costUnits.value, issue.cost_unit].sort()
  const releaseLabel = issue.type === 'release' ? issue.title : issue.release
  if (releaseLabel && !releases.value.includes(releaseLabel))
    releases.value = [...releases.value, releaseLabel].sort()
}

function onUpdated(issue: Issue) {
  const idx = issues.value.findIndex(i => i.id === issue.id)
  if (idx >= 0) issues.value[idx] = issue
}

function onDeleted(id: number) {
  issues.value = issues.value.filter(i => i.id !== id)
}

// ── Last change metadata ───────────────────────────────────────────────────────
// Derived from loaded issues — most recently updated, zero extra API calls.
const lastChanged = computed(() => {
  if (!issues.value.length) return null
  const latest = [...issues.value].sort((a, b) =>
    b.updated_at.localeCompare(a.updated_at)
  )[0]
  // Relative time
  const diff = Date.now() - new Date(latest.updated_at.replace(' ', 'T') + 'Z').getTime()
  const mins  = Math.floor(diff / 60000)
  const hours = Math.floor(diff / 3600000)
  const days  = Math.floor(diff / 86400000)
  const when  = mins < 1   ? 'just now'
              : mins < 60  ? `${mins}m ago`
              : hours < 24 ? `${hours}h ago`
              : days < 30  ? `${days}d ago`
              : new Date(latest.updated_at).toLocaleDateString()
  return { id: latest.id, key: latest.issue_key, when, title: latest.title }
})

function currentMainScroll(): HTMLElement | null {
  return document.querySelector('.main-content')
}

function applyFreshProjectIssues(nextIssues: Issue[]) {
  const scroller = currentMainScroll()
  const top = scroller?.scrollTop ?? 0
  issues.value = nextIssues
  void nextTick(() => {
    if (scroller) scroller.scrollTop = top
  })
}

const issueFreshness = useFreshness<Issue[]>(issueListPath, {
  apply: applyFreshProjectIssues,
  count: (payload) => payload.length,
})
const issueFreshnessStale = computed(() => issueFreshness.stale.value)
const issueFreshnessCount = computed(() => issueFreshness.newCount.value)

function refreshProjectIssueListFromHeader() {
  issueFreshness.refresh()
}

watch(
  [issueFreshnessStale, issueFreshnessCount],
  ([stale, count]) => {
    if (stale) issueRefreshPrompt.show(count, refreshProjectIssueListFromHeader)
    else issueRefreshPrompt.clear(refreshProjectIssueListFromHeader)
  },
  { immediate: true },
)
</script>

<template>
  <div class="pd-page">
    <LoadingText v-if="loading" class="loading" label="Loading…" />
    <template v-else-if="project">
      <Teleport defer to="#app-header-left">
        <RouterLink to="/projects" class="ah-back">
          <AppIcon name="arrow-left" :size="13" />
        </RouterLink>
        <img v-if="project.logo_path" :src="project.logo_path" class="ah-project-logo" :alt="project.name" />
        <span class="ah-key-badge">{{ project.key }}</span>
        <span class="ah-title">{{ project.name }}</span>
        <span v-if="project.description" class="ah-subtitle">{{ project.description }}</span>
      </Teleport>
      <Teleport defer to="#app-header-right">
        <RouterLink
          v-if="project.customer_id"
          :to="`/customers/${project.customer_id}`"
          class="pd-customer-pill"
          :title="`Customer: ${project.customer_name ?? ''}`"
        >
          <AppIcon name="building-2" :size="12" />
          <span>{{ project.customer_name }}</span>
        </RouterLink>
        <span v-if="lastChanged" class="ah-meta-text">
          <span class="ah-meta-prefix">updated {{ lastChanged.when }}</span>
          <RouterLink :to="`/projects/${projectId}/issues/${lastChanged.id}`" class="ah-meta-link">{{ lastChanged.key }}</RouterLink>
        </span>
        <span :class="`badge badge-${project.status}`">{{ project.status }}</span>
        <TagChip v-for="t in project.tags" :key="t.id" :tag="t" />
        <input ref="importInputRef" type="file" accept=".csv" style="display:none" @change="onImportFile" />
        <!-- PAI-246: Export / Import / Edit folded into a single ⋯ menu so
             the right cluster doesn't outweigh the global Undo / Edit
             controls. Icon convention here is data-flow oriented:
             Export = data leaves (up arrow), Import = data enters (down). -->
        <button
          ref="overflowTriggerRef"
          class="btn btn-ghost btn-sm icon-only pd-overflow-trigger"
          :class="{ active: overflowOpen }"
          :title="overflowOpen ? 'Close menu' : 'More project actions'"
          @click="toggleOverflow"
        >
          <AppIcon name="more-horizontal" :size="14" />
        </button>
      </Teleport>

      <!-- PAI-265: panel teleported to <body> with position:fixed so it
           escapes #app-header-right's overflow:hidden clip. -->
      <Teleport to="body">
        <div
          v-if="overflowOpen"
          ref="overflowPanelRef"
          class="pd-overflow-menu"
          role="menu"
          :style="overflowPanelStyle"
        >
          <button class="pd-overflow-item" :disabled="exporting" @click="onMenuExport">
            <AppIcon v-if="!exporting" name="upload" :size="14" />
            <AppIcon v-else name="loader" :size="14" class="spin" />
            <span>{{ exporting ? 'Preparing download…' : 'Export CSV' }}</span>
          </button>
          <button v-if="isAdmin && canEditProject" class="pd-overflow-item" :disabled="importing" @click="onMenuImport">
            <AppIcon name="download" :size="14" />
            <span>Import CSV</span>
          </button>
          <button v-if="isAdmin && canEditProject" class="pd-overflow-item" @click="onMenuEdit">
            <AppIcon name="pencil" :size="14" />
            <span>Edit project</span>
          </button>
        </div>
      </Teleport>

      <!-- Export error banner -->
      <div v-if="exportError" class="import-error">
        {{ exportError }}
        <button class="import-dismiss" @click="exportError=''"><AppIcon name="x" :size="14" /></button>
      </div>

      <!-- Import result banner -->
      <div v-if="importResult" class="import-result">
        <span class="import-ok"><AppIcon name="check" :size="13" /> {{ importResult.imported }} imported</span>
        <span v-if="importResult.updated" class="import-ok"> · {{ importResult.updated }} updated</span>
        <span v-if="importResult.skipped" class="import-skip"> · {{ importResult.skipped }} skipped</span>
        <span v-if="importResult.errors?.length" class="import-errs"> · {{ importResult.errors.length }} error(s): {{ importResult.errors.join('; ') }}</span>
        <button class="import-dismiss" @click="importResult=null"><AppIcon name="x" :size="14" /></button>
      </div>
      <div v-if="importError" class="import-error">{{ importError }} <button class="import-dismiss" @click="importError=''"><AppIcon name="x" :size="14" /></button></div>

      <!-- PAI-178: ProjectContextSection moved into the aux panel
           cluster below. It used to render full-width here which
           wasted space on projects that don't use anchors yet.
           The Context toggle in the IssueList toolbar opens it. -->

      <!-- Tab nav — driven by admin-default views (fallback to synthetic set) -->
      <nav class="tab-nav">
        <button
          v-for="v in displayTabs"
          :key="v.id"
          class="tab-btn"
          :class="{ active: activeTabId === v.id }"
          :data-label="v.title"
          @click="selectTab(v)"
        >
          {{ v.title }}
          <AppIcon name="refresh-cw" :size="11" class="tab-refresh-icon" :class="{ 'tab-refresh-icon--visible': activeTabId === v.id }" />
        </button>
      </nav>

      <!-- Single IssueList for all tabs. Project-level workspaces now live
           in the footer rail instead of competing with issue-list controls. -->
      <IssueList
        ref="issueListRef"
        :project-id="projectId"
        :issues="issues"
        :search-query-override="projectIssueQuery"
        :initial-panel-issue-id="initialPanelIssueId"
        @created="onCreated"
        @updated="onUpdated"
        @deleted="onDeleted"
        @view-applied="onViewApplied"
        @views-changed="refreshViews"
      />

      <section class="pd-workspaces">
        <Transition name="pd-workspace-dock">
          <div v-if="workspacePanel" class="pd-workspace-dock">
            <div class="pd-workspace-dock__head">
              <div class="pd-workspace-dock__title">
                <span class="pd-workspace-dock__eyebrow">Project workspace</span>
                <strong>
                  {{
                    workspacePanel === 'context'
                      ? 'Project Context'
                      : workspacePanel === 'docs'
                        ? 'Documents'
                        : 'Cooperation'
                  }}
                </strong>
                <span class="pd-workspace-dock__state">
                  {{
                    workspacePanel === 'context'
                      ? contextStatusLabel
                      : workspacePanel === 'docs'
                        ? (docCount > 0 ? 'Available' : 'Empty')
                        : (cooperationPopulated ? 'Configured' : 'Not set up')
                  }}
                </span>
              </div>
              <div class="pd-workspace-dock__meta">
                <span class="pd-workspace-dock__pill">
                  {{
                    workspacePanel === 'context'
                      ? `${contextSummary.repoCount} repo${contextSummary.repoCount === 1 ? '' : 's'}`
                      : workspaceSummary[workspacePanel]
                  }}
                </span>
                <button class="btn btn-ghost btn-sm" @click="workspacePanel = null">
                  Close
                </button>
              </div>
            </div>

            <div class="pd-workspace-dock__body">
              <template v-if="workspacePanel === 'context'">
                <div class="pd-context-overview">
                  <div class="pd-context-overview__card">
                    <span class="pd-context-overview__label">Overview</span>
                    <p>
                      Keep repos and manifest together here. This stays full width,
                      so issue work can remain central while setup lives in one predictable place.
                    </p>
                  </div>
                  <div class="pd-context-overview__card">
                    <span class="pd-context-overview__label">Current setup</span>
                    <p>
                      {{ contextSummary.repoCount }} linked repo{{ contextSummary.repoCount === 1 ? '' : 's' }}
                      · {{ contextSummary.hasManifest ? 'manifest present' : 'no manifest yet' }}
                    </p>
                  </div>
                </div>
                <ProjectContextSection
                  :project-id="projectId"
                  :can-write="isAdmin && canEditProject"
                  :show-header="false"
                  @populated="(v: boolean) => contextSummary.populated = v"
                  @summary="updateContextSummary"
                />
              </template>

              <DocumentsSection
                v-else-if="workspacePanel === 'docs'"
                scope="project"
                :scope-id="projectId"
                :can-write="isAdmin && canEditProject"
                @count="(n: number) => docCount = n"
              />

              <CooperationSection
                v-else
                :project-id="projectId"
                :can-write="isAdmin && canEditProject"
                @populated="(v: boolean) => cooperationPopulated = v"
              />
            </div>
          </div>
        </Transition>

        <div class="pd-workspace-footer">
          <div class="pd-workspace-rail">
            <button
              type="button"
              :class="['btn', 'btn-ghost', 'btn-sm', { active: workspacePanel === 'docs' }]"
              :title="workspaceSummary.docs"
              @click="toggleWorkspace('docs')"
            >
              <AppIcon name="file-stack" :size="13" />
              <span>Docs</span>
              <span class="pd-workspace-rail__meta">{{ docCount }} file{{ docCount === 1 ? '' : 's' }}</span>
            </button>
            <button
              type="button"
              :class="['btn', 'btn-ghost', 'btn-sm', { active: workspacePanel === 'cooperation' }]"
              :title="workspaceSummary.cooperation"
              @click="toggleWorkspace('cooperation')"
            >
              <AppIcon name="handshake" :size="13" />
              <span>Coop</span>
              <span class="pd-workspace-rail__meta">{{ cooperationPopulated ? 'configured' : 'empty' }}</span>
            </button>
            <button
              type="button"
              :class="['btn', 'btn-ghost', 'btn-sm', { active: workspacePanel === 'context' }]"
              :title="workspaceSummary.context"
              @click="toggleWorkspace('context')"
            >
              <AppIcon name="git-branch" :size="13" />
              <span>Context</span>
              <span class="pd-workspace-rail__meta">{{ contextStatusLabel }}</span>
            </button>
          </div>
        </div>
      </section>

      <!-- Always-mounted, visually-hidden sentinels feed the toolbar
           workspace rail summary without forcing the user to open the
           workspaces first. -->
      <div class="pd-sentinels" aria-hidden="true">
        <DocumentsSection
          v-if="workspacePanel !== 'docs'"
          scope="project"
          :scope-id="projectId"
          :can-write="false"
          @count="(n: number) => docCount = n"
        />
        <CooperationSection
          v-if="workspacePanel !== 'cooperation'"
          :project-id="projectId"
          :can-write="false"
          @populated="(v: boolean) => cooperationPopulated = v"
        />
        <ProjectContextSection
          v-if="workspacePanel !== 'context'"
          :project-id="projectId"
          :can-write="false"
          :show-header="false"
          @populated="(v: boolean) => contextSummary.populated = v"
          @summary="updateContextSummary"
        />
      </div>
    </template>
  </div>

  <!-- Delete confirm modal -->
  <AppModal title="Delete Project" :open="showDeleteConfirm" @close="showDeleteConfirm=false" confirm-key="d" @confirm="deleteProject">
    <p style="font-size:14px;color:var(--text);margin-bottom:1.25rem">
      Soft-delete <strong>{{ project?.name }}</strong>? The project and all its issues will be hidden from the UI. All data is preserved and can be restored via database update.
    </p>
    <div style="display:flex;justify-content:flex-end;gap:.5rem">
      <button class="btn btn-ghost" @click="showDeleteConfirm=false"><u>C</u>ancel</button>
      <button class="btn btn-danger" :disabled="deletingProject" @click="deleteProject"><template v-if="deletingProject">Deleting…</template><template v-else><u>D</u>elete project</template></button>
    </div>
  </AppModal>

  <!-- Import collision modal -->
  <ImportCollisionModal
    :open="showImportModal"
    :preflight="importPreflight"
    @confirm="onImportConfirm"
    @cancel="showImportModal=false; pendingImportFile=null"
  />

  <!-- Edit project modal -->
  <AppModal title="Edit Project" :open="showEdit" @close="showEdit=false" max-width="1100px">
      <form @submit.prevent="saveProject" class="form">
        <div class="field">
          <label>Name</label>
          <input v-model="editForm.name" type="text" required autofocus />
        </div>
        <div class="field">
          <label>Key <span class="label-hint">— used in issue IDs, e.g. WEB-1</span></label>
          <input v-model="editForm.key" type="text" maxlength="6" style="text-transform:uppercase" />
        </div>
        <div class="field">
          <div class="field-label-row">
            <label>Description</label>
            <AiActionMenu surface="issue"
              host-key="project-detail:description"
              field="project_description"
              field-label="Project description"
              :issue-id="0"
              :text="() => editForm.description"
              :on-accept="onProjectDescriptionAccept"
            />
          </div>
          <textarea v-model="editForm.description" rows="3"></textarea>
          <AiSurfaceFeedback host-key="project-detail:description" :apply="applyProjectAiResult" />
        </div>
        <div class="field">
          <label>Status</label>
          <MetaSelect v-model="editForm.status" :options="PROJECT_STATUS_OPTIONS" />
        </div>
        <div class="field">
          <label>Tags</label>
          <TagSelector
            :all-tags="allTags"
            :selected-ids="projectTagIds"
            @add="addProjectTag"
            @remove="removeProjectTag"
          />
        </div>
        <div class="field">
          <label>Product Owner</label>
          <select v-model="editForm.product_owner">
            <option :value="null">— none —</option>
            <option v-for="u in users" :key="u.id" :value="u.id">{{ u.username }}</option>
          </select>
        </div>
        <div class="field">
          <label>Customer <span class="label-hint">— links rates + documents to a Customer record</span></label>
          <select v-model="editForm.customer_id">
            <option :value="null">— Unassigned —</option>
            <option v-for="c in customers" :key="c.id" :value="c.id">{{ c.name }}</option>
          </select>
        </div>
        <div class="field">
          <label>Customer label <span class="label-hint">— legacy freeform reference (PMO26 era)</span></label>
          <input v-model="editForm.customer_label" type="text" placeholder="e.g. CUST-123" />
        </div>
        <div style="display:flex;gap:.75rem">
          <div class="field" style="flex:1">
            <label>Rate (€/h)</label>
            <input v-model.number="editForm.rate_hourly" type="number" step="0.01" placeholder="e.g. 120" />
            <span v-if="inheritedRateLabel('hourly')" class="pd-inherit-hint">
              <AppIcon name="link" :size="11" /> {{ inheritedRateLabel('hourly') }}
            </span>
          </div>
          <div class="field" style="flex:1">
            <label>Rate (€/LP)</label>
            <input v-model.number="editForm.rate_lp" type="number" step="0.01" placeholder="e.g. 1200" />
            <span v-if="inheritedRateLabel('lp')" class="pd-inherit-hint">
              <AppIcon name="link" :size="11" /> {{ inheritedRateLabel('lp') }}
            </span>
          </div>
        </div>
        <div class="field">
          <label>Logo <span class="label-hint">— square image, max 200×200px · JPG/PNG</span></label>
          <div class="logo-upload-row">
            <img v-if="project?.logo_path" :src="project.logo_path" class="logo-preview" alt="Current logo" />
            <div v-else class="logo-placeholder">No logo</div>
            <input ref="logoInputRef" type="file" accept="image/jpeg,image/png" style="display:none" @change="uploadLogo" />
            <button type="button" class="btn btn-ghost btn-sm" :disabled="logoUploading" @click="logoInputRef?.click()">
              {{ logoUploading ? 'Uploading…' : project?.logo_path ? 'Replace' : 'Upload' }}
            </button>
            <button v-if="project?.logo_path" type="button" class="btn btn-ghost btn-sm" @click="deleteLogo">Remove</button>
          </div>
          <div v-if="logoError" class="field-error">{{ logoError }}</div>
        </div>
        <div v-if="editError" class="form-error">{{ editError }}</div>
        <div class="form-actions">
          <button type="button" class="btn btn-ghost" @click="showEdit=false">Cancel</button>
          <button type="submit" class="btn btn-primary" :disabled="saving">
            {{ saving ? 'Saving…' : 'Save changes' }}
          </button>
        </div>

        <!-- Danger zone -->
        <div class="danger-zone">
          <div class="danger-zone-label">Danger zone</div>
          <div class="danger-zone-actions">
            <button
              type="button"
              class="btn btn-ghost btn-sm"
              @click="openPurge"
            >
              Purge time entries
            </button>
            <button
              type="button"
              class="btn btn-ghost btn-sm"
              :disabled="archiving"
              @click="toggleArchive"
            >
              {{ archiving ? 'Saving…' : project?.status === 'active' ? 'Archive project' : 'Restore to active' }}
            </button>
            <button
              type="button"
              class="btn btn-danger btn-sm"
              @click="showDeleteConfirm=true"
            >
              Delete project
            </button>
          </div>
        </div>
      </form>
    </AppModal>

  <!-- Purge time entries modal (admin only) -->
  <AppModal v-if="isAdmin" title="Purge Time Entries" :open="showPurge" @close="closePurge" max-width="540px">
      <div class="purge-body">
        <div class="purge-filters">
          <div class="field">
            <label>Source</label>
            <select v-model="purgeForm.source">
              <option value="all">All entries</option>
              <option value="mite">Mite-imported only</option>
              <option value="manual">Manual only</option>
            </select>
          </div>
          <div style="display:flex;gap:.75rem">
            <div class="field" style="flex:1">
              <label>From date</label>
              <input v-model="purgeForm.from_date" type="date" />
            </div>
            <div class="field" style="flex:1">
              <label>To date</label>
              <input v-model="purgeForm.to_date" type="date" />
            </div>
          </div>
          <div class="field">
            <label>User</label>
            <select v-model="purgeForm.user_id">
              <option :value="null">All users</option>
              <option v-for="u in purgeUsers" :key="u.id" :value="u.id">{{ u.username }}</option>
            </select>
          </div>
          <button class="btn btn-ghost" :disabled="purgeLoading" @click="previewPurge" style="align-self:flex-start">
            {{ purgeLoading ? 'Loading...' : 'Preview' }}
          </button>
        </div>

        <div v-if="purgePreview" class="purge-preview">
          <p class="purge-warning">
            This will permanently delete <strong>{{ purgePreview.count }}</strong> time
            {{ purgePreview.count === 1 ? 'entry' : 'entries' }}
            ({{ purgePreview.total_hours.toFixed(1) }}h). This cannot be undone.
          </p>
          <div class="field">
            <label>Type <strong>{{ project?.key }}</strong> to confirm</label>
            <input v-model="purgeConfirmKey" type="text" :placeholder="project?.key" autocomplete="off" />
          </div>
          <button
            class="btn btn-danger"
            :disabled="purgeBusy || purgeConfirmKey.toUpperCase() !== project?.key?.toUpperCase()"
            @click="executePurge"
          >
            {{ purgeBusy ? 'Purging...' : `Purge ${purgePreview.count} entries` }}
          </button>
        </div>

        <div v-if="purgeSuccess" class="purge-success">
          Deleted {{ purgeSuccess.count }} {{ purgeSuccess.count === 1 ? 'entry' : 'entries' }} ({{ purgeSuccess.total_hours.toFixed(1) }}h).
        </div>
        <div v-if="purgeError" class="form-error">{{ purgeError }}</div>
      </div>
    </AppModal>
</template>

<style scoped>
/* PAI-146: per-field label row holds the label + the AI optimize
   button on the right. */
.field-label-row {
  display: flex; align-items: center; justify-content: space-between;
  gap: .5rem;
}
.field-label-row > label { margin-bottom: 0; }

.loading { color: var(--text-muted); padding: 2rem 0; }

/* ── Project logo ─────────────────────────────────────────────────────────── */
.ah-project-logo {
  width: 24px; height: 24px; object-fit: contain; border-radius: 4px; flex-shrink: 0;
}
.logo-upload-row {
  display: flex; align-items: center; gap: .5rem;
}
.logo-preview {
  width: 40px; height: 40px; object-fit: contain; border-radius: 6px;
  border: 1px solid var(--border);
}
.logo-placeholder {
  width: 40px; height: 40px; border-radius: 6px;
  border: 1px dashed var(--border); background: var(--bg);
  display: flex; align-items: center; justify-content: center;
  font-size: 10px; color: var(--text-muted);
}

@keyframes spin { to { transform: rotate(360deg); } }
.spin { animation: spin .8s linear infinite; }
.icon-only { padding: .3rem .45rem; }
.header-tags { display: flex; flex-wrap: wrap; gap: .3rem; }

/* ── Project header ─────────────────────────────────────────────────────────── */
.project-header {
  display: flex; flex-direction: column; gap: .35rem;
  margin-bottom: 1.75rem;
}

/* Row 1 — big key */
.ph-key {
  font-size: 28px; font-weight: 800; color: var(--text);
  letter-spacing: -.03em; line-height: 1;
}

/* Row 2 — name · desc  ←spacer→  meta · badge  |  actions */
.ph-row2 {
  display: flex; align-items: center; gap: .75rem; flex-wrap: wrap;
}
.ph-identity {
  display: flex; align-items: baseline; gap: .55rem; flex-wrap: wrap;
}
.ph-name {
  font-size: 15px; font-weight: 600; color: var(--text);
}
.ph-desc {
  font-size: 13px; color: var(--text-muted);
}

/* Spacer pushes meta + badge + actions to the right */
.ph-meta {
  margin-left: auto;
  display: flex; align-items: center; gap: .65rem;
}
.ph-last {
  font-size: 12px; color: var(--text-muted);
  display: flex; align-items: center; gap: .3rem;
}
.ph-last-link {
  font-size: 12px; font-weight: 600; font-family: monospace;
  color: var(--bp-blue);
}
.ph-last-link:hover { text-decoration: underline; }
.ph-badge { flex-shrink: 0; }

.ph-actions {
  display: flex; align-items: center; gap: .5rem; flex-shrink: 0;
}

.import-result, .import-error {
  display: flex; align-items: center; gap: .5rem; flex-wrap: wrap;
  font-size: 13px; padding: .6rem 1rem; border-radius: var(--radius);
  margin-bottom: .75rem;
}
.import-result { background: #d4edda; color: #155724; }
.import-error  { background: #fde8e8; color: #c0392b; }
.import-ok   { font-weight: 600; }
.import-skip { color: #856404; }
.import-errs { color: #721c24; }
.import-dismiss { margin-left: auto; background: none; border: none; font-size: 16px; cursor: pointer; color: inherit; line-height: 1; padding: 0 .25rem; }

.form { display: flex; flex-direction: column; gap: 1rem; }
.field { display: flex; flex-direction: column; gap: .35rem; }
.field label { font-size: 12px; font-weight: 600; color: var(--text-muted); text-transform: uppercase; letter-spacing: .05em; }
.label-hint { font-weight: 400; text-transform: none; letter-spacing: 0; font-size: 11px; }
.form-error { font-size: 13px; color: #c0392b; background: #fde8e8; padding: .5rem .75rem; border-radius: var(--radius); }
.form-actions { display: flex; justify-content: flex-end; gap: .5rem; margin-top: .25rem; }
textarea { resize: vertical; min-height: 80px; }

.danger-zone {
  margin-top: .5rem;
  border-top: 1px solid var(--border);
  padding-top: 1rem;
}
.danger-zone-label {
  font-size: 11px; font-weight: 600; text-transform: uppercase; letter-spacing: .05em;
  color: #c0392b; margin-bottom: .6rem;
}
.danger-zone-actions {
  display: flex; gap: .5rem; flex-wrap: wrap;
}
.btn-sm { padding: .3rem .65rem; font-size: 12px; }

/* ── Purge modal ──────────────────────────────────────────────────────────── */
.purge-body { display: flex; flex-direction: column; gap: 1rem; }
.purge-filters { display: flex; flex-direction: column; gap: .75rem; }
.purge-preview {
  border: 1px solid #e8c4c4; background: #fdf2f2; border-radius: var(--radius);
  padding: .75rem 1rem; display: flex; flex-direction: column; gap: .75rem;
}
.purge-warning { font-size: 13px; color: #7a1a1a; margin: 0; line-height: 1.5; }
.purge-success {
  font-size: 13px; color: #1a6b3a; background: #edf7ed; border: 1px solid #c4e0c4;
  border-radius: var(--radius); padding: .6rem .75rem;
}

/* ── Tabs ───────────────────────────────────────────────────────────────────── */
.tab-nav {
  display: flex; gap: 0; margin-bottom: 1.25rem;
  border-bottom: 2px solid var(--border);
}
.tab-btn {
  position: relative;
  background: none; border: none; cursor: pointer;
  padding: .5rem 1rem; font-size: 13px; font-weight: 500;
  color: var(--text-muted); border-bottom: 2px solid transparent;
  margin-bottom: -2px; display: inline-flex; align-items: center;
  transition: color .15s, border-color .15s;
  font-family: inherit;
}
/* Pre-reserve bold width to prevent layout shift */
.tab-btn::after {
  content: attr(data-label);
  font-weight: 600;
  visibility: hidden;
  height: 0;
  display: block;
  overflow: hidden;
}
.tab-btn:hover { color: var(--text); }
.tab-btn.active { color: var(--bp-blue); border-bottom-color: var(--bp-blue); font-weight: 600; }
.tab-count {
  font-size: 11px; font-weight: 700; background: var(--surface-2);
  color: var(--text-muted); border-radius: 10px; padding: 0 .45rem; line-height: 1.6;
}
.tab-btn.active .tab-count { background: var(--bp-blue); color: #fff; }
.tab-refresh-icon { opacity: 0; margin-left: .25rem; flex-shrink: 0; transition: opacity .15s; pointer-events: none; }
.tab-btn:hover .tab-refresh-icon--visible { opacity: .5; }
.tab-refresh-icon--visible:hover { opacity: .8; }
.tab-btn:hover .tab-refresh-icon { opacity: .7; }

/* ── Group list ─────────────────────────────────────────────────────────────── */
.group-empty { color: var(--text-muted); font-size: 13px; padding: 1.5rem 0; }
.group-table { width: 100%; border-collapse: collapse; font-size: 13px; }
.group-table th {
  text-align: left; font-size: 11px; font-weight: 600; color: var(--text-muted);
  text-transform: uppercase; letter-spacing: .05em;
  padding: .4rem .75rem; border-bottom: 1px solid var(--border);
}
.group-row { border-bottom: 1px solid var(--border-subtle, var(--border)); }
.group-row:hover { background: var(--surface-2); }
.group-row td { padding: .55rem .75rem; vertical-align: middle; }
.group-key { font-family: monospace; font-size: 12px; white-space: nowrap; }
.group-title { max-width: 340px; }
.group-meta { color: var(--text-muted); white-space: nowrap; }
.group-link { color: var(--text); text-decoration: none; }
.group-link:hover { color: var(--bp-blue); text-decoration: underline; }

/* ── PAI-58/59 customer link + rate inheritance ───────────────────── */
.pd-customer-pill {
  display: inline-flex; align-items: center; gap: .35rem;
  padding: .15rem .55rem;
  border: 1px solid var(--border); border-radius: 999px;
  font-size: 11px; font-weight: 600;
  color: var(--text-muted);
  text-decoration: none;
  background: var(--bg-card);
  transition: border-color .15s, color .15s, background .15s;
  white-space: nowrap;
}

/* PAI-246: ⋯ overflow menu in the right cluster (Export / Import / Edit). */
.pd-overflow-trigger.active { background: var(--bg); color: var(--text); }
/* PAI-265: position:fixed + inline top/right (computed from trigger rect)
   so the panel escapes #app-header-right's overflow:hidden clip. */
.pd-overflow-menu {
  position: fixed; z-index: 60;
  min-width: 180px;
  padding: .25rem;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 8px;
  box-shadow: 0 6px 20px rgba(15, 23, 42, .08);
  display: flex; flex-direction: column; gap: 1px;
}
.pd-overflow-item {
  display: flex; align-items: center; gap: .55rem;
  padding: .45rem .6rem;
  font-size: 12.5px; color: var(--text);
  background: transparent; border: none; border-radius: 6px;
  cursor: pointer; text-align: left; font-family: inherit;
  white-space: nowrap;
}
.pd-overflow-item:hover:not(:disabled) { background: var(--bg); }
.pd-overflow-item:disabled { color: var(--text-muted); cursor: not-allowed; }
.pd-overflow-item :deep(svg) { color: var(--text-muted); flex-shrink: 0; }
.pd-customer-pill:hover {
  border-color: var(--bp-blue);
  color: var(--bp-blue-dark);
  background: var(--bp-blue-pale);
}

.pd-inherit-hint {
  display: inline-flex; align-items: center; gap: .25rem;
  font-size: 11px; color: var(--bp-blue);
  margin-top: .15rem;
}

.pd-page {
  display: flex;
  flex-direction: column;
  /* PAI-274: participate in AppLayout's `.view-body--self-scroll` flex chain
     so IssueList's table-wrap (flex:1; min-height:0; overflow:auto) actually
     has a bounded scrolling viewport — restoring sticky thead + frozen
     columns inside this view. `min-height:100%` was the old page-scroll
     contract; `flex:1; min-height:0` is the bounded-flex contract. */
  flex: 1;
  min-height: 0;
  min-width: 0;
}

.pd-workspaces {
  /* PAI-279: tighter padding around the rail strip — was .6rem; the
     rail itself uses btn-sm vocabulary now and doesn't need much
     breathing room. */
  margin-top: auto;
  padding-top: .35rem;
}

.pd-workspace-dock {
  margin-bottom: 0.85rem;
  border: 1px solid var(--border);
  border-radius: 16px;
  background: var(--bg-card);
  box-shadow: var(--shadow);
  overflow: hidden;
}

.pd-workspace-dock__head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 1rem;
  padding: 1rem 1.2rem;
  background: linear-gradient(180deg, color-mix(in srgb, var(--bp-blue-pale) 42%, white), white);
  border-bottom: 1px solid color-mix(in srgb, var(--bp-blue-pale) 60%, var(--border));
}

.pd-workspace-dock__title {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 0.45rem 0.65rem;
}

.pd-workspace-dock__eyebrow,
.pd-context-overview__label {
  display: inline-flex;
  align-items: center;
  font-size: 10.5px;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--text-muted);
}

.pd-workspace-dock__state {
  padding: 0.16rem 0.55rem;
  border-radius: 999px;
  background: color-mix(in srgb, var(--bp-blue) 10%, transparent);
  color: var(--bp-blue-dark);
  font-size: 11px;
  font-weight: 700;
}

.pd-workspace-dock__meta {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 0.55rem;
  color: var(--text-muted);
}

.pd-workspace-dock__pill {
  padding: 0.18rem 0.55rem;
  border-radius: 999px;
  border: 1px solid var(--border);
  background: rgba(255, 255, 255, 0.9);
  font-size: 11px;
  white-space: nowrap;
}

.pd-workspace-dock__body {
  padding: 1rem 1.1rem 1.2rem;
  background: var(--bg-card);
}

.pd-context-overview {
  display: grid;
  grid-template-columns: minmax(0, 1.4fr) minmax(280px, 0.9fr);
  gap: 0.85rem;
  margin-bottom: 1rem;
}

.pd-context-overview__card {
  padding: 0.95rem 1rem;
  border-radius: 12px;
  background: var(--bg);
  border: 1px solid var(--border);
}

.pd-context-overview__card p {
  margin-top: 0.35rem;
  color: var(--text-muted);
  font-size: 13px;
  line-height: 1.5;
}

.pd-workspace-footer {
  display: flex;
  flex-direction: column;
}

/* PAI-279: rail uses the global `.btn .btn-ghost .btn-sm` vocabulary
   for consistency with every other toggle in the app (e.g. IssueList's
   `⌥ Tree`, the filter / views buttons). The container only owns
   spacing between buttons + bottom breathing room above AppFooter. */
.pd-workspace-rail {
  display: flex;
  align-items: center;
  gap: 0.4rem;
  padding: 0.1rem 0 0.4rem;
}

/* `.btn-sm.active` matches `IssueList.vue:1264` — keeping the active
   workspace toggle tied to the dock above with the same brand-blue
   pale fill the rest of the app uses for active toggles. */
.btn-sm.active {
  background: var(--bp-blue-pale);
  color: var(--bp-blue-dark);
  border-color: var(--bp-blue-pale);
}

.pd-workspace-rail__meta {
  color: inherit;
  opacity: .65;
  font-size: 10px;
  font-weight: 700;
  letter-spacing: 0.01em;
}

.pd-workspace-dock-enter-active,
.pd-workspace-dock-leave-active {
  transition: opacity .18s ease, transform .18s ease;
}

.pd-workspace-dock-enter-from,
.pd-workspace-dock-leave-to {
  opacity: 0;
  transform: translateY(8px);
}

@media (max-width: 980px) {
  .pd-workspace-dock__head {
    flex-direction: column;
    align-items: flex-start;
  }
  .pd-context-overview {
    grid-template-columns: 1fr;
  }

  .pd-workspace-rail {
    flex-wrap: wrap;
  }
}

/* Sentinel components keep reactivity but render no UI. */
.pd-sentinels { display: none; }
</style>
