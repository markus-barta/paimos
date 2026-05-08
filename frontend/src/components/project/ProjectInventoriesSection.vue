<script setup lang="ts">
// PAI-329 — project-level shared inventories. Two side-by-side
// editors (environments + deploy recipes) that the canonical agent
// artifact endpoint inlines into every rendered agent.
//
// Mirrors ProjectAgentsSection's inline-add / inline-edit pattern so
// the project-settings panel feels uniform. Validation is the
// pass-through echo of validateInventoryName (server is the source
// of truth for uniqueness; 409 is surfaced as a save error).

import { computed, onMounted, ref, watch } from 'vue'
import { errMsg } from '@/api/client'
import type {
  ProjectDeployRecipe,
  ProjectDeployRecipeInput,
  ProjectEnvironment,
  ProjectEnvironmentInput,
} from '@/types'
import {
  createProjectDeployRecipe,
  createProjectEnvironment,
  deleteProjectDeployRecipe,
  deleteProjectEnvironment,
  listProjectDeployRecipes,
  listProjectEnvironments,
  updateProjectDeployRecipe,
  updateProjectEnvironment,
  validateInventoryName,
} from '@/services/projectInventories'
import LoadingText from '@/components/LoadingText.vue'

const props = defineProps<{
  projectId: number
  canWrite: boolean
}>()

const emit = defineEmits<{
  count: [n: { environments: number; deploy_recipes: number }]
}>()

// ── environments ──────────────────────────────────────────────────

const envs = ref<ProjectEnvironment[]>([])
const envLoading = ref(true)
const envError = ref('')
const envEditingId = ref<number | null>(null)
const envAdding = ref(false)
const envForm = ref<ProjectEnvironmentInput>(emptyEnvForm())
const envSaving = ref(false)
const envSaveError = ref('')

const envNameError = computed(() => validateInventoryName(envForm.value.name))
const envFormValid = computed(() => envNameError.value === '')

function emptyEnvForm(): ProjectEnvironmentInput {
  return { name: '', url: '', host_alias: '', host_ip: '' }
}

async function loadEnvs() {
  envLoading.value = true
  envError.value = ''
  try {
    envs.value = await listProjectEnvironments(props.projectId)
  } catch (e) {
    envError.value = errMsg(e, 'Failed to load environments.')
  } finally {
    envLoading.value = false
  }
}

function startEnvAdd() {
  envEditingId.value = null
  envAdding.value = true
  envForm.value = emptyEnvForm()
  envSaveError.value = ''
}

function startEnvEdit(env: ProjectEnvironment) {
  envEditingId.value = env.id
  envAdding.value = false
  envForm.value = {
    name: env.name,
    url: env.url,
    host_alias: env.host_alias,
    host_ip: env.host_ip,
  }
  envSaveError.value = ''
}

function cancelEnvEdit() {
  envEditingId.value = null
  envAdding.value = false
  envForm.value = emptyEnvForm()
  envSaveError.value = ''
}

async function saveEnv() {
  if (!envFormValid.value) {
    envSaveError.value = envNameError.value
    return
  }
  envSaving.value = true
  envSaveError.value = ''
  const payload: ProjectEnvironmentInput = {
    name: envForm.value.name.trim(),
    url: envForm.value.url.trim(),
    host_alias: envForm.value.host_alias.trim(),
    host_ip: envForm.value.host_ip.trim(),
  }
  try {
    if (envEditingId.value === null) {
      await createProjectEnvironment(props.projectId, payload)
    } else {
      await updateProjectEnvironment(props.projectId, envEditingId.value, payload)
    }
    cancelEnvEdit()
    await loadEnvs()
  } catch (e) {
    envSaveError.value = errMsg(e, 'Failed to save environment.')
  } finally {
    envSaving.value = false
  }
}

async function removeEnv(env: ProjectEnvironment) {
  if (!confirm(`Remove environment "${env.name}"?`)) return
  envSaveError.value = ''
  try {
    await deleteProjectEnvironment(props.projectId, env.id)
    if (envEditingId.value === env.id) cancelEnvEdit()
    await loadEnvs()
  } catch (e) {
    envSaveError.value = errMsg(e, 'Failed to remove environment.')
  }
}

// ── deploy recipes ────────────────────────────────────────────────

const recipes = ref<ProjectDeployRecipe[]>([])
const recLoading = ref(true)
const recError = ref('')
const recEditingId = ref<number | null>(null)
const recAdding = ref(false)
const recForm = ref<ProjectDeployRecipeInput>(emptyRecForm())
const recSaving = ref(false)
const recSaveError = ref('')

const recNameError = computed(() => validateInventoryName(recForm.value.name))
const recFormValid = computed(() => recNameError.value === '')

function emptyRecForm(): ProjectDeployRecipeInput {
  return { name: '', command: '', summary: '' }
}

async function loadRecipes() {
  recLoading.value = true
  recError.value = ''
  try {
    recipes.value = await listProjectDeployRecipes(props.projectId)
  } catch (e) {
    recError.value = errMsg(e, 'Failed to load deploy recipes.')
  } finally {
    recLoading.value = false
  }
}

function startRecAdd() {
  recEditingId.value = null
  recAdding.value = true
  recForm.value = emptyRecForm()
  recSaveError.value = ''
}

function startRecEdit(rec: ProjectDeployRecipe) {
  recEditingId.value = rec.id
  recAdding.value = false
  recForm.value = {
    name: rec.name,
    command: rec.command,
    summary: rec.summary,
  }
  recSaveError.value = ''
}

function cancelRecEdit() {
  recEditingId.value = null
  recAdding.value = false
  recForm.value = emptyRecForm()
  recSaveError.value = ''
}

async function saveRec() {
  if (!recFormValid.value) {
    recSaveError.value = recNameError.value
    return
  }
  recSaving.value = true
  recSaveError.value = ''
  const payload: ProjectDeployRecipeInput = {
    name: recForm.value.name.trim(),
    command: recForm.value.command.trim(),
    summary: recForm.value.summary.trim(),
  }
  try {
    if (recEditingId.value === null) {
      await createProjectDeployRecipe(props.projectId, payload)
    } else {
      await updateProjectDeployRecipe(props.projectId, recEditingId.value, payload)
    }
    cancelRecEdit()
    await loadRecipes()
  } catch (e) {
    recSaveError.value = errMsg(e, 'Failed to save deploy recipe.')
  } finally {
    recSaving.value = false
  }
}

async function removeRec(rec: ProjectDeployRecipe) {
  if (!confirm(`Remove deploy recipe "${rec.name}"?`)) return
  recSaveError.value = ''
  try {
    await deleteProjectDeployRecipe(props.projectId, rec.id)
    if (recEditingId.value === rec.id) cancelRecEdit()
    await loadRecipes()
  } catch (e) {
    recSaveError.value = errMsg(e, 'Failed to remove deploy recipe.')
  }
}

// ── load + count emit ─────────────────────────────────────────────

watch(
  [() => envs.value.length, () => recipes.value.length],
  ([envCount, recCount]) => {
    emit('count', { environments: envCount, deploy_recipes: recCount })
  },
  { immediate: true },
)

onMounted(async () => {
  await Promise.all([loadEnvs(), loadRecipes()])
})
</script>

<template>
  <section class="pi-section">
    <div class="pi-header">
      <h3 class="pi-title">Project inventories</h3>
      <p class="pi-desc">
        Shared across agents — every rendered agent artifact inherits these.
      </p>
    </div>

    <!-- environments -->
    <div class="pi-block">
      <div class="pi-block-head">
        <h4>Environments</h4>
        <button
          v-if="canWrite && !envAdding && envEditingId === null"
          type="button"
          class="btn btn-ghost btn-sm"
          @click="startEnvAdd"
        >
          Add environment
        </button>
      </div>
      <div v-if="envError" class="pi-error">{{ envError }}</div>
      <LoadingText v-if="envLoading" class="pi-empty" label="Loading environments…" />
      <div v-else-if="!envs.length && !envAdding" class="pi-empty">No environments declared yet.</div>
      <div v-else class="pi-list">
        <div v-for="env in envs" :key="env.id" class="pi-row">
          <template v-if="envEditingId === env.id">
            <div class="pi-form">
              <input v-model="envForm.name" type="text" placeholder="name (e.g. staging)" />
              <span v-if="envNameError" class="pi-field-error">{{ envNameError }}</span>
              <input v-model="envForm.url" type="text" placeholder="url (https://…)" />
              <input v-model="envForm.host_alias" type="text" placeholder="host_alias (e.g. ops-staging)" class="pi-mono" />
              <input v-model="envForm.host_ip" type="text" placeholder="host_ip (optional)" class="pi-mono" />
              <div v-if="envSaveError" class="pi-error">{{ envSaveError }}</div>
              <div class="pi-actions">
                <button type="button" class="btn btn-ghost btn-sm" @click="cancelEnvEdit">Cancel</button>
                <button
                  type="button"
                  class="btn btn-primary btn-sm"
                  :disabled="!envFormValid || envSaving"
                  @click="saveEnv"
                >{{ envSaving ? 'Saving…' : 'Save' }}</button>
              </div>
            </div>
          </template>
          <template v-else>
            <div class="pi-row-main">
              <div class="pi-row-head">
                <span class="pi-name">{{ env.name }}</span>
                <a v-if="env.url" :href="env.url" target="_blank" class="pi-link">{{ env.url }}</a>
              </div>
              <div v-if="env.host_alias || env.host_ip" class="pi-row-sub">
                <code v-if="env.host_alias">{{ env.host_alias }}</code>
                <span v-if="env.host_alias && env.host_ip"> — </span>
                <code v-if="env.host_ip">{{ env.host_ip }}</code>
              </div>
            </div>
            <div v-if="canWrite && envEditingId === null && !envAdding" class="pi-row-actions">
              <button type="button" class="btn btn-ghost btn-sm" @click="startEnvEdit(env)">Edit</button>
              <button type="button" class="btn btn-ghost btn-sm danger" @click="removeEnv(env)">Remove</button>
            </div>
          </template>
        </div>
        <div v-if="envAdding" class="pi-row pi-row--adding">
          <div class="pi-form">
            <input v-model="envForm.name" type="text" placeholder="name (e.g. staging)" autofocus />
            <span v-if="envNameError" class="pi-field-error">{{ envNameError }}</span>
            <input v-model="envForm.url" type="text" placeholder="url (https://…)" />
            <input v-model="envForm.host_alias" type="text" placeholder="host_alias (optional)" class="pi-mono" />
            <input v-model="envForm.host_ip" type="text" placeholder="host_ip (optional)" class="pi-mono" />
            <div v-if="envSaveError" class="pi-error">{{ envSaveError }}</div>
            <div class="pi-actions">
              <button type="button" class="btn btn-ghost btn-sm" @click="cancelEnvEdit">Cancel</button>
              <button
                type="button"
                class="btn btn-primary btn-sm"
                :disabled="!envFormValid || envSaving"
                @click="saveEnv"
              >{{ envSaving ? 'Adding…' : 'Add environment' }}</button>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- deploy recipes -->
    <div class="pi-block">
      <div class="pi-block-head">
        <h4>Deploy recipes</h4>
        <button
          v-if="canWrite && !recAdding && recEditingId === null"
          type="button"
          class="btn btn-ghost btn-sm"
          @click="startRecAdd"
        >
          Add recipe
        </button>
      </div>
      <div v-if="recError" class="pi-error">{{ recError }}</div>
      <LoadingText v-if="recLoading" class="pi-empty" label="Loading deploy recipes…" />
      <div v-else-if="!recipes.length && !recAdding" class="pi-empty">No deploy recipes declared yet.</div>
      <div v-else class="pi-list">
        <div v-for="rec in recipes" :key="rec.id" class="pi-row">
          <template v-if="recEditingId === rec.id">
            <div class="pi-form">
              <input v-model="recForm.name" type="text" placeholder="name (e.g. backend-staging)" />
              <span v-if="recNameError" class="pi-field-error">{{ recNameError }}</span>
              <input v-model="recForm.summary" type="text" placeholder="summary (optional)" />
              <textarea v-model="recForm.command" rows="3" placeholder="command — shell text" class="pi-mono" />
              <div v-if="recSaveError" class="pi-error">{{ recSaveError }}</div>
              <div class="pi-actions">
                <button type="button" class="btn btn-ghost btn-sm" @click="cancelRecEdit">Cancel</button>
                <button
                  type="button"
                  class="btn btn-primary btn-sm"
                  :disabled="!recFormValid || recSaving"
                  @click="saveRec"
                >{{ recSaving ? 'Saving…' : 'Save' }}</button>
              </div>
            </div>
          </template>
          <template v-else>
            <div class="pi-row-main">
              <div class="pi-row-head">
                <span class="pi-name">{{ rec.name }}</span>
              </div>
              <div v-if="rec.summary" class="pi-row-desc">{{ rec.summary }}</div>
              <pre v-if="rec.command" class="pi-cmd"><code>{{ rec.command }}</code></pre>
            </div>
            <div v-if="canWrite && recEditingId === null && !recAdding" class="pi-row-actions">
              <button type="button" class="btn btn-ghost btn-sm" @click="startRecEdit(rec)">Edit</button>
              <button type="button" class="btn btn-ghost btn-sm danger" @click="removeRec(rec)">Remove</button>
            </div>
          </template>
        </div>
        <div v-if="recAdding" class="pi-row pi-row--adding">
          <div class="pi-form">
            <input v-model="recForm.name" type="text" placeholder="name (e.g. backend-staging)" autofocus />
            <span v-if="recNameError" class="pi-field-error">{{ recNameError }}</span>
            <input v-model="recForm.summary" type="text" placeholder="summary (optional)" />
            <textarea v-model="recForm.command" rows="3" placeholder="command — shell text" class="pi-mono" />
            <div v-if="recSaveError" class="pi-error">{{ recSaveError }}</div>
            <div class="pi-actions">
              <button type="button" class="btn btn-ghost btn-sm" @click="cancelRecEdit">Cancel</button>
              <button
                type="button"
                class="btn btn-primary btn-sm"
                :disabled="!recFormValid || recSaving"
                @click="saveRec"
              >{{ recSaving ? 'Adding…' : 'Add recipe' }}</button>
            </div>
          </div>
        </div>
      </div>
    </div>
  </section>
</template>

<style scoped>
.pi-section { display: flex; flex-direction: column; gap: 1rem; margin-top: 1rem; }
.pi-header h3 { font-size: 14px; font-weight: 800; color: var(--text); margin: 0 0 .15rem; }
.pi-desc { margin: 0; color: var(--text-muted); font-size: 12px; }
.pi-title { font-size: 14px; font-weight: 800; color: var(--text); margin: 0 0 .15rem; }
.pi-block { display: flex; flex-direction: column; gap: .55rem; }
.pi-block-head { display: flex; align-items: center; justify-content: space-between; gap: 1rem; }
.pi-block-head h4 { font-size: 13px; font-weight: 700; color: var(--text); margin: 0; }
.pi-empty { color: var(--text-muted); font-size: 13px; padding: .35rem 0; }
.pi-error { color: #b42318; background: #fef3f2; border: 1px solid #fecdca; border-radius: 8px; padding: .5rem .65rem; font-size: 12px; }
.pi-list { display: flex; flex-direction: column; gap: .45rem; }
.pi-row { display: flex; align-items: flex-start; justify-content: space-between; gap: .8rem; padding: .55rem .75rem; border: 1px solid var(--border); border-radius: 8px; background: var(--bg); }
.pi-row--adding { background: var(--bg-card); border-style: dashed; }
.pi-row-main { min-width: 0; flex: 1; display: flex; flex-direction: column; gap: .25rem; }
.pi-row-head { display: flex; align-items: baseline; gap: .55rem; }
.pi-name { font-weight: 700; font-size: 13px; color: var(--text); font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
.pi-link { color: var(--text-muted); font-size: 12px; text-decoration: none; }
.pi-link:hover { text-decoration: underline; }
.pi-row-desc { color: var(--text); font-size: 13px; }
.pi-row-sub { color: var(--text-muted); font-size: 12px; display: flex; gap: .25rem; align-items: center; }
.pi-row-sub code { background: var(--bg-card); padding: 0 .3rem; border-radius: 4px; font-size: 11px; }
.pi-cmd { background: var(--bg-card); border: 1px solid var(--border); border-radius: 6px; padding: .4rem .6rem; font-size: 12px; margin: .2rem 0 0; overflow-x: auto; font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
.pi-row-actions { display: flex; gap: .35rem; align-items: flex-start; }
.pi-form { width: 100%; display: flex; flex-direction: column; gap: .4rem; }
.pi-form input,
.pi-form textarea { width: 100%; border: 1px solid var(--border); border-radius: 6px; background: var(--bg); color: var(--text); font: inherit; padding: .4rem .55rem; }
.pi-mono { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 12px; }
.pi-field-error { color: #b42318; font-size: 11px; }
.pi-actions { display: flex; gap: .4rem; justify-content: flex-end; }
</style>
