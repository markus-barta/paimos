<script setup lang="ts">
/*
 * PAIMOS — Your Professional & Personal AI Project OS
 * Copyright (C) 2026 Markus Barta <markus@barta.com>
 *
 * PAI-418 / PAI-424 — bulk customer-facing report-summary generator.
 *
 * Shared by:
 *   - IssueList toolbar — "Generate report summary (AI)" on selected rows.
 *   - LieferberichtExportModal — "Generate missing →" for in-scope
 *     tickets that have no summary yet.
 *
 * Per-issue flow (one worker iteration):
 *   1. POST /api/ai/action with action=customer_rewrite|exec_summary,
 *      field=report_summary, issue_id=<id>. The backend handler reads
 *      description + AC from the issue row directly, so `text` stays
 *      empty.
 *   2. On success, PATCH /api/issues/{id} with { report_summary: text }.
 *   3. Emit `updated` per row so the host re-renders without a refetch.
 *
 * Features layered onto the loop:
 *
 *   - PAI-437. A 3-worker pool over a shared queue. At 585 rows with
 *     ~3s/call sequential, the user waited 30 min; with concurrency=3
 *     that drops to ~10. Each worker handles its own retries; the
 *     AbortController is shared so cancel propagates to all of them.
 *
 *   - PAI-438. Skip-already-summarized toggle (default ON). Drops
 *     rows where `report_summary` is already populated before the
 *     pool starts.
 *
 *   - PAI-439. ETA from a rolling-10 duration window, adjusted for
 *     concurrency: `(remaining * avg) / MAX_CONCURRENT`.
 *
 *   - PAI-440. 429 / 5xx / network retry with exponential back-off +
 *     jitter (up to 3 attempts per row). Sleep is signal-aware so
 *     cancel interrupts a backoff wait immediately.
 *
 *   - PAI-441. Exclude terminal-status rows (accepted, invoiced,
 *     cancelled) by default. Override checkbox to include them.
 *
 *   - PAI-442. Resume after accidental close — queue state persisted
 *     to localStorage between iterations. External close keeps the
 *     slot; explicit user cancel discards it.
 *
 *   - PAI-443. Sliding log of the last 5 results during the run.
 *     Latest at the top; success / failure / retry icons; first ~80
 *     chars of the generated text (or error message) inline.
 *
 *   - PAI-444. Inline status of in-flight rows — count + currently-
 *     retrying breakdown. Replaces the single "current label" — with
 *     a worker pool there's no single current row.
 *
 *   - PAI-445. Throughput line (items/min) alongside the ETA. Derived
 *     from the same rolling window data as the ETA, scaled by
 *     concurrency.
 *
 *   - PAI-446. Synchronous race guard on Generate: a non-reactive
 *     `busy` flag flipped before any await so a double-click within
 *     Vue's event batching window can't kick off two batches.
 *
 *   - PAI-449. Pre-run cost estimate via /api/ai/bulk-cost-estimate.
 *     Refreshes when the style or effective count changes.
 */
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { api, ApiError, errMsg } from '@/api/client'
import type { Issue } from '@/types'
import AppModal from '@/components/AppModal.vue'
import AppIcon from '@/components/AppIcon.vue'
import MetaSelect from '@/components/MetaSelect.vue'
import type { MetaOption } from '@/components/MetaSelect.vue'

const { t } = useI18n()

const props = defineProps<{
  open: boolean
  issueIds: number[]
  defaultStyle?: 'customer' | 'exec'
  inScopeIssues?: Issue[]
}>()

const emit = defineEmits<{
  close: []
  updated: [issue: Issue]
  done: []
}>()

// ── Types + state ────────────────────────────────────────────────

type Phase = 'config' | 'running' | 'done'
type Style = 'customer' | 'exec'

const styleOptions = computed<MetaOption[]>(() => ([
  { value: 'customer', label: t('reportSummary.bulk.styleCustomer') },
  { value: 'exec', label: t('reportSummary.bulk.styleExec') },
]))
const subKeyOptions = computed<MetaOption[]>(() => ([
  { value: 'release_note', label: t('reportSummary.bulk.toneReleaseNote') },
  { value: 'feature', label: t('reportSummary.bulk.toneFeature') },
  { value: 'fix', label: t('reportSummary.bulk.toneFix') },
  { value: 'stability', label: t('reportSummary.bulk.toneStability') },
  { value: 'security_hardening', label: t('reportSummary.bulk.toneSecurity') },
]))

const phase = ref<Phase>('config')
const style = ref<Style>(props.defaultStyle ?? 'customer')
const subKey = ref<string>('release_note')
const skipFilled = ref(true)
const includeTerminal = ref(false)

const completed = ref(0)
const failed = ref<{ issueId: number; error: string }[]>([])
const updatedIds = ref<number[]>([])

// PAI-444. In-flight tracking — set of issue IDs currently being
// processed by one of the workers. Replaces the single "current"
// label that the sequential implementation had.
const inFlightIds = ref<Set<number>>(new Set())
const inFlightCount = computed(() => inFlightIds.value.size)
// Retry breakdown: maps row id → human-readable note (e.g.
// "attempt 2, waiting 4s"). Lets the UI tell the user "1 of 3 is
// retrying" without piling retry events into the recent log.
const retryingMap = ref<Map<number, string>>(new Map())
const retryingCount = computed(() => retryingMap.value.size)

// PAI-443. Sliding log of the last MAX_RECENT completed iterations
// (success or failure). Latest first.
interface RecentEntry {
  id: number
  status: 'ok' | 'fail'
  preview: string
}
const recent = ref<RecentEntry[]>([])

let runAbort: AbortController | null = null
// PAI-442. Distinguishes a user-pressed cancel from an external
// force-close. User cancel discards the resume slot; external
// close (route change, parent toggle) keeps it.
let userCancelled = false
// PAI-446. Synchronous re-entrancy guard for start(). A reactive
// ref alone isn't enough — Vue batches event handlers, so two
// quick clicks both see `phase === 'config'` before the first
// awaiter has had a chance to flip it. The non-reactive `busy`
// flag is checked + set synchronously, before any await.
let busy = false
const pending = ref(false)

// Rolling-window duration tracker for ETA + throughput.
const rollingDurations = ref<number[]>([])

// Resume offer (PAI-442). Populated when the modal opens AND a
// fresh LS slot exists. Cleared when the user picks Resume / Discard.
interface ResumeSlot {
  style: Style
  subKey: string
  skipFilled: boolean
  includeTerminal: boolean
  remainingIds: number[]
  startedAt: string
  totalInitial: number
}
const resumeOffer = ref<ResumeSlot | null>(null)
const resumeOverrideIds = ref<number[] | null>(null)

// PAI-449. Cost-estimate payload.
interface CostEstimate {
  model: string
  pricingPromptPerMtok: number
  pricingCompletionPerMtok: number
  avgPromptTokens: number
  avgCompletionTokens: number
  sampleSize: number
  estMicroUSDLow: number
  estMicroUSDMid: number
  estMicroUSDHigh: number
  heuristicFallback: boolean
}
const costEstimate = ref<CostEstimate | null>(null)
const costEstimateError = ref<string>('')
const costEstimateLoading = ref(false)

// ── Constants ────────────────────────────────────────────────────

const TERMINAL_STATUSES = new Set(['accepted', 'invoiced', 'cancelled'])
const ROLLING_WINDOW = 10
const MAX_RETRY_ATTEMPTS = 3
const RETRY_BASE_MS = 2000
const RETRY_MAX_MS = 60_000
const LS_KEY = 'paimos:bulk-summary-run'
const RESUME_TTL_MS = 24 * 60 * 60 * 1000
// PAI-437. A small concurrency cap. 3 workers is the sweet spot for
// the LLM providers we use — fast enough to get a meaningful
// speedup on 100+ row batches without provoking rate-limit walls
// that PAI-440's retry path would have to keep dodging.
const MAX_CONCURRENT = 3
// PAI-443. Sliding-log capacity. Small enough to keep the running
// phase visually quiet; large enough to spot streaks of empty /
// short / failing rows early.
const MAX_RECENT = 5
const PREVIEW_CHAR_LIMIT = 80

// ── Derived (effective IDs after PAI-438 + PAI-441 filters) ──────

const issuesById = computed<Map<number, Issue>>(() => {
  const m = new Map<number, Issue>()
  for (const i of props.inScopeIssues ?? []) m.set(i.id, i)
  return m
})

const filterImpactSupported = computed(() => (props.inScopeIssues?.length ?? 0) > 0)

const effectiveIds = computed<number[]>(() => {
  const ids = props.issueIds
  if (!filterImpactSupported.value) return ids
  const map = issuesById.value
  return ids.filter((id) => {
    const i = map.get(id)
    if (!i) return true
    if (!includeTerminal.value && TERMINAL_STATUSES.has(i.status)) return false
    if (skipFilled.value && String(i.report_summary ?? '').trim()) return false
    return true
  })
})
const filteredOutCount = computed(() => props.issueIds.length - effectiveIds.value.length)
const total = computed(() => (resumeOverrideIds.value ?? effectiveIds.value).length)
const progressPct = computed(() => total.value === 0 ? 0 : Math.round((completed.value / total.value) * 100))

const avgDurationMs = computed(() => {
  const xs = rollingDurations.value
  if (xs.length === 0) return null
  return xs.reduce((a, b) => a + b, 0) / xs.length
})

// PAI-439. Concurrency-aware ETA. With N workers running in
// parallel, total wall time for the remaining queue is
// approximately (remaining × avg) ÷ N. When fewer items are left
// than workers, the parallelism cap clamps to the remaining count.
const etaMs = computed<number | null>(() => {
  if (avgDurationMs.value == null) return null
  const remaining = total.value - completed.value
  if (remaining <= 0) return null
  const workers = Math.min(MAX_CONCURRENT, Math.max(1, remaining))
  return (remaining * avgDurationMs.value) / workers
})
const etaLabel = computed<string>(() => etaMs.value == null ? '' : formatDuration(etaMs.value))

// PAI-445. items/min, scaled by effective parallelism.
const itemsPerMinute = computed<number | null>(() => {
  if (avgDurationMs.value == null || avgDurationMs.value <= 0) return null
  if (rollingDurations.value.length < 3) return null
  const workers = Math.min(MAX_CONCURRENT, Math.max(1, total.value - completed.value))
  return (60_000 / avgDurationMs.value) * workers
})
const throughputLabel = computed<string>(() => {
  if (itemsPerMinute.value == null) return ''
  const v = itemsPerMinute.value
  const rate = v < 10 ? v.toFixed(1) : String(Math.round(v))
  return t('reportSummary.bulk.throughput', { rate })
})

function formatDuration(ms: number): string {
  if (ms < 60_000) return t('reportSummary.bulk.etaSeconds', { s: Math.round(ms / 1000) })
  const totalSec = Math.round(ms / 1000)
  const min = Math.floor(totalSec / 60)
  const sec = totalSec % 60
  if (min >= 60) {
    const h = Math.floor(min / 60)
    const m = min % 60
    return t('reportSummary.bulk.etaHours', { h, m })
  }
  return sec > 0 && min < 10
    ? t('reportSummary.bulk.etaMinutesSeconds', { min, sec })
    : t('reportSummary.bulk.etaMinutes', { min })
}

// ── Resume / persistence (PAI-442) ───────────────────────────────

function readResumeSlot(): ResumeSlot | null {
  try {
    const raw = localStorage.getItem(LS_KEY)
    if (!raw) return null
    const parsed = JSON.parse(raw) as ResumeSlot
    if (!parsed || !Array.isArray(parsed.remainingIds) || parsed.remainingIds.length === 0) return null
    const startedAtMs = Date.parse(parsed.startedAt)
    if (!Number.isFinite(startedAtMs) || Date.now() - startedAtMs > RESUME_TTL_MS) {
      localStorage.removeItem(LS_KEY)
      return null
    }
    return parsed
  } catch {
    return null
  }
}

function writeResumeSlot(slot: ResumeSlot) {
  try {
    localStorage.setItem(LS_KEY, JSON.stringify(slot))
  } catch { /* quota — ignore, resume becomes best-effort */ }
}

function clearResumeSlot() {
  try { localStorage.removeItem(LS_KEY) } catch { /* ignore */ }
}

// ── Open / close lifecycle ───────────────────────────────────────

watch(
  () => props.open,
  (v) => {
    if (v) {
      phase.value = 'config'
      style.value = props.defaultStyle ?? 'customer'
      subKey.value = 'release_note'
      skipFilled.value = true
      includeTerminal.value = false
      completed.value = 0
      failed.value = []
      updatedIds.value = []
      inFlightIds.value = new Set()
      retryingMap.value = new Map()
      recent.value = []
      rollingDurations.value = []
      resumeOverrideIds.value = null
      costEstimate.value = null
      costEstimateError.value = ''
      busy = false
      pending.value = false
      userCancelled = false
      runAbort?.abort()
      runAbort = null
      resumeOffer.value = readResumeSlot()
    } else {
      runAbort?.abort()
    }
  },
)

function acceptResume() {
  const slot = resumeOffer.value
  if (!slot) return
  style.value = slot.style
  subKey.value = slot.subKey
  skipFilled.value = slot.skipFilled
  includeTerminal.value = slot.includeTerminal
  resumeOverrideIds.value = slot.remainingIds
  resumeOffer.value = null
}

function discardResume() {
  clearResumeSlot()
  resumeOffer.value = null
}

// ── Cost estimate (PAI-449) ──────────────────────────────────────

async function refreshCostEstimate() {
  if (phase.value !== 'config') return
  const n = total.value
  if (n === 0) { costEstimate.value = null; return }
  const action = style.value === 'exec' ? 'exec_summary' : 'customer_rewrite'
  costEstimateLoading.value = true
  costEstimateError.value = ''
  try {
    const res = await api.get<{
      model: string
      pricing_prompt_per_mtok: number
      pricing_completion_per_mtok: number
      avg_prompt_tokens: number
      avg_completion_tokens: number
      sample_size: number
      est_micro_usd_low: number
      est_micro_usd_mid: number
      est_micro_usd_high: number
      heuristic_fallback: boolean
    }>(`/ai/bulk-cost-estimate?action=${encodeURIComponent(action)}&n=${n}`)
    costEstimate.value = {
      model: res.model,
      pricingPromptPerMtok: res.pricing_prompt_per_mtok,
      pricingCompletionPerMtok: res.pricing_completion_per_mtok,
      avgPromptTokens: res.avg_prompt_tokens,
      avgCompletionTokens: res.avg_completion_tokens,
      sampleSize: res.sample_size,
      estMicroUSDLow: res.est_micro_usd_low,
      estMicroUSDMid: res.est_micro_usd_mid,
      estMicroUSDHigh: res.est_micro_usd_high,
      heuristicFallback: res.heuristic_fallback,
    }
  } catch (e) {
    costEstimate.value = null
    costEstimateError.value = errMsg(e, 'cost estimate unavailable')
  } finally {
    costEstimateLoading.value = false
  }
}

watch(
  [() => props.open, () => phase.value, style, total, () => resumeOverrideIds.value],
  () => { if (props.open && phase.value === 'config') void refreshCostEstimate() },
  { immediate: false },
)

function formatUSD(microUSD: number): string {
  const usd = microUSD / 1_000_000
  if (usd < 0.01) return `$${usd.toFixed(4)}`
  if (usd < 1) return `$${usd.toFixed(3)}`
  return `$${usd.toFixed(2)}`
}

// ── Per-row runner + retry (PAI-440) ─────────────────────────────

interface ActionEnvelope {
  body?: { optimized?: string }
}

function jitter(base: number, pct = 0.25): number {
  const variance = base * pct
  return base + (Math.random() * 2 - 1) * variance
}

function isRetryable(e: unknown): boolean {
  if (e instanceof ApiError) {
    if (e.status === 429) return true
    if (e.status >= 500 && e.status < 600) return true
    if (e.status === 0) return true
  }
  return false
}

function sleep(ms: number, signal: AbortSignal): Promise<void> {
  return new Promise<void>((resolve, reject) => {
    const t = setTimeout(() => {
      signal.removeEventListener('abort', onAbort)
      resolve()
    }, ms)
    function onAbort() {
      clearTimeout(t)
      reject(new DOMException('aborted', 'AbortError'))
    }
    signal.addEventListener('abort', onAbort, { once: true })
  })
}

async function runOneWithRetry(issueId: number, signal: AbortSignal): Promise<string> {
  const action = style.value === 'exec' ? 'exec_summary' : 'customer_rewrite'
  const payload: Record<string, unknown> = {
    action,
    field: 'report_summary',
    issue_id: issueId,
    text: '',
  }
  if (action === 'customer_rewrite') payload.sub_action = subKey.value

  let lastErr: unknown = null
  for (let attempt = 1; attempt <= MAX_RETRY_ATTEMPTS; attempt++) {
    if (signal.aborted) throw new DOMException('aborted', 'AbortError')
    if (attempt > 1) {
      const wait = Math.min(RETRY_MAX_MS, jitter(RETRY_BASE_MS * Math.pow(2, attempt - 2)))
      retryingMap.value.set(issueId, t('reportSummary.bulk.errAttemptWaiting', { attempt, seconds: Math.round(wait / 1000) }))
      try {
        await sleep(wait, signal)
      } finally {
        retryingMap.value.delete(issueId)
      }
      if (signal.aborted) throw new DOMException('aborted', 'AbortError')
    }
    try {
      const env = await api.post<ActionEnvelope>('/ai/action', payload, { signal, timeoutMs: 90_000 })
      const text = String(env.body?.optimized ?? '').trim()
      if (!text) throw new Error(t('reportSummary.bulk.errAiEmpty'))
      const updatedIssue = await api.patch<Issue>(`/issues/${issueId}`, { report_summary: text }, { signal })
      updatedIds.value.push(issueId)
      emit('updated', updatedIssue)
      return text
    } catch (e) {
      lastErr = e
      if (signal.aborted) throw e
      if (!isRetryable(e)) throw e
    }
  }
  throw lastErr ?? new Error('all retries failed')
}

function pushDuration(ms: number) {
  rollingDurations.value.push(ms)
  if (rollingDurations.value.length > ROLLING_WINDOW) rollingDurations.value.shift()
}

function pushRecent(e: RecentEntry) {
  recent.value = [e, ...recent.value].slice(0, MAX_RECENT)
}

function recentIcon(status: RecentEntry['status']): string {
  return status === 'ok' ? 'check' : 'x'
}

// ── Worker-pool driver (PAI-437) ─────────────────────────────────

async function start() {
  // PAI-446. Synchronous re-entrancy guard (see `busy` declaration).
  if (busy) return
  busy = true
  pending.value = true
  try {
    if (phase.value !== 'config') return
    const ids = [...(resumeOverrideIds.value ?? effectiveIds.value)]
    if (ids.length === 0) return

    phase.value = 'running'
    runAbort = new AbortController()
    const signal = runAbort.signal

    const todo = new Set<number>(ids)
    const slot: ResumeSlot = {
      style: style.value,
      subKey: subKey.value,
      skipFilled: skipFilled.value,
      includeTerminal: includeTerminal.value,
      remainingIds: [...ids],
      startedAt: new Date().toISOString(),
      totalInitial: ids.length,
    }
    writeResumeSlot(slot)

    let queueIdx = 0
    function nextId(): number | null {
      while (queueIdx < ids.length) {
        const id = ids[queueIdx++]
        if (id != null) return id
      }
      return null
    }

    async function worker(): Promise<void> {
      while (!signal.aborted) {
        const id = nextId()
        if (id == null) return
        inFlightIds.value = new Set(inFlightIds.value).add(id)
        const t0 = performance.now()
        try {
          const text = await runOneWithRetry(id, signal)
          pushDuration(performance.now() - t0)
          completed.value++
          pushRecent({ id, status: 'ok', preview: text.slice(0, PREVIEW_CHAR_LIMIT) })
        } catch (e) {
          if (signal.aborted) {
            const next = new Set(inFlightIds.value); next.delete(id); inFlightIds.value = next
            retryingMap.value.delete(id)
            return
          }
          pushDuration(performance.now() - t0)
          const msg = e instanceof ApiError ? errMsg(e, t('reportSummary.bulk.errAiCallFailed')) : (e as Error).message
          failed.value.push({ issueId: id, error: msg })
          completed.value++
          pushRecent({ id, status: 'fail', preview: msg.slice(0, PREVIEW_CHAR_LIMIT) })
        } finally {
          const next = new Set(inFlightIds.value); next.delete(id); inFlightIds.value = next
          retryingMap.value.delete(id)
          todo.delete(id)
          slot.remainingIds = [...todo]
          if (slot.remainingIds.length > 0) writeResumeSlot(slot)
          else clearResumeSlot()
        }
      }
    }

    const pool = Array.from({ length: Math.min(MAX_CONCURRENT, ids.length) }, () => worker())
    await Promise.all(pool)

    // PAI-442. Three cases on loop exit:
    //   - Normal completion (queue drained): clear the slot, nothing to resume.
    //   - User-initiated cancel: explicit "stop here", clear the slot too.
    //   - External close (parent force-close): keep the slot intact.
    if (!signal.aborted || userCancelled) {
      clearResumeSlot()
    }
    phase.value = 'done'
    if (signal.aborted) finishAndClose()
  } finally {
    busy = false
    pending.value = false
  }
}

function cancel() {
  if (phase.value === 'running') {
    userCancelled = true
    runAbort?.abort()
  }
  finishAndClose()
}

function finishAndClose() {
  emit('done')
  emit('close')
}
</script>

<template>
  <AppModal :open="open" :title="t('reportSummary.bulk.title')" max-width="620px" @close="cancel">
    <div class="bgs">
      <!-- PAI-442. Resume offer banner. -->
      <div v-if="phase === 'config' && resumeOffer" class="bgs-resume">
        <div class="bgs-resume-text">
          <strong>{{ t('reportSummary.bulk.resumeQuestion') }}</strong>
          <span>
            {{ t('reportSummary.bulk.resumeDetail', {
              remaining: resumeOffer.remainingIds.length,
              total: resumeOffer.totalInitial,
              started: new Date(resumeOffer.startedAt).toLocaleString(),
              style: resumeOffer.style === 'exec' ? t('reportSummary.bulk.styleExec') : t('reportSummary.bulk.styleCustomer'),
            }) }}
          </span>
        </div>
        <div class="bgs-resume-actions">
          <button class="btn btn-ghost btn-sm" @click="discardResume">{{ t('reportSummary.bulk.resumeDiscard') }}</button>
          <button class="btn btn-primary btn-sm" @click="acceptResume">{{ t('reportSummary.bulk.resumeAccept') }}</button>
        </div>
      </div>

      <template v-if="phase === 'config'">
        <p class="bgs-lead">
          {{ t('reportSummary.bulk.lead', { n: total, workers: MAX_CONCURRENT })
          }}<span v-if="filterImpactSupported && filteredOutCount > 0" class="bgs-muted">
            {{ ' ' }}{{ t('reportSummary.bulk.filteredFrom', { n: props.issueIds.length }) }}
          </span>
        </p>

        <div class="bgs-row">
          <label class="bgs-label">{{ t('reportSummary.bulk.style') }}</label>
          <MetaSelect v-model="style" :options="styleOptions" />
        </div>
        <div class="bgs-row" v-if="style === 'customer'">
          <label class="bgs-label">{{ t('reportSummary.bulk.toneBias') }}</label>
          <MetaSelect v-model="subKey" :options="subKeyOptions" />
        </div>

        <div v-if="filterImpactSupported" class="bgs-filters">
          <label class="bgs-check">
            <input type="checkbox" v-model="skipFilled" />
            <span>{{ t('reportSummary.bulk.skipFilled') }}</span>
          </label>
          <label class="bgs-check">
            <input type="checkbox" v-model="includeTerminal" />
            <span>
              {{ t('reportSummary.bulk.includeTerminal') }}
              (<code>accepted</code>, <code>invoiced</code>, <code>cancelled</code>)
            </span>
          </label>
        </div>

        <div class="bgs-cost">
          <template v-if="costEstimateLoading">
            <span class="bgs-muted">{{ t('reportSummary.bulk.costLoading') }}</span>
          </template>
          <template v-else-if="costEstimateError">
            <span class="bgs-muted">{{ t('reportSummary.bulk.costUnavailableError', { error: costEstimateError }) }}</span>
          </template>
          <template v-else-if="costEstimate && costEstimate.heuristicFallback">
            <span class="bgs-muted">{{ t('reportSummary.bulk.costUnavailableHeuristic', { model: costEstimate.model }) }}</span>
          </template>
          <template v-else-if="costEstimate && total > 0">
            <span>
              <strong>{{ t('reportSummary.bulk.costRange', {
                low: formatUSD(costEstimate.estMicroUSDLow),
                high: formatUSD(costEstimate.estMicroUSDHigh),
                n: total,
              }) }}</strong>
            </span>
            <span class="bgs-muted">
              · <code>{{ costEstimate.model }}</code>
              · {{ t('reportSummary.bulk.costMeta', {
                prompt: costEstimate.avgPromptTokens,
                completion: costEstimate.avgCompletionTokens,
              }) }}
              <span v-if="costEstimate.sampleSize > 0"> {{ t('reportSummary.bulk.costMetaSample', { n: costEstimate.sampleSize }) }}</span>
            </span>
          </template>
        </div>

        <div class="bgs-actions">
          <button class="btn btn-ghost" @click="finishAndClose">{{ t('reportSummary.bulk.cancel') }}</button>
          <button class="btn btn-primary" :disabled="pending || total === 0" @click="start">
            {{ t('reportSummary.bulk.generate', { n: total }) }}
          </button>
        </div>
      </template>

      <template v-else-if="phase === 'running'">
        <div class="bgs-progress-line">
          <span>{{ t('reportSummary.bulk.processed', { completed, total }) }}</span>
          <span class="bgs-muted" v-if="inFlightCount > 0"> · {{ t('reportSummary.bulk.inFlight', { n: inFlightCount }) }}</span>
          <span class="bgs-muted" v-if="retryingCount > 0"> · {{ t('reportSummary.bulk.retrying', { n: retryingCount }) }}</span>
          <span class="bgs-muted" v-if="etaLabel"> · {{ etaLabel }}</span>
          <span class="bgs-muted" v-if="throughputLabel"> · {{ throughputLabel }}</span>
        </div>
        <div class="bgs-bar"><div class="bgs-bar-fill" :style="{ width: progressPct + '%' }" /></div>

        <div v-if="recent.length" class="bgs-recent">
          <p class="bgs-recent-title">{{ t('reportSummary.bulk.recent') }}</p>
          <ul>
            <li v-for="entry in recent" :key="entry.id" :class="['bgs-recent-item', `bgs-recent-item--${entry.status}`]">
              <AppIcon :name="recentIcon(entry.status)" :size="12" />
              <code>#{{ entry.id }}</code>
              <span class="bgs-recent-preview">{{ entry.preview }}</span>
            </li>
          </ul>
        </div>

        <div class="bgs-actions">
          <button class="btn btn-ghost" @click="cancel">
            <AppIcon name="x" :size="14" /> {{ t('reportSummary.bulk.cancelBatch') }}
          </button>
        </div>
      </template>

      <template v-else>
        <div class="bgs-progress-line">
          <span><strong>{{ updatedIds.length }}</strong> {{ t('reportSummary.bulk.doneUpdatedWord') }}</span>
          <span v-if="failed.length"> · <strong>{{ failed.length }}</strong> {{ t('reportSummary.bulk.doneFailedWord') }}</span>
          <span v-if="completed < total"> · {{ total - completed }} {{ t('reportSummary.bulk.doneSkippedSuffix') }}</span>
        </div>
        <div v-if="failed.length" class="bgs-fail-list">
          <p class="bgs-fail-title">{{ t('reportSummary.bulk.doneFailuresTitle') }}</p>
          <ul>
            <li v-for="f in failed" :key="f.issueId">
              <code>#{{ f.issueId }}</code> — {{ f.error }}
            </li>
          </ul>
        </div>
        <div class="bgs-actions">
          <button class="btn btn-primary" @click="finishAndClose">{{ t('reportSummary.bulk.close') }}</button>
        </div>
      </template>
    </div>
  </AppModal>
</template>

<style scoped>
.bgs { display: flex; flex-direction: column; gap: 1rem; }
.bgs-lead { margin: 0; color: var(--text); font-size: 13px; line-height: 1.5; }
.bgs-lead code { background: var(--bg); padding: 0 .3em; border-radius: 3px; font-size: 11px; }
.bgs-row { display: flex; flex-direction: column; gap: .35rem; }
.bgs-label { font-size: 11px; font-weight: 700; text-transform: uppercase; letter-spacing: .06em; color: var(--text-muted); }
.bgs-actions { display: flex; justify-content: flex-end; gap: .5rem; margin-top: .35rem; }
.bgs-progress-line { font-size: 13px; color: var(--text); }
.bgs-muted { color: var(--text-muted); }
.bgs-bar { height: 6px; background: var(--bg); border-radius: 3px; overflow: hidden; border: 1px solid var(--border); }
.bgs-bar-fill { height: 100%; background: var(--brand, #4a7); transition: width .15s ease; }

.bgs-fail-list { background: #fff8e1; border: 1px solid #f1d68b; border-radius: var(--radius); padding: .6rem .75rem; }
.bgs-fail-title { margin: 0 0 .35rem; font-size: 11px; font-weight: 700; text-transform: uppercase; letter-spacing: .06em; color: #7a5b1f; }
.bgs-fail-list ul { margin: 0; padding-left: 1.2rem; font-size: 12px; color: var(--text); }
.bgs-fail-list code { font-size: 11px; background: rgba(0, 0, 0, .05); padding: 0 .25em; border-radius: 2px; }

.bgs-resume {
  display: flex; align-items: center; justify-content: space-between; gap: .75rem;
  padding: .65rem .85rem;
  background: var(--bg-card, #f6f8fb);
  border: 1px solid var(--border, #dde3eb);
  border-left: 3px solid var(--brand, #4a7);
  border-radius: 6px;
}
.bgs-resume-text { display: flex; flex-direction: column; gap: .15rem; font-size: 12px; color: var(--text); }
.bgs-resume-text strong { font-size: 13px; }
.bgs-resume-text span { color: var(--text-muted); }
.bgs-resume-actions { display: flex; gap: .35rem; }

.bgs-filters { display: flex; flex-direction: column; gap: .35rem; padding: .5rem .65rem; background: var(--bg, #fafbfc); border: 1px solid var(--border); border-radius: 6px; }
.bgs-check { display: inline-flex; align-items: center; gap: .45rem; font-size: 12.5px; color: var(--text); cursor: pointer; }
.bgs-check code { font-size: 11px; background: rgba(0, 0, 0, .04); padding: 0 .25em; border-radius: 3px; }

.bgs-cost { font-size: 12.5px; color: var(--text); line-height: 1.5; }
.bgs-cost code { background: var(--bg); padding: 0 .3em; border-radius: 3px; font-size: 11px; }

.bgs-recent {
  padding: .55rem .7rem;
  background: var(--bg, #fafbfc);
  border: 1px solid var(--border);
  border-radius: 6px;
}
.bgs-recent-title { margin: 0 0 .35rem; font-size: 11px; font-weight: 700; text-transform: uppercase; letter-spacing: .06em; color: var(--text-muted); }
.bgs-recent ul { margin: 0; padding: 0; list-style: none; display: flex; flex-direction: column; gap: .25rem; }
.bgs-recent-item {
  display: grid; grid-template-columns: 16px 64px 1fr; align-items: baseline; column-gap: .5rem;
  font-size: 12px; color: var(--text); white-space: nowrap;
}
.bgs-recent-item code { font-size: 11px; background: rgba(0, 0, 0, .04); padding: 0 .25em; border-radius: 3px; }
.bgs-recent-preview { overflow: hidden; text-overflow: ellipsis; color: var(--text); }
.bgs-recent-item--ok :first-child { color: var(--bp-green, #16a34a); }
.bgs-recent-item--fail :first-child { color: #c0392b; }
.bgs-recent-item--fail .bgs-recent-preview { color: #c0392b; }
</style>
