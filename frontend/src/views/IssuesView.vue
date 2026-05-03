<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, watch, nextTick } from 'vue'
import IssueList from '@/components/IssueList.vue'
import AppIcon from '@/components/AppIcon.vue'
import { api, errMsg } from '@/api/client'
import { useSearchStore } from '@/stores/search'
import { provideIssueContext } from '@/composables/useIssueContext'
import { useFreshness } from '@/composables/useFreshness'
import { useIssueRefreshPromptStore } from '@/stores/issueRefreshPrompt'
import type { Issue, Project, Tag, Sprint, User, SavedView } from '@/types'

interface IssueEnvelope {
  issues: Issue[]
  total: number
  offset: number
  limit: number
}

const search = useSearchStore()
const issueRefreshPrompt = useIssueRefreshPromptStore()

const PAGE = 100

// Browse mode (no ?q)
const issues      = ref<Issue[]>([])
const total       = ref(0)
const loading     = ref(true)
const loadingMore = ref(false)
const error       = ref('')

// Shared
const users     = ref<User[]>([])
const projects  = ref<Project[]>([])
const allTags   = ref<Tag[]>([])
const costUnits = ref<string[]>([])
const releases  = ref<string[]>([])
const sprints   = ref<Sprint[]>([])

provideIssueContext({ users, allTags, costUnits, releases, projects, sprints })

const issueListRef = ref<InstanceType<typeof IssueList> | null>(null)
const issuesPath = computed(() => {
  let url = `/issues?fields=list&limit=${PAGE}&offset=0`
  if (search.query.length >= 2) url += `&q=${encodeURIComponent(search.query)}`
  return url
})

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
  await fetchIssues(PAGE, true)
  nextTick(() => issueListRef.value?.applyView(view))
}

// ── Computed ──────────────────────────────────────────────────────────────────

const isSearchMode = computed(() => search.query.length >= 2)

const remaining = computed(() => Math.max(0, total.value - issues.value.length))
const hasMore   = computed(() => !isSearchMode.value && remaining.value > 0)

const showEmptyFilterBanner = computed(() => {
  if (!issueListRef.value) return false
  const fc = issueListRef.value.activeFilterCount
  const fl = issueListRef.value.filteredIssues?.length ?? 0
  return fl === 0 && fc > 0 && hasMore.value
})

function currentMainScroll(): HTMLElement | null {
  return document.querySelector('.main-content')
}

function applyFreshIssues(envelope: IssueEnvelope) {
  const scroller = currentMainScroll()
  const top = scroller?.scrollTop ?? 0
  issues.value = envelope.issues
  total.value = envelope.total
  void nextTick(() => {
    if (scroller) scroller.scrollTop = top
  })
}

const freshness = useFreshness<IssueEnvelope>(issuesPath, {
  apply: applyFreshIssues,
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
  try {
    let url = `/issues?fields=list&limit=${limit}&offset=${offset}`
    if (search.query.length >= 2) url += `&q=${encodeURIComponent(search.query)}`
    const data = await api.get<IssueEnvelope>(url)
    if (replace) {
      issues.value = data.issues
    } else {
      issues.value = [...issues.value, ...data.issues]
    }
    total.value = data.total
    if (replace && offset === 0) {
      await freshness.prime(data)
    }
  } catch (e: unknown) {
    error.value = errMsg(e, 'Failed to load issues.')
  }
}

async function load() {
  loading.value = true
  error.value = ''
  await Promise.all([fetchIssues(PAGE, true), fetchMeta()])
  loading.value = false
  // Apply first tab
  if (displayTabs.value.length && activeTabId.value == null) {
    activeTabId.value = displayTabs.value[0].id
    nextTick(() => issueListRef.value?.applyView(displayTabs.value[0]))
  }
}

async function loadMore(n: number) {
  loadingMore.value = true
  await fetchIssues(n)
  loadingMore.value = false
}

async function loadAll() {
  loadingMore.value = true
  await fetchIssues(remaining.value)
  loadingMore.value = false
}

// Re-fetch when search query changes (search-as-filter overlay)
watch(() => search.query, () => { fetchIssues(PAGE, true) })

// ── Infinite scroll sentinel ──────────────────────────────────────────────────
const scrollSentinel = ref<HTMLElement | null>(null)
let scrollObserver: IntersectionObserver | null = null

onMounted(async () => {
  await load()
  nextTick(() => {
    if (scrollSentinel.value) {
      scrollObserver = new IntersectionObserver((entries) => {
        if (entries[0]?.isIntersecting && hasMore.value && !loadingMore.value) {
          loadMore(PAGE)
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

function onCreated(issue: Issue) { issues.value.push(issue); total.value++ }
function onUpdated(issue: Issue) {
  const idx = issues.value.findIndex(i => i.id === issue.id)
  if (idx >= 0) issues.value[idx] = issue
}
function onDeleted(id: number) {
  issues.value = issues.value.filter(i => i.id !== id)
  total.value = Math.max(0, total.value - 1)
}
</script>

<template>
  <div class="issues-view-root">
    <Teleport defer to="#app-header-left">
      <span class="ah-title">Issues</span>
      <span v-if="!loading && isSearchMode" class="ah-subtitle">{{ issues.length.toLocaleString() }} matching "{{ search.query }}"</span>
      <span v-else-if="!loading" class="ah-subtitle">
        {{ issues.length.toLocaleString() }} issues
        <button v-if="hasMore" class="load-all-link" :disabled="loadingMore" @click="loadAll">
          · Load all {{ total.toLocaleString() }}
        </button>
      </span>
    </Teleport>

    <div v-if="loading" class="loading">Loading…</div>
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
        :issues="issues"
        @created="onCreated"
        @updated="onUpdated"
        @deleted="onDeleted"
      />

      <div v-if="!isSearchMode && showEmptyFilterBanner" class="empty-filter-banner">
        No matches in the loaded issues —
        <button class="banner-load-btn" :disabled="loadingMore" @click="loadAll">load all</button>
        to search everything.
      </div>

      <!-- Infinite scroll sentinel -->
      <div ref="scrollSentinel" class="scroll-sentinel">
        <span v-if="loadingMore" class="scroll-loading">Loading more…</span>
      </div>
    </template>
  </div>
</template>

<style scoped>
/* PAI-274: participate in AppLayout's `.view-body--self-scroll` flex chain
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
}
.load-all-link:hover { text-decoration: underline; }
.load-all-link:disabled { opacity: .5; cursor: not-allowed; }

.scroll-sentinel { min-height: 1px; margin-top: .5rem; }
.scroll-loading { font-size: 13px; color: var(--text-muted); display: block; text-align: center; padding: 1rem 0; }
</style>
