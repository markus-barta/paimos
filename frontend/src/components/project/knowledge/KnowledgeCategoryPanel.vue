<script setup lang="ts">
// PAI-339 — list view for a single knowledge category. Owns the
// category's data lifecycle (load + CRUD) so the parent
// ProjectKnowledgeTab can render five of these side-by-side without
// coordinating shared state. Each panel has:
//   - filters (memory.type, archived toggle, environment)
//   - full-text search (title / slug / body, client-side filter on
//     the loaded set — the typical project has 30–60 entries; the
//     500-entry case is rare enough that we don't need server-side
//     search yet)
//   - sort (recency / alphabetical / confidence for memory)
//   - bulk archive / unarchive
//   - inline editor (KnowledgeEntryEditor)
//   - cross-references (memory.originating_tickets[] rendered as
//     ticket-key links).

import { computed, onMounted, ref, watch } from 'vue'
import { errMsg } from '@/api/client'
import LoadingText from '@/components/LoadingText.vue'
import AppIcon from '@/components/AppIcon.vue'
import KnowledgeEntryEditor from './KnowledgeEntryEditor.vue'
import {
  acceptProposedMemory,
  archivedStatusValue,
  activeStatusValue,
  bumpMemoryReferences,
  createKnowledgeEntry,
  deleteKnowledgeEntry,
  isArchived,
  isProposed,
  listKnowledgeEntries,
  listStaleMemory,
  rejectProposedMemory,
  updateKnowledgeEntry,
} from '@/services/projectKnowledge'
import { filterKnowledge, type KnowledgeSortMode } from '@/composables/useKnowledgeFilter'
import type { KnowledgeCategory, KnowledgeEntry, KnowledgeEntryInput } from '@/types'

const props = defineProps<{
  projectId: number
  category: KnowledgeCategory
  // Optional shared search query — when the parent's search box is
  // populated, the panel filters its own list against it. Keeps the
  // single-search-box contract from the spec while letting per-panel
  // filters layer on top.
  searchQuery?: string
  canWrite: boolean
  // PAI-342 — when set, after the initial load the panel auto-opens
  // the matching slug in edit mode (or read-only view if the user
  // can't edit). Empty / no-match falls through to the default list
  // view. Consumed once and reset to '' so subsequent re-renders
  // don't keep re-opening the editor.
  initialSlug?: string
}>()

const emit = defineEmits<{
  count: [n: number]
}>()

const entries = ref<KnowledgeEntry[]>([])
const loading = ref(true)
const loadError = ref('')

// Editor state. `editingSlug` is the slug of the row being edited,
// or null when no row is in edit mode. `adding` toggles the create
// form; both are mutually exclusive — opening one closes the other
// in the openers below.
const editingSlug = ref<string | null>(null)
const adding = ref(false)
const draft = ref<KnowledgeEntryInput>(emptyDraft())
const saving = ref(false)
const saveError = ref('')

// Filter state. memoryTypeFilter only renders for the memory tab.
const memoryTypeFilter = ref<string>('all')
const showArchived = ref(false)
const environmentFilter = ref<string>('')
const sortMode = ref<KnowledgeSortMode>('recency')

// PAI-347 — stale-memory filter. When toggled on we narrow the list
// to entries the server flagged as stale (no recent ref + confidence
// ≤ medium + no in-flight originating ticket). The id-set is fetched
// lazily — toggling on issues the GET; toggling off clears the set.
const showStaleOnly = ref(false)
const staleIds = ref<Set<number>>(new Set())
const staleLoading = ref(false)
const staleError = ref('')

// PAI-349 — proposed inbox toggle. When on, the list narrows to
// status='proposed' rows; bulk accept / reject + per-row actions
// become available. The two stale + proposed toggles are mutually
// exclusive at the UX level (showing stale-and-proposed simultaneously
// produces an empty intersection in practice, since stale is computed
// from updated_at and proposed entries are usually fresh enough).
const showProposedOnly = ref(false)
const proposedActionError = ref('')

// Bulk-op state. Selection is a Set of slugs — slugs are unique
// within (project_id, type) so they're sufficient as identity here.
const selection = ref(new Set<string>())
const bulkBusy = ref(false)
const bulkError = ref('')

watch(
  () => entries.value.length,
  (n) => emit('count', n),
  { immediate: true },
)

// Re-load when the parent swaps the active category. Without this,
// switching tabs would render the previous category's data while the
// fetch was in flight.
watch(
  () => props.category,
  () => {
    cancelEdit()
    selection.value = new Set()
    showStaleOnly.value = false
    staleIds.value = new Set()
    showProposedOnly.value = false
    proposedActionError.value = ''
    void load()
  },
)

// PAI-349 — Proposed and Stale toggles are mutually exclusive at the
// UX level. Flipping one off when the other turns on prevents stale
// IDs from leaking into the proposed view and vice versa.
watch(showProposedOnly, (on) => {
  if (on) {
    showStaleOnly.value = false
    staleIds.value = new Set()
  }
})

function emptyDraft(): KnowledgeEntryInput {
  return {
    slug: '',
    title: '',
    body: '',
    status: activeStatusValue(),
    metadata: {},
  }
}

// Tracks whether we've already consumed the initialSlug deep-link.
// Without this, every re-load (e.g. after save) would re-open the
// editor and clobber the user's intent.
const initialSlugConsumed = ref(false)

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    entries.value = await listKnowledgeEntries(props.projectId, props.category)
  } catch (e) {
    loadError.value = errMsg(e, 'Failed to load entries.')
  } finally {
    loading.value = false
  }
  // PAI-342 — open the deep-linked slug in edit mode if the parent
  // routed us here from an outside link. Runs exactly once per
  // mount, after the entries land so the lookup is reliable.
  if (!initialSlugConsumed.value && props.initialSlug) {
    const target = entries.value.find((e) => e.slug === props.initialSlug)
    if (target) {
      startEdit(target)
    }
    initialSlugConsumed.value = true
  }
}

function startAdd() {
  editingSlug.value = null
  adding.value = true
  draft.value = emptyDraft()
  saveError.value = ''
}

function startEdit(entry: KnowledgeEntry) {
  adding.value = false
  editingSlug.value = entry.slug
  draft.value = {
    slug: entry.slug,
    title: entry.title,
    body: entry.body,
    status: entry.status,
    metadata: { ...(entry.metadata ?? {}) },
  }
  saveError.value = ''
}

function cancelEdit() {
  editingSlug.value = null
  adding.value = false
  draft.value = emptyDraft()
  saveError.value = ''
}

// PAI-345: after a successful promote, the source row is soft-deleted
// on the server. Drop the editor and re-load so the list reflects the
// new state. The promoted entry now lives at user / instance scope —
// surfacing it from the project panel would be misleading.
async function onPromoted() {
  cancelEdit()
  await load()
}

async function onSave(payload: KnowledgeEntryInput) {
  saving.value = true
  saveError.value = ''
  try {
    if (editingSlug.value === null) {
      await createKnowledgeEntry(props.projectId, props.category, payload)
    } else {
      await updateKnowledgeEntry(props.projectId, props.category, editingSlug.value, payload)
    }
    cancelEdit()
    await load()
  } catch (e) {
    saveError.value = errMsg(e, 'Failed to save entry.')
  } finally {
    saving.value = false
  }
}

async function remove(entry: KnowledgeEntry) {
  if (!confirm(`Delete "${entry.slug}"? This sends it to Trash.`)) return
  saveError.value = ''
  try {
    await deleteKnowledgeEntry(props.projectId, props.category, entry.slug)
    if (editingSlug.value === entry.slug) cancelEdit()
    await load()
  } catch (e) {
    saveError.value = errMsg(e, 'Failed to delete entry.')
  }
}

function toggleSelection(slug: string, selected: boolean) {
  const next = new Set(selection.value)
  if (selected) next.add(slug)
  else next.delete(slug)
  selection.value = next
}

function toggleSelectAll(selected: boolean) {
  if (!selected) {
    selection.value = new Set()
    return
  }
  selection.value = new Set(filtered.value.map((e) => e.slug))
}

async function bulkArchive(targetStatus: 'archived' | 'active') {
  if (!selection.value.size) return
  bulkBusy.value = true
  bulkError.value = ''
  const status = targetStatus === 'archived' ? archivedStatusValue() : activeStatusValue()
  try {
    // Sequential rather than Promise.all — keeps mutation_log
    // ordering deterministic and avoids 5x parallel writes when a
    // user accidentally selects everything.
    for (const slug of selection.value) {
      const entry = entries.value.find((e) => e.slug === slug)
      if (!entry) continue
      await updateKnowledgeEntry(props.projectId, props.category, slug, {
        slug: entry.slug,
        title: entry.title,
        body: entry.body,
        status,
        metadata: entry.metadata,
      })
    }
    selection.value = new Set()
    await load()
  } catch (e) {
    bulkError.value = errMsg(e, 'Bulk operation failed.')
  } finally {
    bulkBusy.value = false
  }
}

// ── filter / search / sort ───────────────────────────────────────
// Delegated to `filterKnowledge` for testability — the contract is
// covered in src/composables/useKnowledgeFilter.test.ts.

const filtered = computed<KnowledgeEntry[]>(() =>
  filterKnowledge(entries.value, {
    category: props.category,
    search: props.searchQuery ?? '',
    memoryType: memoryTypeFilter.value,
    showArchived: showArchived.value,
    environment: environmentFilter.value,
    sort: sortMode.value,
    staleIds: showStaleOnly.value ? staleIds.value : undefined,
    showProposedOnly: showProposedOnly.value,
  }),
)

const allFilteredSelected = computed(
  () => filtered.value.length > 0 && filtered.value.every((e) => selection.value.has(e.slug)),
)

const someFilteredSelected = computed(() => filtered.value.some((e) => selection.value.has(e.slug)))

const archivedCount = computed(() => entries.value.filter(isArchived).length)

// PAI-347 — stale memory loading + reset. Both surfaces only on the
// memory tab; the dispatcher endpoint is project-scoped to memory
// type so calling it for non-memory categories is a waste.
async function loadStaleProposals() {
  if (props.category !== 'memory') {
    staleIds.value = new Set()
    return
  }
  staleLoading.value = true
  staleError.value = ''
  try {
    const proposals = await listStaleMemory(props.projectId)
    staleIds.value = new Set(proposals.map((p) => p.id))
  } catch (e) {
    staleError.value = errMsg(e, 'Failed to load stale memory proposals.')
  } finally {
    staleLoading.value = false
  }
}

watch(
  () => showStaleOnly.value,
  (on) => {
    if (on) void loadStaleProposals()
    else staleIds.value = new Set()
  },
)

const staleCount = computed(() => staleIds.value.size)

async function markStillRelevant(entry: KnowledgeEntry) {
  if (props.category !== 'memory') return
  try {
    await bumpMemoryReferences(props.projectId, [entry.id], 'ui-still-relevant')
    // Drop the id from the local set so the row immediately disappears
    // from the stale-only view; a follow-up refresh re-pulls fresh
    // data when the user toggles again.
    const next = new Set(staleIds.value)
    next.delete(entry.id)
    staleIds.value = next
  } catch (e) {
    saveError.value = errMsg(e, 'Failed to mark as still relevant.')
  }
}

const showStaleControls = computed(() => props.category === 'memory')

// PAI-349 — Proposed inbox controls live only on the memory tab.
const showProposedControls = computed(() => props.category === 'memory')
const proposedCount = computed(() => entries.value.filter(isProposed).length)

async function acceptOne(entry: KnowledgeEntry) {
  proposedActionError.value = ''
  try {
    await acceptProposedMemory(props.projectId, entry)
    if (selection.value.has(entry.slug)) {
      const next = new Set(selection.value)
      next.delete(entry.slug)
      selection.value = next
    }
    await load()
  } catch (e) {
    proposedActionError.value = errMsg(e, 'Failed to accept proposal.')
  }
}

async function rejectOne(entry: KnowledgeEntry) {
  if (!confirm(`Reject proposed memory "${entry.slug}"? This archives it with reason='rejected'.`)) return
  proposedActionError.value = ''
  try {
    await rejectProposedMemory(props.projectId, entry)
    if (selection.value.has(entry.slug)) {
      const next = new Set(selection.value)
      next.delete(entry.slug)
      selection.value = next
    }
    await load()
  } catch (e) {
    proposedActionError.value = errMsg(e, 'Failed to reject proposal.')
  }
}

async function bulkProposeAction(action: 'accept' | 'reject') {
  if (!selection.value.size) return
  if (action === 'reject' && !confirm(`Reject ${selection.value.size} proposed memories?`)) return
  bulkBusy.value = true
  proposedActionError.value = ''
  try {
    for (const slug of selection.value) {
      const entry = entries.value.find((e) => e.slug === slug)
      if (!entry || !isProposed(entry)) continue
      if (action === 'accept') {
        await acceptProposedMemory(props.projectId, entry)
      } else {
        await rejectProposedMemory(props.projectId, entry)
      }
    }
    selection.value = new Set()
    await load()
  } catch (e) {
    proposedActionError.value = errMsg(e, 'Bulk proposal action failed.')
  } finally {
    bulkBusy.value = false
  }
}

function ticketLinks(entry: KnowledgeEntry): string[] {
  const arr = entry.metadata?.['originating_tickets']
  if (!Array.isArray(arr)) return []
  return arr.filter((s): s is string => typeof s === 'string')
}

function applicableMemorySlugs(entry: KnowledgeEntry): string[] {
  // PAI-342 will populate this on issues; rendering here is purely
  // forward-compat for when issues link back to memories.
  const arr = entry.metadata?.['applicable_memories']
  if (!Array.isArray(arr)) return []
  return arr.filter((s): s is string => typeof s === 'string')
}

// PAI-348 — render an "↗ inherited from X" badge on entries that
// carry a `source` annotation. The CRUD endpoints never set it on the
// project's own Knowledge tab — entries with `source` only show up
// when this view is rendered against a bundle payload (e.g. as a
// future overlay). The component is forward-compatible so the badge
// is wired in once and works as soon as a bundle-fed surface is added.
//
// Click-through behaviour:
//   - Same-instance source: navigate to the source project's
//     Knowledge tab (in-app router link via window.location to avoid
//     coupling to a specific router instance).
//   - Cross-instance source: open the source instance's project URL
//     in a new tab — agents can keep their working tab while
//     inspecting the upstream.
function inheritedSourceLabel(entry: KnowledgeEntry): string {
  const source = entry.source
  if (!source || source.type !== 'inherited') return ''
  const project = source.from_project ?? '?'
  return `↗ inherited from ${project}`
}

function inheritedSourceTooltip(entry: KnowledgeEntry): string {
  const source = entry.source
  if (!source) return ''
  if (source.type === 'warning') return source.message ?? 'inheritance failed'
  const project = source.from_project ?? '?'
  const instance = source.from_instance ?? ''
  return instance ? `${project} on ${instance}` : project
}

function isCrossInstance(entry: KnowledgeEntry): boolean {
  const inst = entry.source?.from_instance
  if (!inst) return false
  // Compare to the current origin — same hostname = same-instance.
  // window.location is unavailable during SSR / unit tests; both fall
  // through to "treat as cross" which is the safer default (opens in
  // a new tab — never silently navigates the user off a tab).
  if (typeof window === 'undefined' || !window.location) return true
  try {
    const origin = window.location.origin
    return new URL(inst).origin !== origin
  } catch {
    return true
  }
}

function inheritedHref(entry: KnowledgeEntry): string {
  const source = entry.source
  if (!source) return ''
  // The downstream Knowledge tab needs the upstream's project key —
  // we don't have its numeric id here. Use the cross-instance form
  // (e.g. https://pm.barta.cm/projects/PAI/knowledge) and let the
  // upstream route resolve key→id. Same-instance click-through uses
  // an in-app path via the project key; the route layer handles the
  // lookup.
  const project = source.from_project ?? ''
  if (!project) return ''
  if (isCrossInstance(entry)) {
    const base = (source.from_instance ?? '').replace(/\/+$/, '')
    return `${base}/projects/${encodeURIComponent(project)}/knowledge`
  }
  return `/projects/${encodeURIComponent(project)}/knowledge`
}

const hasMemoryFilters = computed(() => props.category === 'memory')
const hasConfidenceSort = computed(() => props.category === 'memory')

onMounted(() => {
  void load()
})
</script>

<template>
  <section class="kp-section">
    <div class="kp-toolbar">
      <div class="kp-filters">
        <select v-if="hasMemoryFilters" v-model="memoryTypeFilter" class="kp-select" :title="'Filter by memory type'">
          <option value="all">All types</option>
          <option value="feedback">feedback</option>
          <option value="project">project</option>
          <option value="reference">reference</option>
          <option value="user">user</option>
        </select>
        <input
          v-model="environmentFilter"
          type="text"
          class="kp-input"
          placeholder="env (e.g. prod)"
          :title="'Filter by applies_to_environments'"
        />
        <label class="kp-checkbox" :title="'Show archived entries'">
          <input v-model="showArchived" type="checkbox" />
          <span>Archived <span v-if="archivedCount" class="kp-count">{{ archivedCount }}</span></span>
        </label>
        <label
          v-if="showStaleControls"
          class="kp-checkbox"
          :title="'Show only memory entries flagged stale by the decay heuristic (PAI-347)'"
        >
          <input v-model="showStaleOnly" type="checkbox" />
          <span>
            Stale only
            <span v-if="staleLoading" class="kp-count">…</span>
            <span v-else-if="staleCount" class="kp-count">{{ staleCount }}</span>
          </span>
        </label>
        <label
          v-if="showProposedControls"
          class="kp-checkbox"
          :title="'Show only bot-authored memory drafts pending review (PAI-349)'"
        >
          <input v-model="showProposedOnly" type="checkbox" />
          <span>
            Proposed
            <span v-if="proposedCount" class="kp-count">{{ proposedCount }}</span>
          </span>
        </label>
        <span v-if="staleError" class="kp-error">{{ staleError }}</span>
        <span v-if="proposedActionError" class="kp-error">{{ proposedActionError }}</span>
      </div>
      <div class="kp-sort">
        <label class="kp-sort-label">Sort:</label>
        <select v-model="sortMode" class="kp-select">
          <option value="recency">Most recent</option>
          <option value="alpha">Alphabetical</option>
          <option v-if="hasConfidenceSort" value="confidence">Confidence</option>
        </select>
      </div>
      <button
        v-if="canWrite && editingSlug === null && !adding"
        type="button"
        class="btn btn-ghost btn-sm"
        @click="startAdd"
      >
        <AppIcon name="plus" :size="13" />
        <span>Add entry</span>
      </button>
    </div>

    <!-- Bulk-op bar surfaces only when at least one row is selected.
         Keeps the toolbar above uncluttered for the typical case.
         PAI-349 — when the proposed-only filter is on, the bar swaps
         in Accept / Reject actions instead of Archive / Unarchive. -->
    <div v-if="selection.size > 0" class="kp-bulkbar">
      <span class="kp-bulkbar-count">{{ selection.size }} selected</span>
      <template v-if="showProposedOnly">
        <button
          type="button"
          class="btn btn-ghost btn-sm"
          :disabled="bulkBusy"
          @click="bulkProposeAction('accept')"
        >Accept</button>
        <button
          type="button"
          class="btn btn-ghost btn-sm danger"
          :disabled="bulkBusy"
          @click="bulkProposeAction('reject')"
        >Reject</button>
      </template>
      <template v-else>
        <button
          type="button"
          class="btn btn-ghost btn-sm"
          :disabled="bulkBusy"
          @click="bulkArchive('archived')"
        >Archive</button>
        <button
          type="button"
          class="btn btn-ghost btn-sm"
          :disabled="bulkBusy"
          @click="bulkArchive('active')"
        >Unarchive</button>
      </template>
      <button
        type="button"
        class="btn btn-ghost btn-sm"
        @click="selection = new Set()"
      >Clear</button>
      <span v-if="bulkError" class="kp-error">{{ bulkError }}</span>
    </div>

    <div v-if="loadError" class="kp-error">{{ loadError }}</div>
    <LoadingText v-if="loading" class="kp-empty" label="Loading entries…" />

    <div v-else-if="adding" class="kp-add-slot">
      <KnowledgeEntryEditor
        :category="category"
        :initial="draft"
        :current-slug="null"
        :saving="saving"
        :save-error="saveError"
        :autosuggest-slug="true"
        @save="onSave"
        @cancel="cancelEdit"
      />
    </div>

    <div v-if="!loading && entries.length === 0 && !adding" class="kp-empty">
      No {{ category === 'related_project' ? 'related projects' : category === 'external_system' ? 'external systems' : category + 's' }} yet.
    </div>

    <div v-else-if="!loading" class="kp-list">
      <div
        v-if="filtered.length > 0"
        class="kp-list-head"
      >
        <label class="kp-checkbox">
          <input
            type="checkbox"
            :checked="allFilteredSelected"
            :indeterminate.prop="!allFilteredSelected && someFilteredSelected"
            @change="(e) => toggleSelectAll((e.target as HTMLInputElement).checked)"
          />
          <span class="kp-hint-muted">Select all visible</span>
        </label>
        <span class="kp-list-summary">{{ filtered.length }} shown of {{ entries.length }}</span>
      </div>
      <div
        v-for="entry in filtered"
        :key="entry.slug"
        class="kp-row"
        :class="{ 'kp-row--archived': isArchived(entry) }"
      >
        <template v-if="editingSlug === entry.slug">
          <KnowledgeEntryEditor
            :category="category"
            :initial="draft"
            :current-slug="entry.slug"
            :entry-id="entry.id"
            :project-id="projectId"
            :saving="saving"
            :save-error="saveError"
            :autosuggest-slug="false"
            @save="onSave"
            @cancel="cancelEdit"
            @promoted="onPromoted"
          />
        </template>
        <template v-else>
          <label class="kp-row-select">
            <input
              type="checkbox"
              :checked="selection.has(entry.slug)"
              @change="(e) => toggleSelection(entry.slug, (e.target as HTMLInputElement).checked)"
            />
          </label>
          <div class="kp-row-main" @click="canWrite && startEdit(entry)">
            <div class="kp-row-head">
              <span class="kp-slug">{{ entry.slug }}</span>
              <span v-if="isArchived(entry)" class="kp-pill kp-pill--archived">archived</span>
              <span v-if="isProposed(entry)" class="kp-pill kp-pill--proposed">proposed</span>
              <span v-if="entry.metadata?.['type']" class="kp-pill">{{ entry.metadata.type }}</span>
              <span
                v-if="hasConfidenceSort && entry.metadata?.['confidence']"
                class="kp-pill"
              >
                {{ entry.metadata.confidence }} confidence
              </span>
              <!-- PAI-348 — inherited / warning badge. Clicking the
                   inherited badge navigates to the source project's
                   Knowledge tab; cross-instance opens in a new tab so
                   the user keeps their current shell. The warning
                   variant is non-clickable — the failure message is
                   surfaced via title=. -->
              <a
                v-if="entry.source?.type === 'inherited'"
                class="kp-pill kp-pill--inherited"
                :href="inheritedHref(entry)"
                :target="isCrossInstance(entry) ? '_blank' : undefined"
                :rel="isCrossInstance(entry) ? 'noopener noreferrer' : undefined"
                :title="inheritedSourceTooltip(entry)"
                @click.stop
              >{{ inheritedSourceLabel(entry) }}</a>
              <span
                v-else-if="entry.source?.type === 'warning'"
                class="kp-pill kp-pill--warning"
                :title="inheritedSourceTooltip(entry)"
              >⚠ inheritance failed</span>
            </div>
            <div class="kp-row-title">{{ entry.title }}</div>
            <div v-if="entry.body" class="kp-row-snippet">{{ entry.body.slice(0, 200) }}{{ entry.body.length > 200 ? '…' : '' }}</div>
            <div
              v-if="ticketLinks(entry).length"
              class="kp-row-refs"
            >
              <span class="kp-hint-muted">Originating:</span>
              <a
                v-for="t in ticketLinks(entry)"
                :key="t"
                :href="`/issues?key=${encodeURIComponent(t)}`"
                class="kp-ticket-link"
                @click.stop
              >{{ t }}</a>
            </div>
            <div
              v-if="applicableMemorySlugs(entry).length"
              class="kp-row-refs"
            >
              <span class="kp-hint-muted">Applicable memories:</span>
              <span
                v-for="m in applicableMemorySlugs(entry)"
                :key="m"
                class="kp-pill kp-pill--ref"
              >{{ m }}</span>
            </div>
          </div>
          <div v-if="canWrite" class="kp-row-actions">
            <button
              v-if="showStaleOnly && showStaleControls"
              type="button"
              class="btn btn-ghost btn-sm"
              :title="'Reset the decay clock — keep this memory in active rotation'"
              @click.stop="markStillRelevant(entry)"
            >Still relevant</button>
            <!-- PAI-349 — per-row accept / edit-and-accept / reject for
                 proposed memory drafts. Edit & Accept reuses the regular
                 editor (startEdit) — when the operator saves, status
                 defaults to whatever's in the draft, but the typical
                 flow is to flip it to active in the editor. The Accept
                 button is the no-edit fast path. -->
            <template v-if="isProposed(entry)">
              <button
                type="button"
                class="btn btn-ghost btn-sm"
                :title="'Accept this proposal as-is — flips status to active'"
                @click.stop="acceptOne(entry)"
              >Accept</button>
              <button
                type="button"
                class="btn btn-ghost btn-sm"
                :title="'Edit before accepting — opens the inline editor'"
                @click.stop="startEdit(entry)"
              >Edit &amp; Accept</button>
              <button
                type="button"
                class="btn btn-ghost btn-sm danger"
                :title="'Reject — archives with reason=rejected'"
                @click.stop="rejectOne(entry)"
              >Reject</button>
            </template>
            <template v-else>
              <button type="button" class="btn btn-ghost btn-sm" @click.stop="startEdit(entry)">Edit</button>
              <button type="button" class="btn btn-ghost btn-sm danger" @click.stop="remove(entry)">Delete</button>
            </template>
          </div>
        </template>
      </div>
      <div v-if="filtered.length === 0 && entries.length > 0" class="kp-empty">
        No entries match the current filter.
      </div>
    </div>
  </section>
</template>

<style scoped>
.kp-section { display: flex; flex-direction: column; gap: .55rem; }
.kp-toolbar { display: flex; flex-wrap: wrap; gap: .5rem; align-items: center; }
.kp-filters { display: flex; flex-wrap: wrap; gap: .35rem; align-items: center; }
.kp-sort { display: flex; align-items: center; gap: .35rem; margin-left: auto; }
.kp-sort-label { font-size: 11px; color: var(--text-muted); text-transform: uppercase; letter-spacing: .05em; }
.kp-select, .kp-input { border: 1px solid var(--border); border-radius: 6px; background: var(--bg); color: var(--text); font: inherit; padding: .3rem .5rem; font-size: 12px; }
.kp-input { min-width: 140px; }
.kp-checkbox { display: inline-flex; align-items: center; gap: .35rem; font-size: 12px; color: var(--text); cursor: pointer; }
.kp-count { background: var(--bg); border: 1px solid var(--border); border-radius: 999px; padding: 0 .35rem; font-size: 10px; color: var(--text-muted); }
.kp-bulkbar { display: flex; align-items: center; gap: .4rem; padding: .35rem .55rem; background: var(--bp-blue-pale); border: 1px solid var(--bp-blue); border-radius: 6px; font-size: 12px; }
.kp-bulkbar-count { font-weight: 700; color: var(--bp-blue-dark); margin-right: .35rem; }
.kp-empty { color: var(--text-muted); font-size: 13px; padding: 1rem 0; }
.kp-list { display: flex; flex-direction: column; gap: .4rem; }
.kp-list-head { display: flex; align-items: center; justify-content: space-between; gap: .5rem; padding: 0 .35rem; }
.kp-list-summary { font-size: 11px; color: var(--text-muted); }
.kp-row { display: flex; align-items: flex-start; gap: .45rem; padding: .55rem .65rem; border: 1px solid var(--border); border-radius: 8px; background: var(--bg); }
.kp-row--archived { opacity: .65; background: var(--bg-card); }
.kp-row-select { padding-top: .15rem; flex-shrink: 0; }
.kp-row-main { flex: 1; display: flex; flex-direction: column; gap: .2rem; min-width: 0; cursor: pointer; }
.kp-row-head { display: flex; flex-wrap: wrap; gap: .35rem; align-items: center; }
.kp-slug { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-weight: 700; font-size: 12px; color: var(--text); }
.kp-pill { display: inline-block; background: var(--bg-card); border: 1px solid var(--border); border-radius: 999px; padding: 0 .5rem; font-size: 10px; color: var(--text-muted); line-height: 1.55; }
.kp-pill--archived { background: var(--bg); color: #b45309; border-color: #fcd9a3; }
.kp-pill--proposed { background: #fef9c3; color: #713f12; border-color: #fde68a; }
.kp-pill--inherited { background: var(--bp-blue-pale, #eef4ff); color: var(--bp-blue-dark, #1d4ed8); border-color: var(--bp-blue, #3b82f6); text-decoration: none; cursor: pointer; }
.kp-pill--inherited:hover { background: var(--bp-blue, #3b82f6); color: #fff; }
.kp-pill--warning { background: #fef3f2; color: #b42318; border-color: #fecdca; }
.kp-pill--ref { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
.kp-row-title { font-size: 13px; font-weight: 600; color: var(--text); }
.kp-row-snippet { font-size: 12px; color: var(--text-muted); white-space: pre-wrap; line-height: 1.45; }
.kp-row-refs { display: flex; flex-wrap: wrap; gap: .35rem; align-items: center; font-size: 11px; }
.kp-hint-muted { color: var(--text-muted); font-size: 11px; }
.kp-ticket-link { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-weight: 600; color: var(--bp-blue); text-decoration: none; }
.kp-ticket-link:hover { text-decoration: underline; }
.kp-row-actions { display: flex; gap: .3rem; flex-shrink: 0; }
.kp-add-slot { padding: .15rem 0; }
.kp-error { color: #b42318; font-size: 12px; background: #fef3f2; border: 1px solid #fecdca; border-radius: 8px; padding: .45rem .6rem; }

@media (max-width: 540px) {
  .kp-toolbar { flex-direction: column; align-items: stretch; }
  .kp-sort { margin-left: 0; }
  .kp-row { flex-direction: column; }
  .kp-row-actions { align-self: flex-end; }
}
</style>
