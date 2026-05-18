<script setup lang="ts">
/*
 * PAIMOS — Your Professional & Personal AI Project OS
 * Copyright (C) 2026 Markus Barta <markus@barta.com>
 *
 * PAI-418 / PAI-424. Bulk customer-facing report-summary generator.
 *
 * Shared by:
 *   - IssueList toolbar — "Generate report summary (AI)" on selected rows.
 *   - LieferberichtExportModal — "Generate missing →" for in-scope
 *     tickets that have no summary yet.
 *
 * Per-issue flow:
 *   1. POST /api/ai/action with action=customer_rewrite|exec_summary,
 *      field=report_summary, issue_id=<id>. The backend handler loads
 *      description + AC from the issue row itself, so `text` stays
 *      empty.
 *   2. On success, PATCH /api/issues/{id} with { report_summary: text }.
 *   3. Emit `updated` per row so the host re-renders without a refetch.
 *
 * What this modal carries beyond the simple loop:
 *
 *   - PAI-438. Skip-already-filled toggle (default ON). At scale,
 *     blindly regenerating is expensive in money + minutes. We pre-
 *     filter `issueIds` against `inScopeIssues` to drop rows that
 *     already have `report_summary`.
 *
 *   - PAI-439. ETA from a rolling-10 duration window. Lifetime
 *     averages drift on a single slow row; rolling keeps the number
 *     responsive.
 *
 *   - PAI-440. 429 / 5xx retry with exponential back-off + jitter
 *     (up to 3 attempts). Without this, one rate-limit hit at row
 *     200 of 585 turns the rest into a silent stream of failures.
 *
 *   - PAI-441. Default-exclude terminal statuses
 *     (`accepted | invoiced | cancelled`). Summaries on closed/billed
 *     tickets risk producing different copy from what was already
 *     delivered. A checkbox lets the user opt-in to regenerate.
 *
 *   - PAI-442. Resume after accidental close. The runner persists
 *     `{remaining_ids, style, sub_key, skip_filled, include_terminal,
 *     started_at}` to localStorage on every tick. On reopen, if a
 *     slot exists and is < 24h old, the user gets a banner offering
 *     Resume / Discard before the config phase renders.
 *
 *   - PAI-449. Pre-run cost estimate via /api/ai/bulk-cost-estimate.
 *     Pulls model pricing + rolling-avg token counts for the same
 *     action, returns a ± band so the user sees roughly what a run
 *     costs before they click Generate.
 *
 *   - Cancellation: an AbortController is held in `runAbort` and
 *     signalled on cancel / external close. Per-row in-flight calls
 *     are aborted via the api client's `signal` option; the runner
 *     also checks `signal.aborted` between iterations.
 */
import { computed, ref, watch } from 'vue'
import { api, ApiError, errMsg } from '@/api/client'
import type { Issue } from '@/types'
import AppModal from '@/components/AppModal.vue'
import AppIcon from '@/components/AppIcon.vue'
import MetaSelect from '@/components/MetaSelect.vue'
import type { MetaOption } from '@/components/MetaSelect.vue'

const props = defineProps<{
  open: boolean
  issueIds: number[]
  /** Default style preselect. Customer if omitted — the more common
   *  starting point for a Projektbericht aimed at non-technical
   *  customers. */
  defaultStyle?: 'customer' | 'exec'
  /** When provided, the bulk modal can pre-filter by status +
   *  existing-summary state (PAI-438 / PAI-441). Optional so callers
   *  that don't yet pass it just fall back to the original
   *  "process every ID" behaviour. */
  inScopeIssues?: Issue[]
}>()

const emit = defineEmits<{
  close: []
  updated: [issue: Issue]
  done: []
}>()

// ── State ────────────────────────────────────────────────────────

type Phase = 'config' | 'running' | 'done'
type Style = 'customer' | 'exec'

const styleOptions: MetaOption[] = [
  { value: 'customer', label: 'Customer (warm, Apple-Notes-Stil)' },
  { value: 'exec', label: 'Executive (technical TL;DR)' },
]
const subKeyOptions: MetaOption[] = [
  { value: 'release_note', label: 'Release note (neutral)' },
  { value: 'feature', label: 'Feature' },
  { value: 'fix', label: 'Fix' },
  { value: 'stability', label: 'Stability' },
  { value: 'security_hardening', label: 'Security hardening' },
]

const phase = ref<Phase>('config')
const style = ref<Style>(props.defaultStyle ?? 'customer')
const subKey = ref<string>('release_note')
const skipFilled = ref(true) // PAI-438
const includeTerminal = ref(false) // PAI-441

const completed = ref(0)
const failed = ref<{ issueId: number; error: string }[]>([])
const updatedIds = ref<number[]>([])
const currentLabel = ref<string>('')
const currentAttemptNote = ref<string>('') // "retrying (attempt 2)" line
let runAbort: AbortController | null = null
// PAI-442. Distinguishes a user-pressed cancel from an external
// force-close. User cancel discards the resume slot (they meant
// to stop). External close (route change, parent toggle) keeps
// the slot so the run can be resumed when the modal reopens.
let userCancelled = false

// Rolling-window duration tracker for ETA / throughput (PAI-439).
const rollingDurations = ref<number[]>([])

// Resume offer (PAI-442). Populated when the modal opens AND there's
// a valid LS slot. Cleared when the user picks Resume or Discard.
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

// Cost-estimate payload (PAI-449). Populated when the config phase
// is visible and the effective count changes.
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
const ROLLING_WINDOW = 10 // PAI-439
const MAX_RETRY_ATTEMPTS = 3 // PAI-440
const RETRY_BASE_MS = 2000 // 2s, doubled per attempt + jitter
const RETRY_MAX_MS = 60_000 // cap each individual wait
const LS_KEY = 'paimos:bulk-summary-run' // PAI-442
const RESUME_TTL_MS = 24 * 60 * 60 * 1000 // 24h

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
    if (!i) return true // unknown row — keep, server will validate
    if (!includeTerminal.value && TERMINAL_STATUSES.has(i.status)) return false
    if (skipFilled.value && String(i.report_summary ?? '').trim()) return false
    return true
  })
})

const filteredOutCount = computed(() => props.issueIds.length - effectiveIds.value.length)

const total = computed(() => effectiveIds.value.length)
const progressPct = computed(() => {
  if (total.value === 0) return 0
  return Math.round((completed.value / total.value) * 100)
})

const avgDurationMs = computed(() => {
  const xs = rollingDurations.value
  if (xs.length === 0) return null
  return xs.reduce((a, b) => a + b, 0) / xs.length
})

const etaMs = computed<number | null>(() => {
  if (avgDurationMs.value == null) return null
  const remaining = total.value - completed.value
  if (remaining <= 0) return null
  return remaining * avgDurationMs.value
})

const etaLabel = computed<string>(() => {
  if (etaMs.value == null) return ''
  return formatDuration(etaMs.value)
})

function formatDuration(ms: number): string {
  if (ms < 60_000) return `~ ${Math.round(ms / 1000)}s remaining`
  const totalSec = Math.round(ms / 1000)
  const min = Math.floor(totalSec / 60)
  const sec = totalSec % 60
  if (min >= 60) {
    const h = Math.floor(min / 60)
    const m = min % 60
    return `~ ${h}h ${m}m remaining`
  }
  return sec > 0 && min < 10 ? `~ ${min}m ${sec}s remaining` : `~ ${min} min remaining`
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

// Internal override used when the user accepts the resume offer. When
// non-null, `start()` uses these IDs instead of `effectiveIds`.
const resumeOverrideIds = ref<number[] | null>(null)

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
      currentLabel.value = ''
      currentAttemptNote.value = ''
      rollingDurations.value = []
      resumeOverrideIds.value = null
      costEstimate.value = null
      costEstimateError.value = ''
      runAbort?.abort()
      runAbort = null
      userCancelled = false
      // PAI-442. Offer to resume a previous run when a fresh slot exists.
      resumeOffer.value = readResumeSlot()
    } else {
      // PAI-427. Abort any in-flight run when the parent force-closes
      // the modal. We deliberately do NOT flip userCancelled here —
      // an external close should keep the resume slot intact so the
      // user can pick up where they left off on reopen (PAI-442).
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
  const n = (resumeOverrideIds.value ?? effectiveIds.value).length
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

// ── Runner ───────────────────────────────────────────────────────

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
    if (e.status === 0) return true // network / timeout
  }
  return false
}

async function runOneWithRetry(issueId: number, signal: AbortSignal): Promise<void> {
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
      currentAttemptNote.value = `retrying (attempt ${attempt}, ${Math.round(wait / 1000)}s)`
      await sleep(wait, signal)
      if (signal.aborted) throw new DOMException('aborted', 'AbortError')
    } else {
      currentAttemptNote.value = ''
    }
    try {
      const env = await api.post<ActionEnvelope>('/ai/action', payload, { signal, timeoutMs: 90_000 })
      const text = String(env.body?.optimized ?? '').trim()
      if (!text) throw new Error('AI returned empty result')
      const updatedIssue = await api.patch<Issue>(`/issues/${issueId}`, { report_summary: text }, { signal })
      updatedIds.value.push(issueId)
      emit('updated', updatedIssue)
      currentAttemptNote.value = ''
      return
    } catch (e) {
      lastErr = e
      if (signal.aborted) throw e
      if (!isRetryable(e)) throw e // surface non-retryable straight to the catch
    }
  }
  throw lastErr ?? new Error('all retries failed')
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

async function start() {
  if (phase.value !== 'config') return
  const ids = resumeOverrideIds.value ?? effectiveIds.value
  if (ids.length === 0) return
  phase.value = 'running'
  runAbort = new AbortController()
  const signal = runAbort.signal

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

  for (let idx = 0; idx < ids.length; idx++) {
    if (signal.aborted) break
    const id = ids[idx]
    currentLabel.value = `Issue ${id}`
    currentAttemptNote.value = ''
    const t0 = performance.now()
    try {
      await runOneWithRetry(id, signal)
      pushDuration(performance.now() - t0)
      completed.value++
    } catch (e) {
      if (signal.aborted) break
      pushDuration(performance.now() - t0)
      const msg = e instanceof ApiError ? errMsg(e, 'AI call failed') : (e as Error).message
      failed.value.push({ issueId: id, error: msg })
      completed.value++
    }
    // PAI-442. Update the resume slot with what's left after this
    // iteration so a crash mid-loop leaves the right starting point.
    slot.remainingIds = ids.slice(idx + 1)
    if (slot.remainingIds.length > 0) {
      writeResumeSlot(slot)
    } else {
      clearResumeSlot()
    }
  }
  currentLabel.value = ''
  currentAttemptNote.value = ''
  // PAI-442. Three cases:
  //  - Normal completion (queue drained): nothing left to resume,
  //    drop the slot.
  //  - User-initiated cancel: explicit "stop here", drop the slot.
  //  - External close (parent force-close, route change): keep the
  //    slot so the user can resume on reopen.
  if (!signal.aborted || userCancelled) {
    clearResumeSlot()
  }
  phase.value = 'done'
  if (signal.aborted) finishAndClose()
}

function pushDuration(ms: number) {
  rollingDurations.value.push(ms)
  if (rollingDurations.value.length > ROLLING_WINDOW) rollingDurations.value.shift()
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
  <AppModal :open="open" title="Generate report summary (AI)" max-width="560px" @close="cancel">
    <div class="bgs">
      <!-- PAI-442. Resume offer banner, only on a fresh open with a stored slot. -->
      <div v-if="phase === 'config' && resumeOffer" class="bgs-resume">
        <div class="bgs-resume-text">
          <strong>Resume previous run?</strong>
          <span>
            {{ resumeOffer.remainingIds.length }} of {{ resumeOffer.totalInitial }} left
            · started {{ new Date(resumeOffer.startedAt).toLocaleString() }}
            · {{ resumeOffer.style === 'exec' ? 'Executive' : 'Customer' }} style
          </span>
        </div>
        <div class="bgs-resume-actions">
          <button class="btn btn-ghost btn-sm" @click="discardResume">Discard</button>
          <button class="btn btn-primary btn-sm" @click="acceptResume">Resume</button>
        </div>
      </div>

      <template v-if="phase === 'config'">
        <p class="bgs-lead">
          Run AI generation on <strong>{{ total }} issue{{ total === 1 ? '' : 's' }}</strong
          ><span v-if="filterImpactSupported && filteredOutCount > 0" class="bgs-muted">
            (filtered from {{ props.issueIds.length }} selected)
          </span>.
          Each ticket's <code>report_summary</code> will be overwritten with the generated text.
        </p>

        <div class="bgs-row">
          <label class="bgs-label">Style</label>
          <MetaSelect v-model="style" :options="styleOptions" />
        </div>
        <div class="bgs-row" v-if="style === 'customer'">
          <label class="bgs-label">Tone bias</label>
          <MetaSelect v-model="subKey" :options="subKeyOptions" />
        </div>

        <!-- PAI-438 / PAI-441. Filter toggles. Only meaningful when
             the parent supplied inScopeIssues for the lookup. -->
        <div v-if="filterImpactSupported" class="bgs-filters">
          <label class="bgs-check">
            <input type="checkbox" v-model="skipFilled" />
            <span>Skip already-summarized issues</span>
          </label>
          <label class="bgs-check">
            <input type="checkbox" v-model="includeTerminal" />
            <span>Include final / billed issues (<code>accepted</code>, <code>invoiced</code>, <code>cancelled</code>)</span>
          </label>
        </div>

        <!-- PAI-449. Cost estimate line. -->
        <div class="bgs-cost">
          <template v-if="costEstimateLoading">
            <span class="bgs-muted">Estimating cost…</span>
          </template>
          <template v-else-if="costEstimateError">
            <span class="bgs-muted">Cost estimate unavailable — {{ costEstimateError }}</span>
          </template>
          <template v-else-if="costEstimate && costEstimate.heuristicFallback">
            <span class="bgs-muted">
              Cost estimate unavailable — first run will calibrate.
              Model: <code>{{ costEstimate.model }}</code>.
            </span>
          </template>
          <template v-else-if="costEstimate && total > 0">
            <span>
              <strong>≈ {{ formatUSD(costEstimate.estMicroUSDLow) }} – {{ formatUSD(costEstimate.estMicroUSDHigh) }}</strong>
              for {{ total }} issue{{ total === 1 ? '' : 's' }}
            </span>
            <span class="bgs-muted">
              · <code>{{ costEstimate.model }}</code>
              · ~ {{ costEstimate.avgPromptTokens }} prompt + {{ costEstimate.avgCompletionTokens }} completion tokens
              <span v-if="costEstimate.sampleSize > 0">(avg of last {{ costEstimate.sampleSize }} calls)</span>
            </span>
          </template>
        </div>

        <div class="bgs-actions">
          <button class="btn btn-ghost" @click="finishAndClose">Cancel</button>
          <button class="btn btn-primary" :disabled="total === 0" @click="start">
            Generate {{ total }}
          </button>
        </div>
      </template>

      <template v-else-if="phase === 'running'">
        <div class="bgs-progress-line">
          <span><strong>{{ completed }}</strong> of {{ total }} processed</span>
          <span class="bgs-muted" v-if="currentLabel"> · {{ currentLabel }}</span>
          <span class="bgs-muted" v-if="currentAttemptNote"> · {{ currentAttemptNote }}</span>
          <span class="bgs-muted" v-if="etaLabel"> · {{ etaLabel }}</span>
        </div>
        <div class="bgs-bar"><div class="bgs-bar-fill" :style="{ width: progressPct + '%' }" /></div>
        <div class="bgs-actions">
          <button class="btn btn-ghost" @click="cancel">
            <AppIcon name="x" :size="14" /> Cancel batch
          </button>
        </div>
      </template>

      <template v-else>
        <div class="bgs-progress-line">
          <span><strong>{{ updatedIds.length }}</strong> updated</span>
          <span v-if="failed.length"> · <strong>{{ failed.length }}</strong> failed</span>
          <span v-if="completed < total"> · {{ total - completed }} skipped (cancelled)</span>
        </div>
        <div v-if="failed.length" class="bgs-fail-list">
          <p class="bgs-fail-title">Failures</p>
          <ul>
            <li v-for="f in failed" :key="f.issueId">
              <code>#{{ f.issueId }}</code> — {{ f.error }}
            </li>
          </ul>
        </div>
        <div class="bgs-actions">
          <button class="btn btn-primary" @click="finishAndClose">Close</button>
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
</style>
