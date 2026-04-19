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
import type { Tag, Issue, Project, User, SavedView, Sprint } from '@/types'

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

provideIssueContext({ users, allTags, costUnits, releases, projects: ref([]), sprints })

const loading       = ref(true)
const issueListRef  = ref<InstanceType<typeof IssueList> | null>(null)
const exporting     = ref(false)

// ── Admin-default views as tabs ───────────────────────────────────────────────

// Synthetic fallback views used when no admin-default views exist in the DB.
// Negative IDs ensure they never collide with real DB rows.
const FALLBACK_VIEWS: SavedView[] = [
  {
    id: -100, user_id: 0, owner_username: 'system', title: 'Issues',
    description: 'Tickets and tasks.',
    columns_json: '["billing_type","total_budget","rate_hourly","rate_lp","estimate_hours","estimate_lp","ar_hours","ar_lp","group_state","sprint_state","jira_id","jira_version","jira_text"]',
    filters_json: '{"type":["ticket","task"]}',
    is_shared: true, is_admin_default: true, sort_order: 0, hidden: false, pinned: null, created_at: '', updated_at: '',
  },
  {
    id: -101, user_id: 0, owner_username: 'system', title: 'Epics',
    description: 'Epic planning view.',
    columns_json: '["cost_unit","release","sprint","sprint_state","jira_id","jira_version","jira_text"]',
    filters_json: '{"type":["epic"]}',
    is_shared: true, is_admin_default: true, sort_order: 1, hidden: false, pinned: null, created_at: '', updated_at: '',
  },
  {
    id: -102, user_id: 0, owner_username: 'system', title: 'Cost Units',
    description: 'Cost unit overview.',
    columns_json: '["epic","sprint","sprint_state","jira_id","jira_version","jira_text"]',
    filters_json: '{"type":["cost_unit"]}',
    is_shared: true, is_admin_default: true, sort_order: 2, hidden: false, pinned: null, created_at: '', updated_at: '',
  },
  {
    id: -103, user_id: 0, owner_username: 'system', title: 'Releases',
    description: 'Release planning.',
    columns_json: '["billing_type","total_budget","rate_hourly","rate_lp","estimate_hours","estimate_lp","ar_hours","ar_lp","sprint_state","jira_id","jira_version","jira_text"]',
    filters_json: '{"type":["release"]}',
    is_shared: true, is_admin_default: true, sort_order: 3, hidden: false, pinned: null, created_at: '', updated_at: '',
  },
]

const allViews    = ref<SavedView[]>([])
const activeTabId = ref<number | null>(null)

// Tabs: admin-default (not hidden, or user-pinned) + pinned personal views
const displayTabs = computed(() => {
  const defaults = allViews.value
    .filter(v => v.is_admin_default && (!v.hidden || v.pinned === true) && v.pinned !== false)
    .sort((a, b) => a.sort_order - b.sort_order || a.title.localeCompare(b.title))
  const pinnedPersonal = allViews.value
    .filter(v => !v.is_admin_default && v.pinned === true)
    .sort((a, b) => a.title.localeCompare(b.title))
  const tabs = [...defaults, ...pinnedPersonal]
  return tabs.length ? tabs : FALLBACK_VIEWS
})

async function selectTab(view: SavedView) {
  const isReclick = activeTabId.value === view.id
  activeTabId.value = view.id
  // Re-fetch issues on tab switch or re-click
  const url = `/projects/${projectId.value}/issues?fields=list${search.query.length >= 2 ? '&q=' + encodeURIComponent(search.query) : ''}`
  issues.value = await api.get<Issue[]>(url)
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
const editForm  = ref({ name: '', key: '', description: '', status: 'active', product_owner: null as number | null, customer_id: '', rate_hourly: null as number | null, rate_lp: null as number | null })
const editError = ref('')
const saving    = ref(false)

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
  const fd = new FormData()
  fd.append('logo', file)
  try {
    const updated = await fetch(`/api/projects/${projectId.value}/logo`, { method: 'POST', body: fd, credentials: 'same-origin' })
    if (!updated.ok) { const d = await updated.json(); throw new Error(d.error ?? 'Upload failed.') }
    project.value = await updated.json()
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
    project.value = await api.delete<Project>(`/projects/${projectId.value}/logo`)
  } catch (ex: unknown) {
    logoError.value = errMsg(ex, 'Failed to remove logo.')
  }
}

const projectTagIds = computed(() => project.value?.tags.map(t => t.id) ?? [])

function openEdit() {
  if (!project.value) return
  editForm.value = {
    name:          project.value.name,
    key:           project.value.key,
    description:   project.value.description,
    status:        project.value.status,
    product_owner: project.value.product_owner ?? null,
    customer_id:   project.value.customer_id ?? '',
    rate_hourly:   project.value.rate_hourly ?? null,
    rate_lp:       project.value.rate_lp ?? null,
  }
  editError.value = ''
  showEdit.value = true
}

async function saveProject() {
  editError.value = ''
  if (!editForm.value.name.trim()) { editError.value = 'Name required.'; return }
  saving.value = true
  try {
    project.value = await api.put<Project>(`/projects/${projectId.value}`, editForm.value)
    showEdit.value = false
  } catch (e: unknown) {
    editError.value = errMsg(e)
  } finally {
    saving.value = false
  }
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
const purgeForm      = ref({ source: 'all' as string, from_date: '', to_date: '', user_id: null as number | null })
const purgePreview   = ref<{ count: number; total_hours: number } | null>(null)
const purgeSuccess   = ref<{ count: number; total_hours: number } | null>(null)
const purgeUsers     = ref<{ id: number; username: string }[]>([])

async function openPurge() {
  showEdit.value = false
  purgeForm.value = { source: 'all', from_date: '', to_date: '', user_id: null }
  purgePreview.value = null
  purgeSuccess.value = null
  purgeConfirmKey.value = ''
  purgeError.value = ''
  showPurge.value = true
  try {
    purgeUsers.value = await api.get<{ id: number; username: string }[]>(`/projects/${projectId.value}/time-entries/users`)
  } catch { /* ignore */ }
}

function closePurge() {
  showPurge.value = false
}

function buildPurgePayload() {
  const p: Record<string, unknown> = { source: purgeForm.value.source }
  if (purgeForm.value.from_date) p.from_date = purgeForm.value.from_date
  if (purgeForm.value.to_date) p.to_date = purgeForm.value.to_date
  if (purgeForm.value.user_id != null) p.user_id = purgeForm.value.user_id
  return p
}

async function previewPurge() {
  purgeLoading.value = true
  purgePreview.value = null
  purgeSuccess.value = null
  purgeError.value = ''
  purgeConfirmKey.value = ''
  try {
    purgePreview.value = await api.post<{ count: number; total_hours: number }>(
      `/projects/${projectId.value}/time-entries/purge-preview`, buildPurgePayload()
    )
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
    const payload = { ...buildPurgePayload(), confirmation_key: purgeConfirmKey.value }
    purgeSuccess.value = await api.post<{ count: number; total_hours: number }>(
      `/projects/${projectId.value}/time-entries/purge`, payload
    )
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
    let url = `/api/projects/${projectId.value}/export/csv`
    const il = issueListRef.value
    if (il?.selectionMode && il.selectedIds.size > 0) {
      url += `?ids=${[...il.selectedIds].join(',')}`
    }
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
    const fd = new FormData()
    fd.append('file', file)
    const resp = await fetch(`/api/projects/${projectId.value}/import/csv/preflight`, {
      method: 'POST', credentials: 'include', body: fd,
    })
    const data = await resp.json()
    if (!resp.ok) { importError.value = data.error ?? 'Preflight failed.'; return }
    importPreflight.value = data
    // If no collisions, import directly without showing modal
    if (data.collision_count === 0) {
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
    const fd = new FormData()
    fd.append('file', pendingImportFile.value)
    fd.append('strategy', strategy)
    const resp = await fetch(`/api/projects/${projectId.value}/import/csv`, {
      method: 'POST', credentials: 'include', body: fd,
    })
    const data = await resp.json()
    if (!resp.ok) { importError.value = data.error ?? 'Import failed.'; return }
    importResult.value = data
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
  const [p, iss, u, cu, rel, tags, views] = await Promise.all([
    api.get<Project>(`/projects/${projectId.value}`),
    api.get<Issue[]>(`/projects/${projectId.value}/issues?fields=list${search.query.length >= 2 ? '&q=' + encodeURIComponent(search.query) : ''}`),
    api.get<User[]>('/users'),
    api.get<string[]>(`/projects/${projectId.value}/cost-units`).catch(() => []),
    api.get<string[]>(`/projects/${projectId.value}/releases`).catch(() => []),
    api.get<Tag[]>('/tags'),
    api.get<SavedView[]>('/views').catch(() => []),
  ])
  project.value   = p
  issues.value    = iss
  users.value     = u
  costUnits.value = cu
  releases.value  = rel
  allTags.value   = tags
  allViews.value = views as SavedView[]
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
  const url = `/projects/${projectId.value}/issues?fields=list${q.length >= 2 ? '&q=' + encodeURIComponent(q) : ''}`
  issues.value = await api.get<Issue[]>(url)
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
        <button v-if="isAdmin" class="btn btn-ghost btn-sm icon-only" @click="triggerImport" :disabled="importing" title="Import CSV">
          <AppIcon name="upload" :size="14" />
        </button>
        <input ref="importInputRef" type="file" accept=".csv" style="display:none" @change="onImportFile" />
        <button v-if="isAdmin" class="btn btn-ghost btn-sm" @click="openEdit">Edit</button>
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

      <!-- Single IssueList for all tabs -->
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
      />
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
          <label>Description</label>
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
          <label>Customer ID <span class="label-hint">— external reference</span></label>
          <input v-model="editForm.customer_id" type="text" placeholder="e.g. CUST-123" />
        </div>
        <div style="display:flex;gap:.75rem">
          <div class="field" style="flex:1">
            <label>Rate (€/h)</label>
            <input v-model.number="editForm.rate_hourly" type="number" step="0.01" placeholder="e.g. 120" />
          </div>
          <div class="field" style="flex:1">
            <label>Rate (€/LP)</label>
            <input v-model.number="editForm.rate_lp" type="number" step="0.01" placeholder="e.g. 1200" />
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
</style>
