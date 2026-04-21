<script setup lang="ts">
import { ref, onMounted, computed, watch } from 'vue'
import { useRoute, useRouter, RouterLink } from 'vue-router'
import { api, errMsg } from '@/api/client'
import { useAuthStore } from '@/stores/auth'
import MetaSelect from '@/components/MetaSelect.vue'
import type { MetaOption } from '@/components/MetaSelect.vue'
import AppFooter from '@/components/AppFooter.vue'
import AppIcon from '@/components/AppIcon.vue'
import AppModal from '@/components/AppModal.vue'
import { STATUSES } from '@/constants/status'

const auth   = useAuthStore()
const route  = useRoute()
const router = useRouter()

// Redirect non-admins
if (auth.user && auth.user.role !== 'admin') router.replace('/')

// ── Tabs ──────────────────────────────────────────────────────────────────────
type TabId = 'jira' | 'mite'
const TABS: { id: TabId; label: string; icon: string }[] = [
  { id: 'jira',  label: 'Jira',  icon: 'upload' },
  { id: 'mite',  label: 'Mite',  icon: 'clock' },
]

// Support old tab IDs from bookmarks
function resolveTab(t: string | undefined): TabId {
  if (t === 'jira-import' || t === 'jira') return 'jira'
  if (t === 'mite') return 'mite'
  return 'jira'
}

const activeTab = ref<TabId>(resolveTab(route.query.tab as string))

watch(() => route.query.tab, (t) => {
  activeTab.value = resolveTab(t as string)
})

function selectTab(id: TabId) {
  activeTab.value = id
  router.replace({ query: { ...route.query, tab: id } })
}

// ── Jira credentials ─────────────────────────────────────────────────────────
const jira = ref({ host: '', email: '', token: '', has_token: false })
const jiraConfigured = ref(false)
const jiraProjects   = ref<{ key: string; name: string }[]>([])
const jiraLoading    = ref(true)
const jiraError      = ref('')
const jiraSaving     = ref(false)
const jiraSaveOk     = ref(false)
const jiraCredError  = ref('')
const jiraTesting    = ref(false)
const jiraTestResult = ref<{ ok: boolean; display_name?: string; error?: string } | null>(null)

onMounted(async () => {
  try {
    const cfg = await api.get<{ host: string; email: string; has_token: boolean }>('/integrations/jira')
    jira.value = { host: cfg.host, email: cfg.email, token: '', has_token: cfg.has_token }
    jiraConfigured.value = !!(cfg.host && cfg.email && cfg.has_token)
    if (jiraConfigured.value) await loadJiraProjects()
  } catch { jiraConfigured.value = false }
  finally { jiraLoading.value = false }
})

async function saveJira() {
  jiraCredError.value = ''; jiraSaveOk.value = false; jiraSaving.value = true
  try {
    const body: Record<string,string> = { host: jira.value.host, email: jira.value.email }
    if (jira.value.token) body.token = jira.value.token
    const r = await api.put<{ host: string; email: string; has_token: boolean }>('/integrations/jira', body)
    jira.value.has_token = r.has_token; jira.value.token = ''
    jiraConfigured.value = !!(r.host && r.email && r.has_token)
    jiraSaveOk.value = true
    if (jiraConfigured.value && !jiraProjects.value.length) await loadJiraProjects()
  } catch (e: unknown) { jiraCredError.value = errMsg(e, 'Save failed.') }
  finally { jiraSaving.value = false }
}

async function testJira() {
  jiraTestResult.value = null; jiraTesting.value = true
  try {
    const r = await api.post<{ ok: boolean; display_name: string }>('/integrations/jira/test', {})
    jiraTestResult.value = { ok: true, display_name: r.display_name }
  } catch (e: unknown) { jiraTestResult.value = { ok: false, error: errMsg(e, 'Connection failed.') } }
  finally { jiraTesting.value = false }
}

async function loadJiraProjects() {
  jiraError.value = ''
  try {
    const r = await api.get<{ key: string; name: string }[]>('/import/jira/projects')
    jiraProjects.value = r
  } catch (e: unknown) {
    jiraError.value = errMsg(e, 'Failed to load Jira projects.')
  }
}

// ── Jira project browser modal ────────────────────────────────────────────────
const projectBrowserOpen  = ref(false)
const projectBrowserQuery = ref('')

const filteredJiraProjects = computed(() => {
  const q = projectBrowserQuery.value.trim().toLowerCase()
  if (!q) return jiraProjects.value
  return jiraProjects.value.filter(p =>
    p.key.toLowerCase().includes(q) || p.name.toLowerCase().includes(q)
  )
})

function openProjectBrowser() {
  projectBrowserQuery.value = ''
  projectBrowserOpen.value = true
}

function selectJiraProject(key: string) {
  selectedJiraProject.value = key
  projectBrowserOpen.value = false
  if (targetMode.value === 'new' && !newProjectName.value) {
    newProjectName.value = suggestedName.value
  }
}

const selectedJiraLabel = computed(() => {
  if (!selectedJiraProject.value) return ''
  const p = jiraProjects.value.find(p => p.key === selectedJiraProject.value)
  return p ? `${p.key} — ${p.name}` : selectedJiraProject.value
})

// ── Target project ────────────────────────────────────────────────────────────
interface BpProject { id: number; key: string; name: string }
const bpProjects = ref<BpProject[]>([])
onMounted(async () => {
  bpProjects.value = await api.get<BpProject[]>('/projects')
})

const bpProjectOptions = computed<MetaOption[]>(() =>
  bpProjects.value.map(p => ({ value: String(p.id), label: `${p.key} — ${p.name}` }))
)

// ── Form ──────────────────────────────────────────────────────────────────────
const selectedJiraProject  = ref('')
const selectedBpProject    = ref('')
const targetMode    = ref<'existing' | 'new'>('existing')
const newProjectName = ref('')
const suggestedName = computed(() => {
  if (!selectedJiraProject.value) return 'Jira Import'
  return selectedJiraProject.value
})
watch([targetMode, selectedJiraProject], () => {
  if (targetMode.value === 'new' && !newProjectName.value) {
    newProjectName.value = suggestedName.value
  }
})

const typeMap = ref<Record<string,string>>({
  'Epic':            'epic',
  'Story':           'ticket',
  'Bug':             'ticket',
  'Task':            'ticket',
  'Change Request':  'ticket',
  'Sub-task':        'task',
  'Cost Unit':       'cost_unit',
  'Release':         'release',
})
const typeEnabled = ref<Record<string,boolean>>({
  'Epic': true, 'Story': true, 'Bug': true, 'Task': true, 'Change Request': true, 'Sub-task': true,
  'Cost Unit': false, 'Release': false,
})
const statusMap = ref<Record<string,string>>({
  'To Do':       'backlog',
  'Backlog':     'backlog',
  'Reported':    'new',
  'On Hold':     'backlog',
  'In Progress': 'in-progress',
  'Blocked':     'in-progress',
  'Completed':   'done',
  'Done':        'done',
  'Canceled':    'cancelled',
  'Closed':      'cancelled',
  'Invoiced':    'invoiced',
})
const priorityMap = ref<Record<string,string>>({
  'Highest': 'high',
  'High':    'high',
  'Medium':  'medium',
  'Low':     'low',
  'Lowest':  'low',
  'Minor':   'low',
})

const TYPE_VAL_OPTIONS: MetaOption[]     = ['epic','ticket','task','cost_unit','release'].map(v => ({ value: v, label: v === 'cost_unit' ? 'Cost Unit' : v.charAt(0).toUpperCase()+v.slice(1) }))
const STATUS_VAL_OPTIONS: MetaOption[]   = STATUSES.map(v => ({ value: v, label: v }))
const PRIORITY_VAL_OPTIONS: MetaOption[] = ['high','medium','low'].map(v => ({ value: v, label: v.charAt(0).toUpperCase()+v.slice(1) }))

const opts = ref({
  overwrite:            false,
  collision_suffix:     '_jira',
  skip_done:            false,
  import_labels_as_tags:true,
  import_comments:      true,
  import_attachments:   true,
})

// ── Import run ────────────────────────────────────────────────────────────────
const importing   = ref(false)
const importError = ref('')

interface ImportResult {
  imported: number
  updated: number
  skipped: number
  skipped_details: { key: string; reason: string }[]
  errors: { key: string; reason: string }[]
  target_project_id: number
  import_tag: string
}
const importResult = ref<ImportResult | null>(null)

const canImport = computed(() => {
  if (!selectedJiraProject.value) return false
  if (targetMode.value === 'existing') return !!selectedBpProject.value
  return !!newProjectName.value.trim()
})

async function startImport() {
  importError.value  = ''
  importResult.value = null
  if (!selectedJiraProject.value) { importError.value = 'Select a Jira project.'; return }
  if (targetMode.value === 'existing' && !selectedBpProject.value) { importError.value = 'Select a target project.'; return }
  if (targetMode.value === 'new' && !newProjectName.value.trim())  { importError.value = 'Enter a name for the new project.'; return }
  importing.value = true
  try {
    const skipTypes: string[] = []
    for (const [jiraType] of Object.entries(typeMap.value)) {
      if (typeEnabled.value[jiraType] === false) skipTypes.push(jiraType)
    }
    const body: Record<string, any> = {
      project_key: selectedJiraProject.value,
      type_map:    typeMap.value,
      status_map:  statusMap.value,
      priority_map:priorityMap.value,
      options:     { ...opts.value, skip_types: skipTypes },
    }
    if (targetMode.value === 'existing') {
      body.target_project_id = Number(selectedBpProject.value)
    } else {
      body.new_project_name = newProjectName.value.trim()
    }
    importResult.value = await api.post<ImportResult>('/import/jira', body)
  } catch (e: unknown) {
    importError.value = errMsg(e, 'Import failed.')
  } finally {
    importing.value = false
  }
}

// ── Mite settings ────────────────────────────────────────────────────────────
const miteBaseUrl           = ref('')
const miteApiKey            = ref('')
const miteLoadSince         = ref('')
const miteHasApiKey         = ref(false)
const miteConfigured        = ref(false)
const miteSaving            = ref(false)
const miteSaveMsg           = ref('')
const miteTesting           = ref(false)
const miteTestResult        = ref('')
const miteTestError         = ref('')

onMounted(async () => {
  try {
    const cfg = await api.get<{ base_url: string; load_data_since_date: string; has_api_key: boolean }>('/integrations/mite')
    miteBaseUrl.value   = cfg.base_url
    miteLoadSince.value = cfg.load_data_since_date
    miteHasApiKey.value = cfg.has_api_key
    miteConfigured.value = !!(cfg.base_url && cfg.has_api_key)
  } catch { /* not configured */ }
})

async function saveMiteSettings() {
  miteSaving.value = true
  miteSaveMsg.value = ''
  try {
    const body: Record<string, string> = { base_url: miteBaseUrl.value, load_data_since_date: miteLoadSince.value }
    if (miteApiKey.value) body.api_key = miteApiKey.value
    const r = await api.put<{ base_url: string; load_data_since_date: string; has_api_key: boolean }>('/integrations/mite', body)
    miteHasApiKey.value  = r.has_api_key
    miteConfigured.value = !!(r.base_url && r.has_api_key)
    miteApiKey.value = ''
    miteSaveMsg.value = 'Saved'
    setTimeout(() => { miteSaveMsg.value = '' }, 3000)
  } catch (e: unknown) {
    miteSaveMsg.value = 'Error: ' + errMsg(e, 'save failed')
  } finally {
    miteSaving.value = false
  }
}

async function testMiteConnection() {
  miteTesting.value = true
  miteTestResult.value = ''
  miteTestError.value = ''
  try {
    const r = await api.post<{ ok: boolean; account_name: string }>('/integrations/mite/test', {})
    miteTestResult.value = r.account_name
  } catch (e: unknown) {
    miteTestError.value = errMsg(e, 'connection test failed')
  } finally {
    miteTesting.value = false
  }
}

// ── Mite import ──────────────────────────────────────────────────────────────
const miteFromDate      = ref(new Date().getFullYear() + '-01-01')
const miteToDate        = ref(new Date().toISOString().slice(0, 10))
const miteProjectFilter = ref('')
const miteImporting     = ref(false)
const miteImportError   = ref('')
const miteImportPhase   = ref('')
const miteImportTotal   = ref(0)
const miteImportProcessed = ref(0)
const mitePagesFetched  = ref(0)
const miteMatchedCount  = ref(0)
const miteSkippedCount  = ref(0)
const miteErrorCount    = ref(0)
const miteJobId         = ref('')
const miteDryRun        = ref(false)

interface MiteProjectSummary { project_key: string; imported: number; minutes: number; unmatched: number; duplicates: number }
interface MiteImportResult {
  imported: number
  total_minutes: number
  skipped_duplicates: { mite_id: number; jira_key: string }[]
  unmatched_issues: { mite_id: number; note: string; extracted_key: string; reason: string }[]
  unmatched_users: { mite_user_name: string; mite_user_id: number; count: number }[]
  matched: { mite_id: number; jira_key: string; issue_key: string; project_key: string; minutes: number; user: string }[]
  errors: { mite_id: number; reason: string }[]
  by_project: MiteProjectSummary[]
  dry_run: boolean
}
const miteImportResult = ref<MiteImportResult | null>(null)

// Collapsible sections
const miteShowUnmatched = ref(true)
const miteShowUsers     = ref(false)
const miteShowDups      = ref(false)
const miteShowErrors    = ref(true)
const miteShowByProject = ref(true)

const canMiteImport = computed(() => miteConfigured.value)

// Load resume date and set default from date when switching to mite tab
watch(activeTab, async (tab) => {
  if (tab === 'mite' && !miteFromDate.value) {
    try {
      const r = await api.get<{ resume_date: string | null }>('/import/mite/resume-date')
      if (r.resume_date) { miteFromDate.value = r.resume_date; return }
    } catch { /* ignore */ }
    const jan1 = new Date().getFullYear() + '-01-01'
    miteFromDate.value = miteLoadSince.value || jan1
  }
})

async function runMiteImportJob(dryRun: boolean) {
  miteImportError.value  = ''
  miteImportResult.value = null
  miteImportPhase.value  = ''
  miteImportTotal.value  = 0
  miteImportProcessed.value = 0
  mitePagesFetched.value = 0
  miteMatchedCount.value = 0
  miteSkippedCount.value = 0
  miteErrorCount.value   = 0
  miteDryRun.value       = dryRun
  miteImporting.value    = true

  try {
    const body: Record<string, any> = {
      from_date: miteFromDate.value || miteLoadSince.value,
      to_date: miteToDate.value,
      dry_run: dryRun,
    }
    if (miteProjectFilter.value.trim()) body.mite_projects = miteProjectFilter.value.trim()

    const { job_id } = await api.post<{ job_id: string }>('/import/mite', body)
    miteJobId.value = job_id

    // Poll for completion
    while (true) {
      await new Promise(r => setTimeout(r, 800))
      const job = await api.get<{
        status: string; error?: string; result?: MiteImportResult
        total: number; processed: number; phase?: string
        pages_fetched: number; matched_count: number; skipped_count: number; error_count: number
      }>(`/import/mite/jobs/${job_id}`)

      miteImportPhase.value     = job.phase ?? ''
      miteImportTotal.value     = job.total
      miteImportProcessed.value = job.processed
      mitePagesFetched.value    = job.pages_fetched
      miteMatchedCount.value    = job.matched_count
      miteSkippedCount.value    = job.skipped_count
      miteErrorCount.value      = job.error_count

      if (job.status === 'complete' || job.status === 'cancelled') {
        miteImportResult.value = job.result ?? null
        if (miteImportResult.value) {
          miteShowUnmatched.value = miteImportResult.value.unmatched_issues.length > 0
          miteShowUsers.value     = miteImportResult.value.unmatched_users.length > 0
          miteShowDups.value      = false
          miteShowErrors.value    = miteImportResult.value.errors.length > 0
          miteShowByProject.value = (miteImportResult.value.by_project?.length ?? 0) > 0
        }
        break
      }
      if (job.status === 'error') {
        miteImportError.value = job.error ?? 'import failed'
        break
      }
    }
  } catch (e: unknown) {
    miteImportError.value = errMsg(e, 'Import failed.')
  } finally {
    miteImporting.value = false
  }
}

function startMitePreview() { runMiteImportJob(true) }
function startMiteImport()  { runMiteImportJob(false) }

async function cancelMiteImport() {
  if (!miteJobId.value) return
  try { await api.post(`/import/mite/jobs/${miteJobId.value}/cancel`, {}) } catch { /* ignore */ }
}
</script>

<template>
  <Teleport defer to="#app-header-left">
    <span class="ah-title">Integrations</span>
    <span class="ah-subtitle">Connect external tools and manage data imports.</span>
  </Teleport>

  <!-- Tab bar -->
  <div class="tab-bar">
    <button
      v-for="tab in TABS"
      :key="tab.id"
      :class="['tab-btn', { active: activeTab === tab.id }]"
      @click="selectTab(tab.id)"
    >
      <AppIcon :name="tab.icon" :size="14" />
      {{ tab.label }}
    </button>
  </div>

  <!-- ── Jira tab ───────────────────────────────────────────────────────── -->
  <div v-if="activeTab === 'jira'" class="tab-panel">
    <!-- Connection settings -->
    <div class="import-card">
      <h2 class="card-title">Connection Settings</h2>
      <div class="cred-grid">
        <div class="field">
          <label>Jira Cloud URL</label>
          <input v-model="jira.host" type="text" placeholder="https://yourteam.atlassian.net" />
        </div>
        <div class="field">
          <label>Email</label>
          <input v-model="jira.email" type="email" placeholder="you@company.com" />
        </div>
        <div class="field">
          <label>API Token <span v-if="jira.has_token" class="label-hint">— saved · enter new to replace</span></label>
          <input v-model="jira.token" type="password" autocomplete="new-password"
            :placeholder="jira.has_token ? '••••••••  (leave blank to keep)' : 'Atlassian API token'" />
        </div>
      </div>
      <div v-if="jiraCredError" class="form-error">{{ jiraCredError }}</div>
      <div v-if="jiraSaveOk" class="ok-banner">Credentials saved.</div>
      <div class="cred-actions">
        <button class="btn btn-secondary" :disabled="jiraTesting || !jira.has_token" @click="testJira">
          {{ jiraTesting ? 'Testing…' : 'Test connection' }}
        </button>
        <button class="btn btn-primary" :disabled="jiraSaving" @click="saveJira">
          {{ jiraSaving ? 'Saving…' : 'Save credentials' }}
        </button>
      </div>
      <div v-if="jiraTestResult" class="test-result" :class="jiraTestResult.ok ? 'test-ok' : 'test-fail'">
        <template v-if="jiraTestResult.ok"><AppIcon name="check" :size="13" /> Connected as <strong>{{ jiraTestResult.display_name }}</strong></template>
        <template v-else><AppIcon name="x" :size="13" /> {{ jiraTestResult.error }}</template>
      </div>
    </div>

    <!-- Import section -->
    <div v-if="jiraLoading" class="loading" style="margin-top:1.25rem">Loading…</div>

    <template v-else-if="jiraConfigured">
        <div class="import-grid">
          <div class="import-card">
            <h2 class="card-title">Source — Jira Project</h2>
            <div v-if="jiraError" class="form-error">{{ jiraError }}</div>
            <div class="field">
              <label>Jira project</label>
              <button class="project-picker-btn" @click="openProjectBrowser">
                <span v-if="selectedJiraLabel" class="picker-selected">{{ selectedJiraLabel }}</span>
                <span v-else class="picker-placeholder">Browse {{ jiraProjects.length }} projects…</span>
                <AppIcon name="chevron-down" :size="14" class="picker-chevron" />
              </button>
            </div>
          </div>

          <div class="import-card">
            <h2 class="card-title">Target — PAIMOS Project</h2>
            <div class="target-mode-toggle">
              <button :class="['mode-btn', { active: targetMode === 'existing' }]" @click="targetMode = 'existing'">Existing project</button>
              <button :class="['mode-btn', { active: targetMode === 'new' }]" @click="targetMode = 'new'; if (!newProjectName) newProjectName = suggestedName">New project</button>
            </div>
            <div v-if="targetMode === 'existing'" class="field">
              <label>Target project</label>
              <MetaSelect v-model="selectedBpProject" :options="bpProjectOptions" placeholder="Select target project…" />
            </div>
            <div v-else class="field">
              <label>New project name</label>
              <input v-model="newProjectName" type="text" :placeholder="suggestedName" />
              <span class="opt-hint">A new PAIMOS project will be created before importing.</span>
            </div>
          </div>
        </div>

        <div class="import-card" style="margin-top:1.25rem">
          <h2 class="card-title">Field Mapping</h2>
          <div class="mapping-section">
            <div class="mapping-group">
              <p class="mapping-label">Type</p>
              <div v-for="(val, key) in typeMap" :key="key" class="mapping-row" :class="{ 'mapping-row--disabled': typeEnabled[key] === false }">
                <label class="mapping-check" :title="typeEnabled[key] !== false ? 'Included — click to skip' : 'Skipped — click to include'">
                  <input type="checkbox" :checked="typeEnabled[key] !== false" @change="typeEnabled[key] = ($event.target as HTMLInputElement).checked" />
                </label>
                <span class="mapping-jira">{{ key }}</span>
                <AppIcon name="arrow-right" :size="14" />
                <MetaSelect :model-value="val" @update:model-value="v => typeMap[key] = v" :options="TYPE_VAL_OPTIONS" />
              </div>
            </div>
            <div class="mapping-group">
              <p class="mapping-label">Status</p>
              <div v-for="(val, key) in statusMap" :key="key" class="mapping-row">
                <span class="mapping-jira">{{ key }}</span>
                <AppIcon name="arrow-right" :size="14" />
                <MetaSelect :model-value="val" @update:model-value="v => statusMap[key] = v" :options="STATUS_VAL_OPTIONS" />
              </div>
            </div>
            <div class="mapping-group">
              <p class="mapping-label">Priority</p>
              <div v-for="(val, key) in priorityMap" :key="key" class="mapping-row">
                <span class="mapping-jira">{{ key }}</span>
                <AppIcon name="arrow-right" :size="14" />
                <MetaSelect :model-value="val" @update:model-value="v => priorityMap[key] = v" :options="PRIORITY_VAL_OPTIONS" />
              </div>
            </div>
          </div>
        </div>

        <div class="import-card" style="margin-top:1.25rem">
          <h2 class="card-title">Options</h2>
          <div class="options-grid">
            <label class="opt-toggle">
              <input type="checkbox" v-model="opts.overwrite" />
              <span class="opt-text">
                <span class="opt-title">Overwrite existing</span>
                <span class="opt-desc">Update issues with matching keys instead of skipping</span>
              </span>
            </label>
            <label class="opt-toggle">
              <input type="checkbox" v-model="opts.skip_done" />
              <span class="opt-text">
                <span class="opt-title">Skip complete issues</span>
                <span class="opt-desc">Don't import issues with status "Complete" or "Canceled"</span>
              </span>
            </label>
            <label class="opt-toggle">
              <input type="checkbox" v-model="opts.import_labels_as_tags" />
              <span class="opt-text">
                <span class="opt-title">Import labels as tags</span>
                <span class="opt-desc">Create PAIMOS tags from Jira labels and attach them</span>
              </span>
            </label>
            <label class="opt-toggle">
              <input type="checkbox" v-model="opts.import_comments" />
              <span class="opt-text">
                <span class="opt-title">Import comments</span>
                <span class="opt-desc">Fetch Jira comments and create them in PAIMOS</span>
              </span>
            </label>
            <label class="opt-toggle">
              <input type="checkbox" v-model="opts.import_attachments" />
              <span class="opt-text">
                <span class="opt-title">Import attachments</span>
                <span class="opt-desc">Download Jira attachments and store them in PAIMOS</span>
              </span>
            </label>
            <div class="opt-field">
              <label>Collision suffix</label>
              <input v-model="opts.collision_suffix" type="text" placeholder="_jira" style="max-width:140px" />
              <span class="opt-hint">Appended to new keys when a collision is detected (if not overwriting)</span>
            </div>
          </div>
        </div>

        <div v-if="importResult" class="import-result">
          <div class="res-summary">
            <span class="res-ok"><AppIcon name="check" :size="13" /> {{ importResult.imported }} imported</span>
            <span v-if="importResult.skipped" class="res-skip"> · {{ importResult.skipped }} skipped</span>
            <template v-if="importResult.errors?.length">
              <span class="res-err"> · {{ importResult.errors.length }} error(s)</span>
            </template>
            <span class="res-tag"> · tagged <code class="res-tag-code">{{ importResult.import_tag }}</code></span>
            <RouterLink
              v-if="importResult.target_project_id"
              :to="`/projects/${importResult.target_project_id}`"
              class="res-link"
            ><AppIcon name="arrow-right" :size="13" /> View project</RouterLink>
          </div>
          <ul v-if="importResult.errors?.length" class="err-list">
            <li v-for="e in importResult.errors" :key="e.key"><code>{{ e.key }}</code>: {{ e.reason }}</li>
          </ul>
        </div>

        <div class="import-actions">
          <div v-if="importError" class="form-error">{{ importError }}</div>
          <button
            class="btn btn-primary"
            :disabled="importing || !canImport"
            @click="startImport"
          >
            <AppIcon v-if="!importing" name="upload" :size="14" />
            <AppIcon v-else name="loader" :size="14" class="spin" />
            {{ importing ? 'Importing…' : 'Start import' }}
          </button>
        </div>
      </template>

    <AppModal title="Select Jira Project" :open="projectBrowserOpen" max-width="560px" @close="projectBrowserOpen = false">
      <div class="browser-search-wrap">
        <AppIcon name="search" :size="14" class="browser-search-icon" />
        <input
          v-model="projectBrowserQuery"
          class="browser-search"
          type="text"
          placeholder="Search by key or name…"
          autofocus
        />
      </div>
      <div class="browser-count">{{ filteredJiraProjects.length }} of {{ jiraProjects.length }} projects</div>
      <div class="browser-list">
        <button
          v-for="p in filteredJiraProjects"
          :key="p.key"
          class="browser-row"
          :class="{ selected: p.key === selectedJiraProject }"
          @click="selectJiraProject(p.key)"
        >
          <span class="browser-key">{{ p.key }}</span>
          <span class="browser-name">{{ p.name }}</span>
          <AppIcon v-if="p.key === selectedJiraProject" name="check" :size="14" class="browser-check" />
        </button>
        <div v-if="filteredJiraProjects.length === 0" class="browser-empty">No projects match "{{ projectBrowserQuery }}"</div>
      </div>
    </AppModal>
  </div>

  <!-- ── Mite tab ─────────────────────────────────────────────────────────── -->
  <div v-else-if="activeTab === 'mite'" class="tab-panel">
    <!-- Settings section -->
    <div class="import-card">
      <h2 class="card-title">Connection Settings</h2>
      <div class="mite-settings-grid">
        <div class="field">
          <label>Base URL</label>
          <input v-model="miteBaseUrl" type="text" placeholder="https://paimos.mite.yo.lk" />
        </div>
        <div class="field">
          <label>API Key</label>
          <input v-model="miteApiKey" type="password" :placeholder="miteHasApiKey ? '••••••••  (saved)' : 'Enter mite API key'" />
        </div>
        <div class="field">
          <label>Load data since</label>
          <input v-model="miteLoadSince" type="date" />
        </div>
      </div>
      <div class="mite-settings-actions">
        <button class="btn btn-primary" :disabled="miteSaving" @click="saveMiteSettings">
          {{ miteSaving ? 'Saving…' : 'Save' }}
        </button>
        <button class="btn btn-secondary" :disabled="miteTesting || !miteConfigured" @click="testMiteConnection">
          <AppIcon v-if="miteTesting" name="loader" :size="14" class="spin" />
          {{ miteTesting ? 'Testing…' : 'Test connection' }}
        </button>
        <span v-if="miteSaveMsg" :class="['mite-msg', { 'mite-msg-ok': miteSaveMsg === 'Saved' }]">{{ miteSaveMsg }}</span>
        <span v-if="miteTestResult" class="mite-msg mite-msg-ok">Connected: {{ miteTestResult }}</span>
        <span v-if="miteTestError" class="mite-msg mite-msg-err">{{ miteTestError }}</span>
      </div>
    </div>

    <!-- Import section -->
    <div class="import-card" style="margin-top:1.25rem">
      <h2 class="card-title">Import Time Entries</h2>

      <div v-if="!miteConfigured" class="notice notice-warn">
        <AppIcon name="alert-circle" :size="16" />
        Configure mite connection above before importing.
      </div>

      <template v-else>
        <div class="mite-import-grid">
          <div class="field">
            <label>From</label>
            <input v-model="miteFromDate" type="date" />
          </div>
          <div class="field">
            <label>To</label>
            <input v-model="miteToDate" type="date" />
          </div>
          <div class="field">
            <label>Mite project filter <span class="opt-hint">(optional, comma-separated IDs)</span></label>
            <input v-model="miteProjectFilter" type="text" placeholder="e.g. 12345,67890" />
          </div>
        </div>

        <!-- Progress -->
        <div v-if="miteImporting" class="mite-progress">
          <div class="mite-progress-row">
            <AppIcon name="loader" :size="14" class="spin" />
            <span v-if="miteImportPhase === 'fetching'">Fetching page {{ mitePagesFetched || 1 }}… ({{ miteImportTotal }} entries so far)</span>
            <span v-else-if="miteImportPhase === 'matching'">Matching {{ miteImportTotal }} entries…</span>
            <span v-else>Processing {{ miteImportProcessed }} / {{ miteImportTotal }} — {{ miteMatchedCount }} matched, {{ miteSkippedCount }} skipped</span>
          </div>
          <button class="btn btn-ghost btn-sm" @click="cancelMiteImport">Cancel</button>
        </div>

        <!-- Results -->
        <div v-if="miteImportResult" class="mite-results">
          <div class="mite-summary" :class="{ 'mite-summary--preview': miteImportResult.dry_run }">
            <span v-if="miteImportResult.dry_run" class="res-preview">Preview — </span>
            <span class="res-ok"><AppIcon name="check" :size="13" /> {{ miteImportResult.imported }} {{ miteImportResult.dry_run ? 'would be imported' : 'imported' }} ({{ (miteImportResult.total_minutes / 60).toFixed(1) }}h)</span>
            <span v-if="miteImportResult.skipped_duplicates.length" class="res-skip"> · {{ miteImportResult.skipped_duplicates.length }} duplicates skipped</span>
            <span v-if="miteImportResult.unmatched_issues.length" class="res-warn"> · {{ miteImportResult.unmatched_issues.length }} unmatched</span>
            <span v-if="miteImportResult.errors.length" class="res-err"> · {{ miteImportResult.errors.length }} error(s)</span>
            <button v-if="miteImportResult.dry_run && miteImportResult.imported > 0" class="btn btn-primary btn-sm" style="margin-left:auto" @click="startMiteImport">
              <AppIcon name="upload" :size="13" /> Confirm Import
            </button>
          </div>

          <!-- By project summary -->
          <div v-if="miteImportResult.by_project?.length" class="mite-section">
            <button class="mite-section-toggle" @click="miteShowByProject = !miteShowByProject">
              <AppIcon :name="miteShowByProject ? 'chevron-down' : 'chevron-right'" :size="14" />
              By Project ({{ miteImportResult.by_project.length }})
            </button>
            <table v-if="miteShowByProject" class="mite-table">
              <thead><tr><th>Project</th><th>Entries</th><th>Hours</th></tr></thead>
              <tbody>
                <tr v-for="p in miteImportResult.by_project" :key="p.project_key">
                  <td><strong>{{ p.project_key }}</strong></td>
                  <td>{{ p.imported }}</td>
                  <td>{{ (p.minutes / 60).toFixed(1) }}h</td>
                </tr>
              </tbody>
            </table>
          </div>

          <!-- Unmatched issues -->
          <div v-if="miteImportResult.unmatched_issues.length" class="mite-section">
            <button class="mite-section-toggle" @click="miteShowUnmatched = !miteShowUnmatched">
              <AppIcon :name="miteShowUnmatched ? 'chevron-down' : 'chevron-right'" :size="14" />
              Unmatched Issues ({{ miteImportResult.unmatched_issues.length }})
            </button>
            <table v-if="miteShowUnmatched" class="mite-table">
              <thead><tr><th>Mite Note</th><th>Extracted Key</th><th>Reason</th></tr></thead>
              <tbody>
                <tr v-for="u in miteImportResult.unmatched_issues" :key="u.mite_id">
                  <td>{{ u.note }}</td>
                  <td><code v-if="u.extracted_key">{{ u.extracted_key }}</code><span v-else class="text-muted">—</span></td>
                  <td>{{ u.reason }}</td>
                </tr>
              </tbody>
            </table>
          </div>

          <!-- Unmatched users -->
          <div v-if="miteImportResult.unmatched_users.length" class="mite-section">
            <button class="mite-section-toggle" @click="miteShowUsers = !miteShowUsers">
              <AppIcon :name="miteShowUsers ? 'chevron-down' : 'chevron-right'" :size="14" />
              Unmatched Users ({{ miteImportResult.unmatched_users.length }})
            </button>
            <table v-if="miteShowUsers" class="mite-table">
              <thead><tr><th>Mite User</th><th>Mite User ID</th><th>Entries Affected</th></tr></thead>
              <tbody>
                <tr v-for="u in miteImportResult.unmatched_users" :key="u.mite_user_id">
                  <td>{{ u.mite_user_name }}</td>
                  <td>{{ u.mite_user_id }}</td>
                  <td>{{ u.count }}</td>
                </tr>
              </tbody>
            </table>
          </div>

          <!-- Skipped duplicates -->
          <div v-if="miteImportResult.skipped_duplicates.length" class="mite-section">
            <button class="mite-section-toggle" @click="miteShowDups = !miteShowDups">
              <AppIcon :name="miteShowDups ? 'chevron-down' : 'chevron-right'" :size="14" />
              Skipped Duplicates ({{ miteImportResult.skipped_duplicates.length }})
            </button>
            <table v-if="miteShowDups" class="mite-table">
              <thead><tr><th>Mite ID</th><th>Jira Key</th></tr></thead>
              <tbody>
                <tr v-for="d in miteImportResult.skipped_duplicates" :key="d.mite_id">
                  <td>{{ d.mite_id }}</td>
                  <td><code v-if="d.jira_key">{{ d.jira_key }}</code><span v-else class="text-muted">—</span></td>
                </tr>
              </tbody>
            </table>
          </div>

          <!-- Errors -->
          <div v-if="miteImportResult.errors.length" class="mite-section">
            <button class="mite-section-toggle" @click="miteShowErrors = !miteShowErrors">
              <AppIcon :name="miteShowErrors ? 'chevron-down' : 'chevron-right'" :size="14" />
              Errors ({{ miteImportResult.errors.length }})
            </button>
            <table v-if="miteShowErrors" class="mite-table">
              <thead><tr><th>Mite ID</th><th>Error</th></tr></thead>
              <tbody>
                <tr v-for="e in miteImportResult.errors" :key="e.mite_id">
                  <td>{{ e.mite_id }}</td>
                  <td class="mite-err-text">{{ e.reason }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>

        <div class="import-actions">
          <div v-if="miteImportError" class="form-error">{{ miteImportError }}</div>
          <button class="btn btn-secondary" :disabled="miteImporting || !canMiteImport" @click="startMitePreview">
            <AppIcon name="eye" :size="14" />
            Preview
          </button>
          <button class="btn btn-primary" :disabled="miteImporting || !canMiteImport" @click="startMiteImport">
            <AppIcon v-if="!miteImporting" name="upload" :size="14" />
            <AppIcon v-else name="loader" :size="14" class="spin" />
            {{ miteImporting ? 'Importing…' : 'Import' }}
          </button>
        </div>
      </template>
    </div>
  </div>

  <AppFooter />
</template>

<style scoped>
/* ── Tab bar ──────────────────────────────────────────────────────────────── */
.tab-bar {
  display: flex; gap: 0; margin-bottom: 1.5rem;
  border-bottom: 1px solid var(--border);
}
.tab-btn {
  display: flex; align-items: center; gap: .45rem;
  padding: .55rem 1rem; background: none; border: none;
  font-size: 13px; font-weight: 500; color: var(--text-muted);
  cursor: pointer; border-bottom: 2px solid transparent;
  margin-bottom: -1px; transition: color .12s, border-color .12s;
  font-family: inherit;
}
.tab-btn:hover { color: var(--text); }
.tab-btn.active { color: var(--bp-blue); border-bottom-color: var(--bp-blue); }

/* ── Tab panel ────────────────────────────────────────────────────────────── */
.tab-panel { min-height: 200px; }

/* ── Credential forms ─────────────────────────────────────────────────────── */
.cred-grid { display: grid; grid-template-columns: 1fr 1fr 1fr; gap: 1rem; }
@media (max-width: 700px) { .cred-grid { grid-template-columns: 1fr; } }
.cred-actions { display: flex; gap: .5rem; margin-top: .75rem; }
.label-hint { font-weight: 400; color: var(--text-muted); font-size: 10px; }
.ok-banner { font-size: 12px; color: #155724; background: #d4edda; padding: .35rem .65rem; border-radius: var(--radius); margin-top: .5rem; }
.test-result { display: flex; align-items: center; gap: .35rem; font-size: 12px; margin-top: .5rem; padding: .35rem .65rem; border-radius: var(--radius); }
.test-ok { color: #155724; background: #d4edda; }
.test-fail { color: #721c24; background: #f8d7da; }

/* ── Jira Import styles (kept from original ImportView) ───────────────────── */
.loading { color: var(--text-muted); padding: 2rem 0; font-size: 13px; }

.notice {
  display: flex; align-items: flex-start; gap: .65rem;
  padding: .85rem 1rem; border-radius: var(--radius);
  font-size: 13px; margin-bottom: 1.5rem; line-height: 1.5;
}
.notice-warn { background: #fffbea; border: 1px solid #f6d860; color: #7a5c00; }
.notice-warn svg { flex-shrink: 0; margin-top: 1px; }
.notice-warn a { color: var(--bp-blue); font-weight: 600; }

.import-grid {
  display: grid; grid-template-columns: 1fr 1fr; gap: 1.25rem;
}
@media (max-width: 700px) { .import-grid { grid-template-columns: 1fr; } }

.import-card {
  background: var(--bg-card); border: 1px solid var(--border);
  border-radius: 8px; box-shadow: var(--shadow); padding: 1.25rem 1.5rem;
}
.card-title { font-size: 13px; font-weight: 700; color: var(--text); margin-bottom: 1rem; text-transform: uppercase; letter-spacing: .04em; }

.field { display: flex; flex-direction: column; gap: .35rem; }
.field label { font-size: 11px; font-weight: 700; text-transform: uppercase; letter-spacing: .06em; color: var(--text-muted); }

.mapping-section { display: flex; gap: 2rem; flex-wrap: wrap; }
.mapping-group { flex: 1; min-width: 200px; display: flex; flex-direction: column; gap: .5rem; }
.mapping-label { font-size: 11px; font-weight: 700; text-transform: uppercase; letter-spacing: .06em; color: var(--text-muted); margin-bottom: .25rem; }
.mapping-row { display: flex; align-items: center; gap: .6rem; }
.mapping-row--disabled .mapping-jira { color: var(--text-muted); text-decoration: line-through; }
.mapping-row--disabled :deep(.meta-select) { opacity: .5; pointer-events: none; }
.mapping-check { display: inline-flex; align-items: center; cursor: pointer; }
.mapping-check input { margin: 0; }
.mapping-jira { font-size: 12px; font-weight: 500; color: var(--text); min-width: 80px; white-space: nowrap; }

.options-grid { display: flex; flex-direction: column; gap: .85rem; }
.opt-toggle { display: flex; align-items: flex-start; gap: .7rem; cursor: pointer; }
.opt-toggle input[type="checkbox"] { width: 15px; height: 15px; flex-shrink: 0; margin-top: 2px; accent-color: var(--bp-blue); cursor: pointer; }
.opt-text { display: flex; flex-direction: column; gap: .1rem; }
.opt-title { font-size: 13px; font-weight: 600; color: var(--text); }
.opt-desc  { font-size: 12px; color: var(--text-muted); }
.opt-field { display: flex; flex-direction: column; gap: .35rem; }
.opt-field label { font-size: 11px; font-weight: 700; text-transform: uppercase; letter-spacing: .06em; color: var(--text-muted); }
.opt-hint { font-size: 11px; color: var(--text-muted); }

.target-mode-toggle { display: flex; gap: 0; margin-bottom: .85rem; border: 1px solid var(--border); border-radius: var(--radius); overflow: hidden; width: fit-content; }
.mode-btn { background: var(--bg); border: none; padding: .35rem .85rem; font-size: 12px; font-weight: 500; color: var(--text-muted); cursor: pointer; transition: background .12s, color .12s; }
.mode-btn + .mode-btn { border-left: 1px solid var(--border); }
.mode-btn.active { background: var(--bp-blue); color: #fff; }

.import-result {
  margin-top: 1.25rem; padding: .85rem 1rem; border-radius: var(--radius);
  background: #d4edda; border: 1px solid #b8dac6; font-size: 13px;
}
.res-summary { display: flex; align-items: center; flex-wrap: wrap; gap: .35rem; }
.res-ok   { font-weight: 600; color: #155724; }
.res-skip { color: #856404; }
.res-err  { color: #c0392b; font-weight: 600; }
.res-tag  { color: #155724; }
.res-tag-code { font-family: 'DM Mono', monospace; font-size: 11px; background: rgba(0,0,0,.07); border-radius: 3px; padding: .1rem .3rem; }
.res-link { margin-left: .5rem; color: var(--bp-blue); font-weight: 600; text-decoration: none; }
.res-link:hover { text-decoration: underline; }
.err-list { margin-top: .5rem; padding-left: 1.25rem; font-size: 12px; color: #c0392b; }
.err-list li { margin-bottom: .2rem; }
.err-list code { font-family: monospace; font-weight: 700; }

.import-actions { display: flex; flex-direction: column; align-items: flex-start; gap: .5rem; margin-top: 1.25rem; }
.form-error { font-size: 13px; color: #c0392b; background: #fde8e8; padding: .5rem .75rem; border-radius: var(--radius); width: 100%; }

@keyframes spin { to { transform: rotate(360deg); } }
.spin { animation: spin .8s linear infinite; }

.project-picker-btn {
  display: flex; align-items: center; justify-content: space-between; gap: .5rem;
  width: 100%; padding: .5rem .75rem;
  background: var(--bg); border: 1px solid var(--border); border-radius: var(--radius);
  font-size: 13px; color: var(--text); cursor: pointer; text-align: left;
  transition: border-color .12s, box-shadow .12s;
}
.project-picker-btn:hover { border-color: var(--bp-blue); box-shadow: 0 0 0 2px var(--bp-blue-pale); }
.picker-selected { font-weight: 500; color: var(--text); flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.picker-placeholder { color: var(--text-muted); flex: 1; }
.picker-chevron { color: var(--text-muted); flex-shrink: 0; }

.browser-search-wrap { position: relative; margin-bottom: .65rem; }
.browser-search-icon {
  position: absolute; left: .65rem; top: 50%; transform: translateY(-50%);
  color: var(--text-muted); pointer-events: none;
}
.browser-search {
  width: 100%; padding: .55rem .75rem .55rem 2rem;
  border: 1px solid var(--border); border-radius: var(--radius);
  font-size: 13px; color: var(--text); background: var(--bg);
  outline: none; box-sizing: border-box;
}
.browser-search:focus { border-color: var(--bp-blue); box-shadow: 0 0 0 2px var(--bp-blue-pale); }
.browser-count { font-size: 11px; color: var(--text-muted); margin-bottom: .5rem; text-align: right; letter-spacing: .02em; }
.browser-list { max-height: 380px; overflow-y: auto; border: 1px solid var(--border); border-radius: var(--radius); }
.browser-row {
  display: flex; align-items: center; gap: .75rem;
  width: 100%; padding: .6rem 1rem;
  background: none; border: none; border-bottom: 1px solid var(--border);
  cursor: pointer; text-align: left; transition: background .1s;
}
.browser-row:last-child { border-bottom: none; }
.browser-row:hover { background: var(--bg); }
.browser-row.selected { background: var(--bp-blue-pale); }
.browser-key {
  font-size: 12px; font-weight: 700; color: var(--bp-blue-dark);
  background: var(--bp-blue-pale); border-radius: 3px;
  padding: .1rem .4rem; flex-shrink: 0; font-family: 'DM Mono', monospace;
}
.browser-name { font-size: 13px; color: var(--text); flex: 1; }
.browser-check { color: var(--bp-blue); flex-shrink: 0; }
.browser-empty { padding: 1.5rem; text-align: center; color: var(--text-muted); font-size: 13px; }

/* ── Mite styles ──────────────────────────────────────────────────────────── */
.mite-settings-grid { display: grid; grid-template-columns: 1fr 1fr 1fr; gap: 1rem; }
@media (max-width: 700px) { .mite-settings-grid { grid-template-columns: 1fr; } }
.mite-settings-actions { display: flex; align-items: center; gap: .65rem; margin-top: 1rem; flex-wrap: wrap; }
.mite-msg { font-size: 12px; }
.mite-msg-ok { color: #155724; }
.mite-msg-err { color: #c0392b; }

.mite-import-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 1rem; margin-bottom: 1rem; }
@media (max-width: 700px) { .mite-import-grid { grid-template-columns: 1fr; } }

.mite-progress { display: flex; align-items: center; justify-content: space-between; gap: .5rem; padding: .75rem 0; font-size: 13px; color: var(--text-muted); }
.mite-progress-row { display: flex; align-items: center; gap: .5rem; }

.mite-results { margin-top: 1rem; }
.mite-summary {
  display: flex; align-items: center; flex-wrap: wrap; gap: .35rem;
  padding: .85rem 1rem; border-radius: var(--radius);
  background: #d4edda; border: 1px solid #b8dac6; font-size: 13px;
}
.res-warn { color: #856404; }
.res-preview { font-weight: 700; color: var(--bp-blue); }
.mite-summary--preview { background: #e8f4fd; border-color: #b8d8f0; }

.mite-section { margin-top: .75rem; }
.mite-section-toggle {
  display: flex; align-items: center; gap: .35rem;
  background: none; border: none; cursor: pointer;
  font-size: 12px; font-weight: 600; color: var(--text);
  padding: .35rem 0; font-family: inherit;
}
.mite-section-toggle:hover { color: var(--bp-blue); }

.mite-table {
  width: 100%; border-collapse: collapse; font-size: 12px; margin-top: .35rem;
  border: 1px solid var(--border); border-radius: var(--radius);
}
.mite-table th {
  text-align: left; padding: .4rem .65rem;
  background: var(--bg); border-bottom: 1px solid var(--border);
  font-size: 11px; font-weight: 700; text-transform: uppercase; letter-spacing: .04em; color: var(--text-muted);
}
.mite-table td { padding: .4rem .65rem; border-bottom: 1px solid var(--border); color: var(--text); }
.mite-table tr:last-child td { border-bottom: none; }
.mite-table code { font-family: 'DM Mono', monospace; font-size: 11px; background: rgba(0,0,0,.05); border-radius: 3px; padding: .1rem .3rem; }
.mite-err-text { color: #c0392b; }
.text-muted { color: var(--text-muted); }
</style>
