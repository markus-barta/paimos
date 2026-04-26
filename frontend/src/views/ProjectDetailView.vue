<script setup lang="ts">
import { ref, onMounted, computed, nextTick, watch } from 'vue'
import { useRoute, useRouter, RouterLink } from 'vue-router'

import AppModal from '@/components/AppModal.vue'
import AppFooter from '@/components/AppFooter.vue'
import IssueList from '@/components/IssueList.vue'
import TagChip from '@/components/TagChip.vue'
import TagSelector from '@/components/TagSelector.vue'
import MetaSelect from '@/components/MetaSelect.vue'
import type { MetaOption } from '@/components/MetaSelect.vue'
import ImportCollisionModal from '@/components/ImportCollisionModal.vue'
import type { PreflightResult, CollisionStrategy } from '@/components/ImportCollisionModal.vue'
import { api, errMsg } from '@/api/client'
import { MAX_IMAGE_SIZE } from '@/utils/constants'
import { useAuthStore } from '@/stores/auth'
import { useSearchStore } from '@/stores/search'
import AppIcon from '@/components/AppIcon.vue'
import { useConfirm } from '@/composables/useConfirm'
import { provideIssueContext } from '@/composables/useIssueContext'
import { useProjectAuxPanels } from '@/composables/useProjectAuxPanels'
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
  buildProjectCsvExportUrl,
  deleteProjectLogo,
  executeProjectTimeEntryPurge,
  loadProjectDetailData,
  loadProjectIssues,
  loadProjectPurgeUsers,
  preflightProjectCsvImport,
  previewProjectTimeEntryPurge,
  runProjectCsvImport,
  uploadProjectLogo,
} from '@/services/projectDetail'
import DocumentsSection from '@/components/customer/DocumentsSection.vue'
import CooperationSection from '@/components/customer/CooperationSection.vue'
import ProjectAuxPanel from '@/components/customer/ProjectAuxPanel.vue'
import ProjectContextSection from '@/components/project/ProjectContextSection.vue'
// PAI-146 expansion: AI optimize on the project description.
// project_description is its own field name (not aliased to
// "description") so the prompt reminder fits a stakeholder audience.
import AiActionMenu from '@/components/ai/AiActionMenu.vue'
import AiOptimizeOverlay from '@/components/ai/AiOptimizeOverlay.vue'
import AiOptimizeBanner from '@/components/ai/AiOptimizeBanner.vue'
import { useAiOptimize } from '@/composables/useAiOptimize'

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
const {
  auxPanel,
  toggleAux,
  closeAux,
  contextPopulated,
  docCount,
  cooperationPopulated,
} = useProjectAuxPanels()

provideIssueContext({ users, allTags, costUnits, releases, projects: ref([]), sprints })

const loading       = ref(true)
const issueListRef  = ref<InstanceType<typeof IssueList> | null>(null)
const exporting     = ref(false)

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
  issues.value = await loadProjectIssues(projectId.value, search.query)
  nextTick(() => issueListRef.value?.applyView(view))
}

function onViewApplied(viewId: number) {
  activeTabId.value = viewId
}

async function refreshViews() {
  try { allViews.value = await api.get<SavedView[]>('/views') } catch { /* ignore */ }
}

// Edit project
const showEdit  = ref(false)
const editForm  = ref(emptyProjectEditForm())
const editError = ref('')
const saving    = ref(false)

// PAI-146 expansion: AI optimize composable + onAccept handler for
// the project description. The edit modal is admin-gated, so the
// button only appears for users who already have edit rights here.
const aiOptimize = useAiOptimize()
function onProjectDescriptionAccept(text: string) {
  editForm.value.description = text
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
    project.value = await api.put<Project>(`/projects/${projectId.value}`, payload)
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
    project.value = await api.put<Project>(`/projects/${projectId.value}`, { status: newStatus })
    editForm.value.status = project.value.status
  } finally {
    archiving.value = false
  }
}

async function deleteProject() {
  deletingProject.value = true
  try {
    await api.delete(`/projects/${projectId.value}`)
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
  await api.post(`/projects/${projectId.value}/tags`, { tag_id: tagId })
  const tag = allTags.value.find(t => t.id === tagId)
  if (tag && project.value) project.value.tags = [...project.value.tags, tag]
}

async function removeProjectTag(tagId: number) {
  if (!await confirm({ message: 'Remove this tag from the project?', confirmLabel: 'Remove' })) return
  await api.delete(`/projects/${projectId.value}/tags/${tagId}`)
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
  loading.value = true
  const data = await loadProjectDetailData(projectId.value, search.query)
  project.value = data.project
  issues.value = data.issues
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
    activeTabId.value = null
    load()
  }
})

// Re-fetch issues when search query changes (search-as-filter overlay)
watch(() => search.query, async (q) => {
  issues.value = await loadProjectIssues(projectId.value, q)
})

function onCreated(issue: Issue) {
  issues.value.push(issue)
  if (issue.cost_unit && !costUnits.value.includes(issue.cost_unit))
    costUnits.value = [...costUnits.value, issue.cost_unit].sort()
  if (issue.release && !releases.value.includes(issue.release))
    releases.value = [...releases.value, issue.release].sort()
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
</script>

<template>
    <div v-if="loading" class="loading">Loading…</div>
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
          updated {{ lastChanged.when }}
          <RouterLink :to="`/projects/${projectId}/issues/${lastChanged.id}`" class="ah-meta-link">{{ lastChanged.key }}</RouterLink>
        </span>
        <span :class="`badge badge-${project.status}`">{{ project.status }}</span>
        <TagChip v-for="t in project.tags" :key="t.id" :tag="t" />
        <button class="btn btn-ghost btn-sm icon-only" @click="exportCSV" :disabled="exporting" :title="exporting ? 'Preparing download…' : 'Export CSV'">
          <AppIcon v-if="!exporting" name="download" :size="14" />
          <AppIcon v-else name="loader" :size="14" class="spin" />
        </button>
        <button v-if="isAdmin && canEditProject" class="btn btn-ghost btn-sm icon-only" @click="triggerImport" :disabled="importing" title="Import CSV">
          <AppIcon name="upload" :size="14" />
        </button>
        <input ref="importInputRef" type="file" accept=".csv" style="display:none" @change="onImportFile" />
        <button v-if="isAdmin && canEditProject" class="btn btn-ghost btn-sm" @click="openEdit">Edit</button>
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

      <!-- Single IssueList for all tabs. The `toolbar-extra` slot drops
           the Documents / Cooperation aux-panel toggles in next to the
           Tree/Flat button so the whole toggle cluster lives together. -->
      <IssueList
        ref="issueListRef"
        :project-id="projectId"
        :issues="issues"
        :initial-panel-issue-id="initialPanelIssueId"
        @created="onCreated"
        @updated="onUpdated"
        @deleted="onDeleted"
        @view-applied="onViewApplied"
        @views-changed="refreshViews"
      >
        <template #toolbar-extra>
          <button
            type="button"
            :class="['btn', 'btn-ghost', 'btn-sm', 'pd-aux-btn', { active: auxPanel === 'docs' }]"
            :title="docCount > 0 ? `${docCount} document${docCount === 1 ? '' : 's'}` : 'No documents yet'"
            @click="toggleAux('docs')"
          >
            <AppIcon name="file-stack" :size="13" />
            <span>Docs</span>
            <span v-if="docCount > 0" class="pd-aux-count">{{ docCount }}</span>
          </button>
          <button
            type="button"
            :class="['btn', 'btn-ghost', 'btn-sm', 'pd-aux-btn', { active: auxPanel === 'cooperation' }]"
            :title="cooperationPopulated ? 'Cooperation profile filled in' : 'No cooperation profile yet'"
            @click="toggleAux('cooperation')"
          >
            <AppIcon name="handshake" :size="13" />
            <span>Coop</span>
            <span v-if="cooperationPopulated" class="pd-aux-info" aria-hidden="true">i</span>
          </button>
          <!-- PAI-178: Context toggle — opens the repos + manifest
               panel that used to sit above the tabs. -->
          <button
            type="button"
            :class="['btn', 'btn-ghost', 'btn-sm', 'pd-aux-btn', { active: auxPanel === 'context' }]"
            :title="contextPopulated ? 'Project context configured' : 'No repos or manifest yet'"
            @click="toggleAux('context')"
          >
            <AppIcon name="git-branch" :size="13" />
            <span>Context</span>
            <span v-if="contextPopulated" class="pd-aux-info" aria-hidden="true">i</span>
          </button>
        </template>
      </IssueList>

      <!-- ── Aux side panels (PAI-145) ───────────────────────────
           Slide in from the right; share width with IssueSidePanel
           via useSidePanelWidth so they line up. The DocumentsSection
           and CooperationSection live inside the panel slot so the
           empty-states still read well — just no longer screaming for
           attention from the page body. -->
      <ProjectAuxPanel
        :open="auxPanel === 'docs'"
        title="Documents"
        :subtitle="docCount > 0 ? `${docCount} file${docCount === 1 ? '' : 's'}` : ''"
        @close="closeAux"
      >
        <DocumentsSection
          scope="project"
          :scope-id="projectId"
          :can-write="isAdmin && canEditProject"
          @count="(n: number) => docCount = n"
        />
      </ProjectAuxPanel>

      <ProjectAuxPanel
        :open="auxPanel === 'cooperation'"
        title="Cooperation"
        :subtitle="cooperationPopulated ? 'profile set' : 'not set up'"
        @close="closeAux"
      >
        <CooperationSection
          :project-id="projectId"
          :can-write="isAdmin && canEditProject"
          @populated="(v: boolean) => cooperationPopulated = v"
        />
      </ProjectAuxPanel>

      <!-- PAI-178: Context aux panel. The ProjectContextSection
           component already emits a `populated` signal we wire to
           the toolbar badge below. -->
      <ProjectAuxPanel
        :open="auxPanel === 'context'"
        title="Project Context"
        :subtitle="contextPopulated ? 'repos + manifest set' : 'not set up'"
        @close="closeAux"
      >
        <ProjectContextSection
          :project-id="projectId"
          :can-write="isAdmin && canEditProject"
          @populated="(v: boolean) => contextPopulated = v"
        />
      </ProjectAuxPanel>

      <!-- Always-mounted, visually-hidden sentinels feed the toolbar
           toggle badges (count + (i)) without forcing the user to open
           the panels first. `display: none` on .pd-sentinels keeps Vue
           reactivity alive while taking no visual space.

           When a panel opens, the sentinel for that scope unmounts and
           the real component mounts inside the panel — one wasted
           fetch per toggle, acceptable for v1. -->
      <div class="pd-sentinels" aria-hidden="true">
        <DocumentsSection
          v-if="auxPanel !== 'docs'"
          scope="project"
          :scope-id="projectId"
          :can-write="false"
          @count="(n: number) => docCount = n"
        />
        <CooperationSection
          v-if="auxPanel !== 'cooperation'"
          :project-id="projectId"
          :can-write="false"
          @populated="(v: boolean) => cooperationPopulated = v"
        />
        <ProjectContextSection
          v-if="auxPanel !== 'context'"
          :project-id="projectId"
          :can-write="false"
          @populated="(v: boolean) => contextPopulated = v"
        />
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
</template>

    <AppFooter />

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
              field="project_description"
              field-label="Project description"
              :issue-id="0"
              :text="() => editForm.description"
              :on-accept="onProjectDescriptionAccept"
            />
          </div>
          <AiOptimizeBanner />
          <textarea v-model="editForm.description" rows="3"></textarea>
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

    <!-- PAI-146 expansion: AI optimize overlay for the project
         description. Single mount per view. -->
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

/* Aux-panel toggles in the IssueList toolbar. Sit next to Tree/Flat;
   inherit .btn / .btn-ghost / .btn-sm sizing, so they line up with
   their neighbours without bespoke metrics. */
.pd-aux-btn {
  display: inline-flex; align-items: center; gap: .35rem;
}
.pd-aux-btn.active {
  background: var(--bp-blue-pale);
  color: var(--bp-blue-dark);
  border-color: var(--bp-blue-light);
}

/* Circled count when there's at least one document. */
.pd-aux-count {
  display: inline-flex; align-items: center; justify-content: center;
  min-width: 16px; height: 16px;
  padding: 0 5px;
  border-radius: 999px;
  background: var(--bp-blue);
  color: #fff;
  font-size: 10px; font-weight: 700;
  font-variant-numeric: tabular-nums;
  line-height: 1;
}
.pd-aux-btn.active .pd-aux-count { background: var(--bp-blue-dark); }

/* Italic 'i' marker when the cooperation profile has any data. */
.pd-aux-info {
  display: inline-flex; align-items: center; justify-content: center;
  width: 16px; height: 16px;
  border-radius: 50%;
  background: var(--bp-blue-pale);
  color: var(--bp-blue-dark);
  font-family: 'Source Serif Pro', 'Charter', Georgia, serif;
  font-style: italic;
  font-size: 12px; font-weight: 700;
  line-height: 1;
}
.pd-aux-btn.active .pd-aux-info {
  background: var(--bp-blue);
  color: #fff;
}

/* Sentinel components keep reactivity but render no UI. */
.pd-sentinels { display: none; }
</style>
