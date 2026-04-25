<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { api, errMsg } from '@/api/client'
import AppIcon from '@/components/AppIcon.vue'
import type { ProjectManifest, ProjectRepo } from '@/types'

const props = defineProps<{
  projectId: number
  canWrite: boolean
}>()

const repos = ref<ProjectRepo[]>([])
const manifest = ref<ProjectManifest>({ project_id: 0, data: {} })
const manifestDraft = ref('{}')
const loading = ref(true)
const savingManifest = ref(false)
const saveError = ref('')
const saveOk = ref('')
const repoForm = ref({ url: '', default_branch: 'main', label: '' })
const addingRepo = ref(false)

const hasManifest = computed(() => Object.keys(manifest.value.data || {}).length > 0)
const manifestPretty = computed(() => JSON.stringify(manifest.value.data || {}, null, 2))

async function load() {
  loading.value = true
  saveError.value = ''
  try {
    const [repoData, manifestData] = await Promise.all([
      api.get<ProjectRepo[]>(`/projects/${props.projectId}/repos`),
      api.get<ProjectManifest>(`/projects/${props.projectId}/manifest`),
    ])
    repos.value = repoData
    manifest.value = manifestData
    manifestDraft.value = JSON.stringify(manifestData.data || {}, null, 2)
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
    await api.post(`/projects/${props.projectId}/repos`, repoForm.value)
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
    await api.delete(`/projects/${props.projectId}/repos/${repo.id}`)
    await load()
  } catch (e) {
    saveError.value = errMsg(e, 'Failed to remove repo.')
  }
}

async function saveManifest() {
  savingManifest.value = true
  saveError.value = ''
  saveOk.value = ''
  try {
    const parsed = JSON.parse(manifestDraft.value || '{}')
    manifest.value = await api.put<ProjectManifest>(`/projects/${props.projectId}/manifest`, { data: parsed })
    manifestDraft.value = JSON.stringify(manifest.value.data || {}, null, 2)
    saveOk.value = 'Manifest saved.'
    setTimeout(() => { saveOk.value = '' }, 2500)
  } catch (e) {
    saveError.value = errMsg(e, 'Failed to save manifest.')
  } finally {
    savingManifest.value = false
  }
}

onMounted(load)
</script>

<template>
  <section class="context-section">
    <div class="context-header">
      <div>
        <h2 class="context-title">Project Context</h2>
        <p class="context-desc">Repos and manifest power agent-friendly context, anchors, and retrieval.</p>
      </div>
      <button class="btn btn-ghost btn-sm" @click="load" :disabled="loading">Refresh</button>
    </div>

    <div v-if="saveError" class="context-error">{{ saveError }}</div>
    <div v-if="saveOk" class="context-ok">{{ saveOk }}</div>

    <div class="context-grid">
      <div class="context-card">
        <div class="card-head">
          <div>
            <h3>Repos</h3>
            <p>Used for anchors, deep links, and future multi-repo retrieval.</p>
          </div>
        </div>

        <div v-if="loading" class="context-empty">Loading repos…</div>
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

      <div class="context-card">
        <div class="card-head">
          <div>
            <h3>Manifest</h3>
            <p>Structured project truth: stack, commands, environments, ADRs, NFRs, and ownership.</p>
          </div>
        </div>

        <template v-if="canWrite">
          <textarea v-model="manifestDraft" class="manifest-editor" spellcheck="false"></textarea>
          <div class="manifest-actions">
            <button class="btn btn-primary btn-sm" @click="saveManifest" :disabled="savingManifest">
              {{ savingManifest ? 'Saving…' : 'Save manifest' }}
            </button>
          </div>
        </template>
        <pre v-else-if="hasManifest" class="manifest-read">{{ manifestPretty }}</pre>
        <div v-else class="context-empty">No manifest saved yet.</div>
      </div>
    </div>
  </section>
</template>

<style scoped>
.context-section { margin-bottom: 1.5rem; display: flex; flex-direction: column; gap: 1rem; }
.context-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 1rem; }
.context-title { font-size: 18px; font-weight: 800; color: var(--text); margin: 0 0 .15rem; }
.context-desc { margin: 0; color: var(--text-muted); font-size: 13px; }
.context-grid { display: grid; grid-template-columns: 1fr 1.1fr; gap: 1rem; }
.context-card { background: var(--bg-card); border: 1px solid var(--border); border-radius: 12px; box-shadow: var(--shadow); padding: 1rem 1.1rem; display: flex; flex-direction: column; gap: .9rem; }
.card-head h3 { margin: 0 0 .2rem; font-size: 15px; }
.card-head p { margin: 0; color: var(--text-muted); font-size: 12px; }
.context-empty { color: var(--text-muted); font-size: 13px; }
.context-error { color: #b42318; background: #fef3f2; border: 1px solid #fecdca; border-radius: 10px; padding: .7rem .85rem; font-size: 13px; }
.context-ok { color: #166534; background: #ecfdf3; border: 1px solid #abefc6; border-radius: 10px; padding: .7rem .85rem; font-size: 13px; }
.repo-list { display: flex; flex-direction: column; gap: .7rem; }
.repo-row { display: flex; align-items: flex-start; justify-content: space-between; gap: .9rem; padding: .75rem .8rem; border: 1px solid var(--border); border-radius: 8px; background: var(--bg); }
.repo-main { min-width: 0; }
.repo-name { font-size: 13px; font-weight: 700; color: var(--text); }
.repo-url { display: inline-block; margin-top: .15rem; font-size: 12px; color: var(--text-muted); word-break: break-all; text-decoration: none; }
.repo-url:hover { color: var(--bp-blue-dark); text-decoration: underline; }
.repo-meta { margin-top: .25rem; font-size: 12px; color: var(--text-muted); }
.repo-form { display: grid; grid-template-columns: 1fr 1.4fr .7fr auto; gap: .55rem; }
.repo-form input, .manifest-editor {
  width: 100%;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--bg);
  color: var(--text);
  font: inherit;
  padding: .55rem .65rem;
}
.manifest-editor { min-height: 320px; font-family: 'JetBrains Mono', ui-monospace, monospace; font-size: 12px; line-height: 1.45; resize: vertical; }
.manifest-read { margin: 0; padding: .85rem .95rem; border-radius: 8px; background: var(--bg); border: 1px solid var(--border); overflow: auto; font-family: 'JetBrains Mono', ui-monospace, monospace; font-size: 12px; line-height: 1.5; color: var(--text); }
.manifest-actions { display: flex; justify-content: flex-end; }
@media (max-width: 980px) {
  .context-grid { grid-template-columns: 1fr; }
  .repo-form { grid-template-columns: 1fr; }
}
</style>
