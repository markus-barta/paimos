<script setup lang="ts">
// PAI-326. Project-settings panel — declarable agents per project.
// List rows show name + description + slash-command + lane-tag chips
// with inline add / edit / delete. Validation mirrors the server's
// rules for fast feedback; the server is the source of truth for
// uniqueness (409 collision is surfaced as a save error).

import { computed, onMounted, ref, watch } from 'vue'
import { errMsg } from '@/api/client'
import type { ProjectAgent, ProjectAgentInput } from '@/types'
import {
  createProjectAgent,
  deleteProjectAgent,
  listProjectAgents,
  updateProjectAgent,
  validateAgentName,
} from '@/services/projectAgents'
import LoadingText from '@/components/LoadingText.vue'

const props = defineProps<{
  projectId: number
  canWrite: boolean
}>()

const emit = defineEmits<{
  count: [n: number]
}>()

const agents = ref<ProjectAgent[]>([])
const loading = ref(true)
const loadError = ref('')

// Inline-editor state. `editingName` is the original name of the row
// being edited (or null = adding a new agent / not editing). The form
// holds the working copy; lane_tags is edited as a comma-separated
// string for typing convenience and split on save.
const editingName = ref<string | null>(null)
const adding = ref(false)
const form = ref<ProjectAgentInput>(emptyForm())
const laneTagsInput = ref('')
const saveError = ref('')
const saving = ref(false)

watch(
  () => agents.value.length,
  (n) => emit('count', n),
  { immediate: true },
)

function emptyForm(): ProjectAgentInput {
  return {
    name: '',
    description: '',
    slash_command_name: '',
    lane_tags: [],
    metadata: {},
  }
}

const nameError = computed(() => validateAgentName(form.value.name))
const isFormValid = computed(() => nameError.value === '')

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    agents.value = await listProjectAgents(props.projectId)
  } catch (e) {
    loadError.value = errMsg(e, 'Failed to load agents.')
  } finally {
    loading.value = false
  }
}

function startAdd() {
  editingName.value = null
  adding.value = true
  form.value = emptyForm()
  laneTagsInput.value = ''
  saveError.value = ''
}

function startEdit(agent: ProjectAgent) {
  editingName.value = agent.name
  adding.value = false
  form.value = {
    name: agent.name,
    description: agent.description,
    slash_command_name: agent.slash_command_name,
    lane_tags: [...agent.lane_tags],
    metadata: { ...agent.metadata },
  }
  laneTagsInput.value = agent.lane_tags.join(', ')
  saveError.value = ''
}

function cancelEdit() {
  editingName.value = null
  adding.value = false
  form.value = emptyForm()
  laneTagsInput.value = ''
  saveError.value = ''
}

function parseLaneTags(raw: string): string[] {
  return raw
    .split(',')
    .map((t) => t.trim())
    .filter((t) => t !== '')
}

async function save() {
  if (!isFormValid.value) {
    saveError.value = nameError.value
    return
  }
  saving.value = true
  saveError.value = ''
  const payload: ProjectAgentInput = {
    ...form.value,
    name: form.value.name.trim(),
    description: form.value.description.trim(),
    slash_command_name: form.value.slash_command_name.trim(),
    lane_tags: parseLaneTags(laneTagsInput.value),
  }
  try {
    if (editingName.value === null) {
      await createProjectAgent(props.projectId, payload)
    } else {
      await updateProjectAgent(props.projectId, editingName.value, payload)
    }
    cancelEdit()
    await load()
  } catch (e) {
    saveError.value = errMsg(e, 'Failed to save agent.')
  } finally {
    saving.value = false
  }
}

async function remove(agent: ProjectAgent) {
  if (!confirm(`Remove agent "${agent.name}"?`)) return
  saveError.value = ''
  try {
    await deleteProjectAgent(props.projectId, agent.name)
    if (editingName.value === agent.name) cancelEdit()
    await load()
  } catch (e) {
    saveError.value = errMsg(e, 'Failed to remove agent.')
  }
}

onMounted(load)
</script>

<template>
  <section class="pa-section">
    <div class="pa-header">
      <div>
        <h3 class="pa-title">Agents</h3>
        <p class="pa-desc">
          Declarable agents that work this project (e.g. <code>ops</code>, <code>dev</code>,
          <code>refinement</code>). Names follow <code>[a-z][a-z0-9_-]*</code>, max 32 chars.
        </p>
      </div>
      <button
        v-if="canWrite && !adding && editingName === null"
        type="button"
        class="btn btn-ghost btn-sm"
        @click="startAdd"
      >
        Add agent
      </button>
    </div>

    <div v-if="loadError" class="pa-error">{{ loadError }}</div>
    <LoadingText v-if="loading" class="pa-empty" label="Loading agents…" />

    <div v-else-if="!agents.length && !adding" class="pa-empty">
      No agents declared yet.
    </div>

    <div v-else class="pa-list">
      <div v-for="agent in agents" :key="agent.id" class="pa-row">
        <template v-if="editingName === agent.name">
          <div class="pa-form">
            <div class="pa-field">
              <label>Name</label>
              <input v-model="form.name" type="text" maxlength="32" />
              <span v-if="nameError" class="pa-field-error">{{ nameError }}</span>
            </div>
            <div class="pa-field">
              <label>Description</label>
              <input v-model="form.description" type="text" placeholder="What does this agent own?" />
            </div>
            <div class="pa-field">
              <label>Slash command</label>
              <input v-model="form.slash_command_name" type="text" placeholder="e.g. ops" />
            </div>
            <div class="pa-field">
              <label>Lane tags <span class="pa-hint">comma-separated</span></label>
              <input v-model="laneTagsInput" type="text" placeholder="ops, infra" />
            </div>
            <div v-if="saveError" class="pa-error">{{ saveError }}</div>
            <div class="pa-actions">
              <button type="button" class="btn btn-ghost btn-sm" @click="cancelEdit">Cancel</button>
              <button
                type="button"
                class="btn btn-primary btn-sm"
                :disabled="!isFormValid || saving"
                @click="save"
              >
                {{ saving ? 'Saving…' : 'Save' }}
              </button>
            </div>
          </div>
        </template>
        <template v-else>
          <div class="pa-row-main">
            <div class="pa-row-head">
              <span class="pa-name">{{ agent.name }}</span>
              <span v-if="agent.slash_command_name" class="pa-slash">/{{ agent.slash_command_name }}</span>
            </div>
            <div v-if="agent.description" class="pa-row-desc">{{ agent.description }}</div>
            <div v-if="agent.lane_tags.length" class="pa-chips">
              <span v-for="tag in agent.lane_tags" :key="tag" class="pa-chip">{{ tag }}</span>
            </div>
          </div>
          <div v-if="canWrite && editingName === null && !adding" class="pa-row-actions">
            <button type="button" class="btn btn-ghost btn-sm" @click="startEdit(agent)">Edit</button>
            <button type="button" class="btn btn-ghost btn-sm danger" @click="remove(agent)">Remove</button>
          </div>
        </template>
      </div>

      <div v-if="adding" class="pa-row pa-row--adding">
        <div class="pa-form">
          <div class="pa-field">
            <label>Name <span class="pa-hint">slug, max 32 chars</span></label>
            <input v-model="form.name" type="text" maxlength="32" autofocus placeholder="e.g. ops" />
            <span v-if="nameError" class="pa-field-error">{{ nameError }}</span>
          </div>
          <div class="pa-field">
            <label>Description</label>
            <input v-model="form.description" type="text" placeholder="What does this agent own?" />
          </div>
          <div class="pa-field">
            <label>Slash command</label>
            <input v-model="form.slash_command_name" type="text" placeholder="e.g. ops" />
          </div>
          <div class="pa-field">
            <label>Lane tags <span class="pa-hint">comma-separated</span></label>
            <input v-model="laneTagsInput" type="text" placeholder="ops, infra" />
          </div>
          <div v-if="saveError" class="pa-error">{{ saveError }}</div>
          <div class="pa-actions">
            <button type="button" class="btn btn-ghost btn-sm" @click="cancelEdit">Cancel</button>
            <button
              type="button"
              class="btn btn-primary btn-sm"
              :disabled="!isFormValid || saving"
              @click="save"
            >
              {{ saving ? 'Adding…' : 'Add agent' }}
            </button>
          </div>
        </div>
      </div>
    </div>
  </section>
</template>

<style scoped>
.pa-section { display: flex; flex-direction: column; gap: .8rem; margin-top: 1rem; }
.pa-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 1rem; }
.pa-title { font-size: 14px; font-weight: 800; color: var(--text); margin: 0 0 .15rem; }
.pa-desc { margin: 0; color: var(--text-muted); font-size: 12px; }
.pa-desc code { background: var(--bg); border: 1px solid var(--border); border-radius: 4px; padding: 0 .2rem; font-size: 11px; }
.pa-empty { color: var(--text-muted); font-size: 13px; padding: .5rem 0; }
.pa-error { color: #b42318; background: #fef3f2; border: 1px solid #fecdca; border-radius: 8px; padding: .5rem .65rem; font-size: 12px; }
.pa-list { display: flex; flex-direction: column; gap: .55rem; }
.pa-row { display: flex; align-items: flex-start; justify-content: space-between; gap: .8rem; padding: .65rem .8rem; border: 1px solid var(--border); border-radius: 8px; background: var(--bg); }
.pa-row--adding { background: var(--bg-card); border-style: dashed; }
.pa-row-main { min-width: 0; flex: 1; display: flex; flex-direction: column; gap: .25rem; }
.pa-row-head { display: flex; align-items: baseline; gap: .5rem; }
.pa-name { font-weight: 700; font-size: 13px; color: var(--text); font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
.pa-slash { color: var(--text-muted); font-size: 12px; font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
.pa-row-desc { color: var(--text); font-size: 13px; }
.pa-chips { display: flex; flex-wrap: wrap; gap: .3rem; margin-top: .15rem; }
.pa-chip { display: inline-block; background: var(--bg-card); border: 1px solid var(--border); border-radius: 999px; padding: .1rem .55rem; font-size: 11px; color: var(--text-muted); }
.pa-row-actions { display: flex; gap: .35rem; align-items: flex-start; }
.pa-form { width: 100%; display: flex; flex-direction: column; gap: .55rem; }
.pa-field { display: flex; flex-direction: column; gap: .2rem; }
.pa-field label { font-size: 12px; color: var(--text-muted); font-weight: 600; }
.pa-field input { width: 100%; border: 1px solid var(--border); border-radius: 6px; background: var(--bg); color: var(--text); font: inherit; padding: .45rem .55rem; }
.pa-field-error { color: #b42318; font-size: 11px; }
.pa-hint { color: var(--text-muted); font-weight: 400; font-size: 11px; }
.pa-actions { display: flex; gap: .4rem; justify-content: flex-end; }
</style>
