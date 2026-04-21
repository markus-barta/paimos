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

import { ref, computed } from 'vue'
import { useRouter } from 'vue-router'
import { useTimerStore } from '@/stores/timer'
import { useIssuePreview } from '@/composables/useIssuePreview'
import { LS_TIMER_PANEL_OPEN as LS_KEY } from '@/constants/storage'

// Shared state — singleton across all callers
const timerPanelOpen = ref(localStorage.getItem(LS_KEY) === '1')
const timerPanelEl = ref<HTMLElement | null>(null)

export function useTimerPanel() {
  const router = useRouter()
  const timer = useTimerStore()
  const preview = useIssuePreview()

  const runningEntries = computed(() => timer.runningEntries)

  function openTimerIssue(entry: { project_id?: number; issue_id: number }) {
    preview.hidePreview()
    timerPanelOpen.value = false
    localStorage.setItem(LS_KEY, '0')
    router.push(`/projects/${entry.project_id}?panel=${entry.issue_id}`)
  }

  function toggleTimerPanel() {
    timerPanelOpen.value = !timerPanelOpen.value
    localStorage.setItem(LS_KEY, timerPanelOpen.value ? '1' : '0')
    if (timerPanelOpen.value) timer.fetchRecent()
  }

  /** Call in onMounted */
  function initTimerPanel() {
    timer.fetchRunning()
    if (timerPanelOpen.value) timer.fetchRecent()
  }

  return {
    timer,
    runningEntries,
    timerPanelOpen,
    timerPanelEl,
    openTimerIssue,
    toggleTimerPanel,
    initTimerPanel,
  }
}
