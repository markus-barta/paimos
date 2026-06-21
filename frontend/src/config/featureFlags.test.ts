import { describe, it, expect, afterEach } from 'vitest'
import { isIssueListV2 } from './featureFlags'

afterEach(() => localStorage.clear())

describe('isIssueListV2 (PAI-575 flip: default ON)', () => {
  it('defaults to on when the flag is unset', () => {
    expect(isIssueListV2()).toBe(true)
  })
  it('stays on for any value other than the explicit off-switch', () => {
    localStorage.setItem('ff_issuelist_v2', '1')
    expect(isIssueListV2()).toBe(true)
  })
  it('is off only when explicitly disabled with "0"', () => {
    localStorage.setItem('ff_issuelist_v2', '0')
    expect(isIssueListV2()).toBe(false)
  })
})
