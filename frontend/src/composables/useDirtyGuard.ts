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

import { computed, onUnmounted, type Ref } from 'vue'
import { useConfirm } from '@/composables/useConfirm'

/**
 * normalizeJSON — canonicalizes a JSON string for stable comparison.
 * Sorts object keys, normalizes null/undefined/empty to consistent values.
 */
export function normalizeJSON(json: string): string {
  if (!json) return ''
  try {
    return JSON.stringify(JSON.parse(json), (_, v) => v === undefined ? null : v)
  } catch {
    return json
  }
}

/**
 * useDirtyGuard — detects unsaved changes and guards destructive actions.
 *
 * @param current  - reactive ref to the current form values (stringified for deep compare)
 * @param saved    - reactive ref to the last-saved snapshot (stringified for deep compare)
 */
export function useDirtyGuard(current: Ref<string>, saved: Ref<string>) {
  const isDirty = computed(() => normalizeJSON(current.value) !== normalizeJSON(saved.value))
  const { confirm } = useConfirm()

  /** Guard an action: if dirty, confirm with the user before executing. */
  async function guardAction(action: () => void, message = 'You have unsaved changes. Discard and continue?') {
    if (!isDirty.value) { action(); return }
    if (await confirm({ message, confirmLabel: 'Discard', danger: true })) action()
  }

  // Browser beforeunload guard (must use native prompt — browser requirement)
  function onBeforeUnload(e: BeforeUnloadEvent) {
    if (isDirty.value) {
      e.preventDefault()
      e.returnValue = ''
    }
  }
  window.addEventListener('beforeunload', onBeforeUnload)
  onUnmounted(() => window.removeEventListener('beforeunload', onBeforeUnload))

  /** Clear both snapshots — call on cancel / discard to reset dirty state. */
  function reset() {
    saved.value = ''
  }

  return { isDirty, guardAction, reset }
}
