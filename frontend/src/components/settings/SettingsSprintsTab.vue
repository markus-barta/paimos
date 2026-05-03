<script setup lang="ts">
import LoadingText from "@/components/LoadingText.vue";
import { ref } from 'vue'
import { api, errMsg } from '@/api/client'
import type { Sprint } from '@/types'
import AppModal from '@/components/AppModal.vue'
import MetaSelect from '@/components/MetaSelect.vue'

const sprintYear        = ref(new Date().getFullYear())
const sprintFirstDay    = ref('')
const sprintDuration    = ref(14)
const sprintFormat      = ref('YY"S"NN')
const sprintsList       = ref<Sprint[]>([])
const sprintsLoading    = ref(false)
const sprintBatchMsg    = ref('')
const sprintBatchError  = ref('')
const sprintBatchBusy   = ref(false)

// Sprint edit
const editSprintTarget  = ref<Sprint | null>(null)
const editSprintForm    = ref({ title: '', start_date: '', end_date: '', sprint_state: '', target_ar: null as number | null })
const editSprintError   = ref('')
const editSprintSaving  = ref(false)

function openEditSprint(s: Sprint) {
  editSprintTarget.value = s
  editSprintForm.value = {
    title: s.title,
    start_date: s.start_date?.slice(0, 10) || '',
    end_date: s.end_date?.slice(0, 10) || '',
    sprint_state: s.sprint_state || 'planned',
    target_ar: s.target_ar ?? null,
  }
  editSprintError.value = ''
}

async function saveSprint() {
  if (!editSprintTarget.value) return
  editSprintError.value = ''
  editSprintSaving.value = true
  try {
    await api.put(`/sprints/${editSprintTarget.value.id}`, editSprintForm.value)
    editSprintTarget.value = null
    await loadSprintsForYear()
  } catch (e: unknown) {
    editSprintError.value = errMsg(e, 'Save failed.')
  } finally {
    editSprintSaving.value = false
  }
}

function previewSprintTitle(num: number): string {
  const yy   = String(sprintYear.value % 100).padStart(2, '0')
  const yyyy = String(sprintYear.value)
  const nn   = String(num).padStart(2, '0')
  const fmt  = sprintFormat.value
  let result = ''
  let i = 0
  while (i < fmt.length) {
    if (fmt[i] === '"') {
      i++
      while (i < fmt.length && fmt[i] !== '"') result += fmt[i++]
      if (i < fmt.length) i++
    } else if (fmt.startsWith('YYYY', i)) {
      result += yyyy; i += 4
    } else if (fmt.startsWith('YY', i)) {
      result += yy;   i += 2
    } else if (fmt.startsWith('NN', i)) {
      result += nn;   i += 2
    } else {
      result += fmt[i++]
    }
  }
  return result
}

async function loadSprintsForYear() {
  sprintsLoading.value = true
  try {
    sprintsList.value = await api.get<Sprint[]>(`/sprints/${sprintYear.value}`)
  } finally {
    sprintsLoading.value = false
  }
}

async function createSprintsBatch() {
  sprintBatchMsg.value   = ''
  sprintBatchError.value = ''
  if (!sprintFirstDay.value) { sprintBatchError.value = 'First day is required.'; return }
  sprintBatchBusy.value = true
  try {
    const res = await api.post<{ created: number; skipped: string[] }>('/sprints/batch', {
      first_day:     sprintFirstDay.value,
      duration_days: sprintDuration.value,
      year:          sprintYear.value,
      title_format:  sprintFormat.value || 'YYsNN',
    })
    sprintBatchMsg.value = `Created ${res.created} sprint${res.created !== 1 ? 's' : ''}.${res.skipped.length ? ` Skipped ${res.skipped.length} existing.` : ''}`
    await loadSprintsForYear()
  } catch (e: unknown) {
    sprintBatchError.value = errMsg(e, 'Failed to create sprints.')
  } finally {
    sprintBatchBusy.value = false
  }
}

async function toggleArchiveSprint(s: Sprint) {
  await api.patch(`/issues/${s.id}/archive`, { archived: !s.archived })
  await loadSprintsForYear()
}

// Init
loadSprintsForYear()
</script>

<template>
  <!-- Sprint Setup -->
  <div class="section">
    <div class="section-header">
      <h2 class="section-title">Sprint Setup</h2>
      <p class="section-desc">Generate the full sprint calendar for a year. Existing sprints with the same title are skipped.</p>
    </div>

    <div class="sprint-setup-form card">
      <div class="sprint-setup-row">
        <label class="sprint-label">Year</label>
        <input type="number" v-model.number="sprintYear" class="form-input sprint-input-sm" min="2020" max="2099" @change="loadSprintsForYear" />
      </div>
      <div class="sprint-setup-row">
        <label class="sprint-label">First day of Sprint 1</label>
        <input type="date" v-model="sprintFirstDay" class="form-input sprint-input-date" />
      </div>
      <div class="sprint-setup-row">
        <label class="sprint-label">Sprint duration</label>
        <div class="sprint-dur-row">
          <input type="number" v-model.number="sprintDuration" class="form-input sprint-input-sm" min="1" max="90" />
          <span class="sprint-dur-unit">days</span>
        </div>
      </div>
      <div class="sprint-setup-row sprint-setup-row--top">
        <label class="sprint-label">
          Title format
          <span class="sprint-label-hint">tokens: <code>YY</code> <code>YYYY</code> <code>NN</code> · wrap literals in <code>"…"</code></span>
        </label>
        <div class="sprint-format-col">
          <input type="text" v-model="sprintFormat" class="form-input sprint-input-format" placeholder="YYsNN" />
          <div class="sprint-format-preview">
            <span class="sprint-format-example">Sprint 0 → <strong>{{ previewSprintTitle(0) }}</strong></span>
            <span class="sprint-format-sep">·</span>
            <span class="sprint-format-example">Sprint 1 → <strong>{{ previewSprintTitle(1) }}</strong></span>
            <span class="sprint-format-sep">·</span>
            <span class="sprint-format-example">Sprint 13 → <strong>{{ previewSprintTitle(13) }}</strong></span>
          </div>
        </div>
      </div>
      <div class="sprint-setup-actions">
        <div v-if="sprintBatchMsg" class="sprint-msg sprint-msg--ok">{{ sprintBatchMsg }}</div>
        <div v-if="sprintBatchError" class="sprint-msg sprint-msg--err">{{ sprintBatchError }}</div>
        <button class="btn btn-primary" :disabled="sprintBatchBusy" @click="createSprintsBatch">
          {{ sprintBatchBusy ? 'Creating…' : 'Create Sprints' }}
        </button>
      </div>
    </div>
  </div>

  <!-- Sprint List -->
  <div class="section" style="margin-top:2rem">
    <div class="section-header">
      <h2 class="section-title">Sprints {{ sprintYear }}</h2>
      <p class="section-desc">Active and archived sprints for the selected year.</p>
    </div>

    <LoadingText v-if="sprintsLoading" class="empty-hint" label="Loading…" />
    <div v-else-if="sprintsList.length === 0" class="empty-hint">No sprints for {{ sprintYear }} yet. Use Sprint Setup above to generate them.</div>
    <div v-else class="card" style="padding:0;overflow:hidden">
      <table class="settings-table">
        <thead>
          <tr>
            <th>Title</th>
            <th>Start</th>
            <th>End</th>
            <th>Status</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="s in sprintsList" :key="s.id" :class="{ 'sprint-row--archived': s.archived }">
            <td class="fw500">{{ s.title }}</td>
            <td class="muted">{{ s.start_date?.slice(0,10) || '—' }}</td>
            <td class="muted">{{ s.end_date?.slice(0,10) || '—' }}</td>
            <td>
              <span v-if="s.archived" class="badge badge-archived">Archived</span>
              <span v-else class="badge badge-active">Active</span>
            </td>
            <td class="actions-cell">
              <button class="btn btn-ghost btn-sm" @click="openEditSprint(s)">Edit</button>
              <button class="btn btn-ghost btn-sm" @click="toggleArchiveSprint(s)">
                {{ s.archived ? 'Unarchive' : 'Archive' }}
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>

  <!-- Edit Sprint modal -->
  <AppModal title="Edit Sprint" :open="!!editSprintTarget" @close="editSprintTarget=null">
    <form @submit.prevent="saveSprint" class="form">
      <div class="field"><label>Title</label><input v-model="editSprintForm.title" type="text" required /></div>
      <div class="field"><label>Start date</label><input v-model="editSprintForm.start_date" type="date" /></div>
      <div class="field"><label>End date</label><input v-model="editSprintForm.end_date" type="date" /></div>
      <div class="field"><label>State</label>
        <MetaSelect v-model="editSprintForm.sprint_state" :options="[
          { value: 'planned', label: 'Planned' },
          { value: 'active', label: 'Active' },
          { value: 'complete', label: 'Complete' },
          { value: 'archived', label: 'Archived' },
        ]" />
      </div>
      <div class="field"><label>Target AR (€)</label><input v-model.number="editSprintForm.target_ar" type="number" step="0.01" placeholder="e.g. 25000" /></div>
      <div v-if="editSprintError" class="form-error">{{ editSprintError }}</div>
      <div class="form-actions">
        <button type="button" class="btn btn-ghost" @click="editSprintTarget=null">Cancel</button>
        <button type="submit" class="btn btn-primary" :disabled="editSprintSaving">{{ editSprintSaving ? 'Saving…' : 'Save' }}</button>
      </div>
    </form>
  </AppModal>
</template>

<style src="./settings-shared.css"></style>
<style scoped>
.sprint-setup-form { display: flex; flex-direction: column; gap: 1rem; }
.sprint-setup-row  { display: flex; align-items: center; gap: 1rem; }
.sprint-setup-row--top { align-items: flex-start; }
.sprint-label      { font-size: 13px; font-weight: 600; color: var(--text); min-width: 180px; flex-shrink: 0; }
.sprint-label-hint { display: block; font-size: 11px; font-weight: 400; color: var(--text-muted); margin-top: .2rem; }
.sprint-label-hint code { font-family: 'DM Mono','Fira Code',monospace; background: var(--bg); border: 1px solid var(--border); border-radius: 3px; padding: .05rem .3rem; font-size: 10px; }
.sprint-input-sm   { width: 100px; }
.sprint-input-date { width: 180px; }
.sprint-input-format { width: 140px; }
.sprint-format-col { display: flex; flex-direction: column; gap: .4rem; }
.sprint-format-preview { display: flex; align-items: center; gap: .4rem; flex-wrap: wrap; }
.sprint-format-example { font-size: 12px; color: var(--text-muted); }
.sprint-format-example strong { color: var(--text); font-family: 'DM Mono','Fira Code',monospace; }
.sprint-format-sep  { color: var(--border); font-size: 12px; }
.sprint-dur-row    { display: flex; align-items: center; gap: .5rem; }
.sprint-dur-unit   { font-size: 13px; color: var(--text-muted); }
.sprint-setup-actions { display: flex; align-items: center; gap: 1rem; padding-top: .5rem; }
.sprint-msg        { font-size: 13px; padding: .3rem .65rem; border-radius: var(--radius); }
.sprint-msg--ok    { background: #d4edda; color: #155724; }
.sprint-msg--err   { background: #f8d7da; color: #721c24; }
.sprint-row--archived td { opacity: .55; }
.badge-active   { background: #d4edda; color: #155724; }
</style>
