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

import { ref, reactive } from 'vue'
import { api, errMsg, ApiError } from '@/api/client'

// Source state for one optimize attempt. We snapshot it when the user
// clicks the AI button so Retry can replay the exact same request
// without the host page having to remember anything.
interface OptimizeArgs {
  field: string
  fieldLabel?: string
  text: string
  issueId?: number
  onAccept: (text: string) => void
}

interface OptimizeResponse {
  optimized: string
  model: string
  prompt_tokens: number
  completion_tokens: number
  finish_reason: string
}

interface AiOverlayState {
  visible: boolean
  field: string
  fieldLabel: string
  original: string
  optimized: string
  modelName: string
  retrying: boolean
}

// ── module-singleton state ──────────────────────────────────────────
const available = ref(false)
const isOptimizing = ref(false)
const lastError = ref<string | null>(null)
let statusLoaded = false
let statusInflight: Promise<void> | null = null

const overlay = reactive<AiOverlayState>({
  visible: false,
  field: '',
  fieldLabel: '',
  original: '',
  optimized: '',
  modelName: '',
  retrying: false,
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
  return api.post<OptimizeResponse>(
    '/ai/optimize',
    {
      field: args.field,
      text: args.text,
      issue_id: args.issueId ?? 0,
    },
    { timeoutMs: OPTIMIZE_TIMEOUT_MS },
  )
}

async function run(args: OptimizeArgs): Promise<void> {
  if (isOptimizing.value) return
  isOptimizing.value = true
  lastError.value = null
  pendingArgs = args
  try {
    const r = await callOptimize(args)
    overlay.field = args.field
    overlay.fieldLabel = args.fieldLabel ?? args.field
    overlay.original = args.text
    overlay.optimized = r.optimized
    overlay.modelName = r.model
    overlay.retrying = false
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
    pendingArgs = null
  } finally {
    isOptimizing.value = false
  }
}

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
  overlay.visible = false
  pendingArgs = null
  cb(text)
}

function reject(): void {
  overlay.visible = false
  pendingArgs = null
}

export function useAiOptimize() {
  // Lazy first-load. Subsequent callers get the cached value
  // immediately; available will flip when refreshStatus resolves.
  if (!statusLoaded && !statusInflight) void refreshStatus()
  return {
    available,
    isOptimizing,
    lastError,
    overlay,
    run,
    retry,
    accept,
    reject,
    refreshStatus,
  }
}
