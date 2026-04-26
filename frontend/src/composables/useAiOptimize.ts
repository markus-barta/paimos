/*
 * PAIMOS — Your Professional & Personal AI Project OS
 * Copyright (C) 2026 Markus Barta <markus@barta.com>
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as
 * published by the Free Software Foundation, version 3.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
 * GNU Affero General Public License for more details.
 *
 * You should have received a copy of the GNU Affero General Public
 * License along with this program. If not, see <https://www.gnu.org/licenses/>.
 */

// PAI-147 / PAI-148. Composable that orchestrates the AI optimize
// flow: per-request availability, the API call, and the shared
// overlay state.
//
// Design choice: module-singleton state. Every page that mounts an AI
// button shares the same `available` flag and the same overlay slot.
// Two simultaneous overlays would be a UX bug — only one rewrite can
// be reviewed at a time — so a singleton avoids one of those by
// construction.
//
// The composable exposes:
//   - available: feature configured + enabled (polled lazily on first use)
//   - isOptimizing: a request is in flight
//   - lastError: human-readable error from the most recent attempt
//   - overlay: the AiOptimizeOverlay state slot (use with v-if)
//   - run(): start an optimize call
//   - retry(): re-run the same call (used by the overlay's Retry btn)
//   - accept(): apply the optimized text via the saved onAccept cb
//   - reject(): close the overlay without applying
//
// IMPORTANT consumer note: the returned object is a plain object,
// NOT a reactive() proxy. Vue auto-unwraps refs in templates only
// when they're top-level <script setup> bindings or live on a
// reactive() proxy. Accessing the refs (`available`, `isOptimizing`,
// `lastError`) via `obj.lastError` in a template gives the Ref
// itself — always truthy in v-if. Always destructure the refs you
// need at the top of setup so the template treats them as top-level
// refs. `overlay` is reactive() and CAN be accessed via the object.

import { ref, reactive } from 'vue'
import { api, errMsg, ApiError } from '@/api/client'

// Source state for one optimize attempt. We snapshot it when the user
// clicks the AI button so Retry can replay the exact same request
// without the host page having to remember anything.
interface OptimizeArgs {
  hostKey?: string
  surface?: string
  field: string
  fieldLabel?: string
  text: string
  issueId?: number
  onAccept: (text: string) => void
}

interface OptimizeResponse {
  request_id?: string
  optimized: string
  model: string
  prompt_tokens: number
  completion_tokens: number
  finish_reason: string
}

// PAI-164: response envelope from the /api/ai/action dispatcher.
// `body` is the action-specific payload — for the optimize action
// it's `{optimized: string}`. Token counts and model live on the
// envelope so the diff overlay can render the success banner.
interface ActionEnvelope {
  request_id?: string
  action: string
  body: { optimized?: string }
  model: string
  prompt_tokens: number
  completion_tokens: number
  finish_reason: string
}

interface AiOverlayState {
  visible: boolean
  hostKey: string
  actionKey: string
  requestId: string
  field: string
  fieldLabel: string
  original: string
  optimized: string
  modelName: string
  retrying: boolean
  promptTokens: number
  completionTokens: number
}

export interface AiOptimizeActivity {
  hostKey: string
  actionKey: string
  field: string
  fieldLabel: string
  startedAt: number
  surface: string
}

// ── module-singleton state ──────────────────────────────────────────
const available = ref(false)
const isOptimizing = ref(false)
const lastError = ref<string | null>(null)
const lastErrorHostKey = ref('')
let statusLoaded = false
let statusInflight: Promise<void> | null = null
const activity = ref<AiOptimizeActivity | null>(null)

const overlay = reactive<AiOverlayState>({
  visible: false,
  hostKey: '',
  actionKey: '',
  requestId: '',
  field: '',
  fieldLabel: '',
  original: '',
  optimized: '',
  modelName: '',
  retrying: false,
  promptTokens: 0,
  completionTokens: 0,
})

// Stash for Retry. Cleared on Reject/Accept.
let pendingArgs: OptimizeArgs | null = null

async function refreshStatus(): Promise<void> {
  if (statusInflight) return statusInflight
  statusInflight = (async () => {
    try {
      const s = await api.get<{ available: boolean }>('/ai/status')
      available.value = !!s.available
    } catch {
      // 401 or network — the rest of the SPA already surfaces auth
      // failure. Treat as not-available so the button stays disabled
      // rather than crashing the editor.
      available.value = false
    } finally {
      statusLoaded = true
      statusInflight = null
    }
  })()
  return statusInflight
}

/**
 * The /ai/optimize endpoint can take up to 60s on the backend; we
 * give the fetch a small additional buffer so the client surfaces
 * the upstream error rather than its own timeout.
 */
const OPTIMIZE_TIMEOUT_MS = 90_000

async function callOptimize(args: OptimizeArgs): Promise<OptimizeResponse> {
  // PAI-164: the legacy /api/ai/optimize endpoint is gone; this
  // composable now goes through the unified /api/ai/action
  // dispatcher with `action=optimize`. The response envelope is
  // shaped by the dispatcher; we flatten its `body` field here so
  // existing diff-overlay callers don't need to know.
  // Customer-surface fields use `optimize_customer` so the menu
  // descriptor lights up under the customer surface in /api/ai/actions;
  // both keys share the same backend handler.
  const action = isCustomerField(args.field) ? 'optimize_customer' : 'optimize'
  const env = await api.post<ActionEnvelope>(
    '/ai/action',
    {
      action,
      field: args.field,
      text: args.text,
      issue_id: args.issueId ?? 0,
    },
    { timeoutMs: OPTIMIZE_TIMEOUT_MS },
  )
  return {
    request_id: env.request_id,
    optimized: env.body?.optimized ?? '',
    model: env.model,
    prompt_tokens: env.prompt_tokens,
    completion_tokens: env.completion_tokens,
    finish_reason: env.finish_reason,
  }
}

// PAI-164: customer-surface fields route through the
// `optimize_customer` action key so the menu descriptor surfaces
// it on customer-bound editors. The set is kept in lockstep with
// the backend's customer-surface allow-list.
function isCustomerField(field: string): boolean {
  return field === 'customer_notes'
      || field === 'cooperation_sla_details'
      || field === 'cooperation_notes'
}

function resetOverlayState() {
  overlay.visible = false
  overlay.hostKey = ''
  overlay.actionKey = ''
  overlay.requestId = ''
  overlay.field = ''
  overlay.fieldLabel = ''
  overlay.original = ''
  overlay.optimized = ''
  overlay.modelName = ''
  overlay.retrying = false
  overlay.promptTokens = 0
  overlay.completionTokens = 0
}

async function run(args: OptimizeArgs): Promise<void> {
  if (isOptimizing.value) return
  // PAI-155: defensive overlay reset. The UI guards (modal backdrop,
  // singleton isOptimizing) make it nearly impossible for run() to
  // fire while a prior overlay is still on screen, but if a future
  // route reshuffle ever lets that happen we don't want the old
  // diff to linger if the new call fails. Clear before issuing the
  // new call so the failure path leaves a clean slate.
  if (overlay.visible) {
    resetOverlayState()
  }
  isOptimizing.value = true
  lastError.value = null
  lastErrorHostKey.value = ''
  activity.value = {
    hostKey: args.hostKey ?? `${args.field}:${args.issueId ?? 0}`,
    actionKey: isCustomerField(args.field) ? 'optimize_customer' : 'optimize',
    field: args.field,
    fieldLabel: args.fieldLabel ?? args.field,
    startedAt: Date.now(),
    surface: args.surface ?? (isCustomerField(args.field) ? 'customer' : 'issue'),
  }
  pendingArgs = args
  try {
    const r = await callOptimize(args)
    overlay.hostKey = activity.value?.hostKey ?? ''
    overlay.actionKey = activity.value?.actionKey ?? 'optimize'
    overlay.requestId = r.request_id ?? ''
    overlay.field = args.field
    overlay.fieldLabel = args.fieldLabel ?? args.field
    overlay.original = args.text
    overlay.optimized = r.optimized
    overlay.modelName = r.model
    overlay.retrying = false
    overlay.promptTokens = r.prompt_tokens
    overlay.completionTokens = r.completion_tokens
    overlay.visible = true
  } catch (e) {
    // 503s coming back from the backend mean either "not configured"
    // or "provider transiently down"; we re-poll status so the AI
    // button reflects reality on the next render without a full
    // page reload.
    if (e instanceof ApiError && e.status === 503) {
      void refreshStatus()
    }
    lastError.value = errMsg(e, 'Optimization failed')
    lastErrorHostKey.value = args.hostKey ?? `${args.field}:${args.issueId ?? 0}`
    pendingArgs = null
  } finally {
    activity.value = null
    isOptimizing.value = false
  }
}

// PAI-168 / PAI-173: rewrite-style actions that produce new field
// text. Posts to /api/ai/action with the chosen action key and
// reuses the diff overlay state. Identical UX to optimize from the
// user's point of view — the only difference is the backend
// handler that produced the rewrite.
interface RewriteActionArgs extends OptimizeArgs {
  action: string
  subAction?: string
}
async function runRewriteAction(args: RewriteActionArgs): Promise<void> {
  if (isOptimizing.value) return
  if (overlay.visible) {
    resetOverlayState()
  }
  isOptimizing.value = true
  lastError.value = null
  lastErrorHostKey.value = ''
  activity.value = {
    hostKey: args.hostKey ?? `${args.field}:${args.issueId ?? 0}`,
    actionKey: args.action,
    field: args.field,
    fieldLabel: args.fieldLabel ?? args.field,
    startedAt: Date.now(),
    surface: args.surface ?? (isCustomerField(args.field) ? 'customer' : 'issue'),
  }
  // Stash the args for retry — the regular pendingArgs slot expects
  // an OptimizeArgs shape, so we keep a parallel slot here.
  pendingRewriteArgs = args
  pendingArgs = null
  try {
    const env = await api.post<ActionEnvelope>(
      '/ai/action',
      {
        action: args.action,
        sub_action: args.subAction,
        field: args.field,
        text: args.text,
        issue_id: args.issueId ?? 0,
      },
      { timeoutMs: OPTIMIZE_TIMEOUT_MS },
    )
    const optimized = env.body?.optimized ?? ''
    overlay.hostKey = activity.value?.hostKey ?? ''
    overlay.actionKey = args.action
    overlay.requestId = env.request_id ?? ''
    overlay.field = args.field
    overlay.fieldLabel = args.fieldLabel ?? args.field
    overlay.original = args.text
    overlay.optimized = optimized
    overlay.modelName = env.model
    overlay.retrying = false
    overlay.promptTokens = env.prompt_tokens
    overlay.completionTokens = env.completion_tokens
    overlay.visible = true
    // The accept callback uses pendingArgs.onAccept; copy across.
    pendingArgs = {
      field: args.field,
      fieldLabel: args.fieldLabel,
      text: args.text,
      issueId: args.issueId,
      onAccept: args.onAccept,
    }
  } catch (e) {
    if (e instanceof ApiError && e.status === 503) {
      void refreshStatus()
    }
    lastError.value = errMsg(e, 'Action failed')
    lastErrorHostKey.value = args.hostKey ?? `${args.field}:${args.issueId ?? 0}`
    pendingRewriteArgs = null
  } finally {
    activity.value = null
    isOptimizing.value = false
  }
}
let pendingRewriteArgs: RewriteActionArgs | null = null

async function retry(): Promise<void> {
  if (!pendingArgs) return
  overlay.retrying = true
  lastError.value = null
  try {
    const r = await callOptimize(pendingArgs)
    overlay.optimized = r.optimized
    overlay.modelName = r.model
  } catch (e) {
    lastError.value = errMsg(e, 'Optimization failed')
  } finally {
    overlay.retrying = false
  }
}

function accept(): void {
  if (!overlay.visible || !pendingArgs) return
  const cb = pendingArgs.onAccept
  const text = overlay.optimized
  // Reset before calling the callback so a callback that triggers
  // a re-render with the new text doesn't see a stale overlay.
  resetOverlayState()
  pendingArgs = null
  cb(text)
}

function reject(): void {
  resetOverlayState()
  pendingArgs = null
}

function clearError(): void {
  lastError.value = null
  lastErrorHostKey.value = ''
}

export function useAiOptimize() {
  // Lazy first-load. Subsequent callers get the cached value
  // immediately; available will flip when refreshStatus resolves.
  if (import.meta.env.MODE !== 'test' && !statusLoaded && !statusInflight) void refreshStatus()
  return {
    available,
    isOptimizing,
    lastError,
    lastErrorHostKey,
    activity,
    overlay,
    run,
    runRewriteAction,
    retry,
    accept,
    reject,
    clearError,
    refreshStatus,
  }
}
