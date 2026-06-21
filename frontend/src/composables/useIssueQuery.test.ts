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
        status: [], priority: [], type: [], costUnit: [], release: [],
        assignee: [], tags: [], projects: [], sprints: [], epic: [],
        dateField: null, dateFrom: null, dateTo: null,
      },
      search: '', sort: { key: 'created_at', dir: 'desc' },
      window: { mode: 'page', limit: 100, offset: 0 },
      viewId: null, tab: null,
    }
    const paged = queryFingerprint(base)
    const all = queryFingerprint({ ...base, window: { mode: 'all', limit: 0, offset: 0 } })
    expect(all).not.toBe(paged)
  })
})
