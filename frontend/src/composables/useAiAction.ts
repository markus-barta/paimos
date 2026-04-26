/*
 * PAIMOS — Your Professional & Personal AI Project OS
 * Copyright (C) 2026 Markus Barta <markus@barta.com>
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as
 * published by the Free Software Foundation, version 3.
 *
 * PAI-163. Composable that orchestrates the multi-action AI dropdown.
 *
 * Architecture
 * ------------
 *   - Module-singleton state. Only one action runs at a time across
 *     the whole SPA (same UX rule as PAI-147's optimize composable):
 *     two simultaneous "Suggest enhancement" modals would be confusing,
 *     and serialising at the singleton is simpler than serialising per
 *     surface.
 *   - The composable exposes:
 *       available     — feature configured + enabled
 *       isRunning     — a request is in flight
 *       lastError     — human-readable error from the last run
 *       actions       — the catalogue (loaded from /api/ai/actions)
 *       result        — the latest action result (action-specific shape)
 *       run()         — start an action call
 *       reset()       — clear `result` so the modal stops rendering
 *
 *   - Per-action result shapes:
 *       optimize / translate / tone_check → diff overlay UX (existing)
 *       suggest_enhancement / spec_out    → list-of-suggestions modal
 *       find_parent / detect_duplicates   → candidate-cards modal
 *       generate_subtasks                 → checklist-create modal
 *       estimate_effort                   → small popover with numbers
 *       ui_generation                     → markdown preview modal
 *
 *     The composable doesn't render any of these — it just stores
 *     `result` with `{action, body}` and lets the host page render
 *     the right modal/overlay component based on `action`.
 *
 * Note on coexistence with useAiOptimize
 * --------------------------------------
 * The pre-existing useAiOptimize composable (PAI-147) keeps working
 * for the optimize-specific diff overlay. AiActionMenu uses
 * useAiOptimize for actions that want the diff UX (optimize,
 * translate, tone_check) and uses this composable's `result` slot
 * for everything else. That split avoids cramming six unrelated UIs
 * into one giant component while still letting the menu treat all
 * actions uniformly at the dispatch level.
 */

import { ref, reactive, computed } from 'vue'
import { api, errMsg, ApiError } from '@/api/client'
import { useAiOptimize } from '@/composables/useAiOptimize'

export interface AiActionDescriptor {
  key: string
  label: string
  surface: 'issue' | 'customer'
  /** PAI-179: placement ∈ {text, issue, both}. `text` actions
   *  belong inline next to text fields (rewrite a paragraph);
   *  `issue` actions belong in issue-level menus (operate on the
   *  whole record). `both` shows the action everywhere. */
  placement: 'text' | 'issue' | 'both'
  sub_keys?: string[]
  implemented: boolean
}

interface ActionsCatalog {
  actions: AiActionDescriptor[]
}

export interface ActionEnvelope<T = unknown> {
  action: string
  sub_action?: string
  body: T
  model?: string
  prompt_tokens?: number
  completion_tokens?: number
  finish_reason?: string
}

export interface RunArgs {
  action: string
  subAction?: string
  field: string
  fieldLabel?: string
  text: string
  issueId?: number
  onAccept: (text: string) => void
  context?: Record<string, unknown>
}

// ── module-singleton state ────────────────────────────────────────
const actions = ref<AiActionDescriptor[]>([])
const actionsLoaded = ref(false)
const actionsLoadError = ref<string | null>(null)
let actionsInflight: Promise<void> | null = null

const isRunning = ref(false)
const lastError = ref<string | null>(null)

// `result` holds the last action's response. Host pages watch it
// to render the matching modal. `null` = no modal open.
interface ActiveResult {
  action: string
  subAction?: string
  fieldLabel: string
  field: string
  issueId?: number
  // The action-specific body; the host page narrows the type.
  body: unknown
  model?: string
  // Apply callback for actions that need the user's accept (e.g. spec_out)
  onApply?: (chosen: unknown) => void
  // Source text the action was run against — useful for diff UX.
  sourceText: string
  onAccept: (text: string) => void
}
const result = ref<ActiveResult | null>(null)

const optimize = useAiOptimize()

// `available` mirrors the optimize composable's flag. The two endpoints
// share the same ai_settings row so a single status feed serves both.
const available = computed(() => optimize.available.value)
const actionsStatus = computed<'loading' | 'ready' | 'error'>(() => {
  if (actionsInflight) return 'loading'
  if (actionsLoadError.value) return 'error'
  return actionsLoaded.value ? 'ready' : 'loading'
})

// ── catalogue loader ──────────────────────────────────────────────
//
// PAI-179: previously, a single failed load (typically the 401 on
// the very first import-time call when the user wasn't logged in
// yet) left actionsLoaded=true forever, and every menu after login
// rendered "No AI actions are configured for this surface yet"
// despite the backend being fine. The fix:
//   - failures don't flip actionsLoaded → next caller retries
//   - successes mark loaded; next caller short-circuits
//   - the menu component nudges this to refresh when it mounts
//     and the catalogue is empty (cheap; one round-trip)
async function loadActions(): Promise<void> {
  if (actionsInflight) return actionsInflight
  actionsInflight = (async () => {
    let succeeded = false
    try {
      const r = await api.get<ActionsCatalog>('/ai/actions')
      actions.value = (r.actions ?? []).map(a => ({
        ...a,
        // Backend versions before PAI-179 don't return `placement`.
        // Default to 'text' so legacy deployments still surface
        // their actions next to text fields.
        placement: a.placement ?? 'text',
      }))
      actionsLoadError.value = null
      succeeded = true
    } catch (e) {
      // 401 or network — keep the previous list (which may be empty
      // on first load). On a 401-then-login flow, the menu's mount
      // hook re-tries this so the catalogue ends up populated.
      actionsLoadError.value = errMsg(e, 'AI action catalog unavailable')
    } finally {
      // Only mark "loaded" on success. Failures stay in retry-state
      // so the next attempt actually fires instead of being short-
      // circuited by a stale `actionsLoaded=true`.
      if (succeeded) actionsLoaded.value = true
      actionsInflight = null
    }
  })()
  return actionsInflight
}

// Diff-overlay UX is reused for actions that produce rewritten field
// text (optimize, translate, tone_check). For these we pipe the call
// through useAiOptimize so we get the existing accept/reject/retry
// flow for free.
const DIFF_OVERLAY_ACTIONS = new Set(['optimize', 'translate', 'tone_check'])

// ── runner ────────────────────────────────────────────────────────
async function run(args: RunArgs): Promise<void> {
  if (isRunning.value) return
  if (!actionsLoaded.value) await loadActions()
  isRunning.value = true
  lastError.value = null
  result.value = null

  try {
    if (DIFF_OVERLAY_ACTIONS.has(args.action)) {
      // Route through the optimize composable, mapping action +
      // sub_action onto the field-and-text contract. The backend
      // dispatcher recognises action="optimize" exactly today; the
      // others land via this composable once their handlers ship
      // (PAI-168, PAI-173). Until then, the menu disables them.
      await runViaOptimize(args)
      return
    }

    // Generic action call: POST /ai/action with the wire shape.
    const env = await api.post<ActionEnvelope>('/ai/action', {
      action: args.action,
      sub_action: args.subAction,
      field: args.field,
      issue_id: args.issueId ?? 0,
      text: args.text,
      ...(args.context ? { params: args.context } : {}),
    }, { timeoutMs: 90_000 })

    result.value = {
      action: env.action,
      subAction: env.sub_action,
      fieldLabel: args.fieldLabel ?? args.field,
      field: args.field,
      issueId: args.issueId,
      body: env.body,
      model: env.model,
      sourceText: args.text,
      onAccept: args.onAccept,
    }
  } catch (e) {
    if (e instanceof ApiError && e.status === 503) {
      void optimize.refreshStatus()
    }
    lastError.value = errMsg(e, 'AI action failed')
  } finally {
    isRunning.value = false
  }
}

async function runViaOptimize(args: RunArgs): Promise<void> {
  // Optimize uses the legacy composable's run() which posts to
  // /api/ai/action with action="optimize" (PAI-164). Translate and
  // tone_check both produce rewritten field text that should land
  // in the same diff overlay, so they share the overlay state via
  // runRewriteAction(); the composable internally posts to
  // /api/ai/action with the right action key and unwraps the body.
  if (args.action === 'optimize') {
    await optimize.run({
      field: args.field,
      fieldLabel: args.fieldLabel,
      text: args.text,
      issueId: args.issueId,
      onAccept: args.onAccept,
    })
    return
  }
  if (args.action === 'translate' || args.action === 'tone_check') {
    await optimize.runRewriteAction({
      action: args.action,
      subAction: args.subAction,
      field: args.field,
      fieldLabel: args.fieldLabel,
      text: args.text,
      issueId: args.issueId,
      onAccept: args.onAccept,
    })
    return
  }
  lastError.value = `Action ${args.action} is not implemented yet — see PAI-162.`
}

function reset(): void {
  result.value = null
  lastError.value = null
}

function clearError(): void {
  lastError.value = null
}

// First import triggers the catalogue load.
loadActions()

export function useAiAction() {
  return {
    actions,
    actionsStatus,
    actionsLoadError,
    available,
    isRunning,
    lastError,
    result,
    run,
    reset,
    clearError,
    refreshActions: loadActions,
  }
}
