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

import { defineStore } from 'pinia'
import { ref, computed, reactive } from 'vue'
import { api } from '@/api/client'
import type { TimeEntry } from '@/types'

// PAI-244: peer tabs/windows for the same user broadcast timer state
// changes so each one resyncs without a page refresh. BroadcastChannel
// is strict same-origin within one browser; falls back silently when
// not available (older browsers, sandboxed contexts).
const SYNC_CHANNEL = 'paimos:timer'
type SyncMsg = { kind: 'changed' }

export const useTimerStore = defineStore('timer', () => {
  const runningEntries = ref<TimeEntry[]>([])
  const recentEntries = ref<TimeEntry[]>([])
  const elapsedMap = reactive<Map<number, number>>(new Map())
  let tickInterval: ReturnType<typeof setInterval> | null = null
  let bc: BroadcastChannel | null = null
  if (typeof BroadcastChannel !== 'undefined') {
    try {
      bc = new BroadcastChannel(SYNC_CHANNEL)
      bc.onmessage = (e: MessageEvent<SyncMsg>) => {
        if (e.data?.kind === 'changed') void fetchRunning()
      }
    } catch { bc = null }
  }
  function broadcastChanged() { try { bc?.postMessage({ kind: 'changed' }) } catch { /* ignore */ } }

  const hasRunning = computed(() => runningEntries.value.length > 0)

  function isRunning(issueId: number): boolean {
    return runningEntries.value.some(e => e.issue_id === issueId)
  }

  function getRunningEntry(issueId: number): TimeEntry | undefined {
    return runningEntries.value.find(e => e.issue_id === issueId)
  }

  function formattedElapsed(entryId: number): string {
    const totalSeconds = elapsedMap.get(entryId) ?? 0
    const h = Math.floor(totalSeconds / 3600)
    const m = Math.floor((totalSeconds % 3600) / 60)
    const s = totalSeconds % 60
    if (h > 0) return `${h}h ${String(m).padStart(2, '0')}m`
    if (m > 0) return `${m}m ${String(s).padStart(2, '0')}s`
    return `${s}s`
  }

  function startTick() {
    stopTick()
    updateAllElapsed()
    tickInterval = setInterval(updateAllElapsed, 1000)
  }

  function stopTick() {
    if (tickInterval != null) {
      clearInterval(tickInterval)
      tickInterval = null
    }
  }

  function updateAllElapsed() {
    for (const entry of runningEntries.value) {
      const start = new Date(entry.started_at).getTime()
      elapsedMap.set(entry.id, Math.max(0, Math.floor((Date.now() - start) / 1000)))
    }
  }

  async function fetchRunning() {
    try {
      const entries = await api.get<TimeEntry[]>('/time-entries/running')
      runningEntries.value = entries ?? []
      // Clean up elapsed entries that are no longer running
      const runningIds = new Set(runningEntries.value.map(e => e.id))
      for (const key of elapsedMap.keys()) {
        if (!runningIds.has(key)) elapsedMap.delete(key)
      }
      if (runningEntries.value.length > 0) {
        startTick()
      } else {
        stopTick()
        elapsedMap.clear()
      }
    } catch {
      runningEntries.value = []
      elapsedMap.clear()
      stopTick()
    }
  }

  async function fetchRecent() {
    try {
      recentEntries.value = await api.get<TimeEntry[]>('/time-entries/recent') ?? []
    } catch {
      recentEntries.value = []
    }
  }

  // Pending start — used for the confirmation dialog
  const pendingIssueId = ref<number | null>(null)
  const showStartDialog = ref(false)

  /** Start a timer, prompting if other timers are running. */
  async function start(issueId: number) {
    // PAI-244: another tab or session may have started/stopped a timer
    // since we last refreshed. Pull current state from the server so
    // the "other timers running — switch / both / cancel" prompt isn't
    // raised against stale local cache.
    await fetchRunning()
    // Already running on this issue — no-op
    if (isRunning(issueId)) return
    // No other timers running — just start
    if (runningEntries.value.length === 0) {
      await doStart(issueId)
      return
    }
    // Other timers running — show dialog
    pendingIssueId.value = issueId
    showStartDialog.value = true
  }

  /** Switch: stop all running, start new */
  async function confirmSwitch() {
    const id = pendingIssueId.value
    showStartDialog.value = false
    if (!id) return
    for (const e of [...runningEntries.value]) {
      await stop(e.id)
    }
    await doStart(id)
    pendingIssueId.value = null
  }

  /** Both: keep existing, start new alongside */
  async function confirmBoth() {
    const id = pendingIssueId.value
    showStartDialog.value = false
    if (!id) return
    await doStart(id)
    pendingIssueId.value = null
  }

  /** Cancel: dismiss dialog */
  function confirmCancel() {
    showStartDialog.value = false
    pendingIssueId.value = null
  }

  async function doStart(issueId: number) {
    try {
      await api.post(`/issues/${issueId}/time-entries`, {})
      await fetchRunning()
      broadcastChanged()
    } catch (e) {
      /* error swallowed — timer UI shows stale state as feedback */
    }
  }

  async function stop(entryId: number) {
    try {
      const now = new Date().toISOString().replace(/\.\d{3}Z$/, 'Z')
      await api.put(`/time-entries/${entryId}`, { stopped_at: now })
      await fetchRunning()
      await fetchRecent()
      broadcastChanged()
    } catch (e) {
      /* error swallowed */
    }
  }

  async function stopAll() {
    const entries = [...runningEntries.value]
    for (const e of entries) {
      try {
        const now = new Date().toISOString().replace(/\.\d{3}Z$/, 'Z')
        await api.put(`/time-entries/${e.id}`, { stopped_at: now })
      } catch (err) {
        /* error swallowed */
      }
    }
    await fetchRunning()
    await fetchRecent()
    broadcastChanged()
  }

  return {
    runningEntries, recentEntries, elapsedMap, hasRunning,
    isRunning, getRunningEntry, formattedElapsed,
    fetchRunning, fetchRecent, start, stop, stopAll,
    showStartDialog, pendingIssueId, confirmSwitch, confirmBoth, confirmCancel,
  }
})
