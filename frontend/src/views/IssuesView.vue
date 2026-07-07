<script setup lang="ts">
import LoadingText from "@/components/LoadingText.vue";
import { ref, computed, onMounted, onUnmounted, watch, nextTick } from 'vue'
import IssueList from '@/components/IssueList.vue'
import AppIcon from '@/components/AppIcon.vue'
import { api } from '@/api/client'
import { useIssueQuery } from '@/composables/useIssueQuery'
import { createInternalFetcher, controllerFreshnessPath } from '@/composables/issueQueryFetchers'
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
const ISSUE_LIST_CHANGE_SUBJECTS = new Set(['issue', 'issue_tag', 'comment', 'time_entry'])

const loading     = ref(true)
const error       = ref('')

// Shared
const users     = ref<User[]>([])
const projects  = ref<Project[]>([])
const allTags   = ref<Tag[]>([])
const costUnits = ref<string[]>([])
const releases  = ref<string[]>([])
const sprints   = ref<Sprint[]>([])

provideIssueContext({ users, allTags, costUnits, releases, projects, sprints })

// PAI-570/575: the IssueList controller owns fetch + cache + orchestration.
// The display accessors expose its refs to the template unwrapped.
const ctrl = useIssueQuery<Issue>({
  initial: { mode: 'internal-global' },
  fetcher: createInternalFetcher(),
})
const displayIssues = computed(() => ctrl.issues.value)
const displayTotal = computed(() => ctrl.total.value)
const displayHasMore = computed(() => ctrl.hasMore.value)
const displayLoadingMore = computed(() => ctrl.loading.value)
const displayFingerprint = computed(() => ctrl.serverFingerprint.value)
const displaySelFingerprint = computed(() => ctrl.selectionFingerprint.value)

const issueListRef = ref<InstanceType<typeof IssueList> | null>(null)
const trimmedSearchQuery = computed(() => search.query.trim())

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
  // applyView drives IssueList -> server-filter-change -> the controller fetch.
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

// The controller polls its current window for freshness.
const freshnessPath = computed(() =>
  controllerFreshnessPath(ctrl.query, ctrl.loaded.value, PAGE),
)
const freshness = useFreshness<IssueListEnvelope<Issue>>(freshnessPath, {
  apply: (env) => ctrl.reconcile(env.issues ?? [], env.total, env.revision),
  count: (payload) => payload.total,
  changes: (event) =>
    event.project_id != null && ISSUE_LIST_CHANGE_SUBJECTS.has(event.subject_type),
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

async function load() {
  loading.value = true
  error.value = ''
  await Promise.all([ctrl.start(), fetchMeta()])
  await freshness.prime().catch(() => {}) // baseline the poll; never block load
  loading.value = false
  // Apply first tab
  if (displayTabs.value.length && activeTabId.value == null) {
    activeTabId.value = displayTabs.value[0].id
    nextTick(() => issueListRef.value?.applyView(displayTabs.value[0]))
  }
}

async function loadMore() {
  await ctrl.loadMore()
}

async function loadAll() {
  await ctrl.setWindow('all')
}

// Re-fetch when search query changes (search-as-filter overlay).
watch(trimmedSearchQuery, () => {
  ctrl.setSearch(trimmedSearchQuery.value)
})

function onServerFilterChange(query: string) {
  void ctrl.setRawFilter(query)
}

function onServerSortChange(key: string, dir: 'asc' | 'desc') {
  void ctrl.setSort(key, dir)
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
          loadMore()
        }
      }, { rootMargin: '200px' })
      scrollObserver.observe(scrollSentinel.value)
    }
  })
})

onUnmounted(() => {
  scrollObserver?.disconnect()
  issueRefreshPrompt.clear(refreshIssueListFromHeader)
})

// ── Issue list mutations (browse mode only) ───────────────────────────────────

function onCreated() {
  void ctrl.refresh()
}
function onUpdated(issue: Issue) {
  ctrl.confirmMutation(issue.id, issue)
}
function onDeleted() {
  void ctrl.refresh()
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
        <LoadingText v-if="displayLoadingMore" as="span" class="scroll-loading" label="Loading more…" />
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
