<script setup lang="ts">
import LoadingText from '@/components/LoadingText.vue'
import { computed, onMounted, ref, watch } from 'vue'
import { errMsg } from '@/api/client'
import type { ProjectRepo } from '@/types'
import {
  addProjectContextRepo,
  loadProjectContext,
  removeProjectContextRepo,
} from '@/services/projectContext'

// PAI-358 — manifest editor + migration UI removed. This section now
// owns project_repos only. Repos still feed anchors and retrieval; the
// legacy `_guardrails` / `_glossary` / `_dev` / `_ops` JSON taxonomy is
// fully replaced by the PAI-338 knowledge plane.

const props = defineProps<{
  projectId: number
  canWrite: boolean
  showHeader?: boolean
}>()

const emit = defineEmits<{
  populated: [v: boolean]
  summary: [payload: { repoCount: number; hasManifest: boolean; populated: boolean }]
}>()

const repos = ref<ProjectRepo[]>([])
const loading = ref(true)
const saveError = ref('')
const repoForm = ref({ url: '', default_branch: 'main', label: '' })
const addingRepo = ref(false)

const isPopulated = computed(() => repos.value.length > 0)
watch(isPopulated, (v) => emit('populated', v), { immediate: true })
watch(
  [repos, isPopulated],
  () => {
    emit('summary', {
      repoCount: repos.value.length,
      // PAI-358: hasManifest stays in the summary shape so callers
      // don't break, but always reads false now that the manifest
      // surface is gone.
      hasManifest: false,
      populated: isPopulated.value,
    })
  },
  { immediate: true },
)

async function load() {
  loading.value = true
  saveError.value = ''
  try {
    const data = await loadProjectContext(props.projectId)
    repos.value = data.repos
  } catch (e) {
    saveError.value = errMsg(e, 'Failed to load project context.')
  } finally {
    loading.value = false
  }
}

async function addRepo() {
  if (!repoForm.value.url.trim()) return
  addingRepo.value = true
  saveError.value = ''
  try {
    await addProjectContextRepo(props.projectId, repoForm.value)
    repoForm.value = { url: '', default_branch: 'main', label: '' }
    await load()
  } catch (e) {
    saveError.value = errMsg(e, 'Failed to add repo.')
  } finally {
    addingRepo.value = false
  }
}

async function removeRepo(repo: ProjectRepo) {
  if (!confirm(`Remove repo "${repo.label || repo.url}"?`)) return
  saveError.value = ''
  try {
    await removeProjectContextRepo(props.projectId, repo.id)
    await load()
  } catch (e) {
    saveError.value = errMsg(e, 'Failed to remove repo.')
  }
}

onMounted(load)
</script>

<template>
  <section class="context-section">
    <div v-if="showHeader !== false" class="context-header">
      <div>
        <h2 class="context-title">Project Context</h2>
        <p class="context-desc">Linked repos drive anchors, deep links, and multi-repo retrieval.</p>
      </div>
      <button class="btn btn-ghost btn-sm" @click="load" :disabled="loading">Refresh</button>
    </div>

    <div v-if="saveError" class="context-error">{{ saveError }}</div>

    <div class="context-card">
      <div class="card-head">
        <div>
          <h3>Repos</h3>
          <p>Used for anchors, deep links, and future multi-repo retrieval.</p>
        </div>
      </div>

      <LoadingText v-if="loading" class="context-empty" label="Loading repos…" />
      <div v-else-if="!repos.length" class="context-empty">No repos linked yet.</div>
      <div v-else class="repo-list">
        <div v-for="repo in repos" :key="repo.id" class="repo-row">
          <div class="repo-main">
            <div class="repo-name">{{ repo.label || repo.url }}</div>
            <a :href="repo.url" target="_blank" rel="noopener" class="repo-url">{{ repo.url }}</a>
            <div class="repo-meta">default branch: <strong>{{ repo.default_branch }}</strong></div>
          </div>
          <button v-if="canWrite" class="btn btn-ghost btn-sm danger" @click="removeRepo(repo)">Remove</button>
        </div>
      </div>

      <div v-if="canWrite" class="repo-form">
        <input v-model="repoForm.label" type="text" placeholder="Label (e.g. backend)" />
        <input v-model="repoForm.url" type="url" placeholder="https://github.com/org/repo" />
        <input v-model="repoForm.default_branch" type="text" placeholder="main" />
        <button class="btn btn-primary btn-sm" @click="addRepo" :disabled="addingRepo">
          {{ addingRepo ? 'Adding…' : 'Add repo' }}
        </button>
      </div>
    </div>
  </section>
</template>

<style scoped>
.context-section { margin-bottom: 1.5rem; display: flex; flex-direction: column; gap: 1rem; }
.context-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 1rem; }
.context-title { font-size: 18px; font-weight: 800; color: var(--text); margin: 0 0 .15rem; }
.context-desc { margin: 0; color: var(--text-muted); font-size: 13px; }
.context-card { background: var(--bg-card); border: 1px solid var(--border); border-radius: 12px; box-shadow: var(--shadow); padding: 1rem 1.1rem; display: flex; flex-direction: column; gap: .9rem; }
.card-head h3 { margin: 0 0 .2rem; font-size: 15px; }
.card-head p { margin: 0; color: var(--text-muted); font-size: 12px; }
.context-empty { color: var(--text-muted); font-size: 13px; }
.context-error { color: #b42318; background: #fef3f2; border: 1px solid #fecdca; border-radius: 10px; padding: .7rem .85rem; font-size: 13px; }
.repo-list { display: flex; flex-direction: column; gap: .7rem; }
.repo-row { display: flex; align-items: flex-start; justify-content: space-between; gap: .9rem; padding: .75rem .8rem; border: 1px solid var(--border); border-radius: 8px; background: var(--bg); }
.repo-main { min-width: 0; }
.repo-name { font-size: 13px; font-weight: 700; color: var(--text); }
.repo-url { display: inline-block; margin-top: .15rem; font-size: 12px; color: var(--text-muted); word-break: break-all; text-decoration: none; }
.repo-url:hover { color: var(--brand-blue-dark); text-decoration: underline; }
.repo-meta { margin-top: .25rem; font-size: 12px; color: var(--text-muted); }
.repo-form { display: grid; grid-template-columns: 1fr 1.4fr .7fr auto; gap: .55rem; }
.repo-form input {
  width: 100%;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--bg);
  color: var(--text);
  font: inherit;
  font-size: 12px;
  padding: .45rem .6rem;
}
.btn-ghost.danger { color: #b42318; border-color: #fecdca; }
.btn-ghost.danger:hover { background: #fef3f2; }
</style>
