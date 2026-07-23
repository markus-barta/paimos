<script setup lang="ts">
/*
 * PAIMOS — Your Professional & Personal AI Project OS
 * Copyright (C) 2026 Markus Barta <markus@barta.com>
 *
 * PAI-405: Export → Lieferbericht PDF from IssueList. Reads the caller's
 * already-active filter state (status, tags, sprints) and combines it with
 * the report-specific options (language + numeric columns) before opening
 * the PDF endpoint in a new tab. The IssueList's filters that don't yet
 * map to the Lieferbericht endpoint (assignee, priority, search) are
 * surfaced as a "these will be ignored" notice rather than silently
 * dropped.
 */
import { ref, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import { LS_LIEFERBERICHT_COLS, LS_LIEFERBERICHT_LANG, LS_LIEFERBERICHT_TEXT_SOURCE } from '@/constants/storage'
import { isNeg, posOf, OTHER_STATUS_SENTINEL } from '@/composables/useIssueFilter'
import { STATUSES, ACCRUALS_DEFAULT_STATUSES } from '@/constants/status'
import { STATUS_LABEL } from '@/composables/useIssueDisplay'
import type { Issue } from '@/types'
import AppModal from '@/components/AppModal.vue'
import BulkGenerateSummaryModal from '@/components/BulkGenerateSummaryModal.vue'
import MetaSelect from '@/components/MetaSelect.vue'
import type { MetaOption } from '@/components/MetaSelect.vue'

const { t } = useI18n()
const auth = useAuthStore()

const props = defineProps<{
  open: boolean
  projectId: number
  /** Status keys from IssueList's filterStatus. */
  filterStatus: string[]
  filterType: string[]
  filterPriority: string[]
  filterAssignee: string[]
  filterCostUnit: string[]
  filterRelease: string[]
  /** Tag IDs as strings, from IssueList's filterTags. */
  filterTags: string[]
  /** Sprint IDs as strings, from IssueList's filterSprints. */
  filterSprints: string[]
  dateField: string
  dateFrom: string
  dateTo: string
  /** Friction notice list — IssueList filter keys with active values that
   *  the Lieferbericht endpoint doesn't yet honor. Shown to the user so
   *  they aren't silently lost. */
  unsupportedActive: string[]
  /** PAI-418 / PAI-423. The issues actually included in the export
   *  (after the IssueList filter has resolved). Used to compute the
   *  "X of Y issues have a report summary" coverage line and to feed
   *  the bulk generator the missing-summary IDs. Optional so callers
   *  that don't yet pass it don't crash; the coverage hint just hides. */
  inScopeIssues?: Issue[]
}>()
const emit = defineEmits<{
  close: []
  /** Forwarded from the bulk generate modal so the host (IssueList)
   *  can re-emit `updated` and refresh affected rows in place. */
  updated: [issue: Issue]
}>()

type Lang = 'en' | 'de'
const langOptions: MetaOption[] = [
  { value: 'en', label: 'English' },
  { value: 'de', label: 'Deutsch' },
]
function initialLang(): Lang {
  const stored = localStorage.getItem(LS_LIEFERBERICHT_LANG)
  if (stored === 'en' || stored === 'de') return stored
  const userLocale = auth.user?.locale
  if (userLocale === 'en' || userLocale === 'de') return userLocale
  return 'en'
}

// PAI-418 / PAI-423. Which text variant the PDF body cells render.
// "tech"   = issue.description (default, legacy behavior)
// "report" = issue.report_summary, with description fallback + a
//            visible "[keine Kundenfassung]" tag for missing rows
type TextSource = 'tech' | 'report'
const textSourceOptions = computed<MetaOption[]>(() => ([
  { value: 'tech', label: t('lieferbericht.exportModal.textSourceTech') },
  { value: 'report', label: t('lieferbericht.exportModal.textSourceReport') },
]))
function initialTextSource(): TextSource {
  const stored = localStorage.getItem(LS_LIEFERBERICHT_TEXT_SOURCE)
  return stored === 'report' ? 'report' : 'tech'
}

interface ColSet { sp: boolean; h: boolean; arSp: boolean; arH: boolean; arEur: boolean; bookedBy: boolean }
const defaultCols: ColSet = { sp: true, h: true, arSp: true, arH: true, arEur: true, bookedBy: false }
function initialCols(): ColSet {
  try {
    const raw = localStorage.getItem(LS_LIEFERBERICHT_COLS)
    if (!raw) return { ...defaultCols }
    return { ...defaultCols, ...(JSON.parse(raw) as Partial<ColSet>) }
  } catch { return { ...defaultCols } }
}

const lang = ref<Lang>(initialLang())
const cols = ref<ColSet>(initialCols())
const textSource = ref<TextSource>(initialTextSource())

// PAI-580: export scope. "current" = the caller's IssueList filter (legacy).
// "month" = tickets with ≥1 time booking in a [from,to] window (time_booked),
// shown as window-booked hours/material. The month picker is a convenience that
// fills the from/to fields, which are the SSOT the user can freely edit.
type ScopeMode = 'current' | 'month'
const LS_SCOPE_MODE = 'lieferbericht.scopeMode'
const LS_SCOPE_GROUP = 'lieferbericht.scopeGroup'
const LS_SCOPE_STATES = 'lieferbericht.scopeStates'

function pad2(n: number): string { return String(n).padStart(2, '0') }
function ymOf(d: Date): string { return `${d.getFullYear()}-${pad2(d.getMonth() + 1)}` }
function isoDate(d: Date): string { return `${d.getFullYear()}-${pad2(d.getMonth() + 1)}-${pad2(d.getDate())}` }
function prevMonthYM(): string {
  const d = new Date(); d.setDate(1); d.setMonth(d.getMonth() - 1)
  return ymOf(d)
}
function monthRange(ym: string): { from: string; to: string } {
  const [y, m] = ym.split('-').map(Number)
  if (!y || !m) { const r = monthRange(prevMonthYM()); return r }
  return { from: isoDate(new Date(y, m - 1, 1)), to: isoDate(new Date(y, m, 0)) }
}

const scopeMode = ref<ScopeMode>(localStorage.getItem(LS_SCOPE_MODE) === 'month' ? 'month' : 'current')
const monthPick = ref<string>(prevMonthYM())
const initRange = monthRange(monthPick.value)
const dateFrom = ref<string>(initRange.from)
const dateTo = ref<string>(initRange.to)
function initialGroup(): 'flat' | 'month' | 'epic' {
  const g = localStorage.getItem(LS_SCOPE_GROUP)
  return g === 'epic' || g === 'month' ? g : 'flat'
}
const groupMode = ref<'flat' | 'month' | 'epic'>(initialGroup())

function initialStates(): Set<string> {
  try {
    const raw = localStorage.getItem(LS_SCOPE_STATES)
    if (raw) {
      const arr = JSON.parse(raw) as string[]
      const valid = arr.filter((s) => (STATUSES as readonly string[]).includes(s))
      if (valid.length) return new Set(valid)
    }
  } catch { /* fall through to default */ }
  return new Set(ACCRUALS_DEFAULT_STATUSES)
}
const selectedStates = ref<Set<string>>(initialStates())
const stateOptions = STATUSES.map((s) => ({ value: s as string, label: STATUS_LABEL[s] ?? s }))

// Month quick-picker is sugar: changing it rewrites the from/to SSOT. Editing
// from/to directly is allowed and is NOT reflected back into the picker.
function onMonthPick() {
  const r = monthRange(monthPick.value)
  dateFrom.value = r.from
  dateTo.value = r.to
}
function toggleState(value: string, on: boolean) {
  const next = new Set(selectedStates.value)
  if (on) next.add(value); else next.delete(value)
  selectedStates.value = next
}

const monthScopeValid = computed(() =>
  scopeMode.value !== 'month' ||
  (!!dateFrom.value && !!dateTo.value && dateFrom.value <= dateTo.value && selectedStates.value.size > 0),
)

watch(scopeMode, (v) => localStorage.setItem(LS_SCOPE_MODE, v))
watch(groupMode, (v) => localStorage.setItem(LS_SCOPE_GROUP, v))
watch(selectedStates, (v) => localStorage.setItem(LS_SCOPE_STATES, JSON.stringify([...v])), { deep: true })

// PAI-418 / PAI-423. Coverage: how many in-scope tickets still lack a
// customer-facing report_summary? Only meaningful when the user
// picked the "report" text source; otherwise the coverage line hides.
// Computed from `inScopeIssues` (already filtered upstream by the
// caller's IssueList state).
const missingSummaryIssues = computed(() => {
  const list = props.inScopeIssues ?? []
  return list.filter(
    (i) =>
      !String(i.report_summary ?? '').trim() &&
      ['epic', 'cost_unit', 'ticket'].includes(i.type),
  )
})
const inScopeReportableTotal = computed(() => {
  const list = props.inScopeIssues ?? []
  return list.filter((i) => ['epic', 'cost_unit', 'ticket'].includes(i.type)).length
})
// Coverage is computed from the IssueList-filtered set, which is only the
// export scope in "current" mode; hide it for the month scope.
const showCoverageLine = computed(() => scopeMode.value === 'current' && textSource.value === 'report' && (props.inScopeIssues?.length ?? 0) > 0)

// State for the bulk Generate Missing flow (opens BulkGenerateSummaryModal
// over this one with the missing-summary IDs).
const showBulkGen = ref(false)
const bulkGenIds = ref<number[]>([])
function openGenerateMissing() {
  const ids = missingSummaryIssues.value.map((i) => i.id)
  if (ids.length === 0) return
  bulkGenIds.value = ids
  showBulkGen.value = true
}

// Sync the same prefs the dedicated report view uses so the choice
// persists across both entry points.
watch(lang, (v) => localStorage.setItem(LS_LIEFERBERICHT_LANG, v))
watch(cols, (v) => localStorage.setItem(LS_LIEFERBERICHT_COLS, JSON.stringify(v)), { deep: true })
watch(textSource, (v) => localStorage.setItem(LS_LIEFERBERICHT_TEXT_SOURCE, v))

const colsParam = computed(() => {
  const xs: string[] = []
  if (cols.value.sp)    xs.push('sp')
  if (cols.value.h)     xs.push('h')
  if (cols.value.arSp)  xs.push('ar_sp')
  if (cols.value.arH)   xs.push('ar_h')
  if (cols.value.arEur) xs.push('ar_eur')
  if (cols.value.bookedBy) xs.push('booked_by')
  return xs.join(',')
})

function encodeSignedList(values: string[], opts: { numeric: boolean; dropOtherStatus?: boolean } = { numeric: false }): string[] {
  const out: string[] = []
  for (const raw of values) {
    const neg = isNeg(raw)
    const value = posOf(raw)
    if (opts.dropOtherStatus && value === OTHER_STATUS_SENTINEL) continue
    if (opts.numeric) {
      const n = Number(value)
      if (!Number.isInteger(n) || n <= 0) continue
    }
    out.push(neg ? `!${value}` : value)
  }
  return out
}

function download() {
  // PAI-580: time-booked scope ignores the inherited IssueList filters
  // entirely — the month/window + state checkboxes are authoritative.
  if (scopeMode.value === 'month') {
    if (!monthScopeValid.value) return
    const p = new URLSearchParams()
    p.set('scope', 'time_booked')
    p.set('from', dateFrom.value)
    p.set('to', dateTo.value)
    p.set('group', groupMode.value)
    p.set('lang', lang.value)
    p.set('cols', colsParam.value)
    p.set('snapshot', '1')
    p.set('text_source', textSource.value)
    p.set('statuses', [...selectedStates.value].join(','))
    window.open(`/api/projects/${props.projectId}/reports/projektbericht/pdf?${p.toString()}`, '_blank')
    emit('close')
    return
  }
  const params = new URLSearchParams()
  // Sprint mapping: if exactly one or more sprints are filtered in the
  // IssueList, use scope=sprint with those IDs. Otherwise scope=date_range
  // with no from/to so the backend applies no time-window or default-status
  // narrowing — the user's explicit status filter (if any) is authoritative.
  const sprintIDs = encodeSignedList(props.filterSprints, { numeric: true }).filter(v => !isNeg(v))
  if (sprintIDs.length > 0) {
    params.set('scope', 'sprint')
    params.set('sprint_ids', sprintIDs.join(','))
  } else {
    params.set('scope', 'date_range')
  }
  params.set('lang', lang.value)
  params.set('cols', colsParam.value)
  params.set('snapshot', '1')
  params.set('text_source', textSource.value)
  const tagIDs = encodeSignedList(props.filterTags, { numeric: true })
  if (tagIDs.length > 0) params.set('tag_ids', tagIDs.join(','))
  const statuses = encodeSignedList(props.filterStatus, { numeric: false, dropOtherStatus: true })
  if (statuses.length > 0) params.set('statuses', statuses.join(','))
  const types = encodeSignedList(props.filterType, { numeric: false })
  if (types.length > 0) params.set('type', types.join(','))
  const priorities = encodeSignedList(props.filterPriority, { numeric: false })
  if (priorities.length > 0) params.set('priority', priorities.join(','))
  const assignees = encodeSignedList(props.filterAssignee, { numeric: false }).filter(v => !isNeg(v))
  if (assignees.length > 0) params.set('assignee_id', assignees.join(','))
  const costUnits = encodeSignedList(props.filterCostUnit, { numeric: false })
  if (costUnits.length > 0) params.set('cost_unit', costUnits.join(','))
  const releases = encodeSignedList(props.filterRelease, { numeric: false })
  if (releases.length > 0) params.set('release', releases.join(','))
  if (props.dateField && (props.dateFrom || props.dateTo)) {
    params.set('date_field', props.dateField)
    if (props.dateFrom) params.set('date_from', props.dateFrom)
    if (props.dateTo) params.set('date_to', props.dateTo)
  }
  window.open(`/api/projects/${props.projectId}/reports/projektbericht/pdf?${params.toString()}`, '_blank')
  emit('close')
}
</script>

<template>
  <AppModal :open="open" :title="t('lieferbericht.exportModal.title')" max-width="480px" @close="emit('close')">
    <div class="lb-export">
      <!-- PAI-580: export scope. Inline segmented toggle; "By month" reveals
           the time-booked controls (month convenience + from/to SSOT + states
           + grouping). -->
      <div class="lb-export-row">
        <label class="lb-export-label">{{ t('lieferbericht.exportModal.scope') }}</label>
        <div class="lb-export-seg" role="radiogroup">
          <button
            type="button" class="lb-export-seg-btn" :class="{ active: scopeMode === 'current' }"
            data-testid="lb-scope-current" role="radio" :aria-checked="scopeMode === 'current'"
            @click="scopeMode = 'current'"
          >{{ t('lieferbericht.exportModal.scopeCurrent') }}</button>
          <button
            type="button" class="lb-export-seg-btn" :class="{ active: scopeMode === 'month' }"
            data-testid="lb-scope-month" role="radio" :aria-checked="scopeMode === 'month'"
            @click="scopeMode = 'month'"
          >{{ t('lieferbericht.exportModal.scopeMonth') }}</button>
        </div>
      </div>

      <template v-if="scopeMode === 'month'">
        <div class="lb-export-row">
          <label class="lb-export-label">{{ t('lieferbericht.exportModal.month') }}</label>
          <div class="lb-export-monthrow">
            <input type="month" v-model="monthPick" data-testid="lb-month-pick" class="lb-export-input" @change="onMonthPick" />
            <span class="lb-export-daterange">
              <input type="date" v-model="dateFrom" data-testid="lb-date-from" class="lb-export-input" :aria-label="t('lieferbericht.filters.from')" />
              <span class="lb-export-dash">–</span>
              <input type="date" v-model="dateTo" data-testid="lb-date-to" class="lb-export-input" :aria-label="t('lieferbericht.filters.to')" />
            </span>
          </div>
          <p class="lb-export-hint">{{ t('lieferbericht.exportModal.monthHint') }}</p>
        </div>

        <div class="lb-export-row">
          <label class="lb-export-label">{{ t('lieferbericht.exportModal.includeStates') }}</label>
          <div class="lb-export-cols">
            <label v-for="o in stateOptions" :key="o.value" class="lb-export-check">
              <input
                type="checkbox" :data-testid="`lb-state-${o.value}`"
                :checked="selectedStates.has(o.value)"
                @change="toggleState(o.value, ($event.target as HTMLInputElement).checked)"
              /> {{ o.label }}
            </label>
          </div>
        </div>

        <div class="lb-export-row">
          <label class="lb-export-label">{{ t('lieferbericht.exportModal.grouping') }}</label>
          <div class="lb-export-seg">
            <button
              type="button" class="lb-export-seg-btn" :class="{ active: groupMode === 'flat' }"
              data-testid="lb-group-flat" @click="groupMode = 'flat'"
            >{{ t('lieferbericht.exportModal.groupFlat') }}</button>
            <button
              type="button" class="lb-export-seg-btn" :class="{ active: groupMode === 'month' }"
              data-testid="lb-group-month" @click="groupMode = 'month'"
            >{{ t('lieferbericht.exportModal.groupMonth') }}</button>
            <button
              type="button" class="lb-export-seg-btn" :class="{ active: groupMode === 'epic' }"
              data-testid="lb-group-epic" @click="groupMode = 'epic'"
            >{{ t('lieferbericht.exportModal.groupEpic') }}</button>
          </div>
        </div>
      </template>

      <div class="lb-export-row">
        <label class="lb-export-label">{{ t('lieferbericht.exportModal.textSource') }}</label>
        <MetaSelect v-model="textSource" :options="textSourceOptions" />
        <p class="lb-export-hint" v-if="textSource === 'report'">
          {{ t('lieferbericht.exportModal.textSourceReportHint') }} <code>[keine Kundenfassung]</code>.
        </p>
        <div class="lb-export-coverage" v-if="showCoverageLine">
          <template v-if="missingSummaryIssues.length === 0">
            <span class="lb-export-coverage--ok">
              ✓ {{ t('lieferbericht.exportModal.coverageAllOk', { count: inScopeReportableTotal }) }}
            </span>
          </template>
          <template v-else>
            <span>{{ t('lieferbericht.exportModal.coverageMissing', { missing: missingSummaryIssues.length, total: inScopeReportableTotal }) }}</span>
            <button type="button" class="btn btn-ghost btn-sm" @click="openGenerateMissing">
              {{ t('lieferbericht.exportModal.generateMissing') }}
            </button>
          </template>
        </div>
      </div>

      <div class="lb-export-row">
        <label class="lb-export-label">{{ t('lieferbericht.filters.language') }}</label>
        <MetaSelect v-model="lang" :options="langOptions" />
      </div>

      <div class="lb-export-row">
        <label class="lb-export-label">{{ t('lieferbericht.filters.columns') }}</label>
        <div class="lb-export-cols">
          <label class="lb-export-check"><input data-testid="lb-col-sp" type="checkbox" v-model="cols.sp" /> {{ t('lieferbericht.table.sp') }}</label>
          <label class="lb-export-check"><input data-testid="lb-col-h" type="checkbox" v-model="cols.h" /> {{ t('lieferbericht.table.hours') }}</label>
          <label class="lb-export-check"><input data-testid="lb-col-ar-sp" type="checkbox" v-model="cols.arSp" /> {{ t('lieferbericht.table.arSp') }}</label>
          <label class="lb-export-check"><input data-testid="lb-col-ar-h" type="checkbox" v-model="cols.arH" /> {{ t('lieferbericht.table.arHours') }}</label>
          <label class="lb-export-check"><input data-testid="lb-col-ar-eur" type="checkbox" v-model="cols.arEur" /> {{ t('lieferbericht.table.arEur') }} EUR</label>
          <label class="lb-export-check"><input data-testid="lb-col-booked-by" type="checkbox" v-model="cols.bookedBy" /> {{ t('lieferbericht.exportModal.bookedByCol') }}</label>
        </div>
      </div>

      <div v-if="scopeMode === 'current' && unsupportedActive.length > 0" class="lb-export-warn">
        {{ t('lieferbericht.exportModal.ignoredFiltersPrefix') }} {{ unsupportedActive.join(', ') }}.
      </div>
      <div v-if="scopeMode === 'month' && selectedStates.size === 0" class="lb-export-warn">
        {{ t('lieferbericht.exportModal.noStates') }}
      </div>

      <div class="lb-export-actions">
        <button class="btn btn-ghost" @click="emit('close')">{{ t('lieferbericht.exportModal.cancel') }}</button>
        <button class="btn btn-primary" data-testid="lb-download" :disabled="!monthScopeValid" @click="download">
          {{ t('lieferbericht.actions.downloadPdf') }}
        </button>
      </div>
    </div>

    <!-- PAI-418 / PAI-423. Generate missing bulk modal, layered on top
         of the export modal. Closes back to the export modal so the
         user can immediately download with the freshly-filled rows.
         inScopeIssues is forwarded so the bulk modal's PAI-438 /
         PAI-441 filter toggles can look up status + existing-summary
         state. -->
    <BulkGenerateSummaryModal
      :open="showBulkGen"
      :issue-ids="bulkGenIds"
      :in-scope-issues="props.inScopeIssues"
      @close="showBulkGen = false"
      @updated="(issue) => emit('updated', issue)"
      @done="showBulkGen = false"
    />
  </AppModal>
</template>

<style scoped>
.lb-export { display: flex; flex-direction: column; gap: 1rem; }
.lb-export-row { display: flex; flex-direction: column; gap: .35rem; }
.lb-export-label { font-size: 11px; font-weight: 700; text-transform: uppercase; letter-spacing: .06em; color: var(--text-muted); }
.lb-export-hint { margin: .35rem 0 0; font-size: 12px; color: var(--text-muted); line-height: 1.45; }
.lb-export-hint code { background: var(--bg); padding: 0 .3em; border-radius: 3px; font-size: 11px; }
.lb-export-coverage { display: flex; align-items: center; justify-content: space-between; gap: .65rem; margin-top: .35rem; font-size: 12px; color: var(--text); }
.lb-export-coverage--ok { color: var(--text-muted); }
.lb-export-cols { display: flex; gap: .75rem; flex-wrap: wrap; }
.lb-export-seg { display: inline-flex; border: 1px solid var(--border); border-radius: 6px; overflow: hidden; width: fit-content; }
.lb-export-seg-btn {
  appearance: none; border: 0; background: var(--bg-card); color: var(--text);
  padding: .4rem .8rem; font-size: 13px; cursor: pointer; border-right: 1px solid var(--border);
}
.lb-export-seg-btn:last-child { border-right: 0; }
.lb-export-seg-btn.active { background: var(--brand-blue); color: #fff; }
.lb-export-monthrow { display: flex; flex-wrap: wrap; align-items: center; gap: .6rem; }
.lb-export-daterange { display: inline-flex; align-items: center; gap: .35rem; }
.lb-export-dash { color: var(--text-muted); }
.lb-export-input {
  font-size: 13px; padding: .35rem .5rem; border: 1px solid var(--border);
  border-radius: 6px; background: var(--bg-card); color: var(--text);
}
.lb-export-actions .btn[disabled] { opacity: .5; cursor: not-allowed; }
.lb-export-check { display: inline-flex; align-items: center; gap: .3rem; font-size: 12px; color: var(--text); cursor: pointer; }
.lb-export-warn {
  font-size: 12px; color: #7a5b1f;
  background: #fff8e1; border: 1px solid #f1d68b;
  border-radius: var(--radius); padding: .5rem .65rem;
}
.lb-export-actions { display: flex; justify-content: flex-end; gap: .5rem; margin-top: .25rem; }
</style>
