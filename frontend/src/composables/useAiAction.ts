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
 *       optimize / translate              → diff overlay UX (existing)
 *       tone_check                        → inline strip + replace action
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
 * translate) and uses this composable's `result` slot
 * for everything else. That split avoids cramming six unrelated UIs
 * into one giant component while still letting the menu treat all
 * actions uniformly at the dispatch level.
 */

import { ref, reactive, computed } from 'vue'
import { api, errMsg, ApiError } from '@/api/client'
import { useAiOptimize } from '@/composables/useAiOptimize'
import type { AgentActionCapability } from '@/types'

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
  default_profile_id?: string
  default_effort?: string
}

interface ActionsCatalog {
  actions: AiActionDescriptor[]
}

export interface ActionEnvelope<T = unknown> {
  request_id?: string
  action: string
  sub_action?: string
  body: T
  // PAI-240: 'ok' for provider-backed success, 'no_op' when the
  // backend handler decided the action was a deliberate no-op (e.g.
  // detect_duplicates on a project with no peer issues). Older
  // backends omit it; treat absent as 'ok' for backwards compat.
  outcome?: 'ok' | 'no_op'
  model?: string
  prompt_tokens?: number
  completion_tokens?: number
  finish_reason?: string
  options?: AiActionResolvedOptions
}

export interface AiActionOptions {
  profile_id?: string
  profile?: string
  model_profile?: string
  model_id?: string
  model?: string
  effort?: string
  prompt_preset?: string
  prompt_preset_ref?: string
  context_pack?: string
}

export interface AiActionResolvedOptions {
  profile_id: string
  model: string
  effort: string
  prompt_preset_ref: string
  prompt_preset_label?: string
  context_pack: string
  context_pack_label?: string
  context_truncated?: boolean
  context_sources?: AiContextSource[]
}

export interface AiExecutionProfile {
  id: string
  label: string
  provider: string
  model: string
  effort: string
  speed_label: string
  cost_label: string
  capability_hints?: string[]
}

export interface AiActionDefaultOptions {
  profile_id: string
  effort: string
}

export interface AiSelectorDefault {
  action_key?: string
  profile_id: string
  profile_label?: string
  model?: string
  effort: string
  prompt_preset_ref: string
  context_pack: string
  context_pack_label?: string
  provider_id?: string
  provider_label?: string
  agent_name?: string
  source: 'global' | 'project' | string
}

export interface AiSelectorDefaults {
  actions: Record<string, AiSelectorDefault>
  runs: Record<string, AiSelectorDefault>
  row_launch: AiSelectorDefault
  workbench: AiSelectorDefault
}

export interface AiPromptPresetChoice {
  ref: string
  label: string
  type: string
  slug: string
  status: string
  revision: string
  actions: string[]
}

export interface AiContextPackChoice {
  id: string
  label: string
  description?: string
}

export interface AiContextSource {
  kind: string
  label: string
  count?: number
  truncated?: boolean
}

export interface AiKnowledgeSuggestion {
  ref: string
  type: string
  slug: string
  title: string
  status: string
  revision: string
  suggested_use: 'prompt' | 'context' | string
  prompt_preset: boolean
  prompt_preset_ref?: string
  prompt_preset_label?: string
  prompt_preset_status?: string
  actions?: string[]
}

export interface AiExecutionOptionsCatalog {
  profiles: AiExecutionProfile[]
  efforts: string[]
  action_defaults: Record<string, AiActionDefaultOptions>
  selector_defaults?: AiSelectorDefaults
  prompt_presets?: AiPromptPresetChoice[]
  knowledge_suggestions?: AiKnowledgeSuggestion[]
  context_packs?: AiContextPackChoice[]
  run_providers?: AgentActionCapability[]
  project_policy?: {
    disable_hosted_draft?: boolean
    disable_local_model_draft?: boolean
  }
}

export interface AiExecutionOptionsScope {
  projectId?: number
  issueId?: number
}

export interface RunArgs {
  hostKey?: string
  surface?: 'issue' | 'customer'
  action: string
  subAction?: string
  field: string
  fieldLabel?: string
  text: string
  issueId?: number
  onAccept: (text: string) => void
  context?: Record<string, unknown>
  options?: AiActionOptions
}

// ── module-singleton state ────────────────────────────────────────
const actions = ref<AiActionDescriptor[]>([])
const actionsLoaded = ref(false)
const actionsLoadError = ref<string | null>(null)
let actionsInflight: Promise<void> | null = null
const executionOptions = ref<AiExecutionOptionsCatalog | null>(null)
const executionOptionsLoaded = ref(false)
const executionOptionsLoadError = ref<string | null>(null)
let executionOptionsInflight: Promise<void> | null = null
let executionOptionsInflightKey = ''
let executionOptionsCacheKey = ''

const isRunning = ref(false)
const lastError = ref<string | null>(null)
const lastErrorHostKey = ref('')

export interface AiActionActivity {
  hostKey: string
  action: string
  subAction?: string
  field: string
  fieldLabel: string
  startedAt: number
  surface: string
}
const activity = ref<AiActionActivity | null>(null)

// `result` holds the last action's response. Host pages watch it
// to render the matching modal. `null` = no modal open.
interface ActiveResult {
  requestId?: string
  hostKey: string
  promptTokens?: number
  completionTokens?: number
  action: string
  subAction?: string
  fieldLabel: string
  field: string
  issueId?: number
  // The action-specific body; the host page narrows the type.
  body: unknown
  // PAI-240: 'no_op' means no provider call was made — the result
  // surface should render the body's `reason` instead of the usual
  // candidate list, and skip token/model metadata.
  outcome?: 'ok' | 'no_op'
  model?: string
  options?: AiActionResolvedOptions
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
const executionOptionsStatus = computed<'loading' | 'ready' | 'error'>(() => {
  if (executionOptionsInflight) return 'loading'
  if (executionOptionsLoadError.value) return 'error'
  return executionOptionsLoaded.value ? 'ready' : 'loading'
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
      actions.value = (r.actions ?? []).map((a) => ({
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

function executionOptionsKey(scope?: AiExecutionOptionsScope): string {
  if (scope?.issueId && scope.issueId > 0) return `issue:${scope.issueId}`
  if (scope?.projectId && scope.projectId > 0) return `project:${scope.projectId}`
  return 'global'
}

function executionOptionsPath(scope?: AiExecutionOptionsScope): string {
  const q = new URLSearchParams()
  if (scope?.issueId && scope.issueId > 0) q.set('issue_id', String(scope.issueId))
  else if (scope?.projectId && scope.projectId > 0) q.set('project_id', String(scope.projectId))
  const suffix = q.toString()
  return suffix ? `/ai/execution-options?${suffix}` : '/ai/execution-options'
}

async function loadExecutionOptions(scope?: AiExecutionOptionsScope): Promise<void> {
  const key = executionOptionsKey(scope)
  if (executionOptionsInflight) {
    if (executionOptionsInflightKey === key) return executionOptionsInflight
    await executionOptionsInflight
  }
  if (executionOptionsLoaded.value && executionOptionsCacheKey === key && executionOptions.value)
    return
  executionOptionsInflight = (async () => {
    let succeeded = false
    try {
      executionOptions.value = await api.get<AiExecutionOptionsCatalog>(executionOptionsPath(scope))
      executionOptionsLoadError.value = null
      succeeded = true
    } catch (e) {
      executionOptionsLoadError.value = errMsg(e, 'AI execution options unavailable')
    } finally {
      if (succeeded) {
        executionOptionsLoaded.value = true
        executionOptionsCacheKey = key
      }
      executionOptionsInflight = null
      executionOptionsInflightKey = ''
    }
  })()
  executionOptionsInflightKey = key
  return executionOptionsInflight
}

// Diff-overlay UX is reused for actions that produce rewritten field
// text when we want a full before/after overlay. For these we pipe the call
// through useAiOptimize so we get the existing accept/reject/retry
// flow for free.
// PAI-418 + PAI-173. Actions whose backend response shape is
// `{optimized: "..."}` — same as `optimize` — and whose UX matches
// the diff-overlay review-and-accept flow. Newly-implemented
// rewrite-style actions belong here; the generic action path is
// reserved for actions with a structured response (suggest, spec_out,
// find_parent, …) where a different UI applies the result.
const DIFF_OVERLAY_ACTIONS = new Set([
  'optimize',
  'translate',
  'tone_check',
  'customer_rewrite',
  'exec_summary',
])

// ── runner ────────────────────────────────────────────────────────
async function run(args: RunArgs): Promise<void> {
  if (isRunning.value) return
  if (!actionsLoaded.value) await loadActions()
  isRunning.value = true
  lastError.value = null
  lastErrorHostKey.value = ''
  result.value = null
  activity.value = {
    hostKey: args.hostKey ?? `${args.field}:${args.issueId ?? 0}:${args.action}`,
    action: args.action,
    subAction: args.subAction,
    field: args.field,
    fieldLabel: args.fieldLabel ?? args.field,
    startedAt: Date.now(),
    surface: args.surface ?? 'issue',
  }

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
    const env = await api.post<ActionEnvelope>(
      '/ai/action',
      {
        action: args.action,
        sub_action: args.subAction,
        field: args.field,
        issue_id: args.issueId ?? 0,
        text: args.text,
        ...(args.context ? { params: args.context } : {}),
        ...(args.options ? { options: args.options } : {}),
      },
      { timeoutMs: 90_000 },
    )

    result.value = {
      requestId: env.request_id,
      hostKey:
        activity.value?.hostKey ??
        args.hostKey ??
        `${args.field}:${args.issueId ?? 0}:${args.action}`,
      promptTokens: env.prompt_tokens,
      completionTokens: env.completion_tokens,
      action: env.action,
      subAction: env.sub_action,
      fieldLabel: args.fieldLabel ?? args.field,
      field: args.field,
      issueId: args.issueId,
      body: env.body,
      outcome: env.outcome,
      model: env.model,
      options: env.options,
      sourceText: args.text,
      onAccept: args.onAccept,
    }
  } catch (e) {
    if (e instanceof ApiError && e.status === 503) {
      void optimize.refreshStatus()
    }
    lastError.value = errMsg(e, 'AI action failed')
    lastErrorHostKey.value = args.hostKey ?? `${args.field}:${args.issueId ?? 0}:${args.action}`
  } finally {
    activity.value = null
    isRunning.value = false
  }
}

async function runViaOptimize(args: RunArgs): Promise<void> {
  // Optimize uses the legacy composable's run() which posts to
  // /api/ai/action with action="optimize" (PAI-164). Translate shares
  // the same overlay state via runRewriteAction(); tone_check now uses
  // the strip/detail/apply path instead so users can review the neutralized
  // rewrite inline without hijacking the diff overlay.
  if (args.action === 'optimize') {
    await optimize.run({
      hostKey: args.hostKey,
      surface: args.surface,
      field: args.field,
      fieldLabel: args.fieldLabel,
      text: args.text,
      issueId: args.issueId,
      onAccept: args.onAccept,
      context: args.context,
      options: args.options,
    })
    return
  }
  if (
    args.action === 'translate' ||
    args.action === 'tone_check' ||
    args.action === 'customer_rewrite' ||
    args.action === 'exec_summary'
  ) {
    await optimize.runRewriteAction({
      action: args.action,
      subAction: args.subAction,
      hostKey: args.hostKey,
      surface: args.surface,
      field: args.field,
      fieldLabel: args.fieldLabel,
      text: args.text,
      issueId: args.issueId,
      onAccept: args.onAccept,
      context: args.context,
      options: args.options,
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
  lastErrorHostKey.value = ''
}

// First import triggers the catalogue load in the real app. Tests mount
// surfaces in isolation and don't run against a live backend, so skip the
// eager fetch there and let explicit refreshes drive the catalogue instead.
if (import.meta.env.MODE !== 'test') {
  loadActions()
}

export function useAiAction() {
  return {
    actions,
    actionsStatus,
    actionsLoadError,
    executionOptions,
    executionOptionsStatus,
    executionOptionsLoadError,
    available,
    isRunning,
    lastError,
    lastErrorHostKey,
    activity,
    result,
    run,
    reset,
    clearError,
    refreshActions: loadActions,
    refreshExecutionOptions: loadExecutionOptions,
  }
}
