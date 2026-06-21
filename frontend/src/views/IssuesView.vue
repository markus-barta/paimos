<script setup lang="ts">
import LoadingText from "@/components/LoadingText.vue";
import { ref, computed, onMounted, onUnmounted, watch, nextTick } from 'vue'
import IssueList from '@/components/IssueList.vue'
import AppIcon from '@/components/AppIcon.vue'
import { api, errMsg } from '@/api/client'
import { useIssueQuery } from '@/composables/useIssueQuery'
import { createInternalFetcher, controllerFreshnessPath } from '@/composables/issueQueryFetchers'
import { isIssueListV2 } from '@/config/featureFlags'
import { useSearchStore } from '@/stores/search'
import { provideIssueContext } from '@/composables/useIssueContext'
import { useFreshness } from '@/composables/useFreshness'
import { useIssueRefreshPromptStore } from '@/stores/issueRefreshPrompt'
import { issueSearchSummary } from '@/utils/issueSearchSummary'
import { formatInteger } from '@/composables/useNumberFormat'
import type { Issue, IssueListEnvelope, Project, Tag, Sprint, User, SavedView } from '@/types'

const search = useSearchStore()
const issueRefreshPrompt = useIssueRefreshPromptStore()

const PAGE = 100

// Browse mode (no ?q)
const issues      = ref<Issue[]>([])
const total       = ref(0)
const issueHasMore = ref(false)
const issueFingerprint = ref('')
const issueSelectionFingerprint = ref('')
const loading     = ref(true)
const loadingMore = ref(false)
const error       = ref('')
const issueWindowMode = ref<'page' | 'all'>('page')

// Shared
const users     = ref<User[]>([])
const projects  = ref<Project[]>([])
const allTags   = ref<Tag[]>([])
const costUnits = ref<string[]>([])
const releases  = ref<string[]>([])
const sprints   = ref<Sprint[]>([])

provideIssueContext({ users, allTags, costUnits, releases, projects, sprints })

// PAI-570: IssueList v2 behind a flag. When on, the controller owns fetch +
// cache + orchestration; the v1 path stays the default until v2 is runtime-
// verified (PAI-575). Display accessors pass through to v1 when the flag is
// off, so default behavior is byte-identical.
const V2 = isIssueListV2()
const ctrl = useIssueQuery<Issue>({
  initial: { mode: 'internal-global' },
  fetcher: createInternalFetcher(),
})
const displayIssues = computed(() => (V2 ? ctrl.issues.value : issues.value))
const displayTotal = computed(() => (V2 ? ctrl.total.value : total.value))
const displayHasMore = computed(() => (V2 ? ctrl.hasMore.value : issueHasMore.value))
const displayLoadingMore = computed(() => (V2 ? ctrl.loading.value : loadingMore.value))
const displayFingerprint = computed(() => (V2 ? ctrl.serverFingerprint.value : issueFingerprint.value))
const displaySelFingerprint = computed(() => (V2 ? ctrl.selectionFingerprint.value : issueSelectionFingerprint.value))

const issueListRef = ref<InstanceType<typeof IssueList> | null>(null)
const trimmedSearchQuery = computed(() => search.query.trim())
const serverFilterQuery = ref('')
const serverSortKey = ref('')
const serverSortDir = ref<'asc' | 'desc'>('asc')
const freshnessLimit = computed(() =>
  issueWindowMode.value === 'all' ? 0 : Math.max(PAGE, issues.value.length || PAGE),
)
const issuesPath = computed(() => {
  const params = new URLSearchParams(serverFilterQuery.value)
  params.set('fields', 'list')
  params.set('limit', String(freshnessLimit.value))
  params.set('offset', '0')
  if (serverSortKey.value) {
    params.set('sort', serverSortKey.value)
    params.set('order', serverSortDir.value)
  }
  if (trimmedSearchQuery.value.length >= 2) params.set('q', trimmedSearchQuery.value)
  return `/issues?${params.toString()}`
})
let issueRequestSeq = 0
let searchReloadTimer: ReturnType<typeof setTimeout> | null = null

// ── Saved view tabs ──────────────────────────────────────────────────────────

const FALLBACK_VIEWS: SavedView[] = [
  {
    id: -200, user_id: 0, owner_username: 'system', title: 'All Issues',
    description: 'All issues across projects.',
    columns_json: '["billing_type","total_budget","rate_hourly","rate_lp","group_state","sprint_state","jira_id","jira_version","jira_text"]',
    filters_json: '{"type":["ticket","task"],"treeView":false}',
    is_shared: true, is_admin_default: true, sort_order: 0, hidden: false, pinned: null, created_at: '', updated_at: '',
  },
  {
    id: -201, user_id: 0, owner_username: 'system', title: 'Epics',
    description: 'Epic planning view.',
    columns_json: '["cost_unit","release","sprint","sprint_state","jira_id","jira_version","jira_text"]',
    filters_json: '{"type":["epic"],"treeView":false}',
    is_shared: true, is_admin_default: true, sort_order: 1, hidden: false, pinned: null, created_at: '', updated_at: '',
  },
]

const allViews    = ref<SavedView[]>([])
const activeTabId = ref<number | null>(null)

const displayTabs = computed(() => {
  const defaults = allViews.value
    .filter(v => v.is_admin_default && (!v.hidden || v.pinned === true) && v.pinned !== false)
    .sort((a, b) => a.sort_order - b.sort_order || a.title.localeCompare(b.title))
  const pinnedPersonal = allViews.value
    .filter(v => !v.is_admin_default && v.pinned === true)
    .sort((a, b) => a.title.localeCompare(b.title))
  const tabs = [...defaults, ...pinnedPersonal]
  return tabs.length ? tabs : FALLBACK_VIEWS
})

async function selectTab(view: SavedView) {
  activeTabId.value = view.id
  // In v2, applyView drives IssueList -> server-filter-change -> controller,
  // which does the fetch. v1 fetches here directly.
  if (!V2) {
    issueWindowMode.value = 'page'
    await fetchIssues(PAGE, true)
  }
  nextTick(() => issueListRef.value?.applyView(view))
}

// ── Computed ──────────────────────────────────────────────────────────────────

const isSearchMode = computed(() => trimmedSearchQuery.value.length >= 2)

const remaining = computed(() => Math.max(0, displayTotal.value - displayIssues.value.length))
const hasMore   = computed(() => displayHasMore.value || remaining.value > 0)
const canAutoLoadMore = computed(() => !isSearchMode.value && hasMore.value)
const searchHasMore = computed(() => isSearchMode.value && hasMore.value)
const searchSubtitle = computed(() =>
  issueSearchSummary(displayIssues.value.length, displayTotal.value, trimmedSearchQuery.value),
)
const browseSubtitle = computed(() =>
  displayTotal.value > displayIssues.value.length
    ? `${formatInteger(displayTotal.value)} issues · ${formatInteger(displayIssues.value.length)} loaded`
    : `${formatInteger(displayTotal.value)} issues`,
)

const showEmptyFilterBanner = computed(() => {
  if (!issueListRef.value) return false
  const fc = issueListRef.value.activeFilterCount
  const fl = issueListRef.value.filteredIssues?.length ?? 0
  return fl === 0 && fc > 0 && !isSearchMode.value && hasMore.value
})

function currentMainScroll(): HTMLElement | null {
  return document.querySelector('.main-content')
}

function applyFreshIssues(envelope: IssueListEnvelope<Issue>) {
  const scroller = currentMainScroll()
  const top = scroller?.scrollTop ?? 0
  issues.value = envelope.issues
  total.value = envelope.total
  issueHasMore.value = envelope.has_more ?? (envelope.total > envelope.issues.length)
  issueFingerprint.value = envelope.fingerprint ?? ''
  issueSelectionFingerprint.value = envelope.selection_fingerprint ?? ''
  void nextTick(() => {
    if (scroller) scroller.scrollTop = top
  })
}

// v2 polls the controller's current window; v1 keeps its own path.
const freshnessPath = computed(() =>
  V2 ? controllerFreshnessPath(ctrl.query, ctrl.loaded.value, PAGE) : issuesPath.value,
)
const freshness = useFreshness<IssueListEnvelope<Issue>>(freshnessPath, {
  apply: (env) => {
    if (V2) ctrl.reconcile(env.issues ?? [], env.total, env.revision)
    else applyFreshIssues(env)
  },
  count: (payload) => payload.total,
})
const freshnessStale = computed(() => freshness.stale.value)
const freshnessCount = computed(() => freshness.newCount.value)

function refreshIssueListFromHeader() {
  freshness.refresh()
}

watch(
  [freshnessStale, freshnessCount],
  ([stale, count]) => {
    if (stale) issueRefreshPrompt.show(count, refreshIssueListFromHeader)
    else issueRefreshPrompt.clear(refreshIssueListFromHeader)
  },
  { immediate: true },
)

// ── Data loading ──────────────────────────────────────────────────────────────

async function fetchMeta() {
  const [u, p, cu, rel, tags, spr, views] = await Promise.all([
    api.get<User[]>('/users'),
    api.get<Project[]>('/projects'),
    api.get<string[]>('/cost-units').catch(() => [] as string[]),
    api.get<string[]>('/releases').catch(() => [] as string[]),
    api.get<Tag[]>('/tags').catch(() => [] as Tag[]),
    api.get<Sprint[]>('/sprints').catch(() => [] as Sprint[]),
    api.get<SavedView[]>('/views').catch(() => [] as SavedView[]),
  ])
  users.value     = u
  projects.value  = p
  costUnits.value = cu
  releases.value  = rel
  allTags.value   = tags
  sprints.value   = spr
  allViews.value  = views
}

async function fetchIssues(limit: number, replace = false) {
  const offset = replace ? 0 : issues.value.length
  const request = ++issueRequestSeq
  try {
    const params = new URLSearchParams(serverFilterQuery.value)
    params.set('fields', 'list')
    params.set('limit', String(limit))
    params.set('offset', String(offset))
    if (serverSortKey.value) {
      params.set('sort', serverSortKey.value)
      params.set('order', serverSortDir.value)
    }
    if (trimmedSearchQuery.value.length >= 2) params.set('q', trimmedSearchQuery.value)
    const url = `/issues?${params.toString()}`
    const data = await api.get<IssueListEnvelope<Issue>>(url)
    if (request !== issueRequestSeq) return
    if (replace) {
      issues.value = data.issues
    } else {
      issues.value = [...issues.value, ...data.issues]
    }
    total.value = data.total
    issueHasMore.value = data.has_more ?? (total.value > issues.value.length)
    issueFingerprint.value = data.fingerprint ?? ''
    issueSelectionFingerprint.value = data.selection_fingerprint ?? ''
    await freshness.prime({
      issues: issues.value,
      total: data.total,
      offset: 0,
      limit: issues.value.length,
      has_more: issueHasMore.value,
      returned: issues.value.length,
      fingerprint: issueFingerprint.value,
      selection_fingerprint: issueSelectionFingerprint.value,
    })
  } catch (e: unknown) {
    if (request !== issueRequestSeq) return
    error.value = errMsg(e, 'Failed to load issues.')
  }
}

async function load() {
  loading.value = true
  error.value = ''
  issueWindowMode.value = 'page'
  await Promise.all([V2 ? ctrl.start() : fetchIssues(PAGE, true), fetchMeta()])
  if (V2) await freshness.prime().catch(() => {}) // baseline the poll; never block load
  loading.value = false
  // Apply first tab
  if (displayTabs.value.length && activeTabId.value == null) {
    activeTabId.value = displayTabs.value[0].id
    nextTick(() => issueListRef.value?.applyView(displayTabs.value[0]))
  }
}

async function loadMore(n: number) {
  if (V2) { await ctrl.loadMore(); return }
  loadingMore.value = true
  await fetchIssues(n)
  loadingMore.value = false
}

async function loadAll() {
  if (V2) { await ctrl.setWindow('all'); return }
  loadingMore.value = true
  issueWindowMode.value = 'all'
  await fetchIssues(0, true)
  loadingMore.value = false
}

function currentWindowLimit() {
  return issueWindowMode.value === 'all' ? 0 : PAGE
}

// Re-fetch when search query changes (search-as-filter overlay).
watch(trimmedSearchQuery, () => {
  if (V2) { ctrl.setSearch(trimmedSearchQuery.value); return }
  if (searchReloadTimer) clearTimeout(searchReloadTimer)
  searchReloadTimer = setTimeout(() => {
    void fetchIssues(currentWindowLimit(), true)
  }, 150)
})

function onServerFilterChange(query: string) {
  if (V2) { void ctrl.setRawFilter(query); return }
  if (serverFilterQuery.value === query) return
  serverFilterQuery.value = query
  void fetchIssues(currentWindowLimit(), true)
}

function onServerSortChange(key: string, dir: 'asc' | 'desc') {
  if (V2) { void ctrl.setSort(key, dir); return }
  if (serverSortKey.value === key && serverSortDir.value === dir) return
  serverSortKey.value = key
  serverSortDir.value = dir
  void fetchIssues(currentWindowLimit(), true)
}

// ── Infinite scroll sentinel ──────────────────────────────────────────────────
const scrollSentinel = ref<HTMLElement | null>(null)
let scrollObserver: IntersectionObserver | null = null

onMounted(async () => {
  await load()
  nextTick(() => {
    if (scrollSentinel.value) {
      scrollObserver = new IntersectionObserver((entries) => {
        if (entries[0]?.isIntersecting && canAutoLoadMore.value && !displayLoadingMore.value) {
          loadMore(PAGE)
        }
      }, { rootMargin: '200px' })
      scrollObserver.observe(scrollSentinel.value)
    }
  })
})

onUnmounted(() => {
  scrollObserver?.disconnect()
  if (searchReloadTimer) clearTimeout(searchReloadTimer)
  issueRefreshPrompt.clear(refreshIssueListFromHeader)
})

// ── Issue list mutations (browse mode only) ───────────────────────────────────

function onCreated(issue: Issue) {
  if (V2) { void ctrl.refresh(); return }
  issues.value.push(issue); total.value++
}
function onUpdated(issue: Issue) {
  if (V2) { ctrl.confirmMutation(issue.id, issue); return }
  const idx = issues.value.findIndex(i => i.id === issue.id)
  if (idx >= 0) issues.value[idx] = issue
}
function onDeleted(id: number) {
  if (V2) { void ctrl.refresh(); return }
  issues.value = issues.value.filter(i => i.id !== id)
  total.value = Math.max(0, total.value - 1)
}
</script>

<template>
  <div class="issues-view-root">
    <Teleport defer to="#app-header-left">
      <span class="ah-title">Issues</span>
      <template v-if="!loading && isSearchMode">
        <span class="ah-subtitle">{{ searchSubtitle }}</span>
        <button
          v-if="searchHasMore"
          class="load-all-link"
          :disabled="displayLoadingMore"
          @click="loadAll"
        >
          · Load all {{ formatInteger(displayTotal) }}
        </button>
      </template>
      <span v-else-if="!loading" class="ah-subtitle">
        {{ browseSubtitle }}
        <button v-if="hasMore" class="load-all-link" :disabled="displayLoadingMore" @click="loadAll">
          · Load all {{ formatInteger(displayTotal) }}
        </button>
      </span>
    </Teleport>

    <LoadingText v-if="loading" class="loading" label="Loading…" />
    <div v-else-if="error" class="load-error">{{ error }}</div>

    <template v-else>
      <!-- Tabs -->
      <div class="view-tabs">
        <button
          v-for="v in displayTabs"
          :key="v.id"
          class="tab-btn"
          :class="{ active: activeTabId === v.id }"
          :data-label="v.title"
          @click="selectTab(v)"
        >
          {{ v.title }}
          <AppIcon v-if="activeTabId === v.id" name="refresh-cw" :size="11" class="tab-refresh-icon" />
        </button>
      </div>

      <IssueList
        ref="issueListRef"
        :issues="displayIssues"
        :result-total="displayTotal"
        :result-has-more="displayHasMore"
        :result-fingerprint="displayFingerprint"
        :selection-fingerprint="displaySelFingerprint"
        :loading-more="displayLoadingMore"
        :url-sync-selection="true"
        @load-all="loadAll"
        @created="onCreated"
        @updated="onUpdated"
        @deleted="onDeleted"
        @server-filter-change="onServerFilterChange"
        @server-sort-change="onServerSortChange"
      />

      <div v-if="!isSearchMode && showEmptyFilterBanner" class="empty-filter-banner">
        No matches in the loaded issues —
        <button class="banner-load-btn" :disabled="displayLoadingMore" @click="loadAll">load all</button>
        to search everything.
      </div>

      <!-- Infinite scroll sentinel -->
      <div ref="scrollSentinel" class="scroll-sentinel">
        <LoadingText v-if="loadingMore" as="span" class="scroll-loading" label="Loading more…" />
      </div>
    </template>
  </div>
</template>

<style scoped>
/* PAI-274 / PAI-361: participate in AppLayout's `.main-content--self-scroll` flex chain
   so IssueList's table-wrap (flex:1; min-height:0; overflow:auto) actually
   has a bounded scrolling viewport — restoring sticky thead + frozen
   columns. The `<template v-else>` fragment below collapses two flex
   children (.view-tabs + IssueList) into the column; that's intentional. */
.issues-view-root {
  flex: 1;
  min-height: 0;
  min-width: 0;
  display: flex;
  flex-direction: column;
}

.loading, .load-error, .no-results {
  color: var(--text-muted); padding: 2rem 0; font-size: 13px;
}
.load-error { color: #c0392b; }

.view-tabs {
  display: flex; gap: 0; margin-bottom: .75rem;
  border-bottom: 2px solid var(--border);
}
.tab-btn {
  position: relative;
  display: inline-flex; align-items: center;
  background: none; border: none; cursor: pointer;
  padding: .45rem .75rem; font-size: 13px; font-weight: 500;
  color: var(--text-muted); border-bottom: 2px solid transparent;
  margin-bottom: -2px; transition: color .15s, border-color .15s;
  font-family: inherit;
}
.tab-btn::after {
  content: attr(data-label);
  font-weight: 600;
  visibility: hidden;
  height: 0;
  display: block;
  overflow: hidden;
}
.tab-btn:hover { color: var(--text); }
.tab-btn.active { color: var(--bp-blue); border-bottom-color: var(--bp-blue); font-weight: 600; }
.tab-refresh-icon { position: absolute; right: 2px; top: 50%; transform: translateY(-50%); opacity: .35; transition: opacity .15s; pointer-events: none; }
.tab-btn:hover .tab-refresh-icon { opacity: .7; }

.empty-filter-banner {
  margin-top: .75rem; padding: .65rem 1rem;
  background: color-mix(in srgb, var(--bp-blue) 8%, var(--bg-card));
  border: 1px solid color-mix(in srgb, var(--bp-blue) 25%, transparent);
  border-radius: 8px; font-size: 13px; color: var(--text);
}
.banner-load-btn {
  background: none; border: none; padding: 0; cursor: pointer;
  font-size: 13px; color: var(--bp-blue); font-weight: 600; font-family: inherit;
}
.banner-load-btn:hover { text-decoration: underline; }
.banner-load-btn:disabled { opacity: .5; cursor: not-allowed; }

.load-all-link {
  background: none; border: none; padding: 0; cursor: pointer;
  font: inherit; font-size: 13px; color: var(--bp-blue); font-weight: 500;
  white-space: nowrap; flex-shrink: 0;
}
.load-all-link:hover { text-decoration: underline; }
.load-all-link:disabled { opacity: .5; cursor: not-allowed; }

.scroll-sentinel { min-height: 1px; margin-top: .5rem; }
.scroll-loading { font-size: 13px; color: var(--text-muted); display: block; text-align: center; padding: 1rem 0; }
</style>
