<script setup lang="ts">
import { ref, onMounted, computed, watch, nextTick } from 'vue'
import { api, errMsg } from '@/api/client'
import { useAuthStore } from '@/stores/auth'
import { useRouter, RouterLink } from 'vue-router'
import MetaSelect from '@/components/MetaSelect.vue'
import type { MetaOption } from '@/components/MetaSelect.vue'
import AppFooter from '@/components/AppFooter.vue'

import AppIcon from '@/components/AppIcon.vue'
import AppModal from '@/components/AppModal.vue'
import { STATUSES } from '@/constants/status'

const auth   = useAuthStore()
const router = useRouter()

// Redirect non-admins
if (auth.user && auth.user.role !== 'admin') router.replace('/')

// ── Jira connection status ────────────────────────────────────────────────────
const jiraConfigured = ref(false)
const jiraProjects   = ref<{ key: string; name: string }[]>([])
const jiraLoading    = ref(true)
const jiraError      = ref('')

onMounted(async () => {
  try {
    const cfg = await api.get<{ host: string; email: string; has_token: boolean }>('/integrations/jira')
    jiraConfigured.value = !!(cfg.host && cfg.email && cfg.has_token)
    if (jiraConfigured.value) await loadJiraProjects()
  } catch { jiraConfigured.value = false }
  finally { jiraLoading.value = false }
})

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

const browserSearchRef = ref<HTMLInputElement | null>(null)

function openProjectBrowser() {
  projectBrowserQuery.value = ''
  projectBrowserOpen.value = true
  nextTick(() => browserSearchRef.value?.focus())
}

function selectJiraProject(key: string) {
  selectedJiraProject.value = key
  projectBrowserOpen.value = false
  // auto-fill new project name whenever in new mode
  if (targetMode.value === 'new') {
    newProjectName.value = key
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

// Target mode: existing vs new
const targetMode    = ref<'existing' | 'new'>('existing')
const newProjectName = ref('')
const suggestedName = computed(() => {
  if (!selectedJiraProject.value) return 'Jira Import'
  // Use the Jira project key as the suggested name — concise, unique, matches PAIMOS key conventions
  return selectedJiraProject.value
})
// Auto-fill suggested name when switching to new mode or jira project changes
watch([targetMode, selectedJiraProject], () => {
  if (targetMode.value === 'new' && selectedJiraProject.value) {
    newProjectName.value = suggestedName.value
  }
})

// Field mapping
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
const typeEnabled = ref<Record<string,boolean>>({
  'Epic': true, 'Story': true, 'Bug': true, 'Task': true, 'Change Request': true, 'Sub-task': true,
  'Cost Unit': false, 'Release': false,
})
const STATUS_VAL_OPTIONS: MetaOption[]   = STATUSES.map(v => ({ value: v, label: v }))
const PRIORITY_VAL_OPTIONS: MetaOption[] = ['high','medium','low'].map(v => ({ value: v, label: v.charAt(0).toUpperCase()+v.slice(1) }))

// Smart options
const opts = ref({
  overwrite:            false,
  collision_suffix:     '_jira',
  skip_done:            false,
  import_labels_as_tags:true,
  import_comments:      true,
  import_attachments:   true,
  create_import_tag:    false,
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
const importProgress = ref<{ total: number; processed: number; currentKey: string; phase: string } | null>(null)

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
    // Build type map with only enabled types; collect skip list
    const enabledTypeMap: Record<string,string> = {}
    const skipTypes: string[] = []
    for (const [jiraType, paimosType] of Object.entries(typeMap.value)) {
      if (typeEnabled.value[jiraType] !== false) {
        enabledTypeMap[jiraType] = paimosType
      } else {
        skipTypes.push(jiraType)
      }
    }
    const body: Record<string, any> = {
      project_key: selectedJiraProject.value,
      type_map:    enabledTypeMap,
      status_map:  statusMap.value,
      priority_map:priorityMap.value,
      options:     { ...opts.value, skip_types: skipTypes },
    }
    if (targetMode.value === 'existing') {
      body.target_project_id = Number(selectedBpProject.value)
    } else {
      body.new_project_name = newProjectName.value.trim()
    }
    // Start async job — returns immediately with job ID
    const { job_id } = await api.post<{ job_id: string }>('/import/jira', body)
    // Poll for completion
    while (true) {
      await new Promise(r => setTimeout(r, 2000))
      const job = await api.get<{ status: string; result?: ImportResult; error?: string; total?: number; processed?: number; current_key?: string; phase?: string }>(`/import/jira/jobs/${job_id}`)
      if (job.status === 'complete') {
        importResult.value = job.result ?? null
        importProgress.value = null
        break
      }
      if (job.status === 'error') {
        importError.value = job.error ?? 'Import failed.'
        importProgress.value = null
        break
      }
      // Update progress
      importProgress.value = { total: job.total ?? 0, processed: job.processed ?? 0, currentKey: job.current_key ?? '', phase: job.phase ?? '' }
    }
  } catch (e: unknown) {
    importError.value = errMsg(e, 'Import failed.')
  } finally {
    importing.value = false
  }
}
</script>

<template>
  <Teleport defer to="#app-header-left">
    <span class="ah-title">Import from Jira</span>
    <span class="ah-subtitle">Fetch issues from Jira Cloud and create them in PAIMOS.</span>
  </Teleport>

  <div v-if="jiraLoading" class="loading">Checking Jira configuration…</div>

  <template v-else>
    <!-- Not configured -->
    <div v-if="!jiraConfigured" class="notice notice-warn">
      <AppIcon name="alert-circle" :size="16" />
      Jira credentials not configured. Go to
      <RouterLink to="/settings?tab=integrations">Settings → Integrations</RouterLink>
      to add your Jira Cloud URL, email, and API token.
    </div>

    <template v-else>
      <!-- Source + target -->
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
            <MetaSelect v-model="selectedBpProject" :options="bpProjectOptions" placeholder="Select target project…" searchable />
          </div>
          <div v-else class="field">
            <label>New project name</label>
            <input v-model="newProjectName" type="text" :placeholder="suggestedName" />
            <span class="opt-hint">A new PAIMOS project will be created before importing.</span>
          </div>
        </div>
      </div>

      <!-- Field mapping -->
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

      <!-- Smart options -->
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
              <span class="opt-desc">Download Jira attachments and upload to PAIMOS (max 10 MB each)</span>
            </span>
          </label>
          <label class="opt-toggle">
            <input type="checkbox" v-model="opts.create_import_tag" />
            <span class="opt-text">
              <span class="opt-title">Create import tag</span>
              <span class="opt-desc">Create a JI-timestamp tag and attach it to every imported issue (useful for bulk cleanup)</span>
            </span>
          </label>
          <div class="opt-field">
            <label>Collision suffix</label>
            <input v-model="opts.collision_suffix" type="text" placeholder="_jira" style="max-width:140px" />
            <span class="opt-hint">Appended to new keys when a collision is detected (if not overwriting)</span>
          </div>
        </div>
      </div>

      <!-- Result -->
      <div v-if="importResult" class="import-result">
        <div class="res-summary">
          <span class="res-ok"><AppIcon name="check" :size="13" /> {{ importResult.imported }} imported</span>
          <span v-if="importResult.updated" class="res-updated"> · {{ importResult.updated }} updated</span>
          <span v-if="importResult.skipped" class="res-skip"> · {{ importResult.skipped }} skipped</span>
          <template v-if="importResult.errors?.length">
            <span class="res-err"> · {{ importResult.errors.length }} error(s)</span>
          </template>
          <span v-if="importResult.import_tag" class="res-tag"> · tagged <code class="res-tag-code">{{ importResult.import_tag }}</code></span>
          <RouterLink
            v-if="importResult.target_project_id"
            :to="`/projects/${importResult.target_project_id}`"
            class="res-link"
          ><AppIcon name="arrow-right" :size="13" /> View project</RouterLink>
        </div>
        <details v-if="importResult.skipped_details?.length" class="skip-details">
          <summary class="skip-details-toggle">{{ importResult.skipped_details.length }} skipped — show details</summary>
          <ul class="skip-list">
            <li v-for="s in importResult.skipped_details" :key="s.key"><code>{{ s.key }}</code>: {{ s.reason }}</li>
          </ul>
        </details>
        <ul v-if="importResult.errors?.length" class="err-list">
          <li v-for="e in importResult.errors" :key="e.key"><code>{{ e.key }}</code>: {{ e.reason }}</li>
        </ul>
      </div>

      <!-- Actions -->
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
        <div v-if="importProgress" class="import-progress">
          <div class="progress-bar-wrap">
            <div class="progress-bar-fill" :style="{ width: importProgress.total ? (importProgress.processed / importProgress.total * 100) + '%' : '0%' }" />
          </div>
          <span class="progress-text">
            <template v-if="importProgress.phase === 'fetching'">Fetching issues from Jira…</template>
            <template v-else>{{ importProgress.processed }} / {{ importProgress.total }} issues · {{ importProgress.currentKey }}</template>
          </span>
        </div>
      </div>
    </template>
  </template>

  <!-- Jira project browser modal -->
  <AppModal title="Select Jira Project" :open="projectBrowserOpen" max-width="560px" @close="projectBrowserOpen = false">
    <div class="browser-search-wrap">
      <AppIcon name="search" :size="14" class="browser-search-icon" />
      <input
        ref="browserSearchRef"
        v-model="projectBrowserQuery"
        class="browser-search"
        type="text"
        placeholder="Search by key or name…"
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

  <AppFooter />
</template>

<style scoped>
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
  margin-bottom: 0;
}
@media (max-width: 700px) { .import-grid { grid-template-columns: 1fr; } }

.import-card {
  background: var(--bg-card); border: 1px solid var(--border);
  border-radius: 8px; box-shadow: var(--shadow); padding: 1.25rem 1.5rem;
}
.card-title { font-size: 13px; font-weight: 700; color: var(--text); margin-bottom: 1rem; text-transform: uppercase; letter-spacing: .04em; }

.field { display: flex; flex-direction: column; gap: .35rem; }
.field label { font-size: 11px; font-weight: 700; text-transform: uppercase; letter-spacing: .06em; color: var(--text-muted); }

/* Mapping */
.mapping-section { display: flex; gap: 2rem; flex-wrap: wrap; }
.mapping-group { flex: 1; min-width: 200px; display: flex; flex-direction: column; gap: .5rem; }
.mapping-label { font-size: 11px; font-weight: 700; text-transform: uppercase; letter-spacing: .06em; color: var(--text-muted); margin-bottom: .25rem; }
.mapping-row { display: flex; align-items: center; gap: .6rem; }
.mapping-row--disabled { opacity: .4; }
.mapping-check { display: flex; align-items: center; flex-shrink: 0; cursor: pointer; }
.mapping-check input { margin: 0; cursor: pointer; }
.mapping-jira { font-size: 12px; font-weight: 500; color: var(--text); min-width: 80px; white-space: nowrap; }

/* Options */
.options-grid { display: flex; flex-direction: column; gap: .85rem; }
.opt-toggle { display: flex; align-items: flex-start; gap: .7rem; cursor: pointer; }
.opt-toggle input[type="checkbox"] { width: 15px; height: 15px; flex-shrink: 0; margin-top: 2px; accent-color: var(--bp-blue); cursor: pointer; }
.opt-text { display: flex; flex-direction: column; gap: .1rem; }
.opt-title { font-size: 13px; font-weight: 600; color: var(--text); }
.opt-desc  { font-size: 12px; color: var(--text-muted); }
.opt-field { display: flex; flex-direction: column; gap: .35rem; }
.opt-field label { font-size: 11px; font-weight: 700; text-transform: uppercase; letter-spacing: .06em; color: var(--text-muted); }
.opt-hint { font-size: 11px; color: var(--text-muted); }

/* Target mode toggle */
.target-mode-toggle { display: flex; gap: 0; margin-bottom: .85rem; border: 1px solid var(--border); border-radius: var(--radius); overflow: hidden; width: fit-content; }
.mode-btn { background: var(--bg); border: none; padding: .35rem .85rem; font-size: 12px; font-weight: 500; color: var(--text-muted); cursor: pointer; transition: background .12s, color .12s; }
.mode-btn + .mode-btn { border-left: 1px solid var(--border); }
.mode-btn.active { background: var(--bp-blue); color: #fff; }

/* Result */
.import-result {
  margin-top: 1.25rem; padding: .85rem 1rem; border-radius: var(--radius);
  background: #d4edda; border: 1px solid #b8dac6; font-size: 13px;
}
.res-summary { display: flex; align-items: center; flex-wrap: wrap; gap: .35rem; }
.res-ok      { font-weight: 600; color: #155724; }
.res-updated { font-weight: 600; color: #0c5460; }
.res-skip    { color: #856404; }
.res-err     { color: #c0392b; font-weight: 600; }
.res-tag  { color: #155724; }
.res-tag-code { font-family: 'DM Mono', monospace; font-size: 11px; background: rgba(0,0,0,.07); border-radius: 3px; padding: .1rem .3rem; }
.res-link { margin-left: .5rem; color: var(--bp-blue); font-weight: 600; text-decoration: none; }
.res-link:hover { text-decoration: underline; }
.skip-details { margin-top: .5rem; }
.skip-details-toggle { font-size: 12px; color: #856404; cursor: pointer; font-weight: 500; }
.skip-details-toggle:hover { text-decoration: underline; }
.skip-list { margin-top: .35rem; padding-left: 1.25rem; font-size: 12px; color: #856404; }
.skip-list li { margin-bottom: .2rem; }
.skip-list code { font-family: monospace; font-weight: 700; }
.err-list { margin-top: .5rem; padding-left: 1.25rem; font-size: 12px; color: #c0392b; }
.err-list li { margin-bottom: .2rem; }
.err-list code { font-family: monospace; font-weight: 700; }

.import-actions { display: flex; flex-direction: column; align-items: flex-start; gap: .5rem; margin-top: 1.25rem; }
.import-progress { width: 100%; }
.progress-bar-wrap { width: 100%; height: 6px; background: #e5e7eb; border-radius: 3px; overflow: hidden; }
.progress-bar-fill { height: 100%; background: var(--bp-blue); border-radius: 3px; transition: width .3s; }
.progress-text { font-size: 12px; color: var(--text-muted); margin-top: .25rem; display: block; }
.form-error { font-size: 13px; color: #c0392b; background: #fde8e8; padding: .5rem .75rem; border-radius: var(--radius); width: 100%; }

@keyframes spin { to { transform: rotate(360deg); } }
.spin { animation: spin .8s linear infinite; }

/* Project picker button */
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

/* Browser modal */
.browser-search-wrap {
  position: relative; margin-bottom: .65rem;
}
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
.browser-count {
  font-size: 11px; color: var(--text-muted); margin-bottom: .5rem;
  text-align: right; letter-spacing: .02em;
}
.browser-list {
  max-height: 380px; overflow-y: auto;
  border: 1px solid var(--border); border-radius: var(--radius);
}
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
</style>
