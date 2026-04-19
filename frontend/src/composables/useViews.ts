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
 * useViews — saved Views management (ACME-1 epic).
 *
 * Handles API calls, MRU ordering (localStorage), and section grouping.
 * Sections in the dropdown:
 *   - My Views      (own views, MRU sorted)
 *   - Basics        (admin is_admin_default views, not owned by current user)
 *   - Shared        (is_shared views by others, not is_admin_default)
 */

import { ref, computed } from 'vue'
import { api } from '@/api/client'
import type { SavedView } from '@/types'
import { ALL_COLUMNS } from '@/composables/useColumnConfig'

const MRU_KEY      = 'paimos:views:mru'
const LAST_VIEW_KEY = (userId: number | undefined, scope: string) =>
  `paimos:views:last:${userId ?? 0}:${scope}`

export function getLastViewId(userId: number | undefined, scope: string): number | null {
  try {
    const raw = localStorage.getItem(LAST_VIEW_KEY(userId, scope))
    if (!raw) return null
    const n = parseInt(raw, 10)
    return isNaN(n) ? null : n
  } catch { return null }
}

export function setLastViewId(userId: number | undefined, scope: string, id: number | null) {
  try {
    if (id === null) localStorage.removeItem(LAST_VIEW_KEY(userId, scope))
    else localStorage.setItem(LAST_VIEW_KEY(userId, scope), String(id))
  } catch { /* ignore */ }
}

// ── Sentinel views (never stored in DB) ──────────────────────────────────────

// Minimum view: Key + Type + Title only (pinned cols). Everything else hidden.
const MINIMUM_HIDDEN = ALL_COLUMNS
  .filter(c => !c.pinned && c.key !== 'type')   // type is not pinned but part of minimum
  .map(c => c.key)

export const MINIMUM_VIEW: SavedView = {
  id:               -1,
  user_id:          0,
  owner_username:   'system',
  title:            'Minimum',
  description:      'Key, Type and Title only.',
  columns_json:     JSON.stringify(MINIMUM_HIDDEN),
  filters_json:     '{}',
  is_shared:        true,
  is_admin_default: true,
  sort_order:       9999,
  hidden:           false,
  pinned:           null,
  created_at:       '',
  updated_at:       '',
}

function loadMRU(): Record<number, number> {
  try {
    return JSON.parse(localStorage.getItem(MRU_KEY) ?? '{}')
  } catch {
    return {}
  }
}

function saveMRU(mru: Record<number, number>) {
  localStorage.setItem(MRU_KEY, JSON.stringify(mru))
}

export function useViews(currentUserId: () => number | undefined) {
  const views     = ref<SavedView[]>([])
  const loading   = ref(false)
  const activeId  = ref<number | null>(null)

  const activeView = computed(() =>
    activeId.value !== null ? views.value.find(v => v.id === activeId.value) ?? null : null
  )

  // ── Default view ─────────────────────────────────────────────────────────
  // Returns the admin-seeded "Default" view if it exists, else the MINIMUM_VIEW sentinel.
  const effectiveDefaultView = computed<SavedView>(() => {
    const def = views.value.find(v => v.is_admin_default && v.title === 'Default')
    return def ?? MINIMUM_VIEW
  })

  // ── Sections ─────────────────────────────────────────────────────────────
  const myViews = computed(() => {
    const uid = currentUserId()
    const mru = loadMRU()
    return views.value
      .filter(v => v.user_id === uid && !v.is_admin_default)
      .sort((a, b) => (mru[b.id] ?? 0) - (mru[a.id] ?? 0))
  })

  const basicsViews = computed(() => {
    return views.value
      .filter(v => v.is_admin_default)
      .sort((a, b) => a.sort_order - b.sort_order || a.title.localeCompare(b.title))
  })

  const sharedViews = computed(() => {
    const uid = currentUserId()
    return views.value
      .filter(v => v.is_shared && !v.is_admin_default && v.user_id !== uid)
      .sort((a, b) => a.title.localeCompare(b.title))
  })

  // ── API ───────────────────────────────────────────────────────────────────
  async function load() {
    loading.value = true
    try {
      views.value = await api.get<SavedView[]>('/views')
    } finally {
      loading.value = false
    }
  }

  async function create(payload: {
    title: string
    description?: string
    columns_json: string
    filters_json: string
    is_shared?: boolean
    is_admin_default?: boolean
  }): Promise<SavedView> {
    const v = await api.post<SavedView>('/views', payload)
    views.value = [...views.value, v]
    return v
  }

  async function update(id: number, payload: Partial<{
    title: string
    description: string
    columns_json: string
    filters_json: string
    is_shared: boolean
    is_admin_default: boolean
  }>): Promise<SavedView> {
    const v = await api.put<SavedView>(`/views/${id}`, payload)
    views.value = views.value.map(x => x.id === id ? v : x)
    if (activeId.value === id) activeId.value = id // keep active
    return v
  }

  async function remove(id: number) {
    await api.delete(`/views/${id}`)
    views.value = views.value.filter(v => v.id !== id)
    if (activeId.value === id) activeId.value = null
    // Clean up MRU
    const mru = loadMRU()
    delete mru[id]
    saveMRU(mru)
  }

  // ── Apply / clear ─────────────────────────────────────────────────────────
  function recordMRU(id: number) {
    const mru = loadMRU()
    mru[id] = Date.now()
    saveMRU(mru)
  }

  function selectView(id: number | null) {
    activeId.value = id
    if (id !== null) recordMRU(id)
  }

  // Copy a shared/admin view to own personal views
  async function copyToMine(source: SavedView): Promise<SavedView> {
    const uid = currentUserId()
    const title = source.user_id === uid ? `${source.title} (copy)` : source.title
    return create({
      title,
      description: source.description,
      columns_json: source.columns_json,
      filters_json: source.filters_json,
      is_shared: false,
      is_admin_default: false,
    })
  }

  async function pinView(id: number) {
    await api.post(`/views/${id}/pin`, {})
    const v = views.value.find(x => x.id === id)
    if (v) v.pinned = true
  }

  async function unpinView(id: number) {
    await api.delete(`/views/${id}/pin`)
    const v = views.value.find(x => x.id === id)
    if (v) v.pinned = false
  }

  async function reorder(items: { id: number; sort_order: number }[]) {
    await api.patch('/views/order', items)
    for (const item of items) {
      const v = views.value.find(x => x.id === item.id)
      if (v) v.sort_order = item.sort_order
    }
  }

  return {
    views, loading, activeId, activeView,
    myViews, basicsViews, sharedViews,
    effectiveDefaultView,
    load, create, update, remove, selectView, copyToMine,
    pinView, unpinView, reorder,
  }
}
