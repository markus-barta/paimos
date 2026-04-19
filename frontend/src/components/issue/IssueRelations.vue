<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { RouterLink } from 'vue-router'
import { api, errMsg } from '@/api/client'
import { useAuthStore } from '@/stores/auth'
import { useConfirm } from '@/composables/useConfirm'
import AppIcon from '@/components/AppIcon.vue'
import type { Issue, IssueRelation } from '@/types'

const props = defineProps<{
  issueId: number
  projectId: number
  projectIssues: Issue[]
}>()

const authStore = useAuthStore()
const { confirm } = useConfirm()

const relations      = ref<IssueRelation[]>([])
const relLoading     = ref(false)
const showRelForm    = ref(false)
const relFormTarget  = ref('')
const relFormType    = ref<'depends_on' | 'impacts'>('depends_on')
const relFormError   = ref('')
const relSaving      = ref(false)

async function load() {
  if (!props.issueId) return
  relLoading.value = true
  try {
    relations.value = await api.get<IssueRelation[]>(`/issues/${props.issueId}/relations`)
  } catch { relations.value = [] }
  finally { relLoading.value = false }
}

defineExpose({ load })

watch(() => props.issueId, () => load())

const relSuggestions = computed(() => {
  const q = relFormTarget.value.trim().toLowerCase()
  if (!q || q.length < 2) return []
  const existingTargets = new Set(relations.value.map(r => r.target_id))
  return props.projectIssues
    .filter(i => {
      if (i.id === props.issueId) return false
      if (existingTargets.has(i.id)) return false
      return (i.issue_key?.toLowerCase().includes(q)) || (i.title?.toLowerCase().includes(q))
    })
    .slice(0, 8)
})
const relShowSuggestions = ref(false)
function hideRelSuggestions() { setTimeout(() => { relShowSuggestions.value = false }, 150) }

function selectRelSuggestion(iss: Issue) {
  relFormTarget.value = iss.issue_key ?? ''
  relShowSuggestions.value = false
}

async function addRelation() {
  relFormError.value = ''
  const key = relFormTarget.value.trim().toUpperCase()
  if (!key) { relFormError.value = 'Enter an issue key.'; return }
  const target = props.projectIssues.find(i => i.issue_key?.toUpperCase() === key)
  if (!target) { relFormError.value = `Issue "${key}" not found in this project.`; return }
  relSaving.value = true
  try {
    await api.post(`/issues/${props.issueId}/relations`, { target_id: target.id, type: relFormType.value })
    await load()
    relFormTarget.value = ''
  } catch (e: unknown) { relFormError.value = errMsg(e, 'Failed to add relation.') }
  finally { relSaving.value = false }
}

async function removeRelation(rel: IssueRelation) {
  if (!await confirm({ message: 'Remove this relation?', confirmLabel: 'Remove' })) return
  await api.delete(`/issues/${props.issueId}/relations`, { target_id: rel.target_id, type: rel.type })
  relations.value = relations.value.filter(r => !(r.target_id === rel.target_id && r.type === rel.type))
}

const relsByType = computed(() => ({
  depends_on: relations.value.filter(r => r.type === 'depends_on'),
  impacts:    relations.value.filter(r => r.type === 'impacts'),
}))
</script>

<template>
  <div class="relations-section">
    <div class="section-header">
      <h3 class="section-title">Relations</h3>
      <button class="btn btn-ghost btn-sm" @click="showRelForm = !showRelForm">+ Add</button>
    </div>

    <div v-if="showRelForm" class="rel-form rel-form--inline">
      <select v-model="relFormType" class="rel-type-select">
        <option value="depends_on">Depends On</option>
        <option value="impacts">Impacts</option>
      </select>
      <div class="rel-key-wrap">
        <input v-model="relFormTarget" type="text" placeholder="Issue key or title…" class="rel-key-input"
          @keydown.enter="addRelation"
          @focus="relShowSuggestions = true"
          @blur="hideRelSuggestions"
          autocomplete="off"
        />
        <div v-if="relShowSuggestions && relSuggestions.length" class="rel-suggestions">
          <div v-for="s in relSuggestions" :key="s.id" class="rel-suggestion" @mousedown.prevent="selectRelSuggestion(s)">
            <span class="rel-sug-key">{{ s.issue_key }}</span>
            <span class="rel-sug-title">{{ s.title }}</span>
          </div>
        </div>
      </div>
      <button class="btn btn-primary btn-sm" @click="addRelation" :disabled="relSaving">
        {{ relSaving ? '…' : 'Add' }}
      </button>
      <button class="btn btn-ghost btn-sm" @click="showRelForm=false; relFormError=''">×</button>
      <span v-if="relFormError" class="rel-error">{{ relFormError }}</span>
    </div>

    <div v-if="relsByType.depends_on.length" class="rel-group">
      <span class="rel-group-label">Depends On</span>
      <div class="rel-chips">
        <div v-for="r in relsByType.depends_on" :key="r.target_id" class="rel-chip">
          <RouterLink :to="`/projects/${projectId}/issues/${r.target_id}`" class="rel-chip-key">
            {{ r.target_key || r.target_id }}
          </RouterLink>
          <span v-if="r.target_title" class="rel-chip-title">{{ r.target_title }}</span>
          <button v-if="authStore.user?.role === 'admin'" class="rel-chip-del" @click="removeRelation(r)" title="Remove"><AppIcon name="x" :size="11" /></button>
        </div>
      </div>
    </div>
    <div v-if="relsByType.impacts.length" class="rel-group">
      <span class="rel-group-label">Impacts</span>
      <div class="rel-chips">
        <div v-for="r in relsByType.impacts" :key="r.target_id" class="rel-chip">
          <RouterLink :to="`/projects/${projectId}/issues/${r.target_id}`" class="rel-chip-key">
            {{ r.target_key || r.target_id }}
          </RouterLink>
          <span v-if="r.target_title" class="rel-chip-title">{{ r.target_title }}</span>
          <button v-if="authStore.user?.role === 'admin'" class="rel-chip-del" @click="removeRelation(r)" title="Remove"><AppIcon name="x" :size="11" /></button>
        </div>
      </div>
    </div>
    <div v-if="!relLoading && !relsByType.depends_on.length && !relsByType.impacts.length && !showRelForm" class="rel-empty">
      No relations yet.
    </div>
  </div>
</template>

<style scoped>
.relations-section {
  margin-top: 1.75rem;
  padding-top: 1.5rem;
  border-top: 1px solid var(--border);
}
.section-header {
  display: flex; align-items: center; justify-content: space-between;
  margin-bottom: .75rem;
}
.section-title {
  font-size: 13px; font-weight: 700; text-transform: uppercase;
  letter-spacing: .06em; color: var(--text-muted);
  display: flex; align-items: center; gap: .5rem;
}
.rel-form {
  display: flex; align-items: center; gap: .5rem; flex-wrap: wrap;
  margin-bottom: .75rem;
  padding: .6rem .75rem; background: var(--surface-2); border-radius: var(--radius);
}
.rel-form--inline { flex-wrap: nowrap; overflow: visible; }
.rel-type-select { font-size: 12px; padding: .3rem .5rem; flex-shrink: 0; width: 130px; }
.rel-key-wrap { position: relative; flex: 1 1 0; min-width: 100px; }
.rel-key-input { font-size: 13px; padding: .3rem .6rem; width: 100%; box-sizing: border-box; }
.rel-suggestions {
  position: absolute; top: 100%; left: 0; right: 0; z-index: 500;
  background: var(--bg-card); border: 1px solid var(--border); border-radius: 6px;
  box-shadow: 0 4px 16px rgba(0,0,0,.12); max-height: 240px; overflow-y: auto;
  margin-top: 2px;
}
.rel-suggestion {
  display: flex; align-items: center; gap: .4rem; padding: .4rem .6rem;
  cursor: pointer; font-size: 12px; transition: background .1s;
}
.rel-suggestion:hover { background: var(--surface-2); }
.rel-sug-key { font-family: monospace; font-weight: 700; color: var(--bp-blue); white-space: nowrap; flex-shrink: 0; }
.rel-sug-title { color: var(--text-muted); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.rel-error { font-size: 12px; color: #c0392b; flex-basis: 100%; }
.rel-group { margin-bottom: .6rem; }
.rel-group-label {
  font-size: 11px; font-weight: 700; text-transform: uppercase; letter-spacing: .05em;
  color: var(--text-muted); display: block; margin-bottom: .35rem;
}
.rel-chips { display: flex; flex-wrap: wrap; gap: .35rem; }
.rel-chip {
  display: inline-flex; align-items: center; gap: .3rem;
  background: var(--surface-2); border: 1px solid var(--border);
  border-radius: 6px; padding: .2rem .5rem; font-size: 12px;
}
.rel-chip-key {
  font-family: monospace; font-weight: 700; color: var(--bp-blue);
  text-decoration: none;
}
.rel-chip-key:hover { text-decoration: underline; }
.rel-chip-title { color: var(--text-muted); max-width: 200px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.rel-chip-del {
  background: none; border: none; cursor: pointer; color: var(--text-muted);
  font-size: 14px; line-height: 1; padding: 0 .15rem; border-radius: 3px;
}
.rel-chip-del:hover { color: #c0392b; }
.rel-empty { font-size: 13px; color: var(--text-muted); padding: .5rem 0; }
</style>
