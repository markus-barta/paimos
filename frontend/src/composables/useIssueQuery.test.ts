import { describe, it, expect, vi } from 'vitest'
import {
  useIssueQuery,
  queryFingerprint,
  type IssueQuery,
  type IssueListResult,
} from './useIssueQuery'

type Row = { id: number }

function makeFetcher(
  rows: (q: IssueQuery) => Row[],
  opts?: { total?: number; hasMore?: boolean },
) {
  const calls: IssueQuery[] = []
  const fetcher = vi.fn(
    async (q: IssueQuery, _signal: AbortSignal): Promise<IssueListResult<Row>> => {
      calls.push(q)
      const issues = rows(q)
      return { issues, total: opts?.total ?? issues.length, hasMore: opts?.hasMore ?? false }
    },
  )
  return { fetcher, calls }
}

describe('useIssueQuery', () => {
  it('setSort does not reset the show-all window (PAI-563 AC)', async () => {
    const { fetcher } = makeFetcher(() => [{ id: 1 }])
    const c = useIssueQuery<Row>({ initial: { mode: 'internal-global' }, fetcher })
    await c.start()

    await c.setWindow('all')
    expect(c.query.window.mode).toBe('all')
    expect(c.query.window.limit).toBe(0)

    await c.setSort('title', 'asc')
    expect(c.query.window.mode).toBe('all') // unchanged
    expect(c.query.window.limit).toBe(0)
    expect(c.query.sort).toEqual({ key: 'title', dir: 'asc' })
  })

  it('debounces setSearch into a single reload with the latest term', async () => {
    vi.useFakeTimers()
    try {
      const { fetcher, calls } = makeFetcher((q) => [{ id: q.search.length }])
      const c = useIssueQuery<Row>({ initial: { mode: 'internal-global' }, fetcher, debounceMs: 150 })
      await c.start()
      const before = calls.length

      c.setSearch('a')
      c.setSearch('ab')
      c.setSearch('abc')
      expect(calls.length).toBe(before) // nothing fired yet

      await vi.advanceTimersByTimeAsync(150)
      expect(calls.length).toBe(before + 1)
      expect(calls[calls.length - 1].search).toBe('abc')
    } finally {
      vi.useRealTimers()
    }
  })

  it('applyView applies filters + sort + viewId in one fetch', async () => {
    const { fetcher, calls } = makeFetcher(() => [{ id: 1 }])
    const c = useIssueQuery<Row>({ initial: { mode: 'internal-project', projectId: 7 }, fetcher })
    await c.start()
    const before = calls.length

    await c.applyView({
      filters: { status: ['done'] },
      sort: { key: 'priority', dir: 'asc' },
      viewId: 3,
    })

    expect(calls.length).toBe(before + 1) // single atomic fetch
    expect(c.query.filters.status).toEqual(['done'])
    expect(c.query.sort).toEqual({ key: 'priority', dir: 'asc' })
    expect(c.query.viewId).toBe(3)
  })

  it('ignores a stale (out-of-order) response (PAI-563 AC)', async () => {
    const deferreds: Array<(r: IssueListResult<Row>) => void> = []
    const fetcher = vi.fn(
      (_q: IssueQuery, _signal: AbortSignal) =>
        new Promise<IssueListResult<Row>>((resolve) => { deferreds.push(resolve) }),
    )
    const c = useIssueQuery<Row>({ initial: { mode: 'internal-global' }, fetcher })

    const p1 = c.setSearchNow('first') // request #1 (pending)
    const p2 = c.setSearchNow('second') // request #2 (pending), supersedes #1

    // Resolve the NEWER request first, then the older one.
    deferreds[1]({ issues: [{ id: 2 }], total: 1, hasMore: false })
    deferreds[0]({ issues: [{ id: 1 }], total: 1, hasMore: false })
    await Promise.all([p1, p2])

    expect(c.issues.value.map((r) => r.id)).toEqual([2]) // late #1 dropped
  })

  it('loadMore appends with the same fingerprint', async () => {
    let page = 0
    const { fetcher } = makeFetcher(
      () => (++page === 1 ? [{ id: 1 }, { id: 2 }] : [{ id: 3 }, { id: 4 }]),
      { total: 4, hasMore: true },
    )
    const c = useIssueQuery<Row>({ initial: { mode: 'internal-global' }, fetcher })
    await c.start()
    const fp = c.fingerprint.value
    expect(c.issues.value.map((r) => r.id)).toEqual([1, 2])

    await c.loadMore()
    expect(c.issues.value.map((r) => r.id)).toEqual([1, 2, 3, 4]) // appended
    expect(c.fingerprint.value).toBe(fp) // pagination is not a new query
  })

  it('fingerprint excludes offset and is array-order independent', async () => {
    const { fetcher } = makeFetcher(() => [])
    const c = useIssueQuery<Row>({ initial: { mode: 'internal-global' }, fetcher })
    const fp0 = c.fingerprint.value

    c.query.window.offset = 999
    expect(c.fingerprint.value).toBe(fp0) // offset excluded

    await c.setFilter({ status: ['a', 'b'] })
    const fpAB = c.fingerprint.value
    await c.setFilter({ status: ['b', 'a'] })
    expect(c.fingerprint.value).toBe(fpAB) // selection order irrelevant
  })

  it('switching window mode changes the fingerprint (different result set)', () => {
    const base: IssueQuery = {
      mode: 'internal-global', projectId: null,
      filters: {
        status: [], priority: [], type: [], costUnit: [], release: [], aiStatus: [],
        assignee: [], tags: [], projects: [], sprints: [], epic: [],
        dateField: null, dateFrom: null, dateTo: null,
      },
      search: '', sort: { key: 'created_at', dir: 'desc' },
      window: { mode: 'page', limit: 100, offset: 0 },
      viewId: null, tab: null, rawFilter: '',
    }
    const paged = queryFingerprint(base)
    const all = queryFingerprint({ ...base, window: { mode: 'all', limit: 0, offset: 0 } })
    expect(all).not.toBe(paged)
  })
})

describe('useIssueQuery inline reconciliation (PAI-567)', () => {
  type R = { id: number; status?: string }
  function ctrl(rows: () => R[]) {
    const fetcher = vi.fn(async (): Promise<IssueListResult<R>> => {
      const issues = rows()
      return { issues, total: issues.length, hasMore: false }
    })
    return useIssueQuery<R>({ initial: { mode: 'internal-global' }, fetcher })
  }

  it('optimistic edit is not reverted by a stale reload', async () => {
    const serverRows: R[] = [{ id: 1, status: 'todo' }]
    const c = ctrl(() => serverRows)
    await c.start()
    c.mutateRow(1, { status: 'done' })
    expect(c.issues.value).toEqual([{ id: 1, status: 'done' }])
    await c.refresh() // server still returns the old value
    expect(c.issues.value).toEqual([{ id: 1, status: 'done' }]) // optimistic wins
  })

  it('confirm makes the server row canonical and clears the gate', async () => {
    let serverRows: R[] = [{ id: 1, status: 'todo' }]
    const c = ctrl(() => serverRows)
    await c.start()
    const mid = c.mutateRow(1, { status: 'done' })!
    c.confirmMutation(1, { id: 1, status: 'done' }, mid)
    serverRows = [{ id: 1, status: 'todo' }] // server authority changes
    await c.refresh()
    expect(c.issues.value).toEqual([{ id: 1, status: 'todo' }]) // gate gone
  })

  it('reject rolls back only the affected row and records an error', async () => {
    const c = ctrl(() => [{ id: 1, status: 'todo' }, { id: 2, status: 'todo' }])
    await c.start()
    const mid = c.mutateRow(1, { status: 'done' })!
    c.rejectMutation(1, mid, 'nope')
    expect(c.issues.value.find((r) => r.id === 1)!.status).toBe('todo')
    expect(c.issues.value.find((r) => r.id === 2)!.status).toBe('todo')
    expect(c.rowErrors.value.get(1)).toBe('nope')
  })

  it('an older confirm cannot override a newer pending edit', async () => {
    const c = ctrl(() => [{ id: 1, status: 'todo' }])
    await c.start()
    const m1 = c.mutateRow(1, { status: 'a' })!
    c.mutateRow(1, { status: 'b' }) // newer
    c.confirmMutation(1, { id: 1, status: 'a' }, m1) // stale confirm, ignored
    expect(c.issues.value).toEqual([{ id: 1, status: 'b' }])
  })
})

describe('useIssueQuery delta refresh (PAI-568)', () => {
  type R = { id: number; status?: string }
  function ctrl(rows: R[], revision = 'r1') {
    const fetcher = vi.fn(async (): Promise<IssueListResult<R>> => ({
      issues: rows, total: rows.length, hasMore: false, revision,
    }))
    return useIssueQuery<R>({ initial: { mode: 'internal-global' }, fetcher })
  }

  it('patches a changed row in place, preserving order', async () => {
    const c = ctrl([{ id: 1, status: 'a' }, { id: 2, status: 'a' }, { id: 3, status: 'a' }])
    await c.start()
    const res = c.applyDelta({ fingerprint: c.fingerprint.value, upserts: [{ id: 2, status: 'b' }] })
    expect(res).toBe('patched')
    expect(c.issues.value.map((r) => `${r.id}:${r.status}`)).toEqual(['1:a', '2:b', '3:a'])
  })

  it('removes a deleted row and decrements total', async () => {
    const c = ctrl([{ id: 1 }, { id: 2 }, { id: 3 }])
    await c.start()
    expect(c.applyDelta({ fingerprint: c.fingerprint.value, deletes: [2] })).toBe('patched')
    expect(c.issues.value.map((r) => r.id)).toEqual([1, 3])
    expect(c.total.value).toBe(2)
  })

  it('falls back to reload for a moved-in (unloaded) row', async () => {
    const c = ctrl([{ id: 1 }])
    await c.start()
    expect(c.applyDelta({ fingerprint: c.fingerprint.value, upserts: [{ id: 99 }] })).toBe('reload')
    expect(c.issues.value.map((r) => r.id)).toEqual([1]) // unchanged
  })

  it('reloads on the full flag and ignores a stale fingerprint', async () => {
    const c = ctrl([{ id: 1 }])
    await c.start()
    expect(c.applyDelta({ fingerprint: c.fingerprint.value, full: true })).toBe('reload')
    expect(c.applyDelta({ fingerprint: 'other', deletes: [1] })).toBe('ignored')
    expect(c.issues.value.map((r) => r.id)).toEqual([1])
  })

  it('reloads on a revision gap', async () => {
    const c = ctrl([{ id: 1 }], 'r1')
    await c.start()
    expect(c.applyDelta({ fingerprint: c.fingerprint.value, baseRevision: 'r0', upserts: [{ id: 1 }] })).toBe('reload')
  })

  it('a delta cannot clobber a newer pending edit', async () => {
    const c = ctrl([{ id: 1, status: 'todo' }])
    await c.start()
    c.mutateRow(1, { status: 'local' })
    c.applyDelta({ fingerprint: c.fingerprint.value, upserts: [{ id: 1, status: 'server' }] })
    expect(c.issues.value).toEqual([{ id: 1, status: 'local' }])
  })

  it('reconcile patches changed rows and removes gone ones in place', async () => {
    const c = ctrl([{ id: 1, status: 'a' }, { id: 2, status: 'a' }, { id: 3, status: 'a' }])
    await c.start()
    // fresh poll: id 2 changed, id 3 gone (id 1 unchanged)
    const res = c.reconcile([{ id: 1, status: 'a' }, { id: 2, status: 'b' }], 2)
    expect(res).toBe('patched')
    expect(c.issues.value.map((r) => `${r.id}:${r.status}`)).toEqual(['1:a', '2:b'])
    expect(c.total.value).toBe(2)
  })

  it('reconcile full-reloads when a new row appears', async () => {
    let serverRows: R[] = [{ id: 1 }]
    const fetcher = vi.fn(async (): Promise<IssueListResult<R>> => ({
      issues: serverRows, total: serverRows.length, hasMore: false,
    }))
    const c = useIssueQuery<R>({ initial: { mode: 'internal-global' }, fetcher })
    await c.start()
    serverRows = [{ id: 1 }, { id: 2 }] // a new row exists server-side now
    const res = c.reconcile([{ id: 1 }, { id: 2 }])
    expect(res).toBe('reload')
    await Promise.resolve(); await Promise.resolve() // let refresh() settle
    expect(c.issues.value.map((r) => r.id)).toEqual([1, 2])
  })
})
