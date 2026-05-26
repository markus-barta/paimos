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

function localMidnight(d: Date): Date {
  return new Date(d.getFullYear(), d.getMonth(), d.getDate())
}

export const useTimerStore = defineStore('timer', () => {
  const runningEntries = ref<TimeEntry[]>([])
  const recentEntries = ref<TimeEntry[]>([])
  const elapsedMap = reactive<Map<number, number>>(new Map())
  // PAI-495: hours summed from stopped time entries whose stopped_at
  // falls inside the user's local day. The footer adds Σ elapsedMap for
  // the currently-running timers on top so the displayed total ticks
  // live without a server round-trip.
  const todayStoppedHours = ref<number>(0)
  // Local midnight of the day shown in the footer. Defaults to today;
  // the footer's prev/today/next buttons mutate this and re-fetch.
  // Future days are blocked client-side (canGoNext), not server-side.
  const selectedDate = ref<Date>(localMidnight(new Date()))
  let tickInterval: ReturnType<typeof setInterval> | null = null
  let bc: BroadcastChannel | null = null
  if (typeof BroadcastChannel !== 'undefined') {
    try {
      bc = new BroadcastChannel(SYNC_CHANNEL)
      bc.onmessage = (e: MessageEvent<SyncMsg>) => {
        if (e.data?.kind === 'changed') {
          void fetchRunning()
          void fetchTodayTotal()
        }
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

  // PAI-495: refresh the day-total from the server using the caller's
  // local-day window. Browser-local midnight bounds get serialised as
  // UTC ISO timestamps so the backend can range-filter without knowing
  // the user's timezone. The window comes from selectedDate so the
  // footer's prev/next buttons can scrub through past days.
  async function fetchTodayTotal() {
    const start = selectedDate.value
    const end = new Date(start)
    end.setDate(end.getDate() + 1)
    const params = new URLSearchParams({
      from: start.toISOString().replace(/\.\d{3}Z$/, 'Z'),
      to: end.toISOString().replace(/\.\d{3}Z$/, 'Z'),
    })
    try {
      const res = await api.get<{ total_hours: number; count: number }>(
        `/time-entries/today-summary?${params.toString()}`,
      )
      todayStoppedHours.value = res?.total_hours ?? 0
    } catch {
      todayStoppedHours.value = 0
    }
  }

  function isSelectedToday(): boolean {
    return selectedDate.value.getTime() === localMidnight(new Date()).getTime()
  }

  function shiftSelectedDay(deltaDays: number) {
    const next = new Date(selectedDate.value)
    next.setDate(next.getDate() + deltaDays)
    // Clamp at today — no future days.
    const todayMidnight = localMidnight(new Date())
    if (next.getTime() > todayMidnight.getTime()) return
    selectedDate.value = next
    void fetchTodayTotal()
  }

  function selectToday() {
    selectedDate.value = localMidnight(new Date())
    void fetchTodayTotal()
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
      await fetchTodayTotal()
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
    await fetchTodayTotal()
    broadcastChanged()
  }

  return {
    runningEntries, recentEntries, elapsedMap, hasRunning,
    todayStoppedHours, selectedDate,
    isRunning, getRunningEntry, formattedElapsed,
    fetchRunning, fetchRecent, fetchTodayTotal,
    isSelectedToday, shiftSelectedDay, selectToday,
    start, stop, stopAll,
    showStartDialog, pendingIssueId, confirmSwitch, confirmBoth, confirmCancel,
  }
})
