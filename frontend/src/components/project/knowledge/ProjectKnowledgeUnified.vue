<script setup lang="ts">
// PAI-360 — unified Knowledge tab. Replaces PAI-339's pill-tab + per-
// category panel pattern with a single list + type filter chips +
// shared search + in-place editor.
//
// IA: type is a *filter*, not a navigation primitive. One scrollable
// list with all five categories (memory / runbook / external_system /
// related_project / guideline) interleaved by recency; clicking a
// chip narrows the visible types; clicking an entry swaps the list
// for the editor in place. Add Entry button has a small dropdown
// picking the category for the new row.
//
// Contract preserved: `initialMemorySlug` deep-link target from
// PAI-342 still works — the unified view auto-applies the 'memory'
// chip filter and pre-opens the matching slug in the editor.

import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import AppIcon from '@/components/AppIcon.vue'
import LoadingText from '@/components/LoadingText.vue'
import KnowledgeSidePanel from './KnowledgeSidePanel.vue'
import {
  acknowledgeMemoryReview,
  activeStatusValue,
  createKnowledgeEntry,
  deleteKnowledgeEntry,
  isArchived,
  isProposed,
  listKnowledgeEntries,
  needsReview,
  reviewReason,
  updateKnowledgeEntry,
} from '@/services/projectKnowledge'
import { errMsg } from '@/api/client'
import {
  useSidePanelPinned,
  setSidePanelVisible,
  setSidePanelPinned,
} from '@/composables/useSidePanelPinned'
import type {
  KnowledgeCategory,
  KnowledgeEntry,
  KnowledgeEntryInput,
} from '@/types'
import { fmtRelative } from '@/utils/formatTime'

const props = defineProps<{
  projectId: number
  canWrite: boolean
  // PAI-342 deep-link: slug of a memory entry to auto-open in the
  // editor on first mount. Empty string = no target.
  initialMemorySlug?: string
}>()

interface CategoryDef {
  key: KnowledgeCategory
  label: string
  icon: string
}

const CATEGORIES: CategoryDef[] = [
  { key: 'memory',           label: 'Memory',           icon: 'lightbulb'    },
  { key: 'runbook',          label: 'Runbook',          icon: 'list-checks'  },
  { key: 'external_system',  label: 'External system',  icon: 'plug'         },
  { key: 'related_project',  label: 'Related project',  icon: 'link'         },
  { key: 'guideline',        label: 'Guideline',        icon: 'shield-check' },
]

const labelFor: Record<KnowledgeCategory, string> = Object.fromEntries(
  CATEGORIES.map((c) => [c.key, c.label]),
) as Record<KnowledgeCategory, string>

// Loaded entries by category. Flat list derived in `entries` below.
const byCategory = ref<Record<KnowledgeCategory, KnowledgeEntry[]>>({
  memory: [],
  runbook: [],
  external_system: [],
  related_project: [],
  guideline: [],
})
const loading = ref(true)
const loadError = ref('')

const entries = computed<KnowledgeEntry[]>(() =>
  CATEGORIES.flatMap((c) => byCategory.value[c.key]),
)

// Filter chips. Empty set = "all" (no narrowing). Toggling a chip
// adds / removes from the set; clicking a chip with shift narrows
// to "solo" mode (only that one selected). Default empty = show all.
const activeTypes = ref<Set<KnowledgeCategory>>(new Set())
const search = ref('')
const showArchived = ref(false)
const showNeedsReviewOnly = ref(false)
const showProposedOnly = ref(false)

function toggleType(key: KnowledgeCategory, soloOverride: boolean) {
  if (soloOverride) {
    // Solo mode: only this chip selected.
    activeTypes.value = new Set([key])
    return
  }
  const next = new Set(activeTypes.value)
  if (next.has(key)) next.delete(key)
  else next.add(key)
  activeTypes.value = next
}

function clearTypes() {
  activeTypes.value = new Set()
}

// Editor state. `editingEntry` is null in "list mode" and a clone of
// the selected entry in "edit mode". `creatingCategory` holds the
// new-entry category when the user clicked Add Entry but hasn't
// saved yet; both are mutually exclusive.
const editingEntry = ref<KnowledgeEntry | null>(null)
const creatingCategory = ref<KnowledgeCategory | null>(null)
const editorDraft = ref<KnowledgeEntryInput>(emptyDraft())
const saving = ref(false)
const saveError = ref('')

// Add Entry dropdown — separate small UI piece. The dropdown picks
// a category, then transitions the editor into create mode.
const addOpen = ref(false)
function openAdd(category: KnowledgeCategory) {
  addOpen.value = false
  creatingCategory.value = category
  editingEntry.value = null
  editorDraft.value = emptyDraft()
  saveError.value = ''
}
function toggleAdd() { addOpen.value = !addOpen.value }
function closeAdd() { addOpen.value = false }

function openEntry(entry: KnowledgeEntry) {
  creatingCategory.value = null
  editingEntry.value = entry
  editorDraft.value = {
    slug: entry.slug,
    title: entry.title,
    body: entry.body,
    status: entry.status,
    metadata: { ...entry.metadata },
  }
  saveError.value = ''
}

// PAI-395 phase 4: backToList is now backToList-via-close-panel —
// kept as the name because callers (deep-link watcher, save/delete
// completion) all want the same semantics: drop the selection so
// the panel closes and the list is the sole focus.
function backToList() {
  editingEntry.value = null
  creatingCategory.value = null
  editorDraft.value = emptyDraft()
  saveError.value = ''
}

// PAI-395 phase 4: shared side-panel pinned + visible state.
// AppLayout reads these singletons to inset .main when pinned + visible.
const { pinned: panelPinned } = useSidePanelPinned()

// Drive the global `visible` ref from the local selection + pin
// state. Matches IssueList's `!!id || pinned` invariant: pinned
// keeps the panel inset reserved even with no entry selected (the
// panel then shows its "pick an entry" placeholder). On tab unmount
// we drop visible regardless so AppLayout's inset retracts when the
// surface is gone.
watch(
  [editingEntry, creatingCategory, panelPinned],
  ([entry, creating, pinned]) => {
    setSidePanelVisible(entry !== null || creating !== null || pinned)
  },
  { immediate: true },
)
onBeforeUnmount(() => setSidePanelVisible(false))

function onPanelPinUpdate(v: boolean) {
  setSidePanelPinned(v)
}

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    const results = await Promise.all(
      CATEGORIES.map((c) => listKnowledgeEntries(props.projectId, c.key)),
    )
    CATEGORIES.forEach((c, i) => {
      byCategory.value[c.key] = results[i]
    })
  } catch (e) {
    loadError.value = errMsg(e, 'Failed to load knowledge entries.')
  } finally {
    loading.value = false
  }
}

onMounted(async () => {
  await load()
  // PAI-342 deep-link: auto-open the matching memory slug if requested.
  if (props.initialMemorySlug) {
    const target = byCategory.value.memory.find(
      (e) => e.slug === props.initialMemorySlug,
    )
    if (target) {
      activeTypes.value = new Set(['memory'])
      openEntry(target)
    }
  }
  document.addEventListener('mousedown', onOutsideClickAdd)
})
onBeforeUnmount(() => {
  document.removeEventListener('mousedown', onOutsideClickAdd)
})

// Click outside the Add dropdown closes it.
const addAnchor = ref<HTMLElement | null>(null)
function onOutsideClickAdd(ev: MouseEvent) {
  if (!addOpen.value) return
  const el = addAnchor.value
  if (el && !el.contains(ev.target as Node)) closeAdd()
}

// Apply chips + search + archived/proposed toggles client-side. The
// per-category list usually has < 100 entries; full-text scan is
// imperceptible. The IssueList table-style is the precedent — same
// no-server-search-needed shape.
const filtered = computed<KnowledgeEntry[]>(() => {
  const s = search.value.trim().toLowerCase()
  const types = activeTypes.value
  const wantArchived = showArchived.value
  const wantProposedOnly = showProposedOnly.value
  const wantNeedsReviewOnly = showNeedsReviewOnly.value

  return entries.value
    .filter((e) => {
      if (types.size > 0 && !types.has(e.type)) return false
      if (!wantArchived && isArchived(e)) return false
      if (wantProposedOnly && !isProposed(e)) return false
      if (wantNeedsReviewOnly && !needsReview(e)) return false
      if (s) {
        const hay = `${e.title} ${e.slug} ${e.body}`.toLowerCase()
        if (!hay.includes(s)) return false
      }
      return true
    })
    .sort((a, b) => (b.updated_at || '').localeCompare(a.updated_at || ''))
})

// Per-type counts for the chips. Show all loaded counts, not the
// post-filter ones — the chip count is "what's available", not
// "what's shown given other filters".
const counts = computed<Record<KnowledgeCategory, number>>(() => {
  const out: Record<KnowledgeCategory, number> = {
    memory: 0, runbook: 0, external_system: 0, related_project: 0, guideline: 0,
  }
  for (const e of entries.value) {
    if (!showArchived.value && isArchived(e)) continue
    out[e.type]++
  }
  return out
})

const totalLoaded = computed(() => entries.value.length)

// Save handler — branches on create vs update by which state ref is
// populated. Both go through the canonical convenience endpoints
// (PAI-353 hooks pull them onto issue_history + mutation_log).
async function onSave(payload: KnowledgeEntryInput) {
  saving.value = true
  saveError.value = ''
  try {
    if (creatingCategory.value) {
      const created = await createKnowledgeEntry(
        props.projectId,
        creatingCategory.value,
        payload,
      )
      byCategory.value[created.type] = [created, ...byCategory.value[created.type]]
      backToList()
    } else if (editingEntry.value) {
      const cat = editingEntry.value.type
      const oldSlug = editingEntry.value.slug
      const updated = await updateKnowledgeEntry(
        props.projectId,
        cat,
        oldSlug,
        payload,
      )
      byCategory.value[cat] = byCategory.value[cat].map((e) =>
        e.slug === oldSlug ? updated : e,
      )
      // PAI-351 slice 2 — needs_review is cross-entry derived: editing a
      // memory body revises content_revised_at and can flag/unflag dependents,
      // and the convenience PUT response can't carry the recomputed flag. Re-
      // fetch the memory list so every pill reflects the new state (best-effort
      // — the optimistic swap above already applied if this refresh fails).
      if (cat === 'memory') {
        byCategory.value.memory = await listKnowledgeEntries(
          props.projectId,
          'memory',
        ).catch(() => byCategory.value.memory)
      }
      backToList()
    }
  } catch (e) {
    saveError.value = errMsg(e, 'Save failed.')
  } finally {
    saving.value = false
  }
}

async function onDelete() {
  if (!editingEntry.value) return
  if (!confirm(`Delete ${editingEntry.value.slug}? This is a soft-delete; admin can restore via Trash.`)) return
  saving.value = true
  saveError.value = ''
  try {
    const cat = editingEntry.value.type
    const slug = editingEntry.value.slug
    await deleteKnowledgeEntry(props.projectId, cat, slug)
    byCategory.value[cat] = byCategory.value[cat].filter((e) => e.slug !== slug)
    backToList()
  } catch (e) {
    saveError.value = errMsg(e, 'Delete failed.')
  } finally {
    saving.value = false
  }
}

// PAI-351 slice 2 — acknowledge the open memory's needs-review flag. The
// server stamps deps_reviewed_at and returns the reloaded entry (flag
// cleared); we swap it in place so the pill + editor banner drop without
// closing the panel.
async function onAcknowledgeReview() {
  const entry = editingEntry.value
  if (!entry) return
  saving.value = true
  saveError.value = ''
  try {
    const updated = await acknowledgeMemoryReview(props.projectId, entry.slug)
    const cat = entry.type
    byCategory.value[cat] = byCategory.value[cat].map((e) =>
      e.slug === entry.slug ? updated : e,
    )
    editingEntry.value = updated
  } catch (e) {
    saveError.value = errMsg(e, 'Acknowledge failed.')
  } finally {
    saving.value = false
  }
}

function formatRelative(iso: string | undefined): string {
  return iso ? fmtRelative(iso) : ''
}

function emptyDraft(): KnowledgeEntryInput {
  return { slug: '', title: '', body: '', status: activeStatusValue(), metadata: {} }
}

// PAI-342 reactive deep-link: if the parent updates initialMemorySlug
// after mount (e.g. user navigates with a new ?memory= query), pop
// the editor open for the new target.
watch(
  () => props.initialMemorySlug,
  (slug) => {
    if (!slug || loading.value) return
    const target = byCategory.value.memory.find((e) => e.slug === slug)
    if (target) {
      activeTypes.value = new Set(['memory'])
      openEntry(target)
    }
  },
)
</script>

<template>
  <div class="pku-root">
    <LoadingText v-if="loading" label="Loading knowledge…" class="pku-empty" />
    <div v-else-if="loadError" class="pku-error">{{ loadError }}</div>

    <!-- PAI-395 phase 4: list is always rendered; selection opens a
         side-panel sibling instead of swapping the panel into edit
         mode. -->
    <template v-else>
      <header class="pku-header">
        <div class="pku-search-row">
          <div class="pku-search">
            <AppIcon name="search" :size="14" class="pku-search__icon" />
            <input
              v-model="search"
              type="search"
              placeholder="Search title, slug, or body…"
              class="pku-search__input"
            />
          </div>

          <div class="pku-add" ref="addAnchor">
            <button
              v-if="canWrite"
              class="btn btn-primary btn-sm pku-add__trigger"
              :class="{ active: addOpen }"
              :disabled="!canWrite"
              @click="toggleAdd"
            >
              <AppIcon name="plus" :size="13" /> Add entry
              <AppIcon name="chevron-down" :size="11" />
            </button>
            <div v-if="addOpen" class="pku-add__menu" role="menu">
              <button
                v-for="c in CATEGORIES"
                :key="c.key"
                class="pku-add__item"
                role="menuitem"
                @click="openAdd(c.key)"
              >
                <AppIcon :name="c.icon" :size="13" />
                <span>{{ c.label }}</span>
              </button>
            </div>
          </div>
        </div>

        <!-- Type filter chips. Click toggles; shift-click solos.
             "All" chip clears the filter set. -->
        <div class="pku-chips" role="tablist" aria-label="Knowledge type filter">
          <button
            type="button"
            class="pku-chip"
            :class="{ 'pku-chip--active': activeTypes.size === 0 }"
            @click="clearTypes"
          >
            All <span class="pku-chip__count">{{ totalLoaded }}</span>
          </button>
          <button
            v-for="c in CATEGORIES"
            :key="c.key"
            type="button"
            class="pku-chip"
            :class="{ 'pku-chip--active': activeTypes.has(c.key) }"
            :title="`Click to toggle · shift+click to solo`"
            @click="(e) => toggleType(c.key, e.shiftKey)"
          >
            <AppIcon :name="c.icon" :size="11" class="pku-chip__icon" />
            {{ c.label }}
            <span class="pku-chip__count">{{ counts[c.key] }}</span>
          </button>
        </div>

        <div class="pku-toggles">
          <label class="pku-toggle">
            <input v-model="showArchived" type="checkbox" />
            <span>Archived</span>
          </label>
          <label class="pku-toggle">
            <input v-model="showProposedOnly" type="checkbox" />
            <span>Proposed only</span>
          </label>
          <label class="pku-toggle">
            <input v-model="showNeedsReviewOnly" type="checkbox" />
            <span>Needs review</span>
          </label>
        </div>
      </header>

      <ul v-if="filtered.length" class="pku-list">
        <li
          v-for="e in filtered"
          :key="`${e.type}::${e.slug}`"
          class="pku-row"
          :class="{
            'pku-row--archived': isArchived(e),
            'pku-row--proposed': isProposed(e),
            'pku-row--needs-review': needsReview(e),
            'pku-row--selected':
              editingEntry !== null
              && editingEntry.type === e.type
              && editingEntry.slug === e.slug,
          }"
          tabindex="0"
          @click="openEntry(e)"
          @keydown.enter="openEntry(e)"
        >
          <span class="pku-badge" :data-type="e.type">{{ labelFor[e.type] }}</span>
          <div class="pku-row__main">
            <strong class="pku-row__title">{{ e.title || e.slug }}</strong>
            <code class="pku-row__slug">{{ e.slug }}</code>
          </div>
          <span v-if="isProposed(e)" class="pku-row__pill">Proposed</span>
          <span v-if="isArchived(e)" class="pku-row__pill pku-row__pill--muted">Archived</span>
          <span
            v-if="needsReview(e)"
            class="pku-row__pill pku-row__pill--review"
            :title="reviewReason(e)"
          >Needs review</span>
          <span class="pku-row__time">{{ formatRelative(e.updated_at) }}</span>
        </li>
      </ul>
      <div v-else class="pku-empty">
        <p v-if="search || activeTypes.size > 0">No entries match the current filters.</p>
        <p v-else>No knowledge entries yet.</p>
        <button v-if="search || activeTypes.size > 0" class="btn btn-ghost btn-sm" @click="search=''; clearTypes()">
          Clear filters
        </button>
      </div>
    </template>

    <!-- PAI-395 phase 4: side panel sibling. Mounts as fixed-right
         via the IssueSidePanel chrome twin. AppLayout's right-inset
         applies when pinned + visible (visible is driven by the
         watcher in <script setup>). -->
    <KnowledgeSidePanel
      :entry="editingEntry"
      :creating-category="creatingCategory"
      :draft="editorDraft"
      :saving="saving"
      :save-error="saveError"
      :pinned="panelPinned"
      :can-write="canWrite"
      :project-id="projectId"
      @close="backToList"
      @save="onSave"
      @delete="onDelete"
      @promoted="backToList"
      @reviewed="onAcknowledgeReview"
      @update:pinned="onPanelPinUpdate"
    />
  </div>
</template>

<style scoped>
/* PAI-360 — single root: search + chips + list, or editor in place.
   No pill sub-nav. Keeps to the same chrome-family the v3.0 footer
   bar uses — neutral active tints, monospace for slugs, no fresh
   accent colors. */
.pku-root {
  display: flex;
  flex-direction: column;
  gap: 1rem;
  padding: .25rem 0;
  min-width: 0;
}

.pku-empty {
  color: var(--text-muted);
  font-size: 13px;
  padding: 2rem 0;
  text-align: center;
}

.pku-error {
  color: #b42318;
  background: #fef3f2;
  border: 1px solid #fecdca;
  border-radius: 10px;
  padding: .7rem .85rem;
  font-size: 13px;
}

/* ── header (search + add + chips + toggles) ─────────────── */
.pku-header {
  display: flex;
  flex-direction: column;
  gap: .65rem;
}

.pku-search-row {
  display: flex;
  gap: .55rem;
  align-items: stretch;
}

.pku-search {
  position: relative;
  flex: 1;
  min-width: 0;
}
.pku-search__icon {
  position: absolute;
  left: .65rem;
  top: 50%;
  transform: translateY(-50%);
  color: var(--text-muted);
  pointer-events: none;
}
.pku-search__input {
  width: 100%;
  padding: .5rem .65rem .5rem 2rem;
  font-size: 13px;
  font-family: inherit;
  background: var(--bg);
  color: var(--text);
  border: 1px solid var(--border);
  border-radius: 8px;
  outline: 0;
  transition: border-color .12s, box-shadow .12s;
}
.pku-search__input:focus {
  border-color: var(--brand-blue);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--brand-blue) 18%, transparent);
}

.pku-add {
  position: relative;
}
.pku-add__trigger {
  display: inline-flex;
  align-items: center;
  gap: .35rem;
}
.pku-add__menu {
  position: absolute;
  top: calc(100% + .25rem);
  right: 0;
  z-index: 20;
  min-width: 200px;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 10px;
  box-shadow: var(--shadow);
  padding: .25rem;
  display: flex;
  flex-direction: column;
  gap: 1px;
}
.pku-add__item {
  display: inline-flex;
  align-items: center;
  gap: .5rem;
  padding: .5rem .65rem;
  font: inherit;
  font-size: 13px;
  color: var(--text);
  background: none;
  border: 0;
  border-radius: 6px;
  text-align: left;
  cursor: pointer;
  transition: background-color .1s;
}
.pku-add__item:hover {
  background: color-mix(in srgb, var(--brand-blue) 10%, transparent);
}

.pku-chips {
  display: flex;
  flex-wrap: wrap;
  gap: .35rem;
}
.pku-chip {
  display: inline-flex;
  align-items: center;
  gap: .35rem;
  padding: .25rem .6rem;
  font: inherit;
  font-size: 12px;
  font-weight: 500;
  color: var(--text-muted);
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 999px;
  cursor: pointer;
  transition: color .12s, background-color .12s, border-color .12s;
}
.pku-chip:hover {
  color: var(--text);
  background: var(--bg);
}
.pku-chip--active {
  color: var(--text);
  background: color-mix(in srgb, var(--brand-blue) 12%, transparent);
  border-color: color-mix(in srgb, var(--brand-blue) 35%, var(--border));
  font-weight: 600;
}
.pku-chip__icon {
  flex-shrink: 0;
  opacity: .7;
}
.pku-chip__count {
  font-size: 11px;
  font-weight: 700;
  font-variant-numeric: tabular-nums;
  color: var(--text-muted);
  background: color-mix(in srgb, var(--text-muted) 8%, transparent);
  padding: 0 .35rem;
  border-radius: 8px;
  min-width: 1.1rem;
  text-align: center;
}
.pku-chip--active .pku-chip__count {
  color: var(--text);
  background: color-mix(in srgb, var(--brand-blue) 18%, transparent);
}

.pku-toggles {
  display: flex;
  gap: .85rem;
  font-size: 12px;
  color: var(--text-muted);
}
.pku-toggle {
  display: inline-flex;
  align-items: center;
  gap: .35rem;
  cursor: pointer;
  user-select: none;
}
.pku-toggle input { cursor: pointer; }

/* ── list ─────────────────────────────────────────────────── */
.pku-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 0;
  border-top: 1px solid var(--border);
}
.pku-row {
  display: flex;
  align-items: center;
  gap: .75rem;
  padding: .55rem .15rem;
  border-bottom: 1px solid var(--border);
  cursor: pointer;
  transition: background-color .1s;
  outline: 0;
}
.pku-row:hover {
  background: var(--surface-2, var(--bg));
}
.pku-row:focus-visible {
  background: color-mix(in srgb, var(--brand-blue) 8%, transparent);
}
.pku-row--archived {
  opacity: .55;
}

.pku-badge {
  flex-shrink: 0;
  font-size: 10px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: .04em;
  padding: .15rem .45rem;
  border-radius: 4px;
  background: var(--surface-2, var(--bg));
  color: var(--text-muted);
  border: 1px solid var(--border);
  font-variant-numeric: tabular-nums;
  min-width: 5rem;
  text-align: center;
}
/* Light type-coding via subtle bg tint. Kept neutral overall — no
   loud accents. */
.pku-badge[data-type='memory']           { background: color-mix(in srgb, var(--brand-blue) 8%, transparent); color: var(--brand-blue-dark); border-color: color-mix(in srgb, var(--brand-blue) 20%, var(--border)); }
.pku-badge[data-type='runbook']          { background: color-mix(in srgb, #f59e0b 12%, transparent);     color: #92400e;             border-color: color-mix(in srgb, #f59e0b 25%, var(--border)); }
.pku-badge[data-type='external_system']  { background: color-mix(in srgb, #8b5cf6 10%, transparent);     color: #5b21b6;             border-color: color-mix(in srgb, #8b5cf6 22%, var(--border)); }
.pku-badge[data-type='related_project']  { background: color-mix(in srgb, #06b6d4 12%, transparent);     color: #155e75;             border-color: color-mix(in srgb, #06b6d4 25%, var(--border)); }
.pku-badge[data-type='guideline']        { background: color-mix(in srgb, var(--brand-green) 10%, transparent); color: #166534; border-color: color-mix(in srgb, var(--brand-green) 22%, var(--border)); }

.pku-row__main {
  flex: 1;
  min-width: 0;
  display: flex;
  align-items: baseline;
  gap: .55rem;
}
.pku-row__title {
  font-size: 13px;
  font-weight: 600;
  color: var(--text);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.pku-row__slug {
  font-size: 11px;
  color: var(--text-muted);
  font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
  flex-shrink: 0;
}
.pku-row__time {
  flex-shrink: 0;
  font-size: 11px;
  color: var(--text-muted);
  font-variant-numeric: tabular-nums;
}
.pku-row__pill {
  flex-shrink: 0;
  font-size: 10px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: .04em;
  padding: .1rem .4rem;
  border-radius: 4px;
  background: color-mix(in srgb, var(--brand-blue) 12%, transparent);
  color: var(--brand-blue-dark);
}
.pku-row__pill--review {
  background: color-mix(in srgb, #f59e0b 18%, transparent);
  color: #b45309;
  cursor: help;
}
.pku-row--needs-review {
  box-shadow: inset 3px 0 0 #f59e0b;
}
.pku-row__pill--muted {
  background: var(--surface-2, var(--bg));
  color: var(--text-muted);
}

/* PAI-395 phase 4: selected row highlight while the side panel is
   open on this entry. Neutral blue-tint same as IssueList's selected
   row. */
.pku-row--selected {
  background: color-mix(in srgb, var(--brand-blue) 10%, transparent);
}
.pku-row--selected:hover {
  background: color-mix(in srgb, var(--brand-blue) 14%, transparent);
}

/* ── responsive ──────────────────────────────────────────── */
@media (max-width: 640px) {
  .pku-search-row {
    flex-direction: column;
  }
  .pku-row {
    flex-wrap: wrap;
  }
  .pku-row__time {
    margin-left: auto;
  }
  .pku-row__main {
    flex-basis: 100%;
    margin-top: .15rem;
  }
}
</style>
