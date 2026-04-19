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

/**
 * useIssuePreview — hover logic for floating issue preview cards.
 *
 * Manages delay, Shift key detection, caching, and positioning.
 * Shared singleton state so all hover triggers coordinate.
 */

import { ref, readonly } from 'vue'
import { api } from '@/api/client'
import { useAuthStore } from '@/stores/auth'
import type { Issue } from '@/types'

const activeIssue = ref<Issue | null>(null)
const visible = ref(false)
const position = ref({ x: 0, y: 0 })
const skipAnimation = ref(false)

const cache = new Map<number, Issue>()
let hoverTimer: ReturnType<typeof setTimeout> | null = null
let hideTimer: ReturnType<typeof setTimeout> | null = null

function getDelay(): number {
  const auth = useAuthStore()
  return auth.user?.preview_hover_delay ?? 1000
}

async function fetchIssue(id: number): Promise<Issue | null> {
  if (cache.has(id)) return cache.get(id)!
  try {
    const issue = await api.get<Issue>(`/issues/${id}`)
    cache.set(id, issue)
    return issue
  } catch {
    return null
  }
}

function showPreview(issueId: number, event: MouseEvent, xOffset = 16) {
  if (hideTimer) { clearTimeout(hideTimer); hideTimer = null }
  if (hoverTimer) clearTimeout(hoverTimer)

  const shiftHeld = event.shiftKey
  const delay = shiftHeld ? 0 : getDelay()
  skipAnimation.value = shiftHeld

  // Position near cursor but offset into main content area
  const x = Math.min(event.clientX + xOffset, window.innerWidth - 380)
  const y = Math.min(event.clientY - 20, window.innerHeight - 300)
  position.value = { x, y }

  hoverTimer = setTimeout(async () => {
    const issue = await fetchIssue(issueId)
    if (issue) {
      activeIssue.value = issue
      visible.value = true
    }
  }, delay)
}

function hidePreview() {
  if (hoverTimer) { clearTimeout(hoverTimer); hoverTimer = null }
  // Grace period so user can move to the card
  hideTimer = setTimeout(() => {
    visible.value = false
    activeIssue.value = null
  }, 200)
}

function keepPreview() {
  if (hideTimer) { clearTimeout(hideTimer); hideTimer = null }
}

export function useIssuePreview() {
  return {
    activeIssue: readonly(activeIssue),
    visible: readonly(visible),
    position: readonly(position),
    skipAnimation: readonly(skipAnimation),
    showPreview,
    hidePreview,
    keepPreview,
  }
}
