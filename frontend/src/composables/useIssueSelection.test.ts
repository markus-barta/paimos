import { describe, it, expect } from 'vitest'
import { ref, nextTick } from 'vue'
import { useIssueSelection } from './useIssueSelection'

function setup(opts?: { loaded?: number[]; total?: number; fp?: string }) {
  const fp = ref(opts?.fp ?? 'fp1')
  const loaded = ref(opts?.loaded ?? [1, 2, 3])
  const total = ref(opts?.total ?? 100)
  const sel = useIssueSelection({
    fingerprint: () => fp.value,
    loadedIds: () => loaded.value,
    total: () => total.value,
  })
  return { sel, fp, loaded, total }
}

describe('useIssueSelection (PAI-565)', () => {
  it('explicit toggle tracks ids/count and collapses to none when empty', () => {
    const { sel } = setup()
    sel.toggle(1)
    sel.toggle(2)
    expect(sel.mode.value).toBe('explicit')
    expect(sel.isSelected(1)).toBe(true)
    expect(sel.effectiveCount.value).toBe(2)
    sel.toggle(1)
    expect(sel.effectiveCount.value).toBe(1)
    sel.toggle(2)
    expect(sel.mode.value).toBe('none')
  })

  it('selectAllLoaded selects exactly the loaded rows', () => {
    const { sel } = setup({ loaded: [5, 6] })
    sel.selectAllLoaded()
    expect(sel.mode.value).toBe('explicit')
    expect(sel.effectiveCount.value).toBe(2)
    expect(sel.isSelected(5)).toBe(true)
    expect(sel.isSelected(7)).toBe(false)
  })

  it('all-matching counts the full set minus exclusions', () => {
    const { sel } = setup({ total: 100, loaded: [1, 2, 3] })
    sel.selectAllMatching()
    expect(sel.mode.value).toBe('all-matching')
    expect(sel.effectiveCount.value).toBe(100)
    expect(sel.isSelected(2)).toBe(true)
    sel.toggle(2) // exclude a loaded row
    expect(sel.isSelected(2)).toBe(false)
    expect(sel.effectiveCount.value).toBe(99)
    expect(sel.loadedSelectedCount.value).toBe(2) // 1 and 3 of the loaded 3
  })

  it('a query change clears all-matching but preserves explicit', async () => {
    const { sel, fp } = setup()
    sel.selectAllMatching()
    fp.value = 'fp2'
    await nextTick()
    expect(sel.mode.value).toBe('none') // invalidated

    sel.toggle(1) // explicit
    fp.value = 'fp3'
    await nextTick()
    expect(sel.mode.value).toBe('explicit') // preserved
    expect(sel.isSelected(1)).toBe(true)
  })

  it('isStale flags an all-matching selection against a changed fingerprint', () => {
    const { sel } = setup({ fp: 'fpA' })
    sel.selectAllMatching()
    expect(sel.isStale('fpA')).toBe(false)
    expect(sel.isStale('fpB')).toBe(true)
  })

  it('resolve returns an executable description', () => {
    const { sel } = setup({ total: 50 })
    sel.setExplicit([4, 5])
    expect(sel.resolve()).toEqual({ mode: 'explicit', ids: [4, 5], count: 2 })
    sel.selectAllMatching()
    sel.toggle(9)
    expect(sel.resolve()).toEqual({
      mode: 'all-matching', fingerprint: 'fp1', exclude: [9], count: 49,
    })
  })
})
