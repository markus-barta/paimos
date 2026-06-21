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
 * useIssueQuery — PAI-563 (IssueList v2 foundation).
 *
 * One canonical, typed query model + one fetch lifecycle for every issue
 * list (internal global, internal project, customer portal). Hosts mutate
 * the query through a single set of typed mutators instead of scattered
 * watchers, and a monotonic request id + AbortController + fingerprint
 * guard make out-of-order responses impossible to apply.
 *
 * The controller is wire-format- and endpoint-agnostic: the host injects a
 * `fetcher(query, signal)` that maps the query onto the right API
 * (`/issues`, `/projects/{id}/issues`, `/portal/projects/{id}/issues`) and
 * returns a normalized { issues, total, hasMore }. That injection is what
 * lets the internal and portal lists share this one engine (PAI-570/474).
 */

import { reactive, ref, computed, readonly } from 'vue'
import type { Ref, DeepReadonly } from 'vue'

export type IssueQueryMode = 'internal-global' | 'internal-project' | 'portal'
export type SortDir = 'asc' | 'desc'

// All list filters hold signed string values matching the v1 representation
// (a leading "!" negates, e.g. "!done" / "!7"). Numeric lists (tags, sprints,
// projects, epic) carry id strings and are validated when encoded.
export interface IssueFilters {
  status: string[]
  priority: string[]
  type: string[]
  costUnit: string[]
  release: string[]
  assignee: string[]
  tags: string[]
  projects: string[]
  sprints: string[]
  epic: string[]
  dateField: string | null
  dateFrom: string | null
  dateTo: string | null
}

export interface IssueQueryWindow {
  /** 'page' = capped/paged loading; 'all' = the full result set in one go. */
  mode: 'page' | 'all'
  /** page size for 'page'; 0 means unbounded ('all'). */
  limit: number
  /** pagination cursor — deliberately NOT part of the fingerprint. */
  offset: number
}

export interface IssueQuery {
  mode: IssueQueryMode
  projectId: number | null
  filters: IssueFilters
  search: string
  sort: { key: string; dir: SortDir }
  window: IssueQueryWindow
  /** active saved-view id — a projection; resolves into filters/sort. */
  viewId: number | null
  /** active tab/scope — a projection; resolves into filters. */
  tab: string | null
}

export interface IssueListResult<T> {
  issues: T[]
  total: number
  hasMore: boolean
}

export type IssueFetcher<T> = (
  query: IssueQuery,
  signal: AbortSignal,
) => Promise<IssueListResult<T>>

export const DEFAULT_PAGE_SIZE = 100

export function emptyFilters(): IssueFilters {
  return {
    status: [], priority: [], type: [], costUnit: [], release: [],
    assignee: [], tags: [], projects: [], sprints: [], epic: [],
    dateField: null, dateFrom: null, dateTo: null,
  }
}

function makeQuery(initial: Partial<IssueQuery> & { mode: IssueQueryMode }): IssueQuery {
  return {
    mode: initial.mode,
    projectId: initial.projectId ?? null,
    filters: { ...emptyFilters(), ...(initial.filters ?? {}) },
    search: initial.search ?? '',
    sort: initial.sort ? { ...initial.sort } : { key: 'created_at', dir: 'desc' },
    window: initial.window
      ? { ...initial.window }
      : { mode: 'page', limit: DEFAULT_PAGE_SIZE, offset: 0 },
    viewId: initial.viewId ?? null,
    tab: initial.tab ?? null,
  }
}

/**
 * Stable fingerprint over the dimensions that change the result set or its
 * order. Excludes window.offset (pagination), viewId and tab (projections
 * that resolve into filters/sort), and all presentation-only state. Arrays
 * are normalized to strings + sorted so selection order never matters.
 */
export function queryFingerprint(q: IssueQuery): string {
  const f = q.filters
  const norm = (a: ReadonlyArray<string | number>) => [...a].map(String).sort()
  return JSON.stringify({
    mode: q.mode,
    projectId: q.projectId,
    search: q.search.trim(),
    sort: q.sort,
    window: { mode: q.window.mode, limit: q.window.limit },
    filters: {
      status: norm(f.status), priority: norm(f.priority), type: norm(f.type),
      costUnit: norm(f.costUnit), release: norm(f.release), assignee: norm(f.assignee),
      tags: norm(f.tags), projects: norm(f.projects), sprints: norm(f.sprints), epic: norm(f.epic),
      dateField: f.dateField, dateFrom: f.dateFrom, dateTo: f.dateTo,
    },
  })
}

/** Plain, non-reactive copy handed to the fetcher (decoupled from the proxy). */
function snapshot(q: IssueQuery): IssueQuery {
  return {
    ...q,
    filters: { ...q.filters },
    sort: { ...q.sort },
    window: { ...q.window },
  }
}

export interface UseIssueQueryOptions<T> {
  initial: Partial<IssueQuery> & { mode: IssueQueryMode }
  fetcher: IssueFetcher<T>
  /** debounce for `setSearch` keystrokes (ms). Default 150. */
  debounceMs?: number
}

export function useIssueQuery<T extends { id: number }>(opts: UseIssueQueryOptions<T>) {
  const debounceMs = opts.debounceMs ?? 150
  const query = reactive<IssueQuery>(makeQuery(opts.initial))
  const pageLimit = opts.initial.window?.limit ?? DEFAULT_PAGE_SIZE

  const issues = ref<T[]>([]) as Ref<T[]>
  const total = ref(0)
  const hasMore = ref(false)
  const loading = ref(false)
  const error = ref<unknown>(null)
  const fingerprint = computed(() => queryFingerprint(query))

  let seq = 0
  let inFlight: AbortController | null = null
  let searchTimer: ReturnType<typeof setTimeout> | null = null

  async function run(replace: boolean): Promise<void> {
    const id = ++seq
    const fp = fingerprint.value
    if (inFlight) inFlight.abort()
    const ac = new AbortController()
    inFlight = ac
    loading.value = true
    error.value = null
    try {
      const res = await opts.fetcher(snapshot(query), ac.signal)
      if (id !== seq) return                  // superseded by a newer request
      if (fingerprint.value !== fp) return    // query mutated mid-flight
      issues.value = replace ? res.issues : [...issues.value, ...res.issues]
      total.value = res.total
      hasMore.value = res.hasMore
    } catch (e) {
      if (ac.signal.aborted || id !== seq) return
      error.value = e
    } finally {
      if (id === seq) {
        loading.value = false
        inFlight = null
      }
    }
  }

  /** Reset pagination and fetch the first window for the current query. */
  function reload(): Promise<void> {
    query.window.offset = 0
    return run(true)
  }

  function clearSearchTimer() {
    if (searchTimer) { clearTimeout(searchTimer); searchTimer = null }
  }

  // ── Mutators — the single write path for the query ───────────────────────

  function setFilter(patch: Partial<IssueFilters>): Promise<void> {
    Object.assign(query.filters, patch)
    return reload()
  }

  /** Sort change MUST NOT reset the window — fixes the v1 "show all" reset. */
  function setSort(key: string, dir: SortDir): Promise<void> {
    query.sort = { key, dir }
    return reload()
  }

  function setWindow(mode: 'page' | 'all'): Promise<void> {
    query.window.mode = mode
    query.window.limit = mode === 'all' ? 0 : pageLimit
    return reload()
  }

  function setTab(tab: string | null): Promise<void> {
    query.tab = tab
    return reload()
  }

  /** Apply a saved view atomically (filters + sort + id) → one fetch. */
  function applyView(patch: {
    filters?: Partial<IssueFilters>
    sort?: { key: string; dir: SortDir }
    viewId?: number | null
  }): Promise<void> {
    if (patch.filters) Object.assign(query.filters, patch.filters)
    if (patch.sort) query.sort = { ...patch.sort }
    if (patch.viewId !== undefined) query.viewId = patch.viewId
    return reload()
  }

  /** Debounced search (for keystrokes). */
  function setSearch(value: string): void {
    query.search = value
    clearSearchTimer()
    searchTimer = setTimeout(() => { searchTimer = null; void reload() }, debounceMs)
  }

  /** Immediate search (programmatic / tests / explicit submit). */
  function setSearchNow(value: string): Promise<void> {
    clearSearchTimer()
    query.search = value
    return reload()
  }

  function loadMore(): Promise<void> {
    if (!hasMore.value || loading.value) return Promise.resolve()
    query.window.offset = issues.value.length
    return run(false)
  }

  /** Re-fetch the current query without changing it (refresh button/poll). */
  function refresh(): Promise<void> {
    query.window.offset = 0
    return run(true)
  }

  function reset(): Promise<void> {
    clearSearchTimer()
    Object.assign(query, makeQuery(opts.initial))
    return reload()
  }

  /** Initial load — call from the host's onMounted. */
  function start(): Promise<void> {
    return reload()
  }

  return {
    query,
    fingerprint,
    issues: issues as Readonly<Ref<T[]>>,
    total: readonly(total) as DeepReadonly<Ref<number>>,
    hasMore: readonly(hasMore) as DeepReadonly<Ref<boolean>>,
    loading: readonly(loading) as DeepReadonly<Ref<boolean>>,
    error: readonly(error),
    start,
    reload,
    refresh,
    setFilter,
    setSort,
    setWindow,
    setSearch,
    setSearchNow,
    setTab,
    applyView,
    loadMore,
    reset,
  }
}
