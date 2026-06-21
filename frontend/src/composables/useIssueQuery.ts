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
import { IssueRowWindow } from './issueRowWindow'

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
  /**
   * Pre-encoded filter query string for internal hosts that still own the
   * filter UI (IssueList emits `server-filter-change`). Opaque to the model
   * but part of the fingerprint so it invalidates the cache like any filter.
   * Portal/structured hosts leave this empty and use `filters` instead.
   */
  rawFilter: string
}

export interface IssueListResult<T> {
  issues: T[]
  total: number
  hasMore: boolean
  /** server revision after this window, for delta reconciliation (PAI-568). */
  revision?: string
  /** server result fingerprint (passed through to IssueList's selection UI). */
  fingerprint?: string
  /** server selection fingerprint for safe all-matching bulk ops. */
  selectionFingerprint?: string
}

/** PAI-568: an incremental refresh patch for the current query. */
export interface IssueListDelta<T> {
  /** the query fingerprint this delta was computed for. */
  fingerprint: string
  /** the revision this delta builds on; a mismatch forces a full reload. */
  baseRevision?: string
  /** the revision after applying this delta. */
  revision?: string
  /** changed/updated rows that are already in the loaded window. */
  upserts?: T[]
  /** ids removed or no longer matching the query. */
  deletes?: number[]
  /** new total matching count (authoritative if present). */
  total?: number
  /** server signal: too many changes / reorder — caller should full-reload. */
  full?: boolean
}

/** Outcome of applyDelta: rows patched, caller must full-reload, or ignored. */
export type DeltaResult = 'patched' | 'reload' | 'ignored'

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
    rawFilter: initial.rawFilter ?? '',
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
    rawFilter: q.rawFilter,
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

  const win = new IssueRowWindow<T>()
  const rev = ref(0) // bumped on every window mutation to drive the computeds
  const revision = ref('') // server revision of the loaded window (PAI-568)
  const serverFingerprint = ref('') // server result fingerprint (for IssueList)
  const serverSelectionFingerprint = ref('')
  const loading = ref(false)
  const error = ref<unknown>(null)
  const fingerprint = computed(() => queryFingerprint(query))

  // PAI-567: optimistic inline-edit reconciliation. Pending mutations are
  // re-asserted after every window write, so a stale reload or an older
  // in-flight response can never visually revert a fresh edit.
  const pending = new Map<number, { mutationId: number; prev: T; optimistic: T }>()
  const errs = new Map<number, string>()
  let mutationSeq = 0

  const issues = computed<T[]>(() => { void rev.value; return win.rows() })
  const total = computed(() => { void rev.value; return win.total })
  const loaded = computed(() => { void rev.value; return win.loaded })
  const hasMore = computed(() => { void rev.value; return win.hasMore })
  /** True when every matching row is loaded (showing all, not a subset). */
  const complete = computed(() => { void rev.value; return win.complete })
  /** Per-row write errors from rejected optimistic mutations. */
  const rowErrors = computed(() => { void rev.value; return new Map(errs) })

  function reassertPending() {
    for (const m of pending.values()) win.patch(m.optimistic)
  }

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
      if (replace) win.setWindow(res.issues, res.total, res.hasMore, fp)
      else win.appendWindow(res.issues, res.total, res.hasMore)
      if (res.revision !== undefined) revision.value = res.revision
      if (res.fingerprint !== undefined) serverFingerprint.value = res.fingerprint
      if (res.selectionFingerprint !== undefined) serverSelectionFingerprint.value = res.selectionFingerprint
      reassertPending() // optimistic edits win over a freshly loaded snapshot
      rev.value++
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

  /** Set the pre-encoded filter string (internal hosts; IssueList emit). */
  function setRawFilter(raw: string): Promise<void> {
    if (query.rawFilter === raw) return Promise.resolve()
    query.rawFilter = raw
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
    query.window.offset = win.loaded
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

  // ── PAI-567: inline mutation reconciliation ──────────────────────────────

  /** Optimistically patch a loaded row; returns a mutation id (or null). */
  function mutateRow(id: number, patch: Partial<T>): number | null {
    const cur = win.get(id)
    if (!cur) return null
    const mutationId = ++mutationSeq
    const optimistic = { ...cur, ...patch } as T
    const existing = pending.get(id)
    pending.set(id, { mutationId, prev: existing ? existing.prev : cur, optimistic })
    errs.delete(id)
    win.patch(optimistic)
    rev.value++
    return mutationId
  }

  /** Server confirmed: make `serverRow` canonical (unless a newer edit is pending). */
  function confirmMutation(id: number, serverRow: T, mutationId?: number): void {
    const m = pending.get(id)
    if (m && mutationId !== undefined && mutationId !== m.mutationId) return
    if (m) pending.delete(id)
    errs.delete(id)
    win.patch(serverRow)
    rev.value++
  }

  /** Server rejected: roll back only this row and record an error. */
  function rejectMutation(id: number, mutationId?: number, message = 'Update failed'): void {
    const m = pending.get(id)
    if (!m) return
    if (mutationId !== undefined && mutationId !== m.mutationId) return
    pending.delete(id)
    win.patch(m.prev)
    errs.set(id, message)
    rev.value++
  }

  // ── PAI-568: incremental refresh / delta reconciliation ──────────────────

  /**
   * Apply an incremental refresh delta to the loaded window. Patches changed
   * rows and removes deleted/no-longer-matching ones in place (preserving row
   * identity + order, which is what lets the host keep scroll, selection, the
   * side panel, and pending edits). Falls back to a full reload when the delta
   * can't be applied deterministically: stale query, server `full` flag, a
   * revision gap, or a moved-in row that can't be placed in sort order.
   */
  function applyDelta(delta: IssueListDelta<T>): DeltaResult {
    if (delta.fingerprint !== fingerprint.value) return 'ignored'
    if (delta.full) return 'reload'
    if (delta.baseRevision !== undefined && revision.value && delta.baseRevision !== revision.value) {
      return 'reload' // we missed intermediate changes
    }
    const upserts = delta.upserts ?? []
    // A row not already loaded is a moved-in/new match — we can't place it in
    // the current sort order deterministically, so defer to a full reload.
    for (const row of upserts) {
      if (!win.has(row.id)) return 'reload'
    }
    for (const row of upserts) {
      if (pending.has(row.id)) continue // a newer local edit wins
      win.patch(row)
    }
    for (const id of delta.deletes ?? []) {
      pending.delete(id)
      win.remove(id, { decrementTotal: delta.total === undefined })
    }
    if (delta.total !== undefined) win.setTotal(delta.total)
    if (delta.revision !== undefined) revision.value = delta.revision
    reassertPending()
    rev.value++
    return 'patched'
  }

  /** Initial load — call from the host's onMounted. */
  function start(): Promise<void> {
    return reload()
  }

  return {
    query,
    fingerprint,
    issues,
    total,
    loaded,
    hasMore,
    complete,
    rowErrors,
    revision: readonly(revision),
    serverFingerprint: readonly(serverFingerprint),
    selectionFingerprint: readonly(serverSelectionFingerprint),
    loading: readonly(loading),
    error: readonly(error),
    start,
    mutateRow,
    confirmMutation,
    rejectMutation,
    applyDelta,
    reload,
    refresh,
    setFilter,
    setRawFilter,
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
