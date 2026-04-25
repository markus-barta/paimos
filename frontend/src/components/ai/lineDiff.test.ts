/*
 * PAIMOS — Your Professional & Personal AI Project OS
 * Copyright (C) 2026 Markus Barta <markus@barta.com>
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as
 * published by the Free Software Foundation, version 3.
 *
 * PAI-148. Unit tests for the line-diff helper used by the optimize
 * preview overlay. The component contract is "left.length ===
 * right.length and the two arrays are renderable side-by-side";
 * these tests pin that down.
 */

import { describe, it, expect } from 'vitest'
import { lineDiff } from './lineDiff'

describe('lineDiff', () => {
  it('returns equal-length aligned arrays', () => {
    const r = lineDiff('a\nb\nc', 'a\nB\nc')
    expect(r.left.length).toBe(r.right.length)
    // Aligned middle row: del+add, not eq+eq.
    expect(r.left[1].type).toBe('del')
    expect(r.right[1].type).toBe('pad')
  })

  it('marks identical text as all-eq', () => {
    const r = lineDiff('one\ntwo', 'one\ntwo')
    for (const l of r.left) expect(l.type).toBe('eq')
    for (const l of r.right) expect(l.type).toBe('eq')
  })

  it('handles empty original (every right line is an add or pad)', () => {
    const r = lineDiff('', 'hello\nworld')
    // No content from the original survived — left has no eq rows.
    expect(r.left.some(l => l.type === 'eq' && l.text !== '')).toBe(false)
    expect(r.right.filter(l => l.type === 'add').length).toBe(2)
  })

  it('handles empty optimized (every left non-empty line is a del)', () => {
    const r = lineDiff('hello\nworld', '')
    // No content survived on the right.
    expect(r.right.some(l => l.type === 'eq' && l.text !== '')).toBe(false)
    expect(r.left.filter(l => l.type === 'del' && l.text !== '').length).toBe(2)
  })

  it('preserves checklist line shape', () => {
    // Checklists are the most common acceptance_criteria shape — we
    // want del/add per item, not a single hunk that flattens them.
    const before = '- [ ] one\n- [ ] two\n- [ ] three'
    const after  = '- [ ] One\n- [ ] two\n- [x] three'
    const r = lineDiff(before, after)
    // Two changed lines (one capitalised, one ticked); one unchanged.
    const dels = r.left.filter(l => l.type === 'del').length
    const adds = r.right.filter(l => l.type === 'add').length
    expect(dels).toBe(2)
    expect(adds).toBe(2)
  })

  it('keeps blank-line context anchored', () => {
    // A blank line between paragraphs should anchor the diff; without
    // anchoring, the two paragraphs would be marked as a single
    // del-then-add hunk and the user would lose the visual landmark.
    const before = 'first paragraph.\n\nsecond paragraph.'
    const after  = 'first paragraph!\n\nsecond paragraph?'
    const r = lineDiff(before, after)
    // The middle blank row should appear as eq on both sides.
    const blankRows = r.left.filter((l, i) =>
      l.type === 'eq' && l.text === '' && r.right[i].type === 'eq' && r.right[i].text === '',
    )
    expect(blankRows.length).toBeGreaterThan(0)
  })
})
