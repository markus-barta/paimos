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

// ── catalogue loader ──────────────────────────────────────────────
async function loadActions(): Promise<void> {
  if (actionsInflight) return actionsInflight
  actionsInflight = (async () => {
    try {
      const r = await api.get<ActionsCatalog>('/ai/actions')
      actions.value = r.actions ?? []
    } catch {
      // 401 or network — leave the list empty so the menu renders
      // an honest "not available" state without crashing.
      actions.value = []
    } finally {
      actionsLoaded.value = true
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
  // The legacy /api/ai/optimize path stays alive for backwards
  // compatibility (PAI-164 plans to retire it). For now, "optimize"
  // calls go through it; "translate" / "tone_check" land here once
  // their backend handlers ship.
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
  // Future: fan out translate / tone_check via /ai/action and feed
  // the rewritten text into the same overlay.
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
