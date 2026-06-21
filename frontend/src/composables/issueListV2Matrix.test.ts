/*
 * PAIMOS — Your Professional & Personal AI Project OS
 * Copyright (C) 2026 Markus Barta <markus@barta.com>
 *
 * Licensed under the GNU AGPL v3. See the project LICENSE.
 */

/**
 * PAI-574 — IssueList v2 regression matrix.
 *
 * Cross-cutting, composition-level scenarios for the v2 engine — the things
 * that span more than one composable and are easy to regress when the pieces
 * change independently: query transitions, lazy-load windowing, selection
 * across lazy-loaded sets, optimistic mutation vs refresh reconciliation, and
 * mode-agnostic (portal) reuse. Per-unit behavior lives in the individual
 * *.test.ts files; this file guards the seams between them.
 */
import { describe, it, expect, vi } from 'vitest'
import { useIssueQuery, type IssueListResult } from './useIssueQuery'
import { useIssueSelection } from './useIssueSelection'

type Row = { id: number; status?: string }

/** A fake server: a fixed corpus, paged + filtered like the real endpoints. */
function fakeServer(corpus: Row[]) {
  const calls: string[] = []
  const fetcher = vi.fn(async (q): Promise<IssueListResult<Row>> => {
    calls.push(q.rawFilter || q.search || 'all')
    let rows = corpus
    if (q.search.trim().length >= 2) rows = rows.filter((r) => String(r.id).includes(q.search.trim()))
    const total = rows.length
    const limit = q.window.mode === 'all' ? total : q.window.limit
    const page = rows.slice(q.window.offset, q.window.offset + limit)
    return { issues: page, total, hasMore: q.window.offset + page.length < total }
  })
  return { fetcher, calls }
}

const corpus = (n: number): Row[] => Array.from({ length: n }, (_, i) => ({ id: i + 1, status: 'todo' }))

describe('IssueList v2 matrix — query transitions', () => {
  it('show-all survives a sort change; search then clear restores the window', async () => {
    const { fetcher } = fakeServer(corpus(250))
    const c = useIssueQuery<Row>({ initial: { mode: 'internal-global' }, fetcher })
    await c.start()
    expect(c.loaded.value).toBe(100) // page
    await c.setWindow('all')
    expect(c.loaded.value).toBe(250)
    await c.setSort('id', 'asc')
    expect(c.query.window.mode).toBe('all') // not reset
    expect(c.loaded.value).toBe(250)
    await c.setSearchNow('1') // <2 chars → ignored as a filter by the fake server
    await c.setSearchNow('12') // matches ids containing "12"
    expect(c.total.value).toBeLessThan(250)
    await c.setSearchNow('')
    expect(c.total.value).toBe(250) // restored
  })
})

describe('IssueList v2 matrix — lazy load', () => {
  it('page → loadMore appends without dup → show-all completes', async () => {
    const { fetcher } = fakeServer(corpus(250))
    const c = useIssueQuery<Row>({ initial: { mode: 'internal-global' }, fetcher })
    await c.start()
    expect(c.issues.value.map((r) => r.id).slice(0, 2)).toEqual([1, 2])
    expect(c.complete.value).toBe(false)
    await c.loadMore()
    expect(c.loaded.value).toBe(200)
    expect(new Set(c.issues.value.map((r) => r.id)).size).toBe(200) // no dups
    await c.loadMore()
    expect(c.loaded.value).toBe(250)
    expect(c.complete.value).toBe(true)
  })
})

describe('IssueList v2 matrix — selection × lazy load', () => {
  function wired() {
    const { fetcher } = fakeServer(corpus(250))
    const c = useIssueQuery<Row>({ initial: { mode: 'internal-global' }, fetcher })
    const sel = useIssueSelection({
      fingerprint: () => c.fingerprint.value,
      loadedIds: () => c.issues.value.map((r) => r.id),
      total: () => c.total.value,
    })
    return { c, sel }
  }

  it('explicit selection is unaffected by loading more', async () => {
    const { c, sel } = wired()
    await c.start()
    sel.toggle(1); sel.toggle(2)
    expect(sel.effectiveCount.value).toBe(2)
    await c.loadMore()
    expect(sel.effectiveCount.value).toBe(2) // load-more didn't change it
    expect(sel.isSelected(150)).toBe(false)
  })

  it('all-matching covers newly loaded rows and survives load-more; resolve is fingerprint-bound', async () => {
    const { c, sel } = wired()
    await c.start()
    sel.selectAllMatching()
    expect(sel.effectiveCount.value).toBe(250) // full set, not just loaded
    expect(sel.isSelected(1)).toBe(true)
    await c.loadMore()
    expect(sel.isSelected(150)).toBe(true) // newly loaded row included
    const fp = c.fingerprint.value
    expect(sel.resolve()).toMatchObject({ mode: 'all-matching', fingerprint: fp, count: 250 })
    // a query change invalidates the all-matching selection
    await c.setSearchNow('12')
    expect(sel.mode.value).toBe('none')
  })
})

describe('IssueList v2 matrix — mutation × refresh', () => {
  it('optimistic edit survives a stale refresh, then confirm makes it canonical', async () => {
    const server = corpus(3)
    const fetcher = vi.fn(async (q): Promise<IssueListResult<Row>> => ({
      issues: server.slice(0, q.window.mode === 'all' ? 3 : q.window.limit),
      total: 3, hasMore: false,
    }))
    const c = useIssueQuery<Row>({ initial: { mode: 'internal-global' }, fetcher })
    await c.start()
    c.mutateRow(1, { status: 'done' })
    await c.refresh() // server still says 'todo'
    expect(c.issues.value.find((r) => r.id === 1)!.status).toBe('done') // optimistic held
    c.confirmMutation(1, { id: 1, status: 'done' })
    server[0].status = 'done' // server now reflects the confirmed change
    await c.refresh()
    expect(c.issues.value.find((r) => r.id === 1)!.status).toBe('done') // canonical
    // and the gate is cleared: a later server-side revert is no longer masked
    server[0].status = 'todo'
    await c.refresh()
    expect(c.issues.value.find((r) => r.id === 1)!.status).toBe('todo')
  })

  it('reconcile patches changed rows, removes gone, and reloads on a new row', async () => {
    const { fetcher } = fakeServer(corpus(3))
    const c = useIssueQuery<Row>({ initial: { mode: 'internal-global' }, fetcher })
    await c.start()
    expect(c.reconcile([{ id: 1, status: 'x' }, { id: 2 }], 2)).toBe('patched')
    expect(c.issues.value.map((r) => r.id)).toEqual([1, 2])
    expect(c.issues.value[0].status).toBe('x')
    expect(c.reconcile([{ id: 1 }, { id: 2 }, { id: 99 }])).toBe('reload')
  })
})

describe('IssueList v2 matrix — portal reuse (mode-agnostic)', () => {
  it('the same engine works with a portal fetcher', async () => {
    const { fetcher, calls } = fakeServer(corpus(120))
    const c = useIssueQuery<Row>({ initial: { mode: 'portal', projectId: 9 }, fetcher })
    await c.start()
    expect(c.query.mode).toBe('portal')
    expect(c.loaded.value).toBe(100)
    await c.setWindow('all')
    expect(c.loaded.value).toBe(120)
    await c.setSearchNow('11') // ids containing "11": 11, 110..119, 11 -> several
    expect(c.total.value).toBeGreaterThan(0)
    expect(calls.length).toBeGreaterThan(0)
  })
})
