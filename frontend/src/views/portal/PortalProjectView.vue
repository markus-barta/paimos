<!--
  PAI-470 / PAI-476 — customer-portal project detail view, rebuilt on top
  of the shared IssueList table target.

  Layout (matches /tmp/paimos-portal-projectview-v2.html):
    1. Crumb row              ← All Projects
    2. Project header card    logo · key chip · name · tagline · [+ New Request]
    3. KPI stat bar           Total · Backlog · In Progress · Done · Awaiting
    4. Filter card            customer filters → tab strip → IssueList
       Action column          Accept + Reject for delivered/done items when
                              user is editor; locked indicator for invoiced;
                              empty otherwise.

  Tab counts are derived from the active-filter-result set (so "All 12"
  means "12 visible after current filters"). Tabs apply an additional
  status constraint on top of the filter bar.

  URL state — filter params status[]/type[]/priority[]/tag_ids[]/q/sort/
  order — round-trips through the query string so bookmarks and refresh
  preserve the working set.
-->
<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'

import { api, errMsg } from '@/api/client'
import { useBranding } from '@/composables/useBranding'
import IssueList from '@/components/IssueList.vue'
import { useIssueQuery } from '@/composables/useIssueQuery'
import { createPortalFetcher } from '@/composables/issueQueryFetchers'
import { provideIssueContext } from '@/composables/useIssueContext'
import { formatInteger } from '@/composables/useNumberFormat'
import type { Issue, Project, Sprint, Tag, User } from '@/types'

interface PortalProject {
  id: number
  key: string
  name: string
  description: string
  status: string
  logo_path: string
  issue_count: number
  done_count: number
}
// PAI-474: mirrors the cleaned portalIssue Go struct (no cost / effort
// fields ever cross the wire on this path). Tag display is also gone —
// the customer should not see internal taxonomy plumbing, including the
// CUSTOMERPORTAL marker itself.
interface PortalIssue {
  id: number
  issue_key: string
  title: string
  status: string
  priority: string
  type: string
  description?: string
  acceptance_criteria?: string
  report_summary?: string
  accepted_at: string | null
  created_at: string
  updated_at: string
}

const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const { branding } = useBranding()

const projectId = Number(route.params.id)

const project = ref<PortalProject | null>(null)
const issues = ref<PortalIssue[]>([])
const totalIssues = ref(0)
const portalIssueHasMore = ref(false)
const loading = ref(true)
const loadingMore = ref(false)

provideIssueContext({
  users: ref<User[]>([]),
  allTags: ref<Tag[]>([]),
  costUnits: ref<string[]>([]),
  releases: ref<string[]>([]),
  projects: ref<Project[]>([]),
  sprints: ref<Sprint[]>([]),
})

// PAI-570: portal on the shared core. useIssueQuery (portal mode) owns fetch +
// orchestration; a mirror watcher syncs its outputs into the existing refs so
// the table, tabs, and client-side search are unchanged.
const portalCtrl = useIssueQuery<PortalIssue>({
  initial: { mode: 'portal', projectId },
  fetcher: createPortalFetcher<PortalIssue>(),
})
watch(
  () => [
    portalCtrl.issues.value, portalCtrl.total.value, portalCtrl.hasMore.value,
    portalCtrl.loading.value, portalCtrl.serverFingerprint.value, portalCtrl.selectionFingerprint.value,
  ],
  () => {
    issues.value = portalCtrl.issues.value
    totalIssues.value = portalCtrl.total.value
    portalIssueHasMore.value = portalCtrl.hasMore.value
    loadingMore.value = portalCtrl.loading.value
  },
)
function copyFiltersToCtrl() {
  portalCtrl.query.projectId = projectId
  portalCtrl.query.filters.status = [...filters.value.status]
  portalCtrl.query.filters.type = [...filters.value.type]
  portalCtrl.query.filters.priority = [...filters.value.priority]
  portalCtrl.query.filters.tags = filters.value.tagIds.map(String)
  portalCtrl.query.search = filters.value.q
}
function applyPortalQueryToCtrl() { copyFiltersToCtrl(); void portalCtrl.reload() }

// Filter state owned by this view; mirrored to the URL on every change.
const filters = ref({
  status: [],
  type: [],
  priority: [],
  tagIds: [],
  q: '',
} as { status: string[]; type: string[]; priority: string[]; tagIds: number[]; q: string })

const statusFilter = computed({
  get: () => filters.value.status[0] ?? '',
  set: (value: string) => { filters.value.status = value ? [value] : [] },
})
const typeFilter = computed({
  get: () => filters.value.type[0] ?? '',
  set: (value: string) => { filters.value.type = value ? [value] : [] },
})
const priorityFilter = computed({
  get: () => filters.value.priority[0] ?? '',
  set: (value: string) => { filters.value.priority = value ? [value] : [] },
})

type Tab = 'all' | 'open' | 'review' | 'accepted'
const activeTab = ref<Tab>('all')

// New-Request modal state
const showRequestModal = ref(false)
const requestTitle = ref('')
const requestDesc = ref('')
const requestLoading = ref(false)
const requestError = ref('')

// ── Filter options ───────────────────────────────────────────────────────
// PAI-474: dropdown shows the four customer-meaningful buckets, each
// mapped to the dominant internal status. The internal statuses that
// overlap with each bucket (new, qa, delivered, invoiced, cancelled)
// are intentionally dropped from this list — customers reason about
// "Planned / In Progress / Ready for Review / Accepted", not our
// pipeline micro-stages. The full status set still flows into the
// table via the cell renderer, which maps each to the same labels.
const STATUS_OPTIONS = computed(() => [
  { value: 'backlog', label: t('portal.statusLabel.planned') },
  { value: 'in-progress', label: t('portal.statusLabel.inProgress') },
  { value: 'done', label: t('portal.statusLabel.readyForReview') },
  { value: 'accepted', label: t('portal.statusLabel.accepted') },
])

const TYPE_OPTIONS = computed(() => [
  { value: 'ticket', label: t('portal.typeLabel.ticket') },
  { value: 'task', label: t('portal.typeLabel.task') },
  { value: 'bug', label: t('portal.typeLabel.bug') },
])

const PRIORITY_OPTIONS = [
  { value: 'low', label: 'Low' },
  { value: 'medium', label: 'Medium' },
  { value: 'high', label: 'High' },
]

// PAI-474: tagOptions removed — customer never sees internal tags, so
// no tag filter exists in the portal bar (see enabledFilters above).

// ── Data fetching ────────────────────────────────────────────────────────
async function loadAll() {
  loading.value = true
  try {
    copyFiltersToCtrl()
    const [p] = await Promise.all([
      api.get<PortalProject>(`/portal/projects/${projectId}`),
      portalCtrl.start(),
    ])
    project.value = p
  } catch {
    /* error surfaces in UI as the loading-then-empty state */
  } finally {
    loading.value = false
  }
}

// URL state round-trip — restore on mount, mirror on every change.
function readUrlState() {
  const q = route.query
  if (typeof q.status === 'string' && q.status) filters.value.status = q.status.split(',')
  if (typeof q.type === 'string' && q.type) filters.value.type = q.type.split(',')
  if (typeof q.priority === 'string' && q.priority) filters.value.priority = q.priority.split(',')
  if (typeof q.tag_ids === 'string' && q.tag_ids) {
    filters.value.tagIds = q.tag_ids.split(',').map((s) => Number(s)).filter((n) => !Number.isNaN(n))
  }
  if (typeof q.q === 'string') filters.value.q = q.q
  if (typeof q.tab === 'string' && ['all', 'open', 'review', 'accepted'].includes(q.tab)) {
    activeTab.value = q.tab as Tab
  }
}

function writeUrlState() {
  const query: Record<string, string> = {}
  if (filters.value.status.length) query.status = filters.value.status.join(',')
  if (filters.value.type.length) query.type = filters.value.type.join(',')
  if (filters.value.priority.length) query.priority = filters.value.priority.join(',')
  if (filters.value.tagIds.length) query.tag_ids = filters.value.tagIds.join(',')
  if (filters.value.q) query.q = filters.value.q
  if (activeTab.value !== 'all') query.tab = activeTab.value
  void router.replace({ query })
}

watch(filters, () => {
  writeUrlState()
  applyPortalQueryToCtrl()
}, { deep: true })

watch(activeTab, writeUrlState)

onMounted(() => {
  readUrlState()
  void loadAll()
})

// ── Tabs + tab-scoped filtering ─────────────────────────────────────────
// `tabBoundIssues` applies the tab's status constraint on top of the
// backend-filtered set. Tab counts come from this same source — "All
// 12" means "12 visible after every active filter".

const TAB_STATUSES: Record<Tab, string[] | null> = {
  all: null,
  open: ['new', 'backlog', 'in-progress', 'qa'],
  review: ['done', 'delivered'],
  accepted: ['accepted', 'invoiced'],
}

function inTab(issue: PortalIssue, tab: Tab): boolean {
  const allowed = TAB_STATUSES[tab]
  if (!allowed) return true
  return allowed.includes(issue.status)
}

// PAI-474: client-side search filter, applied on top of the
// backend-filtered set. The backend FTS5 endpoint requires whole-token
// matches and ≥2 chars, which breaks the natural "type a partial title
// and see matches" expectation. The substring check here matches title
// OR issue key — case-insensitive — so 1-char searches and partial
// words just work. The backend filter remains for deep-history search
// (description text); UI search is the common-case fast path.
function matchesSearch(issue: PortalIssue, q: string): boolean {
  if (!q) return true
  const needle = q.toLowerCase()
  return (
    issue.title.toLowerCase().includes(needle) ||
    issue.issue_key.toLowerCase().includes(needle)
  )
}

const searchedIssues = computed(() =>
  issues.value.filter((iss) => matchesSearch(iss, filters.value.q.trim())),
)

const tabBoundIssues = computed(() =>
  searchedIssues.value.filter((iss) => inTab(iss, activeTab.value)),
)

const tabCounts = computed(() => ({
  all: searchedIssues.value.length,
  open: searchedIssues.value.filter((i) => inTab(i, 'open')).length,
  review: searchedIssues.value.filter((i) => inTab(i, 'review')).length,
  accepted: searchedIssues.value.filter((i) => inTab(i, 'accepted')).length,
}))

const portalHasMore = computed(() => portalIssueHasMore.value || totalIssues.value > issues.value.length)

async function loadMoreIssues() {
  if (loadingMore.value || !portalHasMore.value) return
  await portalCtrl.setWindow('all')
}

// ── KPI stat bar ────────────────────────────────────────────────────────
// KPIs reflect the post-search/post-filter set so the strip always
// answers "how does my visible workload break down right now?" rather
// than reading like a separate project-wide totals row that wouldn't
// match what the table shows below it.
const kpis = computed(() => {
  const set = searchedIssues.value
  const total = set.length
  const backlog = set.filter(
    (i) => i.status === 'new' || i.status === 'backlog',
  ).length
  const inProgress = set.filter(
    (i) => i.status === 'in-progress' || i.status === 'qa',
  ).length
  const done = set.filter(
    (i) => i.status === 'done' || i.status === 'accepted' || i.status === 'invoiced',
  ).length
  const awaiting = set.filter(
    (i) => i.status === 'delivered' || i.status === 'done',
  ).length
  return { total, backlog, inProgress, done, awaiting }
})

// PAI-474: customer-friendly status labels. Maps the internal status
// enum to four buckets a customer can reason about — "Planned",
// "In Progress", "Ready for Review", "Accepted". The translation
// strings live under portal.statusLabel.* (en + de).
function portalStatusLabel(status: string): string {
  switch (status) {
    case 'new':
    case 'backlog':
      return t('portal.statusLabel.planned')
    case 'in-progress':
    case 'qa':
      return t('portal.statusLabel.inProgress')
    case 'done':
    case 'delivered':
      return t('portal.statusLabel.readyForReview')
    case 'accepted':
    case 'invoiced':
      return t('portal.statusLabel.accepted')
    default:
      return t('status.' + status)
  }
}

// PAI-474: customer-friendly type labels. Internal `ticket` reads as
// "Request" to a customer, `bug` reads as "Issue", and so on.
function portalTypeLabel(type: string): string {
  const key = `portal.typeLabel.${type}`
  const translated = t(key)
  // vue-i18n returns the key path when missing — fall back to capitalised type.
  if (translated === key) return type.charAt(0).toUpperCase() + type.slice(1)
  return translated
}

const portalStatusLabels = computed<Record<string, string>>(() =>
  Object.fromEntries(['new', 'backlog', 'in-progress', 'qa', 'done', 'delivered', 'accepted', 'invoiced', 'cancelled']
    .map((status) => [status, portalStatusLabel(status)])),
)

const portalTypeLabels = computed<Record<string, string>>(() =>
  Object.fromEntries(['ticket', 'task', 'bug', 'epic', 'cost_unit', 'release', 'sprint']
    .map((type) => [type, portalTypeLabel(type)])),
)

function issueNumberFromKey(key: string): number {
  const raw = key.split('-').pop()
  const n = raw ? Number(raw) : NaN
  return Number.isInteger(n) && n > 0 ? n : 0
}

function portalIssueToIssue(issue: PortalIssue): Issue {
  return {
    id: issue.id,
    project_id: projectId,
    issue_number: issueNumberFromKey(issue.issue_key),
    issue_key: issue.issue_key,
    type: issue.type as Issue['type'],
    parent_id: null,
    title: issue.title,
    description: issue.description ?? '',
    acceptance_criteria: issue.acceptance_criteria ?? '',
    notes: '',
    report_summary: issue.report_summary ?? '',
    status: issue.status as Issue['status'],
    priority: issue.priority as Issue['priority'],
    cost_unit: null,
    release: null,
    billing_type: null,
    total_budget: null,
    rate_hourly: null,
    rate_lp: null,
    estimate_hours: null,
    estimate_lp: null,
    ar_hours: null,
    ar_lp: null,
    time_override: null,
    start_date: null,
    end_date: null,
    group_state: null,
    sprint_state: null,
    jira_id: null,
    jira_version: null,
    jira_text: null,
    color: null,
    sprint_ids: [],
    archived: false,
    assignee_id: null,
    assignee: null,
    tags: [],
    created_at: issue.created_at,
    updated_at: issue.updated_at,
    created_by: null,
    created_by_name: '',
    last_changed_by_name: '',
    booked_hours: 0,
    time_logged: 0,
    time_rollup: 0,
    time_total: 0,
    accepted_at: issue.accepted_at,
    accepted_by: null,
    invoiced_at: null,
    invoice_number: '',
    ai_work_status: null,
  }
}

const tabBoundIssueRows = computed<Issue[]>(() => tabBoundIssues.value.map(portalIssueToIssue))

// After accept / reject, refresh the list so the row's status flips
// and the tab counts / KPIs update. The panel reloads its own copy
// internally so the in-place pill update is already done.
async function onIssueAccepted(_id: number) {
  await portalCtrl.refresh()
}
async function onIssueRejected(_id: number) {
  await portalCtrl.refresh()
}

// ── New Request modal ───────────────────────────────────────────────────
async function submitRequest() {
  if (!requestTitle.value.trim()) return
  requestLoading.value = true
  requestError.value = ''
  try {
    await api.post(`/portal/projects/${projectId}/requests`, {
      title: requestTitle.value.trim(),
      description: requestDesc.value.trim(),
    })
    showRequestModal.value = false
    requestTitle.value = ''
    requestDesc.value = ''
    await portalCtrl.refresh()
  } catch (e: unknown) {
    requestError.value = errMsg(e, 'Failed')
  } finally {
    requestLoading.value = false
  }
}
</script>

<template>
  <div class="pv">
    <div class="pv__crumb">
      <RouterLink to="/portal">← {{ $t('portal.allProjects') }}</RouterLink>
    </div>

    <header v-if="project" class="pv__header">
      <div class="pv__header-left">
        <img
          v-if="project.logo_path"
          :src="project.logo_path"
          alt=""
          class="pv__logo"
        />
        <img v-else :src="branding.logo" alt="" class="pv__logo pv__logo--fallback" />
        <div>
          <div class="pv__key">{{ project.key }}</div>
          <h1 class="pv__name">{{ project.name }}</h1>
          <p v-if="project.description" class="pv__desc">{{ project.description }}</p>
        </div>
      </div>
      <button
        class="pv__new-btn"
        type="button"
        @click="showRequestModal = true"
      >
        {{ $t('portal.newRequest') }}
      </button>
    </header>

    <!-- KPI stat bar -->
    <div class="pv__stats" v-if="project">
      <div class="pv__stat">
        <span class="pv__stat-value">{{ formatInteger(kpis.total) }}</span>
        <span class="pv__stat-label">{{ $t('portal.summary.total') }}</span>
      </div>
      <div class="pv__stat">
        <span class="pv__stat-value">{{ formatInteger(kpis.backlog) }}</span>
        <span class="pv__stat-label">{{ $t('status.backlog') }}</span>
      </div>
      <div class="pv__stat">
        <span class="pv__stat-value">{{ formatInteger(kpis.inProgress) }}</span>
        <span class="pv__stat-label">{{ $t('status.in-progress') }}</span>
      </div>
      <div class="pv__stat">
        <span class="pv__stat-value">{{ formatInteger(kpis.done) }}</span>
        <span class="pv__stat-label">{{ $t('status.done') }}</span>
      </div>
      <div class="pv__stat pv__stat--awaiting">
        <span class="pv__stat-value">{{ formatInteger(kpis.awaiting) }}</span>
        <span class="pv__stat-label">{{ $t('portal.tabs.review') }}</span>
      </div>
    </div>

    <!-- Filter card — PAI-474/476: tag filter stays disabled for customers;
         the shared IssueList renders the table and portal side panel. -->
    <section class="pv__list" v-if="project">
      <div class="pv__filters">
        <input
          v-model="filters.q"
          class="pv__filter-input"
          type="search"
          :placeholder="$t('portal.search')"
        />
        <select v-model="statusFilter" class="pv__filter-select">
          <option value="">{{ $t('portal.filters.allStatus') }}</option>
          <option v-for="option in STATUS_OPTIONS" :key="option.value" :value="option.value">
            {{ option.label }}
          </option>
        </select>
        <select v-model="typeFilter" class="pv__filter-select">
          <option value="">{{ $t('portal.filters.allTypes') }}</option>
          <option v-for="option in TYPE_OPTIONS" :key="option.value" :value="option.value">
            {{ option.label }}
          </option>
        </select>
        <select v-model="priorityFilter" class="pv__filter-select">
          <option value="">{{ $t('portal.filters.allPriorities') }}</option>
          <option v-for="option in PRIORITY_OPTIONS" :key="option.value" :value="option.value">
            {{ option.label }}
          </option>
        </select>
      </div>

      <!-- Tab strip — counts derived from the backend-filtered set -->
      <div class="pv__tabs" role="tablist">
        <button
          v-for="tab in (['all', 'open', 'review', 'accepted'] as Tab[])"
          :key="tab"
          type="button"
          role="tab"
          :class="['pv__tab', { 'pv__tab--active': activeTab === tab }]"
          @click="activeTab = tab"
        >
          {{ $t(`portal.tabs.${tab === 'open' ? 'open' : tab}`) }}
          <span class="pv__tab-count">{{ tabCounts[tab] }}</span>
        </button>
      </div>

      <IssueList
        mode="customer"
        :project-id="projectId"
        :issues="tabBoundIssueRows"
        :result-total="totalIssues"
        :result-has-more="portalIssueHasMore"
        :loading-more="loadingMore"
        :search-query-override="filters.q"
        :status-labels="portalStatusLabels"
        :type-labels="portalTypeLabels"
        url-sync-selection
        @accepted="onIssueAccepted"
        @rejected="onIssueRejected"
      />
      <div
        v-if="portalHasMore"
        class="pv__load-more"
      >
        <span>{{ formatInteger(issues.length) }} / {{ formatInteger(totalIssues) }}</span>
        <button type="button" class="pv__load-more-btn" :disabled="loadingMore" @click="loadMoreIssues">
          {{ loadingMore ? 'Loading...' : 'Load all' }}
        </button>
      </div>
    </section>

    <!-- New-Request modal — minimal inline implementation -->
    <div v-if="showRequestModal" class="pv__modal-backdrop" @click="showRequestModal = false">
      <div class="pv__modal" @click.stop>
        <h2>{{ $t('portal.newRequest') }}</h2>
        <input v-model="requestTitle" class="pv__input" type="text" placeholder="Title" />
        <textarea v-model="requestDesc" class="pv__textarea" rows="4" placeholder="Description" />
        <p v-if="requestError" class="pv__error">{{ requestError }}</p>
        <div class="pv__modal-actions">
          <button type="button" class="pv__btn-ghost" @click="showRequestModal = false">
            Cancel
          </button>
          <button
            type="button"
            class="pv__btn-primary"
            :disabled="!requestTitle.trim() || requestLoading"
            @click="submitRequest"
          >
            Submit
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.pv {
  max-width: 1180px;
  margin: 0 auto;
  padding: 1.5rem 1.25rem 4rem;
}

.pv__crumb {
  margin-bottom: 0.75rem;
  font-size: 0.875rem;
}

.pv__header {
  display: flex;
  align-items: center;
  gap: 1rem;
  padding: 1.25rem;
  background: white;
  border: 1px solid var(--border, #e5e7eb);
  border-radius: 12px;
  margin-bottom: 1rem;
}

.pv__header-left {
  display: flex;
  align-items: center;
  gap: 1rem;
  flex: 1;
  min-width: 0;
}

.pv__logo {
  width: 56px;
  height: 56px;
  border-radius: 12px;
  object-fit: contain;
  background: #f3f4f6;
}
.pv__logo--fallback { opacity: 0.6; }

.pv__key {
  display: inline-block;
  background: color-mix(in srgb, var(--brand, #2563eb) 12%, transparent);
  color: var(--brand, #2563eb);
  font-size: 0.6875rem;
  font-weight: 700;
  letter-spacing: 0.04em;
  padding: 0.15rem 0.5rem;
  border-radius: 999px;
}

.pv__name {
  font-size: 1.25rem;
  font-weight: 700;
  margin: 0.25rem 0 0;
}

.pv__desc {
  margin: 0.25rem 0 0;
  color: var(--text-muted, #6b7280);
  font-size: 0.875rem;
}

.pv__new-btn {
  background: var(--brand, #2563eb);
  color: white;
  border: none;
  font-weight: 600;
  padding: 0.5rem 0.875rem;
  border-radius: 8px;
  cursor: pointer;
  min-height: 40px;
}

.pv__stats {
  display: grid;
  grid-template-columns: repeat(5, minmax(0, 1fr));
  background: white;
  border: 1px solid var(--border, #e5e7eb);
  border-radius: 12px;
  margin-bottom: 1rem;
  overflow: hidden;
}

.pv__stat {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 1rem 0.5rem;
  border-right: 1px solid var(--border, #e5e7eb);
}
.pv__stat:last-child { border-right: none; }

.pv__stat-value {
  font-size: 1.625rem;
  font-weight: 700;
  line-height: 1.1;
}
.pv__stat-label {
  font-size: 0.75rem;
  color: var(--text-muted, #6b7280);
  text-transform: uppercase;
  letter-spacing: 0.04em;
  margin-top: 0.25rem;
}

.pv__stat--awaiting .pv__stat-value {
  color: #059669;
}

.pv__list {
  background: white;
  border: 1px solid var(--border, #e5e7eb);
  border-radius: 12px;
  padding: 1rem 1rem 0.5rem;
}

.pv__filters {
  display: grid;
  grid-template-columns: minmax(220px, 1fr) repeat(3, minmax(140px, 180px));
  gap: 0.75rem;
  align-items: center;
}

.pv__filter-input,
.pv__filter-select {
  min-height: 38px;
  border: 1px solid var(--border, #e5e7eb);
  border-radius: 8px;
  background: white;
  color: var(--text, #1f2937);
  font-size: 0.875rem;
  padding: 0.45rem 0.65rem;
}

.pv__tabs {
  display: flex;
  gap: 0.5rem;
  margin: 1rem 0;
  border-bottom: 1px solid var(--border, #e5e7eb);
}
.pv__tab {
  background: transparent;
  border: none;
  padding: 0.5rem 0.875rem;
  cursor: pointer;
  font-weight: 500;
  color: var(--text-muted, #6b7280);
  position: relative;
  font-size: 0.9rem;
  display: inline-flex;
  align-items: center;
  gap: 0.5rem;
}
.pv__tab--active {
  color: var(--brand, #2563eb);
}
.pv__tab--active::after {
  content: '';
  position: absolute;
  left: 0.5rem;
  right: 0.5rem;
  bottom: -1px;
  height: 2px;
  background: var(--brand, #2563eb);
}
.pv__tab-count {
  background: var(--bg-subtle, #f3f4f6);
  color: var(--text-muted, #6b7280);
  font-size: 0.75rem;
  font-weight: 600;
  padding: 0.05rem 0.4rem;
  border-radius: 999px;
}
.pv__tab--active .pv__tab-count {
  background: color-mix(in srgb, var(--brand, #2563eb) 12%, transparent);
  color: var(--brand, #2563eb);
}

.pv__load-more {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 0.75rem;
  padding: 0.75rem 0 0.25rem;
  color: var(--text-muted, #6b7280);
  font-size: 0.8125rem;
}
.pv__load-more-btn {
  border: 1px solid var(--border, #e5e7eb);
  background: var(--bg-subtle, #f3f4f6);
  color: var(--text, #1f2937);
  border-radius: 6px;
  min-height: 32px;
  padding: 0 0.75rem;
  font-weight: 600;
  cursor: pointer;
}
.pv__load-more-btn:disabled {
  opacity: 0.55;
  cursor: not-allowed;
}

/* Modal */
.pv__modal-backdrop {
  position: fixed;
  inset: 0;
  background: rgba(15, 23, 42, 0.45);
  z-index: 1000;
  display: flex;
  align-items: center;
  justify-content: center;
}
.pv__modal {
  background: white;
  border-radius: 12px;
  padding: 1.5rem;
  width: 90%;
  max-width: 460px;
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
}
.pv__modal h2 { margin: 0; font-size: 1.125rem; font-weight: 700; }
.pv__input,
.pv__textarea {
  border: 1px solid var(--border, #e5e7eb);
  border-radius: 8px;
  padding: 0.5rem 0.625rem;
  font-size: 0.875rem;
  resize: vertical;
}
.pv__error { color: #b91c1c; font-size: 0.875rem; margin: 0; }
.pv__modal-actions {
  display: flex;
  gap: 0.5rem;
  justify-content: flex-end;
}
.pv__btn-ghost,
.pv__btn-primary {
  padding: 0.5rem 0.875rem;
  border-radius: 8px;
  border: none;
  font-weight: 600;
  cursor: pointer;
  min-height: 40px;
}
.pv__btn-ghost { background: var(--bg-subtle, #f3f4f6); color: var(--text, #1f2937); }
.pv__btn-primary { background: var(--brand, #2563eb); color: white; }
.pv__btn-primary:disabled { opacity: 0.55; cursor: not-allowed; }

.pv-status {
  display: inline-flex;
  align-items: center;
  gap: 0.25rem;
}

/* PAI-471: responsive — below 720px the KPI strip wraps to 2 cols, the
   header stacks, and "+ New Request" goes full-width. */
@media (max-width: 720px) {
  .pv__header {
    flex-direction: column;
    align-items: stretch;
  }
  .pv__new-btn {
    width: 100%;
  }
  .pv__stats {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .pv__stat {
    border-right: 1px solid var(--border, #e5e7eb);
    border-bottom: 1px solid var(--border, #e5e7eb);
  }
  .pv__stat:nth-child(2n) {
    border-right: none;
  }
  .pv__stat:nth-last-child(-n + 2) {
    border-bottom: none;
  }
  .pv__filters {
    grid-template-columns: 1fr;
  }
}
</style>
