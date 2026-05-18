<script setup lang="ts">
/*
 * PAIMOS — Your Professional & Personal AI Project OS
 * Copyright (C) 2026 Markus Barta <markus@barta.com>
 *
 * PAI-418 / PAI-424. Bulk customer-facing report-summary generator.
 *
 * Shared by:
 *   - IssueList toolbar — "Generate report summary (AI)" on selected rows.
 *   - LieferberichtExportModal — "Generate missing →" for in-scope tickets
 *     that have no summary yet.
 *
 * Flow per issue ID:
 *   1. POST /api/ai/action with action=customer_rewrite|exec_summary,
 *      field=report_summary, issue_id=<id>. The backend handler loads
 *      description + AC from the issue row itself, so we don't need
 *      to pass them here; `text` stays empty.
 *   2. On success, PATCH /api/issues/{id} with { report_summary: text }.
 *   3. Increment completed counter; surface per-row error on failure
 *      without aborting the batch.
 *
 * Cancellation: an AbortController is held in `runAbort` and signalled
 * from the cancel button. The runner checks `runAbort.signal.aborted`
 * between iterations; in-flight provider calls are aborted via the
 * api client's `signal` option.
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
}>()

const emit = defineEmits<{
  close: []
  /** Per-row event after a successful PATCH. Mirrors BulkChangeModal's
   *  pattern so the host (IssueList) re-emits `updated` upward and the
   *  containing view refreshes the row in place. */
  updated: [issue: Issue]
  /** Emitted once when the whole batch is finished (success, partial,
   *  or cancelled). Closing the modal is the host's responsibility,
   *  matching BulkChangeModal's contract. */
  done: []
}>()

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
const completed = ref(0)
const total = computed(() => props.issueIds.length)
const failed = ref<{ issueId: number; error: string }[]>([])
const updatedIds = ref<number[]>([])
const currentLabel = ref<string>('')
let runAbort: AbortController | null = null

watch(
  () => props.open,
  (v) => {
    if (v) {
      phase.value = 'config'
      style.value = props.defaultStyle ?? 'customer'
      subKey.value = 'release_note'
      completed.value = 0
      failed.value = []
      updatedIds.value = []
      currentLabel.value = ''
      runAbort?.abort()
      runAbort = null
    } else {
      // PAI-427. Parent force-closed the modal mid-run (route change,
      // programmatic close, etc.). Abort the in-flight worker so we
      // don't keep issuing AI calls + PATCHes against rows whose UI
      // the user already navigated away from.
      runAbort?.abort()
    }
  },
)

interface ActionEnvelope {
  body?: { optimized?: string }
}

async function runOne(issueId: number, signal: AbortSignal): Promise<void> {
  const action = style.value === 'exec' ? 'exec_summary' : 'customer_rewrite'
  const payload: Record<string, unknown> = {
    action,
    field: 'report_summary',
    issue_id: issueId,
    text: '',
  }
  if (action === 'customer_rewrite') payload.sub_action = subKey.value
  const env = await api.post<ActionEnvelope>('/ai/action', payload, { signal, timeoutMs: 90_000 })
  const text = String(env.body?.optimized ?? '').trim()
  if (!text) throw new Error('AI returned empty result')
  const updatedIssue = await api.patch<Issue>(`/issues/${issueId}`, { report_summary: text }, { signal })
  updatedIds.value.push(issueId)
  emit('updated', updatedIssue)
}

async function start() {
  if (phase.value !== 'config') return
  phase.value = 'running'
  runAbort = new AbortController()
  const signal = runAbort.signal
  for (const id of props.issueIds) {
    if (signal.aborted) break
    currentLabel.value = `Issue ${id}`
    // PAI-428. `completed` is incremented only on a finished iteration —
    // success OR a non-aborted failure. Bumping it in `finally` would
    // include the aborted-mid-flight iteration too, leaving the
    // closing summary one short across the three buckets.
    try {
      await runOne(id, signal)
      completed.value++
    } catch (e) {
      if (signal.aborted) break
      const msg = e instanceof ApiError ? errMsg(e, 'AI call failed') : (e as Error).message
      failed.value.push({ issueId: id, error: msg })
      completed.value++
    }
  }
  currentLabel.value = ''
  phase.value = 'done'
  // PAI-429. If the user pressed Escape / backdrop / X during the
  // run, the loop just unwound — fall through to close instead of
  // stranding them on a summary screen they didn't ask for.
  if (signal.aborted) finishAndClose()
}

function cancel() {
  // Cancel from any phase: if a run is in flight, signal abort first,
  // then close. The aborted run's tail in start() also calls
  // finishAndClose, but that is idempotent (emit('close') on a
  // closed modal is a no-op) so the double-fire is safe and the
  // user-pressed-cancel case feels immediate.
  if (phase.value === 'running') {
    runAbort?.abort()
  }
  finishAndClose()
}

function finishAndClose() {
  emit('done')
  emit('close')
}

const progressPct = computed(() => {
  if (total.value === 0) return 0
  return Math.round((completed.value / total.value) * 100)
})
</script>

<template>
  <AppModal :open="open" title="Generate report summary (AI)" max-width="520px" @close="cancel">
    <div class="bgs">
      <p class="bgs-lead" v-if="phase === 'config'">
        Run AI generation on <strong>{{ total }} issue{{ total === 1 ? '' : 's' }}</strong>.
        Each ticket's <code>report_summary</code> will be overwritten with the generated text.
      </p>

      <template v-if="phase === 'config'">
        <div class="bgs-row">
          <label class="bgs-label">Style</label>
          <MetaSelect v-model="style" :options="styleOptions" />
        </div>
        <div class="bgs-row" v-if="style === 'customer'">
          <label class="bgs-label">Tone bias</label>
          <MetaSelect v-model="subKey" :options="subKeyOptions" />
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
          <span>{{ completed }} of {{ total }} processed</span>
          <span class="bgs-current" v-if="currentLabel">· {{ currentLabel }}</span>
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
.bgs-current { color: var(--text-muted); }
.bgs-bar { height: 6px; background: var(--bg); border-radius: 3px; overflow: hidden; border: 1px solid var(--border); }
.bgs-bar-fill { height: 100%; background: var(--brand, #4a7); transition: width .15s ease; }
.bgs-fail-list { background: #fff8e1; border: 1px solid #f1d68b; border-radius: var(--radius); padding: .6rem .75rem; }
.bgs-fail-title { margin: 0 0 .35rem; font-size: 11px; font-weight: 700; text-transform: uppercase; letter-spacing: .06em; color: #7a5b1f; }
.bgs-fail-list ul { margin: 0; padding-left: 1.2rem; font-size: 12px; color: var(--text); }
.bgs-fail-list code { font-size: 11px; background: rgba(0, 0, 0, .05); padding: 0 .25em; border-radius: 2px; }
</style>
