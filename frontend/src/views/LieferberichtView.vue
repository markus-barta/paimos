<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import { api, errMsg } from '@/api/client'
import AppIcon from '@/components/AppIcon.vue'
import MetaSelect from '@/components/MetaSelect.vue'
import type { MetaOption } from '@/components/MetaSelect.vue'

// ── Projects ─────────────────────────────────────────────────────────────────
interface BpProject { id: number; key: string; name: string }
const projects = ref<BpProject[]>([])
const selectedProject = ref('')

onMounted(async () => {
  projects.value = await api.get<BpProject[]>('/projects')
})

const projectOptions = computed<MetaOption[]>(() =>
  projects.value.map(p => ({ value: String(p.id), label: `${p.key} — ${p.name}` }))
)

// ── Scope ────────────────────────────────────────────────────────────────────
type Scope = 'all_open' | 'sprint' | 'date_range'
const scope = ref<Scope>('all_open')
const selectedSprint = ref('')
const fromDate = ref('')
const toDate = ref('')

// ── Sprints (loaded when project changes) ────────────────────────────────────
interface Sprint { id: number; title: string }
const sprints = ref<Sprint[]>([])

watch(selectedProject, async (pid) => {
  sprints.value = []
  selectedSprint.value = ''
  if (!pid) return
  try {
    const all = await api.get<Sprint[]>('/sprints')
    sprints.value = all
  } catch { /* empty */ }
})

const sprintOptions = computed<MetaOption[]>(() =>
  sprints.value.map(s => ({ value: String(s.id), label: s.title }))
)

// ── Report data ──────────────────────────────────────────────────────────────
interface LbIssue {
  issue_key: string; type: string; title: string; description: string; status: string
  estimate_lp: number | null; estimate_hours: number | null
  ar_lp: number | null; ar_hours: number | null
  rate_lp: number | null; rate_hourly: number | null
  ar_eur: number
}
interface LbSubtotal { estimate_lp: number; estimate_hours: number; ar_lp: number; ar_hours: number; ar_eur: number }
interface LbGroup { epic_key: string; epic_title: string; issues: LbIssue[]; subtotal: LbSubtotal }
interface LbReport {
  project_key: string; project_name: string; generated_at: string
  groups: LbGroup[]; grand_total: LbSubtotal
}

const report = ref<LbReport | null>(null)
const loading = ref(false)
const error = ref('')

const canGenerate = computed(() => !!selectedProject.value)

function buildParams(): string {
  const params = new URLSearchParams()
  params.set('scope', scope.value)
  if (scope.value === 'sprint' && selectedSprint.value) {
    params.set('sprint_ids', selectedSprint.value)
  }
  if (scope.value === 'date_range') {
    if (fromDate.value) params.set('from', fromDate.value)
    if (toDate.value) params.set('to', toDate.value)
  }
  return params.toString()
}

async function generate() {
  if (!selectedProject.value) return
  loading.value = true
  error.value = ''
  report.value = null
  try {
    const data = await api.get<LbReport>(`/projects/${selectedProject.value}/reports/lieferbericht?${buildParams()}`)
    report.value = data
  } catch (e: unknown) {
    error.value = errMsg(e, 'Failed to generate report.')
  } finally {
    loading.value = false
  }
}

function downloadPDF() {
  if (!selectedProject.value) return
  window.open(`/api/projects/${selectedProject.value}/reports/lieferbericht/pdf?${buildParams()}`, '_blank')
}

// ── Row coloring ─────────────────────────────────────────────────────────────
function rowClass(status: string): string {
  switch (status) {
    case 'done': case 'delivered': case 'accepted': case 'invoiced':
      return 'lb-row--done'
    case 'in-progress': case 'qa':
      return 'lb-row--progress'
    default:
      return 'lb-row--planned'
  }
}

function statusLabel(status: string): string {
  switch (status) {
    case 'done': case 'delivered': case 'accepted': case 'invoiced':
      return 'Geliefert'
    case 'in-progress': case 'qa':
      return 'Umsetzung'
    default:
      return 'Geplant'
  }
}

function fmt(v: number | null): string {
  if (v == null || v === 0) return ''
  if (Number.isInteger(v)) return String(v)
  return v.toFixed(2).replace('.', ',')
}

const totalIssues = computed(() => report.value?.groups.reduce((n, g) => n + g.issues.length, 0) ?? 0)
</script>

<template>
  <Teleport defer to="#app-header-left">
    <span class="ah-title">Lieferbericht</span>
    <span class="ah-subtitle">Delivery report — issues grouped by epic with AR calculations.</span>
  </Teleport>

  <!-- Filter bar -->
  <div class="lb-filters">
    <div class="lb-filter-group">
      <label>Project</label>
      <MetaSelect v-model="selectedProject" :options="projectOptions" placeholder="Select project…" />
    </div>

    <div class="lb-filter-group">
      <label>Scope</label>
      <div class="lb-scope-toggle">
        <button :class="['mode-btn', { active: scope === 'all_open' }]" @click="scope = 'all_open'">All open</button>
        <button :class="['mode-btn', { active: scope === 'sprint' }]" @click="scope = 'sprint'">Sprint</button>
        <button :class="['mode-btn', { active: scope === 'date_range' }]" @click="scope = 'date_range'">Date range</button>
      </div>
    </div>

    <template v-if="scope === 'sprint'">
      <div class="lb-filter-group">
        <label>Sprint</label>
        <MetaSelect v-model="selectedSprint" :options="sprintOptions" placeholder="Select sprint…" />
      </div>
    </template>

    <template v-if="scope === 'date_range'">
      <div class="lb-filter-group">
        <label>From</label>
        <input v-model="fromDate" type="date" />
      </div>
      <div class="lb-filter-group">
        <label>To</label>
        <input v-model="toDate" type="date" />
      </div>
    </template>

    <div class="lb-filter-actions">
      <button class="btn btn-primary" :disabled="loading || !canGenerate" @click="generate">
        <AppIcon v-if="loading" name="loader" :size="14" class="spin" />
        <AppIcon v-else name="bar-chart-2" :size="14" />
        {{ loading ? 'Generating…' : 'Generate' }}
      </button>
      <button v-if="report" class="btn btn-secondary" :disabled="loading" @click="downloadPDF">
        <AppIcon name="download" :size="14" />
        Download PDF
      </button>
    </div>
  </div>

  <!-- Error -->
  <div v-if="error" class="lb-error">{{ error }}</div>

  <!-- Empty state -->
  <div v-if="report && totalIssues === 0" class="lb-empty">
    <AppIcon name="inbox" :size="28" class="lb-empty-icon" />
    <p>No issues match the selected filters.</p>
  </div>

  <!-- Report table -->
  <div v-if="report && totalIssues > 0" class="lb-table-wrap">
    <table class="lb-table">
      <thead>
        <tr>
          <th>Key</th>
          <th>Type</th>
          <th class="col-title">Summary</th>
          <th>Status</th>
          <th class="col-num">SP</th>
          <th class="col-num">h</th>
          <th class="col-num">AR/SP</th>
          <th class="col-num">AR SP</th>
          <th class="col-num">AR/h</th>
          <th class="col-num">AR h</th>
          <th class="col-num">AR</th>
          <th class="col-desc">Description</th>
        </tr>
      </thead>
      <tbody v-for="g in report.groups" :key="g.epic_key">
        <tr class="lb-epic-row">
          <td :colspan="12">{{ g.epic_key }}<span v-if="g.epic_title && g.epic_title !== g.epic_key"> — {{ g.epic_title }}</span></td>
        </tr>
        <tr v-for="issue in g.issues" :key="issue.issue_key" :class="rowClass(issue.status)">
          <td class="mono">{{ issue.issue_key }}</td>
          <td>{{ issue.type }}</td>
          <td class="col-title">{{ issue.title }}</td>
          <td>{{ statusLabel(issue.status) }}</td>
          <td class="col-num">{{ fmt(issue.estimate_lp) }}</td>
          <td class="col-num">{{ fmt(issue.estimate_hours) }}</td>
          <td class="col-num">{{ fmt(issue.rate_lp) }}</td>
          <td class="col-num">{{ fmt(issue.ar_lp) }}</td>
          <td class="col-num">{{ fmt(issue.rate_hourly) }}</td>
          <td class="col-num">{{ fmt(issue.ar_hours) }}</td>
          <td class="col-num lb-ar-total">{{ fmt(issue.ar_eur) }}</td>
          <td class="col-desc">{{ issue.description }}</td>
        </tr>
        <tr class="lb-subtotal-row">
          <td colspan="4" style="text-align:right;font-weight:700">Subtotal</td>
          <td class="col-num">{{ fmt(g.subtotal.estimate_lp) }}</td>
          <td class="col-num">{{ fmt(g.subtotal.estimate_hours) }}</td>
          <td class="col-num"></td>
          <td class="col-num">{{ fmt(g.subtotal.ar_lp) }}</td>
          <td class="col-num"></td>
          <td class="col-num">{{ fmt(g.subtotal.ar_hours) }}</td>
          <td class="col-num lb-ar-total">{{ fmt(g.subtotal.ar_eur) }}</td>
          <td></td>
        </tr>
      </tbody>
      <tfoot>
        <tr class="lb-grand-total">
          <td colspan="4" style="text-align:right;font-weight:700">Grand Total</td>
          <td class="col-num">{{ fmt(report.grand_total.estimate_lp) }}</td>
          <td class="col-num">{{ fmt(report.grand_total.estimate_hours) }}</td>
          <td class="col-num"></td>
          <td class="col-num">{{ fmt(report.grand_total.ar_lp) }}</td>
          <td class="col-num"></td>
          <td class="col-num">{{ fmt(report.grand_total.ar_hours) }}</td>
          <td class="col-num lb-ar-total">{{ fmt(report.grand_total.ar_eur) }}</td>
          <td></td>
        </tr>
      </tfoot>
    </table>
  </div>
</template>

<style scoped>
/* ── Filters ──────────────────────────────────────────────────────────────── */
.lb-filters {
  display: flex; align-items: flex-end; gap: 1rem; flex-wrap: wrap;
  padding: 1rem 1.25rem; background: var(--bg-card); border: 1px solid var(--border);
  border-radius: 8px; box-shadow: var(--shadow);
}
.lb-filter-group { display: flex; flex-direction: column; gap: .3rem; }
.lb-filter-group label { font-size: 11px; font-weight: 700; text-transform: uppercase; letter-spacing: .06em; color: var(--text-muted); }
.lb-filter-actions { display: flex; gap: .5rem; margin-left: auto; }

.lb-scope-toggle { display: flex; border: 1px solid var(--border); border-radius: var(--radius); overflow: hidden; }
.mode-btn { background: var(--bg); border: none; padding: .35rem .75rem; font-size: 12px; font-weight: 500; color: var(--text-muted); cursor: pointer; font-family: inherit; }
.mode-btn + .mode-btn { border-left: 1px solid var(--border); }
.mode-btn.active { background: var(--bp-blue); color: #fff; }

.lb-error { font-size: 13px; color: #c0392b; background: #fde8e8; padding: .5rem .75rem; border-radius: var(--radius); margin-top: 1rem; }
.lb-empty { text-align: center; padding: 3rem 1rem; color: var(--text-muted); font-size: 13px; }
.lb-empty-icon { opacity: .4; margin-bottom: .5rem; }

@keyframes spin { to { transform: rotate(360deg); } }
.spin { animation: spin .8s linear infinite; }

/* ── Table ─────────────────────────────────────────────────────────────────── */
.lb-table-wrap {
  margin-top: 1.25rem; overflow: auto;
  background: var(--bg-card); border: 1px solid var(--border);
  border-radius: 8px; box-shadow: var(--shadow);
}
.lb-table { width: 100%; border-collapse: collapse; font-size: 12px; }
.lb-table th {
  position: sticky; top: 0; z-index: 2;
  padding: .5rem .6rem; text-align: left; white-space: nowrap;
  background: var(--bg); border-bottom: 2px solid var(--border);
  font-size: 11px; font-weight: 700; text-transform: uppercase; letter-spacing: .04em; color: var(--text-muted);
}
.lb-table td { padding: .4rem .6rem; border-bottom: 1px solid var(--border); }
.col-num { text-align: right; font-variant-numeric: tabular-nums; white-space: nowrap; }
.col-title { max-width: 260px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.col-desc { max-width: 280px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 11px; color: var(--text-muted); }
.mono { font-family: 'DM Mono', monospace; font-size: 11px; white-space: nowrap; }

.lb-epic-row td {
  font-weight: 700; font-size: 12px; padding: .55rem .6rem;
  background: #ede8f5; border-bottom: 1px solid var(--border);
}
.lb-subtotal-row td { background: #f0edf5; font-weight: 600; font-size: 11px; }
.lb-grand-total td { background: #ddd6ee; font-weight: 700; font-size: 12px; border-top: 2px solid var(--border); }
.lb-ar-total { font-weight: 700; }

/* Row status colors */
.lb-row--done td    { background: #EEFEEF; }
.lb-row--progress td { background: #ECF5F8; }
.lb-row--planned td  { background: #F5F5F5; }
</style>
