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
 * Shared `/api/instance` state.
 *
 * Module-level refs that any component can read without re-fetching. Loaded
 * once on first call — typically from `AppLayout` at app mount — and then
 * consumed reactively by anything that needs to know whether optional
 * subsystems (attachments, etc.) are wired up on this instance.
 *
 * Defaults are "optimistic" (enabled) so the UI doesn't falsely hide
 * features during the brief window before `loadInstance()` resolves.
 */
import { ref } from 'vue'
import { api } from './client'

export const instanceLabel       = ref('')
export const instanceHostname    = ref('')
export const attachmentsEnabled  = ref(true)

interface InstanceInfo {
  label?: string
  hostname?: string
  attachments_enabled?: boolean
}

let loadPromise: Promise<void> | null = null

export function loadInstance(): Promise<void> {
  if (loadPromise) return loadPromise
  loadPromise = api.get<InstanceInfo>('/instance')
    .then((d) => {
      instanceLabel.value      = d.label ?? ''
      instanceHostname.value   = d.hostname ?? ''
      attachmentsEnabled.value = d.attachments_enabled ?? true
    })
    .catch(() => {
      // Swallow — not logged in yet (401), network blip, etc.
      // Defaults stay optimistic. AppLayout retries on mount via its
      // own call, which is also covered by this memoisation.
      loadPromise = null
    })
  return loadPromise
}
