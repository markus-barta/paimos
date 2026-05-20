<!--
  PAI-470 — customer-portal project detail view, rebuilt on top of the
  PAI-468/469 shared IssueTable + IssueFilterBar.

  Layout (matches /tmp/paimos-portal-projectview-v2.html):
    1. Crumb row              ← All Projects
    2. Project header card    logo · key chip · name · tagline · [+ New Request]
    3. KPI stat bar           Total · Backlog · In Progress · Done · Awaiting
    4. Filter card            IssueFilterBar → tab strip → IssueTable
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
import { computed, h, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'

import { api, errMsg } from '@/api/client'
import { useBranding } from '@/composables/useBranding'
import { useAuthStore } from '@/stores/auth'
import IssueTable from '@/components/issue-list/IssueTable.vue'
import IssueFilterBar from '@/components/issue-list/IssueFilterBar.vue'
import type {
  ColumnDef,
  FilterOption,
  RowAction,
  SharedFilterState,
  TagOption,
} from '@/components/issue-list/types'
import AppIcon from '@/components/AppIcon.vue'
import StatusDot from '@/components/StatusDot.vue'

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
interface PortalIssue {
  id: number
  issue_key: string
  title: string
  status: string
  priority: string
  type: string
  estimate_hours: number | null
  estimate_lp: number | null
  ar_hours: number | null
  ar_lp: number | null
  estimate_eur: number | null
  ar_eur: number | null
  accepted_at: string | null
  created_at: string
  updated_at: string
  tags?: { id: number; name: string }[]
}

const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const auth = useAuthStore()
const { branding } = useBranding()

const projectId = Number(route.params.id)

const project = ref<PortalProject | null>(null)
const issues = ref<PortalIssue[]>([])
const loading = ref(true)

// Filter state owned by this view; mirrored to the URL on every change.
const filters = ref<SharedFilterState>({
  status: [],
  type: [],
  priority: [],
  tagIds: [],
  q: '',
})

type Tab = 'all' | 'open' | 'review' | 'accepted'
const activeTab = ref<Tab>('all')

type SortDir = 'asc' | 'desc'
const sortCol = ref('updated_at')
const sortDir = ref<SortDir>('desc')

// New-Request modal state
const showRequestModal = ref(false)
const requestTitle = ref('')
const requestDesc = ref('')
const requestLoading = ref(false)
const requestError = ref('')

// ── Filter options ───────────────────────────────────────────────────────
const STATUS_OPTIONS: FilterOption[] = [
  { value: 'new', label: t('status.new') },
  { value: 'backlog', label: t('status.backlog') },
  { value: 'in-progress', label: t('status.in-progress') },
  { value: 'done', label: t('status.done') },
  { value: 'delivered', label: t('status.delivered') },
  { value: 'accepted', label: t('status.accepted') },
  { value: 'invoiced', label: t('status.invoiced') },
  { value: 'cancelled', label: t('status.cancelled') },
]

const TYPE_OPTIONS: FilterOption[] = [
  { value: 'epic', label: 'Epic' },
  { value: 'ticket', label: 'Ticket' },
  { value: 'task', label: 'Task' },
]

const PRIORITY_OPTIONS: FilterOption[] = [
  { value: 'low', label: 'Low' },
  { value: 'medium', label: 'Medium' },
  { value: 'high', label: 'High' },
]

// Tags are populated from the loaded issue set so the picker is
// project-scoped — global tag list would over-disclose.
const tagOptions = computed<TagOption[]>(() => {
  const seen = new Map<number, TagOption>()
  for (const iss of issues.value) {
    for (const tag of iss.tags ?? []) {
      if (!seen.has(tag.id)) seen.set(tag.id, { id: tag.id, name: tag.name })
    }
  }
  return [...seen.values()].sort((a, b) => a.name.localeCompare(b.name))
})

// ── Data fetching ────────────────────────────────────────────────────────
async function fetchIssues() {
  const params = new URLSearchParams()
  if (filters.value.status.length) params.set('status', filters.value.status.join(','))
  if (filters.value.type.length) params.set('type', filters.value.type.join(','))
  if (filters.value.priority.length) params.set('priority', filters.value.priority.join(','))
  if (filters.value.tagIds.length) params.set('tag_ids', filters.value.tagIds.join(','))
  if (filters.value.q.trim()) params.set('q', filters.value.q.trim())
  if (sortCol.value) params.set('sort', sortCol.value)
  if (sortDir.value) params.set('order', sortDir.value)
  const qs = params.toString()
  const url = `/portal/projects/${projectId}/issues${qs ? '?' + qs : ''}`
  issues.value = await api.get<PortalIssue[]>(url)
}

async function loadAll() {
  loading.value = true
  try {
    const [p, _] = await Promise.all([
      api.get<PortalProject>(`/portal/projects/${projectId}`),
      fetchIssues(),
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
  if (typeof q.sort === 'string') sortCol.value = q.sort
  if (typeof q.order === 'string') sortDir.value = q.order === 'asc' ? 'asc' : 'desc'
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
  if (sortCol.value !== 'updated_at') query.sort = sortCol.value
  if (sortDir.value !== 'desc') query.order = sortDir.value
  if (activeTab.value !== 'all') query.tab = activeTab.value
  void router.replace({ query })
}

watch(filters, () => {
  writeUrlState()
  void fetchIssues()
}, { deep: true })

watch([sortCol, sortDir], () => {
  writeUrlState()
  void fetchIssues()
})

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

const tabBoundIssues = computed(() =>
  issues.value.filter((iss) => inTab(iss, activeTab.value)),
)

const tabCounts = computed(() => ({
  all: issues.value.length,
  open: issues.value.filter((i) => inTab(i, 'open')).length,
  review: issues.value.filter((i) => inTab(i, 'review')).length,
  accepted: issues.value.filter((i) => inTab(i, 'accepted')).length,
}))

// ── KPI stat bar ────────────────────────────────────────────────────────
const kpis = computed(() => {
  const total = issues.value.length
  const backlog = issues.value.filter(
    (i) => i.status === 'new' || i.status === 'backlog',
  ).length
  const inProgress = issues.value.filter(
    (i) => i.status === 'in-progress' || i.status === 'qa',
  ).length
  const done = issues.value.filter(
    (i) => i.status === 'done' || i.status === 'accepted' || i.status === 'invoiced',
  ).length
  const awaiting = issues.value.filter(
    (i) => i.status === 'delivered' || i.status === 'done',
  ).length
  return { total, backlog, inProgress, done, awaiting }
})

// ── Sort dispatch ───────────────────────────────────────────────────────
function onSort(col: string) {
  if (sortCol.value === col) {
    sortDir.value = sortDir.value === 'asc' ? 'desc' : 'asc'
  } else {
    sortCol.value = col
    sortDir.value = 'asc'
  }
}

// ── Accept/Reject row actions ───────────────────────────────────────────
const canEdit = computed(() => auth.canEdit(projectId))

async function acceptIssue(issue: PortalIssue) {
  try {
    await api.post(`/portal/issues/${issue.id}/accept`, {})
    // Optimistic row removal — the welcome screen does the same; keep
    // the UX consistent with v3.5.3.
    issues.value = issues.value.filter((i) => i.id !== issue.id)
  } catch {
    void fetchIssues()
  }
}

async function rejectIssue(issue: PortalIssue) {
  try {
    await api.post(`/portal/issues/${issue.id}/reject`, {
      title: 'Rejected from portal',
    })
    issues.value = issues.value.filter((i) => i.id !== issue.id)
  } catch {
    void fetchIssues()
  }
}

function rowActions(issue: PortalIssue): RowAction[] {
  if (issue.status === 'invoiced') {
    return [
      {
        key: 'locked',
        label: t('portal.invoicedLabel'),
        variant: 'ghost',
        disabled: true,
        onClick: () => {
          /* locked */
        },
      },
    ]
  }
  if (issue.status === 'delivered' || issue.status === 'done') {
    if (!canEdit.value) return []
    return [
      {
        key: 'accept',
        label: t('portal.tabs.accepted'),
        variant: 'primary',
        onClick: () => void acceptIssue(issue),
      },
      {
        key: 'reject',
        label: t('portal.reject'),
        variant: 'ghost',
        onClick: () => void rejectIssue(issue),
      },
    ]
  }
  return []
}

// ── Columns ─────────────────────────────────────────────────────────────
type PortalColumnDef = ColumnDef<PortalIssue>

const COLUMNS = computed<PortalColumnDef[]>(() => [
  {
    key: 'issue_key',
    label: t('portal.table.key'),
    sortable: true,
    render: (issue) => issue.issue_key,
  },
  {
    key: 'type',
    label: t('portal.table.type'),
    render: (issue) => issue.type,
  },
  {
    key: 'title',
    label: t('portal.table.title'),
    sortable: true,
    render: (issue) => issue.title,
  },
  {
    key: 'status',
    label: t('portal.table.status'),
    sortable: true,
    render: (issue) =>
      h('span', { class: 'pv-status' }, [
        h(StatusDot, { status: issue.status }),
        ` ${t('status.' + issue.status)}`,
      ]),
  },
  {
    key: 'priority',
    label: t('portal.table.priority'),
    sortable: true,
    render: (issue) => issue.priority,
  },
  {
    key: 'estimate_eur',
    label: t('portal.table.estimate'),
    render: (issue) => fmtEur(issue.estimate_eur),
  },
  {
    key: 'ar_eur',
    label: t('portal.table.ar'),
    render: (issue) => fmtEur(issue.ar_eur),
  },
  {
    key: 'accepted_at',
    label: t('portal.table.accepted'),
    sortable: true,
    render: (issue) => issue.accepted_at ?? '—',
  },
])

function fmtEur(v: number | null | undefined): string {
  if (v == null) return '—'
  return new Intl.NumberFormat('de-DE', { style: 'currency', currency: 'EUR' }).format(v)
}

function onRowClick(issue: PortalIssue) {
  void router.push(`/projects/${projectId}/issues/${issue.id}`)
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
    await fetchIssues()
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
        <span class="pv__stat-value">{{ kpis.total }}</span>
        <span class="pv__stat-label">{{ $t('portal.summary.total') }}</span>
      </div>
      <div class="pv__stat">
        <span class="pv__stat-value">{{ kpis.backlog }}</span>
        <span class="pv__stat-label">{{ $t('status.backlog') }}</span>
      </div>
      <div class="pv__stat">
        <span class="pv__stat-value">{{ kpis.inProgress }}</span>
        <span class="pv__stat-label">{{ $t('status.in-progress') }}</span>
      </div>
      <div class="pv__stat">
        <span class="pv__stat-value">{{ kpis.done }}</span>
        <span class="pv__stat-label">{{ $t('status.done') }}</span>
      </div>
      <div class="pv__stat pv__stat--awaiting">
        <span class="pv__stat-value">{{ kpis.awaiting }}</span>
        <span class="pv__stat-label">{{ $t('portal.tabs.review') }}</span>
      </div>
    </div>

    <!-- Filter card -->
    <section class="pv__list" v-if="project">
      <IssueFilterBar
        v-model="filters"
        :enabled-filters="['q', 'status', 'type', 'priority', 'tag']"
        :status-options="STATUS_OPTIONS"
        :type-options="TYPE_OPTIONS"
        :priority-options="PRIORITY_OPTIONS"
        :tag-options="tagOptions"
      />

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

      <IssueTable
        :issues="tabBoundIssues"
        :columns="COLUMNS"
        :row-actions="(issue: any) => rowActions(issue as PortalIssue)"
        :sort="{ col: sortCol, dir: sortDir }"
        :loading="loading"
        :empty-state="{ title: $t('portal.noIssues') }"
        @sort="onSort"
        @row-click="(issue: any) => onRowClick(issue as PortalIssue)"
      />
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
   header stacks, and "+ New Request" goes full-width. The IssueFilterBar
   already collapses its pills into the slide-up sheet at this
   breakpoint per its own internal mobileBreakpoint default. */
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
}
</style>
