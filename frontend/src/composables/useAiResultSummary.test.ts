import { describe, expect, it } from 'vitest'
import { summarizeAiResult } from '@/composables/useAiResultSummary'

describe('summarizeAiResult', () => {
  it('summarizes parent suggestions with score', () => {
    expect(summarizeAiResult({
      action: 'find_parent',
      body: { candidates: [{ issue_key: 'PAI-83', score: 0.87 }] },
    })).toBe('Top match: PAI-83 (87%)')
  })

  it('summarizes estimates with hours and LP', () => {
    expect(summarizeAiResult({
      action: 'estimate_effort',
      body: { hours: 6, lp: 1 },
    })).toBe('6h · 1 LP suggested')
  })

  it('summarizes rewrites by tightened chars', () => {
    expect(summarizeAiResult({
      action: 'optimize',
      body: {},
      sourceText: 'A slightly longer sentence.',
      optimizedText: 'Shorter sentence.',
    })).toContain('chars')
  })
})
