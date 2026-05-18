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

interface ColSet { sp: boolean; h: boolean; arSp: boolean; arH: boolean; arEur: boolean }
const defaultCols: ColSet = { sp: true, h: true, arSp: true, arH: true, arEur: true }
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
const showCoverageLine = computed(() => textSource.value === 'report' && (props.inScopeIssues?.length ?? 0) > 0)

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
          <label class="lb-export-check"><input type="checkbox" v-model="cols.sp" />    {{ t('lieferbericht.table.sp') }}</label>
          <label class="lb-export-check"><input type="checkbox" v-model="cols.h" />     {{ t('lieferbericht.table.hours') }}</label>
          <label class="lb-export-check"><input type="checkbox" v-model="cols.arSp" />  {{ t('lieferbericht.table.arSp') }}</label>
          <label class="lb-export-check"><input type="checkbox" v-model="cols.arH" />   {{ t('lieferbericht.table.arHours') }}</label>
          <label class="lb-export-check"><input type="checkbox" v-model="cols.arEur" /> {{ t('lieferbericht.table.arEur') }} EUR</label>
        </div>
      </div>

      <div v-if="unsupportedActive.length > 0" class="lb-export-warn">
        {{ t('lieferbericht.exportModal.ignoredFiltersPrefix') }} {{ unsupportedActive.join(', ') }}.
      </div>

      <div class="lb-export-actions">
        <button class="btn btn-ghost" @click="emit('close')">{{ t('lieferbericht.exportModal.cancel') }}</button>
        <button class="btn btn-primary" @click="download">
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
.lb-export-check { display: inline-flex; align-items: center; gap: .3rem; font-size: 12px; color: var(--text); cursor: pointer; }
.lb-export-warn {
  font-size: 12px; color: #7a5b1f;
  background: #fff8e1; border: 1px solid #f1d68b;
  border-radius: var(--radius); padding: .5rem .65rem;
}
.lb-export-actions { display: flex; justify-content: flex-end; gap: .5rem; margin-top: .25rem; }
</style>
