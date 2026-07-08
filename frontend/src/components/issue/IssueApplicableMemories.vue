<script setup lang="ts">
// PAI-342 — Applicable Memories panel for the issue detail view.
//
// Renders three things:
//   1. The currently-linked memories (chips with title + first-line
//      preview; click opens the memory in the project's Knowledge
//      tab via /projects/:id?tab=knowledge&memory=:slug).
//   2. "Did you mean these?" auto-suggest cards when the linked set
//      is empty — calls GET /applicable-memories?suggest=1 once,
//      lets the user accept-all / accept-individual / dismiss.
//   3. An add-form (manual link by memory slug typeahead) for the
//      explicit-curation path when suggestions don't fire.
//
// Mutations route through the existing /issues/:id/relations API
// with type='applies_to_memory' so the audit trail / undo /
// history coverage is identical to every other relation type.

import { computed, ref, watch } from 'vue'
import { errMsg } from '@/api/client'
import { useAuthStore } from '@/stores/auth'
import { useConfirm } from '@/composables/useConfirm'
import AppIcon from '@/components/AppIcon.vue'
import {
  type ApplicableMemory,
  listApplicableMemories,
  suggestApplicableMemories,
} from '@/services/applicableMemories'
import { addIssueRelation, removeIssueRelation } from '@/services/issueRelations'
import { listKnowledgeEntries } from '@/services/projectKnowledge'
import type { KnowledgeEntry } from '@/types'

const props = withDefaults(defineProps<{
  issueId: number
  projectId: number | null
  canEdit?: boolean
}>(), {
  canEdit: true,
})

const authStore = useAuthStore()
const { confirm } = useConfirm()

const linked = ref<ApplicableMemory[]>([])
const suggestions = ref<ApplicableMemory[]>([])
const projectMemories = ref<KnowledgeEntry[]>([])
const loading = ref(false)
const suggestionsDismissed = ref(false)
const error = ref('')

const showAddForm = ref(false)
const addQuery = ref('')
const addError = ref('')
const adding = ref(false)
const showSuggestions = ref(false)

async function load() {
  if (!props.issueId) return
  loading.value = true
  error.value = ''
  try {
    linked.value = await listApplicableMemories(props.issueId)
  } catch (e) {
    error.value = errMsg(e, 'Failed to load applicable memories.')
    linked.value = []
  } finally {
    loading.value = false
  }
  // Lazy-load the suggestions only when we're showing the empty-
  // state — saves a fetch on the typical "already curated" case.
  if (linked.value.length === 0 && !suggestionsDismissed.value) {
    void loadSuggestions()
  } else {
    suggestions.value = []
  }
  // Pre-load the project's memory list for the typeahead. Cheap —
  // typical projects have <60 memories — and reused across the
  // session per the parent's @keepalive lifecycle.
  if (props.projectId && projectMemories.value.length === 0) {
    try {
      projectMemories.value = await listKnowledgeEntries(props.projectId, 'memory')
    } catch {
      projectMemories.value = []
    }
  }
}

async function loadSuggestions() {
  try {
    suggestions.value = await suggestApplicableMemories(props.issueId)
  } catch {
    suggestions.value = []
  }
}

defineExpose({ load })

watch(() => props.issueId, () => {
  suggestionsDismissed.value = false
  void load()
})

void load()

// ── linking actions ──────────────────────────────────────────────

async function linkMemory(memoryId: number) {
  await addIssueRelation(props.issueId, memoryId, 'applies_to_memory')
}

async function unlinkMemory(memoryId: number) {
  await removeIssueRelation(props.issueId, memoryId, 'applies_to_memory')
}

async function acceptSuggestion(s: ApplicableMemory) {
  if (!canEdit.value) return
  try {
    await linkMemory(s.id)
    suggestions.value = suggestions.value.filter((x) => x.id !== s.id)
    await load()
  } catch (e) {
    error.value = errMsg(e, 'Failed to link memory.')
  }
}

async function acceptAllSuggestions() {
  if (!canEdit.value) return
  try {
    for (const s of suggestions.value) {
      await linkMemory(s.id)
    }
    suggestions.value = []
    await load()
  } catch (e) {
    error.value = errMsg(e, 'Failed to link memories.')
  }
}

function dismissSuggestions() {
  suggestionsDismissed.value = true
  suggestions.value = []
}

async function removeLinked(m: ApplicableMemory) {
  if (!canEdit.value) return
  if (!await confirm({ message: `Unlink memory "${m.slug}" from this issue?`, confirmLabel: 'Unlink' })) return
  try {
    await unlinkMemory(m.id)
    linked.value = linked.value.filter((x) => x.id !== m.id)
    if (linked.value.length === 0 && !suggestionsDismissed.value) {
      void loadSuggestions()
    }
  } catch (e) {
    error.value = errMsg(e, 'Failed to unlink memory.')
  }
}

// ── manual add form ──────────────────────────────────────────────

const addCandidates = computed<KnowledgeEntry[]>(() => {
  const q = addQuery.value.trim().toLowerCase()
  if (!q || q.length < 2) return []
  const linkedIds = new Set(linked.value.map((m) => m.id))
  return projectMemories.value
    .filter((m) => !linkedIds.has(m.id))
    .filter((m) => m.slug.toLowerCase().includes(q) || m.title.toLowerCase().includes(q))
    .slice(0, 8)
})

function hideSuggestionList() {
  setTimeout(() => { showSuggestions.value = false }, 150)
}

async function addByCandidate(m: KnowledgeEntry) {
  if (!canEdit.value) return
  addError.value = ''
  adding.value = true
  try {
    await linkMemory(m.id)
    addQuery.value = ''
    showSuggestions.value = false
    showAddForm.value = false
    await load()
  } catch (e) {
    addError.value = errMsg(e, 'Failed to link memory.')
  } finally {
    adding.value = false
  }
}

async function addBySlug() {
  if (!canEdit.value) return
  addError.value = ''
  const q = addQuery.value.trim().toLowerCase()
  if (!q) {
    addError.value = 'Type a memory slug or title.'
    return
  }
  const found = projectMemories.value.find(
    (m) => m.slug.toLowerCase() === q || m.title.toLowerCase() === q,
  )
  if (!found) {
    addError.value = `No memory matches "${addQuery.value}".`
    return
  }
  await addByCandidate(found)
}

// ── routing ──────────────────────────────────────────────────────

function memoryRoute(m: { project_id: number; slug: string }): string {
  // The Knowledge tab listens for `?tab=knowledge&memory=:slug` and
  // opens the matching entry in edit mode (PAI-342 wiring on
  // ProjectDetailView). Falls back to the project page if the tab
  // listener hasn't loaded yet — the user can still navigate by
  // hand without breaking.
  return `/projects/${m.project_id}?tab=knowledge&memory=${encodeURIComponent(m.slug)}`
}

const canEdit = computed(() => {
  // Same gate as IssueRelations: admins see edit affordances. The
  // backend rejects non-admin writes anyway, so this is purely UX.
  return props.canEdit !== false && authStore.isAdmin
})
</script>

<template>
  <div class="am-section">
    <div class="section-header">
      <h3 class="section-title">Applicable Memories</h3>
      <button
        v-if="canEdit"
        class="btn btn-ghost btn-sm"
        @click="showAddForm = !showAddForm"
      >
        + Add
      </button>
    </div>

    <div v-if="error" class="am-error">{{ error }}</div>

    <!-- Manual add form: typeahead over the project's memories. -->
    <div v-if="showAddForm" class="am-form">
      <div class="am-form-input">
        <input
          v-model="addQuery"
          type="text"
          placeholder="Memory slug or title…"
          autocomplete="off"
          @keydown.enter="addBySlug"
          @focus="showSuggestions = true"
          @blur="hideSuggestionList"
        />
        <div v-if="showSuggestions && addCandidates.length" class="am-form-hits">
          <div
            v-for="m in addCandidates"
            :key="m.id"
            class="am-form-hit"
            @mousedown.prevent="addByCandidate(m)"
          >
            <span class="am-form-hit-slug">{{ m.slug }}</span>
            <span class="am-form-hit-title">{{ m.title }}</span>
          </div>
        </div>
      </div>
      <button class="btn btn-primary btn-sm" :disabled="adding" @click="addBySlug">
        {{ adding ? '…' : 'Link' }}
      </button>
      <button class="btn btn-ghost btn-sm" @click="showAddForm = false; addError = ''">×</button>
      <span v-if="addError" class="am-form-error">{{ addError }}</span>
    </div>

    <!-- Linked memories — the curated set. -->
    <div v-if="linked.length" class="am-list">
      <div v-for="m in linked" :key="m.id" class="am-row">
        <a :href="memoryRoute(m)" class="am-row-main">
          <div class="am-row-head">
            <span class="am-slug">{{ m.slug }}</span>
            <span v-if="m.project_key" class="am-pill">{{ m.project_key }}</span>
          </div>
          <div class="am-row-title">{{ m.title }}</div>
          <div v-if="m.preview" class="am-row-preview">{{ m.preview }}</div>
        </a>
        <button
          v-if="canEdit"
          class="am-row-del"
          title="Unlink"
          @click="removeLinked(m)"
        >
          <AppIcon name="x" :size="11" />
        </button>
      </div>
    </div>

    <!-- Auto-suggest cards (only when linked is empty). -->
    <div
      v-if="linked.length === 0 && !suggestionsDismissed && suggestions.length"
      class="am-suggest"
    >
      <div class="am-suggest-head">
        <span class="am-suggest-title">Did you mean these?</span>
        <div class="am-suggest-actions">
          <button
            v-if="canEdit && suggestions.length > 1"
            class="btn btn-ghost btn-sm"
            @click="acceptAllSuggestions"
          >
            Accept all
          </button>
          <button
            class="btn btn-ghost btn-sm"
            @click="dismissSuggestions"
          >
            Dismiss
          </button>
        </div>
      </div>
      <div class="am-suggest-grid">
        <div v-for="s in suggestions" :key="s.id" class="am-suggest-card">
          <div class="am-suggest-card-head">
            <span class="am-slug">{{ s.slug }}</span>
            <span v-if="s.score" class="am-pill am-pill--score" :title="(s.matched ?? []).join(', ')">
              {{ s.score }}
            </span>
          </div>
          <div class="am-row-title">{{ s.title }}</div>
          <div v-if="s.preview" class="am-row-preview">{{ s.preview }}</div>
          <div v-if="s.matched && s.matched.length" class="am-suggest-matched">
            <span v-for="m in s.matched" :key="m" class="am-pill am-pill--match">{{ m }}</span>
          </div>
          <div class="am-suggest-card-actions">
            <button
              v-if="canEdit"
              class="btn btn-primary btn-sm"
              @click="acceptSuggestion(s)"
            >
              Link
            </button>
            <a :href="memoryRoute(s)" class="btn btn-ghost btn-sm">Open</a>
          </div>
        </div>
      </div>
    </div>

    <div v-if="!loading && linked.length === 0 && !suggestions.length && !showAddForm" class="am-empty">
      No applicable memories.
    </div>
  </div>
</template>

<style scoped>
.am-section { margin-top: 1.75rem; padding-top: 1.5rem; border-top: 1px solid var(--border); }
.section-header { display: flex; align-items: center; justify-content: space-between; margin-bottom: .75rem; }
.section-title {
  font-size: 13px; font-weight: 700; text-transform: uppercase;
  letter-spacing: .06em; color: var(--text-muted);
  display: flex; align-items: center; gap: .5rem;
}
.am-error { color: #b42318; font-size: 12px; background: #fef3f2; border: 1px solid #fecdca; border-radius: 8px; padding: .45rem .6rem; margin-bottom: .5rem; }

.am-form { display: flex; align-items: center; gap: .5rem; flex-wrap: nowrap; margin-bottom: .75rem; padding: .6rem .75rem; background: var(--surface-2); border-radius: var(--radius); }
.am-form-input { position: relative; flex: 1 1 0; min-width: 100px; }
.am-form-input input { font-size: 13px; padding: .3rem .6rem; width: 100%; box-sizing: border-box; border: 1px solid var(--border); border-radius: 6px; background: var(--bg); color: var(--text); }
.am-form-hits { position: absolute; top: 100%; left: 0; right: 0; z-index: 500; background: var(--bg-card); border: 1px solid var(--border); border-radius: 6px; box-shadow: 0 4px 16px rgba(0,0,0,.12); max-height: 240px; overflow-y: auto; margin-top: 2px; }
.am-form-hit { display: flex; align-items: center; gap: .4rem; padding: .4rem .6rem; cursor: pointer; font-size: 12px; transition: background .1s; }
.am-form-hit:hover { background: var(--surface-2); }
.am-form-hit-slug { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-weight: 700; color: var(--bp-blue); white-space: nowrap; flex-shrink: 0; }
.am-form-hit-title { color: var(--text-muted); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.am-form-error { font-size: 12px; color: #c0392b; flex-basis: 100%; }

.am-list { display: flex; flex-direction: column; gap: .35rem; }
.am-row { display: flex; align-items: flex-start; gap: .45rem; padding: .55rem .65rem; border: 1px solid var(--border); border-radius: 8px; background: var(--bg); }
.am-row-main { flex: 1; display: flex; flex-direction: column; gap: .15rem; min-width: 0; text-decoration: none; color: inherit; cursor: pointer; }
.am-row-main:hover .am-row-title { color: var(--bp-blue); }
.am-row-head { display: flex; flex-wrap: wrap; gap: .35rem; align-items: center; }
.am-slug { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-weight: 700; font-size: 12px; color: var(--text); }
.am-pill { display: inline-block; background: var(--bg-card); border: 1px solid var(--border); border-radius: 999px; padding: 0 .5rem; font-size: 10px; color: var(--text-muted); line-height: 1.55; }
.am-pill--score { background: var(--bp-blue-pale, var(--surface-2)); color: var(--bp-blue-dark, var(--bp-blue)); border-color: var(--bp-blue); font-weight: 700; }
.am-pill--match { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 10px; }
.am-row-title { font-size: 13px; font-weight: 600; color: var(--text); }
.am-row-preview { font-size: 12px; color: var(--text-muted); line-height: 1.4; overflow: hidden; text-overflow: ellipsis; display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; }
.am-row-del { background: none; border: none; cursor: pointer; color: var(--text-muted); font-size: 14px; line-height: 1; padding: 0 .15rem; border-radius: 3px; flex-shrink: 0; }
.am-row-del:hover { color: #c0392b; }

.am-suggest { margin-top: .35rem; padding: .55rem .65rem; border: 1px dashed var(--border); border-radius: 8px; background: var(--bg-card); }
.am-suggest-head { display: flex; align-items: center; justify-content: space-between; margin-bottom: .55rem; }
.am-suggest-title { font-size: 12px; font-weight: 700; text-transform: uppercase; letter-spacing: .05em; color: var(--text-muted); }
.am-suggest-actions { display: flex; gap: .3rem; }
.am-suggest-grid { display: grid; gap: .45rem; grid-template-columns: repeat(auto-fill, minmax(220px, 1fr)); }
.am-suggest-card { display: flex; flex-direction: column; gap: .25rem; padding: .55rem .65rem; border: 1px solid var(--border); border-radius: 8px; background: var(--bg); }
.am-suggest-card-head { display: flex; align-items: center; justify-content: space-between; gap: .35rem; }
.am-suggest-matched { display: flex; flex-wrap: wrap; gap: .25rem; }
.am-suggest-card-actions { display: flex; gap: .3rem; margin-top: .35rem; }

.am-empty { font-size: 13px; color: var(--text-muted); padding: .5rem 0; }
</style>
