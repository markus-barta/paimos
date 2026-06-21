import { describe, it, expect } from 'vitest'
import { IssueRowWindow } from './issueRowWindow'

type Row = { id: number; v?: string }

describe('IssueRowWindow', () => {
  it('setWindow replaces and records total/hasMore/fingerprint', () => {
    const w = new IssueRowWindow<Row>()
    w.setWindow([{ id: 1 }, { id: 2 }], 10, true, 'fp1')
    expect(w.rows().map((r) => r.id)).toEqual([1, 2])
    expect(w.total).toBe(10)
    expect(w.loaded).toBe(2)
    expect(w.hasMore).toBe(true)
    expect(w.complete).toBe(false)
    expect(w.fingerprint).toBe('fp1')

    w.setWindow([{ id: 5 }], 1, false, 'fp2') // replace
    expect(w.rows().map((r) => r.id)).toEqual([5])
    expect(w.complete).toBe(true)
    expect(w.fingerprint).toBe('fp2')
  })

  it('appendWindow accumulates without duplicating ids and keeps order', () => {
    const w = new IssueRowWindow<Row>()
    w.setWindow([{ id: 1 }, { id: 2 }], 4, true, 'fp')
    w.appendWindow([{ id: 2 }, { id: 3 }, { id: 4 }], 4, false) // 2 is a dup
    expect(w.rows().map((r) => r.id)).toEqual([1, 2, 3, 4]) // no dup, stable order
    expect(w.loaded).toBe(4)
    expect(w.hasMore).toBe(false)
    expect(w.complete).toBe(true)
  })

  it('a duplicate append updates the row in place but keeps its position', () => {
    const w = new IssueRowWindow<Row>()
    w.setWindow([{ id: 1, v: 'a' }, { id: 2, v: 'b' }], 2, false, 'fp')
    w.appendWindow([{ id: 1, v: 'A' }], 2, false)
    expect(w.rows()).toEqual([{ id: 1, v: 'A' }, { id: 2, v: 'b' }])
  })

  it('distinguishes loaded subset from all-matching count', () => {
    const w = new IssueRowWindow<Row>()
    w.setWindow([{ id: 1 }, { id: 2 }], 100, true, 'fp')
    expect(w.loaded).toBe(2)
    expect(w.total).toBe(100)
    expect(w.complete).toBe(false) // only a subset
  })

  it('patch replaces a loaded row in place; false for unknown id', () => {
    const w = new IssueRowWindow<Row>()
    w.setWindow([{ id: 1, v: 'a' }], 1, false, 'fp')
    expect(w.patch({ id: 1, v: 'z' })).toBe(true)
    expect(w.rows()).toEqual([{ id: 1, v: 'z' }])
    expect(w.patch({ id: 99, v: 'x' })).toBe(false)
  })

  it('remove drops a row and can decrement the total', () => {
    const w = new IssueRowWindow<Row>()
    w.setWindow([{ id: 1 }, { id: 2 }, { id: 3 }], 3, false, 'fp')
    expect(w.remove(2, { decrementTotal: true })).toBe(true)
    expect(w.rows().map((r) => r.id)).toEqual([1, 3])
    expect(w.total).toBe(2)
    expect(w.remove(2)).toBe(false) // already gone
  })

  it('reset clears everything and rebinds the fingerprint', () => {
    const w = new IssueRowWindow<Row>()
    w.setWindow([{ id: 1 }], 1, false, 'fp')
    w.reset('fp-new')
    expect(w.rows()).toEqual([])
    expect(w.loaded).toBe(0)
    expect(w.total).toBe(0)
    expect(w.fingerprint).toBe('fp-new')
  })
})
