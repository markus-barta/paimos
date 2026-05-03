import { describe, expect, it } from 'vitest'

import { issueSearchSummary } from './issueSearchSummary'

describe('issueSearchSummary', () => {
  it('states when search results are capped and recently updated first', () => {
    expect(issueSearchSummary(100, 1234, 'ma')).toBe(
      `Showing first 100 of ${(1234).toLocaleString()} matches for "ma" · recently updated first`,
    )
  })

  it('states the full match count once all search results are loaded', () => {
    expect(issueSearchSummary(37, 37, 'ma')).toBe(
      '37 matches for "ma" · recently updated first',
    )
  })

  it('trims the displayed query', () => {
    expect(issueSearchSummary(0, 0, '  ma  ')).toBe(
      '0 matches for "ma" · recently updated first',
    )
  })
})
