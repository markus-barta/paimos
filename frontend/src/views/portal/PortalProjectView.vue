<script setup lang="ts">
import { ref, onMounted, computed, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { api, errMsg } from '@/api/client'
import { useSearchStore } from '@/stores/search'
import { highlight } from '@/composables/useHighlight'
import { useBranding } from '@/composables/useBranding'
import StatusDot from '@/components/StatusDot.vue'
import AppIcon from '@/components/AppIcon.vue'

interface PortalProject {
  id: number; key: string; name: string; description: string; status: string
  logo_path: string; issue_count: number; done_count: number
}
interface PortalIssue {
  id: number; issue_key: string; title: string; description: string
  acceptance_criteria: string; status: string; priority: string; type: string
  parent_id: number | null
  cost_unit: string; release: string
  estimate_hours: number | null; estimate_lp: number | null
  ar_hours: number | null; ar_lp: number | null
  estimate_eur: number | null; ar_eur: number | null
  accepted_at: string | null; created_at: string; updated_at: string
}
interface PortalSummary {
  total_issues: number; by_status: Record<string, number>
  total_estimate_eur: number | null; total_ar_eur: number | null
}

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const search = useSearchStore()
const { branding } = useBranding()
const projectId = Number(route.params.id)

const project = ref<PortalProject | null>(null)
const issues = ref<PortalIssue[]>([])
const summary = ref<PortalSummary | null>(null)
const loading = ref(true)

// Filter tabs: all, open (in-progress), review (done), accepted
const filterTab = ref<'all' | 'open' | 'review' | 'accepted'>('all')
const filterStatus = ref('')
const filterType = ref('')

// Request modal
const showRequestModal = ref(false)
const requestTitle = ref('')
const requestDesc = ref('')
const requestLoading = ref(false)
const requestError = ref('')

// Tree view
const treeView = ref(false)

const filteredIssues = computed(() => {
  let list = issues.value
  if (filterTab.value === 'open') {
    list = list.filter(i => i.status === 'new' || i.status === 'backlog' || i.status === 'in-progress' || i.status === 'qa')
  } else if (filterTab.value === 'review') {
    list = list.filter(i => i.status === 'done' || i.status === 'delivered')
  } else if (filterTab.value === 'accepted') {
    list = list.filter(i => i.status === 'accepted' || i.status === 'invoiced')
  }
  if (filterStatus.value) list = list.filter(i => i.status === filterStatus.value)
  if (filterType.value) list = list.filter(i => i.type === filterType.value)
  return list
})

// Sort
type SortKey = 'issue_key' | 'title' | 'status' | 'type' | 'priority' | 'estimate_eur' | 'ar_eur' | 'updated_at'
const sortKey = ref<SortKey>('updated_at')
const sortAsc = ref(false)

const sortedIssues = computed(() => {
  const list = [...filteredIssues.value]
  list.sort((a, b) => {
    const av = a[sortKey.value]
    const bv = b[sortKey.value]
    if (av == null && bv == null) return 0
    if (av == null) return 1
    if (bv == null) return -1
    const cmp = typeof av === 'number' ? av - (bv as number) : String(av).localeCompare(String(bv))
    return sortAsc.value ? cmp : -cmp
  })
  return list
})

// Tree view helpers
const topLevel = computed(() => sortedIssues.value.filter(i => !i.parent_id || !sortedIssues.value.find(p => p.id === i.parent_id)))
function childrenOf(parentId: number) {
  return sortedIssues.value.filter(i => i.parent_id === parentId)
}

function toggleSort(key: SortKey) {
  if (sortKey.value === key) { sortAsc.value = !sortAsc.value }
  else { sortKey.value = key; sortAsc.value = true }
}
function sortIcon(key: SortKey): string {
  if (sortKey.value !== key) return ''
  return sortAsc.value ? ' \u25B2' : ' \u25BC'
}

async function fetchIssues() {
  const url = `/portal/projects/${projectId}/issues${search.query.length >= 2 ? '?q=' + encodeURIComponent(search.query) : ''}`
  issues.value = await api.get<PortalIssue[]>(url)
}

watch(() => search.query, () => { fetchIssues() })

onMounted(async () => {
  try {
    const [p, iss, s] = await Promise.all([
      api.get<PortalProject>(`/portal/projects/${projectId}`),
      api.get<PortalIssue[]>(`/portal/projects/${projectId}/issues${search.query.length >= 2 ? '?q=' + encodeURIComponent(search.query) : ''}`),
      api.get<PortalSummary>(`/portal/projects/${projectId}/summary`),
    ])
    project.value = p
    issues.value = iss
    summary.value = s
  } catch { /* ignore */ }
  loading.value = false
})

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
    issues.value = await api.get<PortalIssue[]>(`/portal/projects/${projectId}/issues`)
  } catch (e: unknown) {
    requestError.value = errMsg(e, 'Failed')
  }
  requestLoading.value = false
}

async function acceptIssue(issue: PortalIssue) {
  try {
    await api.post(`/portal/issues/${issue.id}/accept`, {})
    issue.status = 'accepted'
  } catch { /* ignore */ }
}

function fmtEur(v: number | null | undefined): string {
  if (v == null) return '-'
  return new Intl.NumberFormat('de-DE', { style: 'currency', currency: 'EUR' }).format(v)
}

const STATUS_ICONS: Record<string, string> = {
  'new': 'plus-circle',
  'backlog': 'inbox',
  'in-progress': 'loader',
  'qa': 'search',
  'done': 'check-circle',
  'delivered': 'package-check',
  'accepted': 'shield-check',
  'invoiced': 'receipt-text',
  'cancelled': 'x-circle',
}

const today = new Date().toISOString().slice(0, 10)

// Tab counts
const openCount = computed(() => issues.value.filter(i => i.status === 'new' || i.status === 'backlog' || i.status === 'in-progress' || i.status === 'qa').length)
const reviewCount = computed(() => issues.value.filter(i => i.status === 'done' || i.status === 'delivered').length)
const acceptedCount = computed(() => issues.value.filter(i => i.status === 'accepted' || i.status === 'invoiced').length)
</script>

<template>
  <div class="portal-project" v-if="!loading && project">
    <!-- Header with customer logo -->
    <div class="project-header">
      <router-link to="/portal" class="back-link">{{ $t('portal.allProjects') }}</router-link>
      <div class="header-row">
        <div class="header-left">
          <img v-if="project.logo_path" :src="project.logo_path" alt="" class="project-logo" />
          <img v-else :src="branding.logo" alt="" class="project-logo project-logo--fallback" />
          <div>
            <div class="key-badge">{{ project.key }}</div>
            <h1 class="project-name">{{ project.name }}</h1>
          </div>
        </div>
        <div class="header-actions">
          <a :href="`/api/portal/projects/${projectId}/acceptance-report?date=${today}`"
             target="_blank" class="btn btn-ghost btn-sm report-link">
            <AppIcon name="file-text" :size="13" /> {{ $t('portal.summary.report') }}
          </a>
          <button class="btn btn-primary" @click="showRequestModal = true">{{ $t('portal.newRequest') }}</button>
        </div>
      </div>
      <p v-if="project.description" class="project-desc">{{ project.description }}</p>
    </div>

    <!-- Summary cards with icons -->
    <div class="summary-row" v-if="summary">
      <div class="summary-card">
        <div class="sc-icon"><AppIcon name="layers" :size="18" /></div>
        <div class="sc-data">
          <div class="sc-value">{{ summary.total_issues }}</div>
          <div class="sc-label">{{ $t('portal.summary.total') }}</div>
        </div>
      </div>
      <div class="summary-card" v-for="(count, status) in summary.by_status" :key="status">
        <div class="sc-icon"><AppIcon :name="STATUS_ICONS[status] || 'circle'" :size="18" /></div>
        <div class="sc-data">
          <div class="sc-value">{{ count }}</div>
          <div class="sc-label">{{ $t(`status.${status}`, status) }}</div>
        </div>
      </div>
      <div class="summary-card" v-if="summary.total_estimate_eur != null">
        <div class="sc-icon"><AppIcon name="calculator" :size="18" /></div>
        <div class="sc-data">
          <div class="sc-value sc-value--money">{{ fmtEur(summary.total_estimate_eur) }}</div>
          <div class="sc-label">{{ $t('portal.summary.estimate') }}</div>
        </div>
      </div>
      <div class="summary-card" v-if="summary.total_ar_eur != null">
        <div class="sc-icon"><AppIcon name="trending-up" :size="18" /></div>
        <div class="sc-data">
          <div class="sc-value sc-value--money">{{ fmtEur(summary.total_ar_eur) }}</div>
          <div class="sc-label">{{ $t('portal.summary.arCost') }}</div>
        </div>
      </div>
    </div>

    <!-- Workflow tabs + Filters -->
    <div class="controls-row">
      <div class="tabs">
        <button :class="['tab', { active: filterTab === 'all' }]" @click="filterTab = 'all'">
          {{ $t('portal.tabs.all') }} <span class="tab-count">{{ issues.length }}</span>
        </button>
        <button :class="['tab', { active: filterTab === 'open' }]" @click="filterTab = 'open'">
          {{ $t('portal.tabs.open') }} <span class="tab-count">{{ openCount }}</span>
        </button>
        <button :class="['tab', { active: filterTab === 'review' }]" @click="filterTab = 'review'">
          {{ $t('portal.tabs.review') }} <span class="tab-count">{{ reviewCount }}</span>
        </button>
        <button :class="['tab', { active: filterTab === 'accepted' }]" @click="filterTab = 'accepted'">
          {{ $t('portal.tabs.accepted') }} <span class="tab-count">{{ acceptedCount }}</span>
        </button>
      </div>
      <div class="filters">
        <select v-model="filterStatus">
          <option value="">{{ $t('portal.filters.allStatus') }}</option>
          <option value="new">{{ $t('status.new') }}</option>
          <option value="backlog">{{ $t('status.backlog') }}</option>
          <option value="in-progress">{{ $t('status.in-progress') }}</option>
          <option value="qa">{{ $t('status.qa', 'QA') }}</option>
          <option value="done">{{ $t('status.done') }}</option>
          <option value="accepted">{{ $t('status.accepted', 'Accepted') }}</option>
          <option value="invoiced">{{ $t('status.invoiced', 'Invoiced') }}</option>
          <option value="cancelled">{{ $t('status.cancelled') }}</option>
        </select>
        <select v-model="filterType">
          <option value="">{{ $t('portal.filters.allTypes') }}</option>
          <option value="epic">Epic</option>
          <option value="ticket">Ticket</option>
          <option value="task">Task</option>
        </select>
        <button :class="['btn btn-ghost btn-sm', { active: treeView }]" @click="treeView = !treeView">
          {{ treeView ? '≡' : '⌥' }}
        </button>
      </div>
    </div>

    <!-- Issues table -->
    <div class="table-wrapper">
      <table class="issues-table">
        <thead>
          <tr>
            <th @click="toggleSort('issue_key')" class="sortable">{{ $t('portal.table.key') }}{{ sortIcon('issue_key') }}</th>
            <th @click="toggleSort('title')" class="sortable">{{ $t('portal.table.title') }}{{ sortIcon('title') }}</th>
            <th @click="toggleSort('type')" class="sortable">{{ $t('portal.table.type') }}{{ sortIcon('type') }}</th>
            <th @click="toggleSort('status')" class="sortable">{{ $t('portal.table.status') }}{{ sortIcon('status') }}</th>
            <th @click="toggleSort('priority')" class="sortable">{{ $t('portal.table.priority') }}{{ sortIcon('priority') }}</th>
            <th @click="toggleSort('estimate_eur')" class="sortable num">{{ $t('portal.table.estimate') }}{{ sortIcon('estimate_eur') }}</th>
            <th @click="toggleSort('ar_eur')" class="sortable num">{{ $t('portal.table.ar') }}{{ sortIcon('ar_eur') }}</th>
            <th>{{ $t('portal.table.accepted') }}</th>
          </tr>
        </thead>
        <tbody v-if="!treeView">
          <tr v-for="issue in sortedIssues" :key="issue.id"
              :class="{ 'row-accepted': issue.status === 'accepted' || issue.status === 'invoiced' }"
              @click="router.push(`/portal/projects/${projectId}/issues/${issue.id}`)"
              style="cursor: pointer">
            <td class="key-cell">{{ issue.issue_key }}</td>
            <td><span v-html="highlight(issue.title, search.query)" /></td>
            <td><span :class="`type-badge type-badge--${issue.type}`">{{ issue.type }}</span></td>
            <td><StatusDot :status="issue.status" /> {{ $t(`status.${issue.status}`, issue.status) }}</td>
            <td>{{ issue.priority }}</td>
            <td class="num">{{ fmtEur(issue.estimate_eur) }}</td>
            <td class="num">{{ fmtEur(issue.ar_eur) }}</td>
            <td @click.stop>
              <span v-if="issue.status === 'invoiced'" class="accepted-badge">{{ $t('portal.invoicedLabel', 'Invoiced') }}</span>
              <span v-else-if="issue.status === 'accepted'" class="accepted-badge">{{ $t('portal.acceptedLabel') }}</span>
              <button v-else-if="issue.status === 'done'" class="btn btn-ghost btn-sm" @click="acceptIssue(issue)">
                {{ $t('portal.accept') }}
              </button>
            </td>
          </tr>
        </tbody>
        <!-- Tree view -->
        <tbody v-else>
          <template v-for="parent in topLevel" :key="parent.id">
            <tr :class="{ 'row-accepted': parent.status === 'accepted' || parent.status === 'invoiced' }"
                @click="router.push(`/portal/projects/${projectId}/issues/${parent.id}`)"
                style="cursor: pointer">
              <td class="key-cell">{{ parent.issue_key }}</td>
              <td><strong><span v-html="highlight(parent.title, search.query)" /></strong></td>
              <td><span :class="`type-badge type-badge--${parent.type}`">{{ parent.type }}</span></td>
              <td><StatusDot :status="parent.status" /> {{ $t(`status.${parent.status}`, parent.status) }}</td>
              <td>{{ parent.priority }}</td>
              <td class="num">{{ fmtEur(parent.estimate_eur) }}</td>
              <td class="num">{{ fmtEur(parent.ar_eur) }}</td>
              <td @click.stop>
                <span v-if="parent.status === 'invoiced'" class="accepted-badge">{{ $t('portal.invoicedLabel', 'Invoiced') }}</span>
                <span v-else-if="parent.status === 'accepted'" class="accepted-badge">{{ $t('portal.acceptedLabel') }}</span>
                <button v-else-if="parent.status === 'done'" class="btn btn-ghost btn-sm" @click="acceptIssue(parent)">{{ $t('portal.accept') }}</button>
              </td>
            </tr>
            <tr v-for="child in childrenOf(parent.id)" :key="child.id"
                class="tree-child"
                :class="{ 'row-accepted': child.status === 'accepted' || child.status === 'invoiced' }"
                @click="router.push(`/portal/projects/${projectId}/issues/${child.id}`)"
                style="cursor: pointer">
              <td class="key-cell" style="padding-left: 2rem">{{ child.issue_key }}</td>
              <td><span v-html="highlight(child.title, search.query)" /></td>
              <td><span :class="`type-badge type-badge--${child.type}`">{{ child.type }}</span></td>
              <td><StatusDot :status="child.status" /> {{ $t(`status.${child.status}`, child.status) }}</td>
              <td>{{ child.priority }}</td>
              <td class="num">{{ fmtEur(child.estimate_eur) }}</td>
              <td class="num">{{ fmtEur(child.ar_eur) }}</td>
              <td @click.stop>
                <span v-if="child.status === 'invoiced'" class="accepted-badge">{{ $t('portal.invoicedLabel', 'Invoiced') }}</span>
                <span v-else-if="child.status === 'accepted'" class="accepted-badge">{{ $t('portal.acceptedLabel') }}</span>
                <button v-else-if="child.status === 'done'" class="btn btn-ghost btn-sm" @click="acceptIssue(child)">{{ $t('portal.accept') }}</button>
              </td>
            </tr>
          </template>
        </tbody>
        <tbody v-if="sortedIssues.length === 0">
          <tr><td colspan="8" class="empty-cell">{{ $t('portal.noIssues') }}</td></tr>
        </tbody>
      </table>
    </div>

    <!-- Request modal -->
    <Teleport to="body">
      <div v-if="showRequestModal" class="modal-overlay" @click.self="showRequestModal = false">
        <div class="modal-box">
          <h2 class="modal-title">{{ $t('portal.requestModal.title') }}</h2>
          <form @submit.prevent="submitRequest" class="request-form">
            <div class="field">
              <label>{{ $t('portal.requestModal.titleField') }}</label>
              <input v-model="requestTitle" type="text" required autofocus :placeholder="$t('portal.requestModal.titleField')" />
            </div>
            <div class="field">
              <label>{{ $t('portal.requestModal.description') }}</label>
              <textarea v-model="requestDesc" rows="4" :placeholder="$t('portal.requestModal.description')"></textarea>
            </div>
            <div v-if="requestError" class="form-error">{{ requestError }}</div>
            <div class="form-actions">
              <button type="button" class="btn btn-ghost" @click="showRequestModal = false">{{ $t('portal.requestModal.cancel') }}</button>
              <button type="submit" class="btn btn-primary" :disabled="requestLoading">
                {{ requestLoading ? $t('portal.requestModal.submitting') : $t('portal.requestModal.submit') }}
              </button>
            </div>
          </form>
        </div>
      </div>
    </Teleport>
  </div>
  <div v-else-if="loading" class="loading">{{ $t('portal.loading') }}</div>
</template>

<style scoped>
.project-header { margin-bottom: 1.5rem; }
.back-link { font-size: 13px; color: var(--text-muted); display: inline-block; margin-bottom: .5rem; }
.back-link:hover { color: var(--bp-blue); }
.header-row { display: flex; align-items: center; justify-content: space-between; gap: 1rem; flex-wrap: wrap; }
.header-left { display: flex; align-items: center; gap: 1rem; }
.header-actions { display: flex; align-items: center; gap: .5rem; }
.project-logo { height: 48px; width: auto; border-radius: 6px; }
.project-logo--fallback { opacity: .3; }
.key-badge {
  font-size: 11px; font-weight: 700; letter-spacing: .03em;
  padding: .1rem .4rem; border-radius: 4px;
  background: var(--bp-blue-pale); color: var(--bp-blue-dark);
  display: inline-block; margin-bottom: .15rem;
}
.project-name { font-size: 20px; font-weight: 700; }
.project-desc { font-size: 13px; color: var(--text-muted); margin-top: .5rem; }
.report-link { display: inline-flex; align-items: center; gap: .3rem; }

/* Summary cards */
.summary-row { display: flex; gap: .75rem; margin-bottom: 1.5rem; flex-wrap: wrap; }
.summary-card {
  background: var(--bg-card); border: 1px solid var(--border); border-radius: var(--radius);
  padding: .65rem 1rem; min-width: 90px;
  display: flex; align-items: center; gap: .65rem;
}
.sc-icon { color: var(--text-muted); flex-shrink: 0; }
.sc-value { font-size: 18px; font-weight: 700; }
.sc-value--money { font-size: 14px; }
.sc-label { font-size: 10px; color: var(--text-muted); text-transform: uppercase; letter-spacing: .04em; }

/* Controls */
.controls-row { display: flex; align-items: center; justify-content: space-between; gap: 1rem; margin-bottom: 1rem; flex-wrap: wrap; }
.tabs { display: flex; gap: .25rem; }
.tab {
  padding: .4rem .75rem; font-size: 13px; font-weight: 500;
  border: 1px solid var(--border); background: var(--bg-card);
  border-radius: var(--radius); color: var(--text-muted); cursor: pointer;
  display: inline-flex; align-items: center; gap: .35rem;
}
.tab.active { background: var(--bp-blue); color: #fff; border-color: var(--bp-blue-dark); }
.tab-count {
  font-size: 10px; font-weight: 700; background: rgba(0,0,0,.1); border-radius: 8px;
  padding: .1rem .35rem; min-width: 18px; text-align: center;
}
.tab.active .tab-count { background: rgba(255,255,255,.2); }
.filters { display: flex; gap: .5rem; align-items: center; }
.filters select { width: auto; min-width: 120px; font-size: 13px; padding: .35rem .5rem; }

/* Table */
.table-wrapper { background: var(--bg-card); border: 1px solid var(--border); border-radius: var(--radius); overflow: auto; }
.issues-table { width: 100%; border-collapse: collapse; font-size: 13px; }
.issues-table th, .issues-table td { padding: .55rem .75rem; text-align: left; border-bottom: 1px solid var(--border); }
.issues-table th {
  font-size: 11px; font-weight: 600; text-transform: uppercase;
  letter-spacing: .04em; color: var(--text-muted); background: var(--bg); white-space: nowrap;
}
.sortable { cursor: pointer; }
.sortable:hover { color: var(--bp-blue); }
.num { text-align: right; }
.issues-table tbody tr:hover { background: var(--bg); }
.key-cell { font-weight: 600; color: var(--bp-blue); white-space: nowrap; }
.type-badge { font-size: 11px; font-weight: 600; text-transform: capitalize; }
.type-badge--epic { color: var(--type-epic, #5e35b1); }
.type-badge--ticket { color: var(--type-ticket, var(--bp-blue-dark)); }
.type-badge--task { color: var(--type-task, #2e7d32); }
.tree-child td { font-size: 12px; }
.row-accepted { background: #f0fdf4; }
.accepted-badge { font-size: 11px; font-weight: 600; color: #16a34a; display: inline-flex; align-items: center; gap: .25rem; }
.accepted-badge::before { content: '\2713'; }
.empty-cell { text-align: center; color: var(--text-muted); padding: 2rem !important; }
.btn-sm { padding: .3rem .65rem; font-size: 12px; }
.loading { color: var(--text-muted); padding: 3rem; text-align: center; }

/* Modal */
.modal-overlay { position: fixed; inset: 0; background: rgba(0,0,0,.4); display: flex; align-items: center; justify-content: center; z-index: 1000; }
.modal-box { background: var(--bg-card); border-radius: var(--radius); box-shadow: var(--shadow-md); padding: 1.5rem; width: 480px; max-width: 90vw; }
.modal-title { font-size: 16px; font-weight: 700; margin-bottom: 1rem; }
.request-form { display: flex; flex-direction: column; gap: .75rem; }
.field { display: flex; flex-direction: column; gap: .25rem; }
.field label { font-size: 13px; font-weight: 600; color: var(--text-muted); }
.form-error { font-size: 13px; color: #c0392b; }
.form-actions { display: flex; justify-content: flex-end; gap: .5rem; margin-top: .5rem; }
</style>
