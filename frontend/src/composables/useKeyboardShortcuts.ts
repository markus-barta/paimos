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

import { watch, onMounted, onUnmounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useTimerStore } from '@/stores/timer'
import type AppHeader from '@/components/AppHeader.vue'
import type { Ref } from 'vue'

export function useKeyboardShortcuts(appHeaderRef: Ref<InstanceType<typeof AppHeader> | null>) {
  const route = useRoute()
  const router = useRouter()
  const timer = useTimerStore()

  // / and CMD+K — focus the AppHeader search input
  function onGlobalKeydown(e: KeyboardEvent) {
    const tag = (e.target as HTMLElement).tagName
    if (['INPUT', 'TEXTAREA', 'SELECT'].includes(tag)) return
    if (e.key === '/' || (e.metaKey && e.key === 'k')) {
      e.preventDefault()
      if (route.path === '/') router.replace('/issues')
      setTimeout(() => appHeaderRef.value?.focus(), 50)
    }
  }

  // Keyboard shortcuts for timer conflict dialog
  function onTimerDialogKey(e: KeyboardEvent) {
    if (!timer.showStartDialog) return
    const k = e.key.toLowerCase()
    if (k === 's') { timer.confirmSwitch(); e.preventDefault() }
    else if (k === 'b' || k === 'a') { timer.confirmBoth(); e.preventDefault() }
    else if (k === 'c' || k === 'escape') { timer.confirmCancel(); e.preventDefault() }
  }

  function init() {
    onMounted(() => window.addEventListener('keydown', onGlobalKeydown))
    onUnmounted(() => window.removeEventListener('keydown', onGlobalKeydown))

    watch(() => timer.showStartDialog, (v) => {
      if (v) window.addEventListener('keydown', onTimerDialogKey)
      else   window.removeEventListener('keydown', onTimerDialogKey)
    })
  }

  return { init }
}
