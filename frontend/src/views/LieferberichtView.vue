<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { api, errMsg } from '@/api/client'
import { useAuthStore } from '@/stores/auth'
import { LS_LIEFERBERICHT_COLS, LS_LIEFERBERICHT_LANG } from '@/constants/storage'
import AppIcon from '@/components/AppIcon.vue'
import MetaSelect from '@/components/MetaSelect.vue'
import type { MetaOption } from '@/components/MetaSelect.vue'
import TagChip from '@/components/TagChip.vue'
import type { Tag } from '@/types'

const { t } = useI18n()
const auth = useAuthStore()

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

// ── Sprints + tags (loaded when project changes) ─────────────────────────────
interface Sprint { id: number; title: string }
const sprints = ref<Sprint[]>([])
const projectTags = ref<Tag[]>([])

watch(selectedProject, async (pid) => {
  sprints.value = []
  selectedSprint.value = ''
  projectTags.value = []
  filterTagIDs.value = []
  if (!pid) return
  try { sprints.value = await api.get<Sprint[]>('/sprints') } catch { /* empty */ }
  try { projectTags.value = await api.get<Tag[]>(`/projects/${pid}/tags`) } catch { /* empty */ }
})

// ── Tag + status filters (PAI-404) ───────────────────────────────────────────
// Multi-select chips. Empty array → no narrowing; otherwise filters AND on top
// of the scope preset, so picking statuses excluded by scope=all_open yields
// no rows (acceptable — user should switch scope).
const filterTagIDs = ref<number[]>([])
const filterStatuses = ref<string[]>([])
const allStatuses = ['new', 'backlog', 'in-progress', 'qa', 'done', 'delivered', 'accepted', 'invoiced', 'cancelled'] as const
function toggleTagID(id: number) {
  const i = filterTagIDs.value.indexOf(id)
  if (i === -1) filterTagIDs.value.push(id)
  else filterTagIDs.value.splice(i, 1)
}
function toggleStatus(s: string) {
  const i = filterStatuses.value.indexOf(s)
  if (i === -1) filterStatuses.value.push(s)
  else filterStatuses.value.splice(i, 1)
}

const sprintOptions = computed<MetaOption[]>(() =>
  sprints.value.map(s => ({ value: String(s.id), label: s.title }))
)

// ── Report language (PAI-402) ────────────────────────────────────────────────
// Default: localStorage override → user's profile locale → 'en'. The picker
// only drives THIS report; it doesn't write back to users.locale.
type Lang = 'en' | 'de'
const langOptions: MetaOption[] = [
  { value: 'en', label: 'English' },
  { value: 'de', label: 'Deutsch' },
]
function loadInitialLang(): Lang {
  const stored = localStorage.getItem(LS_LIEFERBERICHT_LANG)
  if (stored === 'en' || stored === 'de') return stored
  const userLocale = auth.user?.locale
  if (userLocale === 'en' || userLocale === 'de') return userLocale
  return 'en'
}
const reportLang = ref<Lang>(loadInitialLang())
watch(reportLang, (v) => localStorage.setItem(LS_LIEFERBERICHT_LANG, v))

// ── Numeric column visibility (PAI-400) ──────────────────────────────────────
interface ColSet { sp: boolean; h: boolean; arSp: boolean; arH: boolean; arEur: boolean }
const defaultCols: ColSet = { sp: true, h: true, arSp: true, arH: true, arEur: true }
function loadCols(): ColSet {
  try {
    const raw = localStorage.getItem(LS_LIEFERBERICHT_COLS)
    if (!raw) return { ...defaultCols }
    const parsed = JSON.parse(raw) as Partial<ColSet>
    return { ...defaultCols, ...parsed }
  } catch {
    return { ...defaultCols }
  }
}
const cols = ref<ColSet>(loadCols())
watch(cols, (v) => localStorage.setItem(LS_LIEFERBERICHT_COLS, JSON.stringify(v)), { deep: true })

const anyNumeric = computed(() => cols.value.sp || cols.value.h || cols.value.arSp || cols.value.arH || cols.value.arEur)
const visibleColsParam = computed(() => {
  const xs: string[] = []
  if (cols.value.sp)    xs.push('sp')
  if (cols.value.h)     xs.push('h')
  if (cols.value.arSp)  xs.push('ar_sp')
  if (cols.value.arH)   xs.push('ar_h')
  if (cols.value.arEur) xs.push('ar_eur')
  // Empty string is meaningful — explicitly "no numeric columns".
  return xs.join(',')
})

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

function buildParams(includeCols: boolean): string {
  const params = new URLSearchParams()
  params.set('scope', scope.value)
  if (scope.value === 'sprint' && selectedSprint.value) {
    params.set('sprint_ids', selectedSprint.value)
  }
  if (scope.value === 'date_range') {
    if (fromDate.value) params.set('from', fromDate.value)
    if (toDate.value) params.set('to', toDate.value)
  }
  params.set('lang', reportLang.value)
  if (includeCols) params.set('cols', visibleColsParam.value)
  // PAI-404: tag + status filters. Omit when empty to keep URLs short and
  // preserve back-compat.
  if (filterTagIDs.value.length > 0) params.set('tag_ids', filterTagIDs.value.join(','))
  if (filterStatuses.value.length > 0) params.set('statuses', filterStatuses.value.join(','))
  return params.toString()
}

async function generate() {
  if (!selectedProject.value) return
  loading.value = true
  error.value = ''
  report.value = null
  try {
    // JSON preview always returns the full record; the on-screen table hides
    // columns client-side. Cols are only sent on the PDF download.
    const data = await api.get<LbReport>(`/projects/${selectedProject.value}/reports/lieferbericht?${buildParams(false)}`)
    report.value = data
  } catch (e: unknown) {
    error.value = errMsg(e, t('lieferbericht.errorGenerate'))
  } finally {
    loading.value = false
  }
}

function downloadPDF() {
  if (!selectedProject.value) return
  window.open(`/api/projects/${selectedProject.value}/reports/lieferbericht/pdf?${buildParams(true)}`, '_blank')
}

// ── Row coloring + localized status ──────────────────────────────────────────
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
      return t('lieferbericht.status.delivered')
    case 'in-progress': case 'qa':
      return t('lieferbericht.status.inProgress')
    default:
      return t('lieferbericht.status.planned')
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
    <span class="ah-title">{{ t('lieferbericht.title') }}</span>
    <span class="ah-subtitle">{{ t('lieferbericht.subtitle') }}</span>
  </Teleport>

  <!-- Filter bar -->
  <div class="lb-filters">
    <div class="lb-filter-group">
      <label>{{ t('lieferbericht.filters.project') }}</label>
      <MetaSelect v-model="selectedProject" :options="projectOptions" :placeholder="t('lieferbericht.filters.projectPlaceholder')" />
    </div>

    <div class="lb-filter-group">
      <label>{{ t('lieferbericht.filters.scope') }}</label>
      <div class="lb-scope-toggle">
        <button :class="['mode-btn', { active: scope === 'all_open' }]" @click="scope = 'all_open'">{{ t('lieferbericht.scope.allOpen') }}</button>
        <button :class="['mode-btn', { active: scope === 'sprint' }]" @click="scope = 'sprint'">{{ t('lieferbericht.scope.sprint') }}</button>
        <button :class="['mode-btn', { active: scope === 'date_range' }]" @click="scope = 'date_range'">{{ t('lieferbericht.scope.dateRange') }}</button>
      </div>
    </div>

    <template v-if="scope === 'sprint'">
      <div class="lb-filter-group">
        <label>{{ t('lieferbericht.filters.sprint') }}</label>
        <MetaSelect v-model="selectedSprint" :options="sprintOptions" :placeholder="t('lieferbericht.filters.sprintPlaceholder')" />
      </div>
    </template>

    <template v-if="scope === 'date_range'">
      <div class="lb-filter-group">
        <label>{{ t('lieferbericht.filters.from') }}</label>
        <input v-model="fromDate" type="date" />
      </div>
      <div class="lb-filter-group">
        <label>{{ t('lieferbericht.filters.to') }}</label>
        <input v-model="toDate" type="date" />
      </div>
    </template>

    <div class="lb-filter-group">
      <label>{{ t('lieferbericht.filters.language') }}</label>
      <MetaSelect v-model="reportLang" :options="langOptions" />
    </div>

    <div class="lb-filter-group lb-cols">
      <label>{{ t('lieferbericht.filters.columns') }}</label>
      <div class="lb-col-toggles">
        <label class="lb-col-check"><input type="checkbox" v-model="cols.sp" /> {{ t('lieferbericht.table.sp') }}</label>
        <label class="lb-col-check"><input type="checkbox" v-model="cols.h" /> {{ t('lieferbericht.table.hours') }}</label>
        <label class="lb-col-check"><input type="checkbox" v-model="cols.arSp" /> {{ t('lieferbericht.table.arSp') }}</label>
        <label class="lb-col-check"><input type="checkbox" v-model="cols.arH" /> {{ t('lieferbericht.table.arHours') }}</label>
        <label class="lb-col-check"><input type="checkbox" v-model="cols.arEur" /> {{ t('lieferbericht.table.arEur') }} EUR</label>
      </div>
    </div>

    <!-- PAI-404: Tag filter (chips). Hidden when project has no tags. -->
    <div v-if="projectTags.length > 0" class="lb-filter-group lb-chips">
      <label>{{ t('lieferbericht.filters.tags') }}</label>
      <div class="lb-chip-toggles">
        <button
          v-for="tg in projectTags"
          :key="tg.id"
          :class="['lb-chip-btn', { 'lb-chip-btn--on': filterTagIDs.includes(tg.id) }]"
          type="button"
          @click="toggleTagID(tg.id)"
        >
          <TagChip :tag="tg" />
        </button>
      </div>
    </div>

    <!-- PAI-404: Status filter (chips). -->
    <div class="lb-filter-group lb-chips">
      <label>{{ t('lieferbericht.filters.status') }}</label>
      <div class="lb-chip-toggles">
        <button
          v-for="s in allStatuses"
          :key="s"
          :class="['lb-chip-btn', 'lb-status-pill', `lb-status-pill--${s}`, { 'lb-chip-btn--on': filterStatuses.includes(s) }]"
          type="button"
          @click="toggleStatus(s)"
        >{{ t(`status.${s}`) }}</button>
      </div>
    </div>

    <div class="lb-filter-actions">
      <button class="btn btn-primary" :disabled="loading || !canGenerate" @click="generate">
        <AppIcon v-if="loading" name="loader" :size="14" class="spin" />
        <AppIcon v-else name="bar-chart-2" :size="14" />
        {{ loading ? t('lieferbericht.actions.generating') : t('lieferbericht.actions.generate') }}
      </button>
      <button v-if="report" class="btn btn-secondary" :disabled="loading" @click="downloadPDF">
        <AppIcon name="download" :size="14" />
        {{ t('lieferbericht.actions.downloadPdf') }}
      </button>
    </div>
  </div>

  <!-- Error -->
  <div v-if="error" class="lb-error">{{ error }}</div>

  <!-- Empty state -->
  <div v-if="report && totalIssues === 0" class="lb-empty">
    <AppIcon name="inbox" :size="28" class="lb-empty-icon" />
    <p>{{ t('lieferbericht.empty') }}</p>
  </div>

  <!-- Report table -->
  <div v-if="report && totalIssues > 0" class="lb-table-wrap">
    <table class="lb-table">
      <thead>
        <tr>
          <th>{{ t('lieferbericht.table.key') }}</th>
          <th>{{ t('lieferbericht.table.type') }}</th>
          <th class="col-title">{{ t('lieferbericht.table.summary') }}</th>
          <th>{{ t('lieferbericht.table.status') }}</th>
          <th v-if="cols.sp"    class="col-num">{{ t('lieferbericht.table.sp') }}</th>
          <th v-if="cols.h"     class="col-num">{{ t('lieferbericht.table.hours') }}</th>
          <th v-if="cols.arSp"  class="col-num">{{ t('lieferbericht.table.arPerSp') }}</th>
          <th v-if="cols.arSp"  class="col-num">{{ t('lieferbericht.table.arSp') }}</th>
          <th v-if="cols.arH"   class="col-num">{{ t('lieferbericht.table.arPerHour') }}</th>
          <th v-if="cols.arH"   class="col-num">{{ t('lieferbericht.table.arHours') }}</th>
          <th v-if="cols.arEur" class="col-num">{{ t('lieferbericht.table.arEur') }}</th>
          <th class="col-desc">{{ t('lieferbericht.table.description') }}</th>
        </tr>
      </thead>
      <tbody v-for="g in report.groups" :key="g.epic_key">
        <tr class="lb-epic-row">
          <td :colspan="4 + (cols.sp ? 1 : 0) + (cols.h ? 1 : 0) + (cols.arSp ? 2 : 0) + (cols.arH ? 2 : 0) + (cols.arEur ? 1 : 0) + 1">{{ g.epic_key }}<span v-if="g.epic_title && g.epic_title !== g.epic_key"> — {{ g.epic_title }}</span></td>
        </tr>
        <tr v-for="issue in g.issues" :key="issue.issue_key" :class="rowClass(issue.status)">
          <td class="mono">{{ issue.issue_key }}</td>
          <td>{{ issue.type }}</td>
          <td class="col-title">{{ issue.title }}</td>
          <td>{{ statusLabel(issue.status) }}</td>
          <td v-if="cols.sp"    class="col-num">{{ fmt(issue.estimate_lp) }}</td>
          <td v-if="cols.h"     class="col-num">{{ fmt(issue.estimate_hours) }}</td>
          <td v-if="cols.arSp"  class="col-num">{{ fmt(issue.rate_lp) }}</td>
          <td v-if="cols.arSp"  class="col-num">{{ fmt(issue.ar_lp) }}</td>
          <td v-if="cols.arH"   class="col-num">{{ fmt(issue.rate_hourly) }}</td>
          <td v-if="cols.arH"   class="col-num">{{ fmt(issue.ar_hours) }}</td>
          <td v-if="cols.arEur" class="col-num lb-ar-total">{{ fmt(issue.ar_eur) }}</td>
          <td class="col-desc">{{ issue.description }}</td>
        </tr>
        <!-- Subtotal: numeric grid when at least one numeric col is visible,
             otherwise (PAI-401) a single "{N} issues" cell to the right of the label. -->
        <tr class="lb-subtotal-row">
          <template v-if="anyNumeric">
            <td colspan="4" style="text-align:right;font-weight:700">{{ t('lieferbericht.table.subtotal') }}</td>
            <td v-if="cols.sp"    class="col-num">{{ fmt(g.subtotal.estimate_lp) }}</td>
            <td v-if="cols.h"     class="col-num">{{ fmt(g.subtotal.estimate_hours) }}</td>
            <td v-if="cols.arSp"  class="col-num"></td>
            <td v-if="cols.arSp"  class="col-num">{{ fmt(g.subtotal.ar_lp) }}</td>
            <td v-if="cols.arH"   class="col-num"></td>
            <td v-if="cols.arH"   class="col-num">{{ fmt(g.subtotal.ar_hours) }}</td>
            <td v-if="cols.arEur" class="col-num lb-ar-total">{{ fmt(g.subtotal.ar_eur) }}</td>
            <td></td>
          </template>
          <template v-else>
            <td colspan="4" style="text-align:right;font-weight:700">{{ t('lieferbericht.table.subtotal') }}</td>
            <td class="col-num lb-ar-total">{{ g.issues.length }} {{ t('lieferbericht.table.issuesUnit') }}</td>
          </template>
        </tr>
      </tbody>
      <tfoot>
        <tr class="lb-grand-total">
          <template v-if="anyNumeric">
            <td colspan="4" style="text-align:right;font-weight:700">{{ t('lieferbericht.table.grandTotal') }}</td>
            <td v-if="cols.sp"    class="col-num">{{ fmt(report.grand_total.estimate_lp) }}</td>
            <td v-if="cols.h"     class="col-num">{{ fmt(report.grand_total.estimate_hours) }}</td>
            <td v-if="cols.arSp"  class="col-num"></td>
            <td v-if="cols.arSp"  class="col-num">{{ fmt(report.grand_total.ar_lp) }}</td>
            <td v-if="cols.arH"   class="col-num"></td>
            <td v-if="cols.arH"   class="col-num">{{ fmt(report.grand_total.ar_hours) }}</td>
            <td v-if="cols.arEur" class="col-num lb-ar-total">{{ fmt(report.grand_total.ar_eur) }}</td>
            <td></td>
          </template>
          <template v-else>
            <td colspan="4" style="text-align:right;font-weight:700">{{ t('lieferbericht.table.grandTotal') }}</td>
            <td class="col-num lb-ar-total">{{ totalIssues }} {{ t('lieferbericht.table.issuesUnit') }}</td>
          </template>
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

/* Column toggle checkboxes (PAI-400) */
.lb-cols { min-width: 0; }
.lb-col-toggles { display: flex; gap: .75rem; flex-wrap: wrap; align-items: center; padding: .35rem 0; }
.lb-col-check { display: inline-flex; align-items: center; gap: .3rem; font-size: 12px; color: var(--text); cursor: pointer; text-transform: none; letter-spacing: normal; font-weight: 400; }
.lb-col-check input { margin: 0; cursor: pointer; }

/* Tag + status chip toggles (PAI-404). Off = muted/outlined; on = full color. */
.lb-chips { flex-basis: 100%; }
.lb-chip-toggles { display: flex; flex-wrap: wrap; gap: .3rem; align-items: center; padding: .25rem 0; }
.lb-chip-btn {
  background: transparent; border: 1px solid transparent; padding: 0;
  border-radius: 20px; cursor: pointer; line-height: 0; opacity: 0.5;
  transition: opacity .12s ease;
}
.lb-chip-btn:hover { opacity: 0.85; }
.lb-chip-btn--on { opacity: 1; border-color: var(--bp-blue); box-shadow: 0 0 0 1px var(--bp-blue); }

/* Status pills */
.lb-status-pill {
  font-size: 11px; font-weight: 600; padding: .2rem .6rem;
  border-radius: 20px; line-height: 1.6;
  background: var(--bg); color: var(--text-muted); border: 1px solid var(--border);
  font-family: inherit;
}
.lb-status-pill--done,
.lb-status-pill--delivered,
.lb-status-pill--accepted,
.lb-status-pill--invoiced { background: #EEFEEF; color: #1f6f2f; }
.lb-status-pill--in-progress,
.lb-status-pill--qa { background: #ECF5F8; color: #1f4d75; }
.lb-status-pill--cancelled { background: #fde8e8; color: #7a1f1f; text-decoration: line-through; }

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
