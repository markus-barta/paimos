<script setup lang="ts">
import LoadingText from "@/components/LoadingText.vue";
import { ref, watch, computed, nextTick, onMounted, onUnmounted } from 'vue'
import { api } from '@/api/client'
import { useSearchStore } from '@/stores/search'
import AppIcon from '@/components/AppIcon.vue'
import StatusDot from '@/components/StatusDot.vue'

// PAI-282: AppHeader has `overflow: hidden` to keep the structural 52px
// chrome from being stretched by overflowing children, which clips any
// absolutely-positioned descendant — including this palette. Teleport to
// body and position `fixed` against the search-wrap's bounding rect so
// the dropdown escapes the header's clip box. Same pattern PAI-265 used
// for the project-detail ⋯ menu.

interface SearchIssue {
  id: number
  issue_key: string
  title: string
  type: string
  status: string
  priority: string
  project_id: number | null
  project_key: string
  assignee_username: string | null
}

interface SearchResults {
  issues: SearchIssue[]
  projects: { id: number; name: string; key: string }[]
  has_more: boolean
}

const props = defineProps<{
  visible: boolean
  anchor?: HTMLElement | null
}>()

const emit = defineEmits<{
  close: []
  navigate: [path: string]
}>()

const search = useSearchStore()
const results = ref<SearchResults | null>(null)
const loading = ref(false)
const activeIndex = ref(-1)
const paletteRef = ref<HTMLElement | null>(null)

let debounceTimer: ReturnType<typeof setTimeout> | null = null
let lastFetchedKey = ''
let requestSeq = 0
const cache = new Map<string, SearchResults>()

const paletteScope = computed(() =>
  search.scope === 'project' && search.projectId ? 'project' : 'global',
)
const paletteProjectId = computed(() =>
  paletteScope.value === 'project' ? search.projectId : null,
)
const paletteScopeLabel = computed(() =>
  paletteScope.value === 'project'
    ? `Searching ${search.projectKey || 'this project'}`
    : 'Searching all projects',
)
const paletteAllResultsLabel = computed(() =>
  paletteScope.value === 'project'
    ? `see all in ${search.projectKey || 'project'}`
    : 'see all globally',
)
const paletteOpenFullLabel = computed(() =>
  paletteScope.value === 'project'
    ? `open ${search.projectKey || 'project'} search`
    : 'open global search',
)

function cacheKey(q: string) {
  return `${paletteScope.value}:${paletteProjectId.value ?? 'all'}:${q}`
}

watch([() => search.query, paletteScope, paletteProjectId], ([q]) => {
  if (debounceTimer) clearTimeout(debounceTimer)
  const trimmed = q.trim()
  if (trimmed.length < 2) {
    results.value = null
    activeIndex.value = -1
    return
  }
  const key = cacheKey(trimmed)
  // Show cached result immediately
  if (cache.has(key)) {
    results.value = cache.get(key)!
  } else {
    results.value = null
  }
  debounceTimer = setTimeout(() => fetchResults(trimmed), 150)
})

// PAI-284: auto-highlight the first visible row whenever results change.
// If a directMatch row exists (user typed an exact issue key), highlight
// that — it's rendered first visually. Otherwise fall back to items[0].
// Means Enter without ArrowDown is predictable and lands on the row the
// user already sees as "selected".
watch(results, () => {
  if (!results.value || paletteOptions.value.length === 0) {
    activeIndex.value = -1
    return
  }
  activeIndex.value = 0
})

async function fetchResults(q: string) {
  const key = cacheKey(q)
  if (key === lastFetchedKey && results.value) return
  const seq = ++requestSeq
  loading.value = true
  try {
    const params = new URLSearchParams({ q, limit: '10' })
    if (paletteScope.value === 'project' && paletteProjectId.value) {
      params.set('scope', 'project')
      params.set('project_id', String(paletteProjectId.value))
    }
    const data = await api.get<SearchResults>(`/search?${params.toString()}`)
    if (seq !== requestSeq) return
    results.value = data
    cache.set(key, data)
    lastFetchedKey = key
  } catch { /* silent */ }
  finally {
    if (seq === requestSeq) loading.value = false
  }
}

const items = computed<SearchIssue[]>(() => results.value?.issues ?? [])

const directMatch = computed<SearchIssue | null>(() => {
  const q = search.query.trim().toUpperCase()
  if (!q || !items.value.length) return null
  return items.value.find(i => i.issue_key?.toUpperCase() === q) ?? null
})

const otherItems = computed(() => {
  const dm = directMatch.value
  if (!dm) return items.value
  return items.value.filter(i => i.id !== dm.id)
})

type PaletteOption =
  | { kind: 'issue'; issue: SearchIssue }
  | { kind: 'all' }

const visibleIssueItems = computed<SearchIssue[]>(() => {
  const dm = directMatch.value
  return dm ? [dm, ...otherItems.value] : items.value
})

const paletteOptions = computed<PaletteOption[]>(() => {
  if (!items.value.length) return []
  return [
    ...visibleIssueItems.value.map(issue => ({ kind: 'issue' as const, issue })),
    { kind: 'all' as const },
  ]
})

const activeOption = computed(() => paletteOptions.value[activeIndex.value] ?? null)

function isIssueActive(issue: SearchIssue) {
  const active = activeOption.value
  return active?.kind === 'issue' && active.issue.id === issue.id
}

function optionIndexForIssue(issue: SearchIssue) {
  return paletteOptions.value.findIndex(
    option => option.kind === 'issue' && option.issue.id === issue.id,
  )
}

const isAllResultsActive = computed(() => activeOption.value?.kind === 'all')

function navigateToIssue(issue: SearchIssue) {
  if (issue.project_id) {
    emit('navigate', `/projects/${issue.project_id}/issues/${issue.id}`)
  }
  emit('close')
}

function navigateToAllResults() {
  const q = search.query.trim()
  const suffix = q.length >= 2 ? `?q=${encodeURIComponent(q)}` : ''
  if (paletteScope.value === 'project' && paletteProjectId.value) {
    emit('navigate', `/projects/${paletteProjectId.value}${suffix}`)
  } else {
    emit('navigate', `/issues${suffix}`)
  }
  emit('close')
}

function onKeydown(e: KeyboardEvent) {
  if (!props.visible || !results.value) return

  const total = paletteOptions.value.length
  if (!total) {
    if (e.key === 'Enter') {
      navigateToAllResults()
      e.preventDefault()
    }
    return
  }

  if (e.key === 'ArrowDown') {
    e.preventDefault()
    activeIndex.value = Math.min(activeIndex.value + 1, total - 1)
  } else if (e.key === 'ArrowUp') {
    // PAI-284: clamp at 0 — first row is always selected, no deselect.
    e.preventDefault()
    activeIndex.value = Math.max(activeIndex.value - 1, 0)
  } else if (e.key === 'Enter') {
    e.preventDefault()
    // PAI-284: ⌘↵ / Ctrl↵ = "see all results" (full search page).
    // Plain ↵ = open the highlighted row (always set when items > 0).
    if (e.metaKey || e.ctrlKey) {
      navigateToAllResults()
    } else if (activeOption.value?.kind === 'issue') {
      navigateToIssue(activeOption.value.issue)
    } else if (activeOption.value?.kind === 'all') {
      navigateToAllResults()
    } else {
      // Defensive fallback — shouldn't fire since results watcher pins
      // activeIndex >= 0 whenever items > 0.
      navigateToIssue(visibleIssueItems.value[0])
    }
  } else if (e.key === 'Escape') {
    emit('close')
  }
}

// Exposed so AppHeader can call directly from its keydown handler
defineExpose({ handleKeydown: onKeydown })

// PAI-282: track the anchor's bounding rect so the teleported palette
// stays glued to the search input on resize. The header doesn't move
// during page scroll (it sits outside the scrolling .main-content), but
// a window scroll can still happen at the document level on narrow
// viewports, so we re-measure on `scroll` capture too.
const anchorRect = ref({ top: 0, left: 0, width: 0 })

function recomputeAnchor() {
  const el = props.anchor
  if (!el) return
  const r = el.getBoundingClientRect()
  anchorRect.value = { top: r.bottom + 4, left: r.left, width: r.width }
}

watch(() => props.visible, (v) => {
  if (v) {
    nextTick(recomputeAnchor)
  }
})

onMounted(() => {
  window.addEventListener('resize', recomputeAnchor)
  window.addEventListener('scroll', recomputeAnchor, true)
})
onUnmounted(() => {
  window.removeEventListener('resize', recomputeAnchor)
  window.removeEventListener('scroll', recomputeAnchor, true)
})

// Keep the active item in view inside the palette's own scroll container.
// PAI-255: we used to call `el.scrollIntoView({ block: 'nearest' })`, which
// walks the DOM looking for any scrollable ancestor. When the active row
// was already mostly visible, the browser would scroll the *page* (not
// the palette), pushing the AppHeader off-screen on hover. Scoping the
// scroll to `paletteRef` itself guarantees we never affect the page.
watch(activeIndex, () => {
  nextTick(() => {
    const palette = paletteRef.value
    if (!palette) return
    const el = palette.querySelector<HTMLElement>('.sp-item--active')
    if (!el) return
    const elTop = el.offsetTop
    const elBottom = elTop + el.offsetHeight
    const viewTop = palette.scrollTop
    const viewBottom = viewTop + palette.clientHeight
    if (elTop < viewTop) {
      palette.scrollTop = elTop
    } else if (elBottom > viewBottom) {
      palette.scrollTop = elBottom - palette.clientHeight
    }
  })
})
</script>

<template>
  <Teleport to="body">
    <div
      v-if="visible && (loading || (results && items.length > 0))"
      ref="paletteRef"
      class="search-palette"
      :style="{ top: anchorRect.top + 'px', left: anchorRect.left + 'px', width: anchorRect.width + 'px' }"
    >
    <LoadingText v-if="loading && !results" class="sp-loading" label="Searching…" />
    <template v-else-if="results">
      <div class="sp-scope">
        <AppIcon :name="paletteScope === 'project' ? 'folder' : 'globe'" :size="12" />
        <span>{{ paletteScopeLabel }}</span>
      </div>
      <!-- Direct match — rich row -->
      <div
        v-if="directMatch"
        class="sp-item sp-item--direct"
        :class="{ 'sp-item--active': isIssueActive(directMatch) }"
        @mousedown.prevent="navigateToIssue(directMatch)"
        @mouseenter="activeIndex = optionIndexForIssue(directMatch)"
      >
        <div class="sp-item-top">
          <span class="sp-key">{{ directMatch.issue_key }}</span>
          <StatusDot :status="directMatch.status" />
          <span class="sp-status">{{ directMatch.status }}</span>
          <span v-if="directMatch.assignee_username" class="sp-assignee">{{ directMatch.assignee_username }}</span>
        </div>
        <div class="sp-item-title">{{ directMatch.title }}</div>
      </div>

      <!-- Separator if both sections -->
      <div v-if="directMatch && otherItems.length" class="sp-separator" />

      <!-- Other results -->
      <div v-for="item in otherItems" :key="item.id"
        class="sp-item" :class="{ 'sp-item--active': isIssueActive(item) }"
        @mousedown.prevent="navigateToIssue(item)"
        @mouseenter="activeIndex = optionIndexForIssue(item)">
        <span class="sp-key">{{ item.issue_key }}</span>
        <StatusDot :status="item.status" />
        <span class="sp-title-compact">{{ item.title }}</span>
      </div>

      <!-- PAI-284: footer always shows the modifier-Enter affordance when
           the palette has items. Click also navigates, so the row reads as
           the "see all" action. -->
      <div
        v-if="items.length > 0"
        class="sp-more"
        :class="{ 'sp-more--active': isAllResultsActive }"
        @mousedown.prevent="navigateToAllResults"
        @mouseenter="activeIndex = paletteOptions.length - 1"
      >
        <span class="sp-more-text">
          <kbd class="sp-kbd">↵</kbd> open
          <span class="sp-more-sep">·</span>
          <kbd class="sp-kbd">⌘</kbd><kbd class="sp-kbd">↵</kbd>
          {{ results.has_more ? paletteAllResultsLabel : paletteOpenFullLabel }}
        </span>
      </div>
    </template>
    </div>
  </Teleport>
</template>

<style scoped>
/* PAI-282: position is `fixed` (viewport-relative) and supplied via inline
   styles from the anchor's bounding rect — the palette is teleported to
   <body> so it escapes AppHeader's `overflow: hidden` clip. */
.search-palette {
  position: fixed;
  z-index: 9999;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 8px;
  box-shadow: 0 8px 32px rgba(0,0,0,.15);
  max-height: 400px;
  overflow-y: auto;
  padding: .25rem 0;
}
.sp-loading {
  padding: .75rem 1rem;
  font-size: 12px;
  color: var(--text-muted);
}
.sp-scope {
  display: flex;
  align-items: center;
  gap: .35rem;
  padding: .42rem .75rem .35rem;
  color: var(--text-muted);
  font-size: 11px;
  font-weight: 650;
  border-bottom: 1px solid color-mix(in srgb, var(--border) 74%, transparent);
  margin-bottom: .15rem;
}
.sp-item {
  display: flex;
  align-items: center;
  gap: .4rem;
  padding: .45rem .75rem;
  cursor: pointer;
  font-size: 13px;
  transition: background .08s;
}
.sp-item:hover,
.sp-item--active,
.sp-more:hover,
.sp-more--active {
  background: color-mix(in srgb, var(--bp-blue) 9%, var(--bg-card));
}
.sp-item--active {
  box-shadow: inset 3px 0 0 var(--bp-blue);
}
.sp-item--direct {
  flex-direction: column;
  align-items: stretch;
  gap: .2rem;
  padding: .6rem .75rem;
}
.sp-item-top {
  display: flex;
  align-items: center;
  gap: .4rem;
}
.sp-item-title {
  font-size: 14px;
  font-weight: 500;
  color: var(--text);
  line-height: 1.35;
}
.sp-key {
  font-family: monospace;
  font-size: 12px;
  font-weight: 700;
  color: var(--bp-blue);
  white-space: nowrap;
  flex-shrink: 0;
}
.sp-status {
  font-size: 11px;
  color: var(--text-muted);
  white-space: nowrap;
}
.sp-assignee {
  font-size: 11px;
  color: var(--text-muted);
  margin-left: auto;
}
.sp-title-compact {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--text);
}
.sp-separator {
  height: 1px;
  background: var(--border);
  margin: .2rem .75rem;
}
.sp-more {
  padding: .4rem .75rem;
  text-align: center;
  cursor: pointer;
  border-top: 1px solid var(--border);
}
.sp-more--active {
  box-shadow: inset 3px 0 0 var(--bp-blue);
}
.sp-more-text {
  font-size: 11px;
  color: var(--text-muted);
  display: inline-flex;
  align-items: center;
  gap: .25rem;
}
.sp-kbd {
  font-family: inherit;
  font-size: 10.5px;
  padding: 1px 5px;
  border: 1px solid var(--border);
  border-radius: 4px;
  background: var(--bg);
  color: var(--text);
  line-height: 1.2;
}
.sp-more-sep {
  margin: 0 .25rem;
  opacity: .5;
}
</style>
