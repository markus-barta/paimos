import { describe, expect, it } from 'vitest'
import { clampColumnWidth, normalizeColumnWidths } from './useColumnWidths'

describe('column width helpers', () => {
  it('keeps only known finite column widths', () => {
    expect(normalizeColumnWidths({
      title: 420.4,
      status: '104',
      unknown: 200,
      assignee: Number.NaN,
    })).toEqual({
      title: 420,
      status: 104,
    })
  })

  it('clamps widths to per-column limits', () => {
    expect(clampColumnWidth('title', 20)).toBe(220)
    expect(clampColumnWidth('title', 1200)).toBe(760)
    expect(clampColumnWidth('status', 20)).toBe(92)
  })
})
