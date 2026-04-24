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
 */

/**
 * useExternalProvider — singleton cache of CRM providers (PAI-106).
 *
 * Backs every place the UI renders a provider-aware affordance: customer
 * card badges, customer detail header, sync button copy, the admin
 * Integrations tab. Hardcoding "HubSpot" anywhere is a bug — go through
 * this composable instead so a future second provider lights up
 * automatically.
 *
 * One fetch shared across consumers; refresh() invalidates the cache
 * (the admin CRM tab calls it after a save so the rest of the UI picks
 * up new logos / names / enabled state).
 */

import { ref, computed, readonly } from 'vue'
import { api } from '@/api/client'
import type { ExternalProvider } from '@/types'

const providers = ref<ExternalProvider[]>([])
const loaded = ref(false)
const loading = ref(false)
let inflight: Promise<void> | null = null

async function load(force = false): Promise<void> {
  if (loaded.value && !force) return
  if (inflight) return inflight
  loading.value = true
  inflight = api.get<ExternalProvider[]>('/integrations/crm')
    .then((res) => {
      providers.value = Array.isArray(res) ? res : []
      loaded.value = true
    })
    .catch(() => {
      // Non-admin users get 403 — that's expected; treat as "no providers
      // visible to me" rather than a hard error so consuming components
      // can render empty-state without try/catch noise.
      providers.value = []
      loaded.value = true
    })
    .finally(() => {
      loading.value = false
      inflight = null
    })
  return inflight
}

export function useExternalProvider(id?: string | null) {
  // Lazy first load — every consumer triggers it; subsequent calls are
  // no-ops thanks to the loaded/inflight guards above.
  load()

  const provider = computed<ExternalProvider | null>(() => {
    if (!id) return null
    return providers.value.find((p) => p.id === id) ?? null
  })

  return {
    provider,
    /** All compiled-in providers, sorted by name. */
    providers: readonly(providers),
    /** True after the first fetch resolves (success or failure). */
    loaded: readonly(loaded),
    loading: readonly(loading),
    /** Force a re-fetch (used by the admin tab after a save). */
    refresh: () => load(true),
    /** Providers the admin has enabled + finished configuring. */
    enabledProviders: computed(() =>
      providers.value.filter((p) => p.enabled && p.configured),
    ),
  }
}
