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
import { LS_LIEFERBERICHT_COLS, LS_LIEFERBERICHT_LANG } from '@/constants/storage'
import { isNeg, posOf, OTHER_STATUS_SENTINEL } from '@/composables/useIssueFilter'
import AppModal from '@/components/AppModal.vue'
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
}>()
const emit = defineEmits<{ close: [] }>()

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

// Sync the same prefs the dedicated report view uses so the choice
// persists across both entry points.
watch(lang, (v) => localStorage.setItem(LS_LIEFERBERICHT_LANG, v))
watch(cols, (v) => localStorage.setItem(LS_LIEFERBERICHT_COLS, JSON.stringify(v)), { deep: true })

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
  window.open(`/api/projects/${props.projectId}/reports/lieferbericht/pdf?${params.toString()}`, '_blank')
  emit('close')
}
</script>

<template>
  <AppModal :open="open" :title="t('lieferbericht.exportModal.title')" max-width="480px" @close="emit('close')">
    <div class="lb-export">
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
  </AppModal>
</template>

<style scoped>
.lb-export { display: flex; flex-direction: column; gap: 1rem; }
.lb-export-row { display: flex; flex-direction: column; gap: .35rem; }
.lb-export-label { font-size: 11px; font-weight: 700; text-transform: uppercase; letter-spacing: .06em; color: var(--text-muted); }
.lb-export-cols { display: flex; gap: .75rem; flex-wrap: wrap; }
.lb-export-check { display: inline-flex; align-items: center; gap: .3rem; font-size: 12px; color: var(--text); cursor: pointer; }
.lb-export-warn {
  font-size: 12px; color: #7a5b1f;
  background: #fff8e1; border: 1px solid #f1d68b;
  border-radius: var(--radius); padding: .5rem .65rem;
}
.lb-export-actions { display: flex; justify-content: flex-end; gap: .5rem; margin-top: .25rem; }
</style>
