import { describe, expect, it } from 'vitest'

import { postLoginRedirectOrFallback, safePostLoginRedirect } from './redirects'

describe('post-login redirects', () => {
  it('accepts same-origin app paths including query strings', () => {
    expect(safePostLoginRedirect('/issues/PAI-265')).toBe('/issues/PAI-265')
    expect(safePostLoginRedirect('/projects/6/issues/PAI-265?edit=1')).toBe(
      '/projects/6/issues/PAI-265?edit=1',
    )
  })

  it('rejects external, protocol-relative, and login-loop targets', () => {
    for (const value of [
      'https://example.com/issues/1',
      '//example.com/issues/1',
      'issues/1',
      '/login',
      '/login?redirect=/issues/1',
      '/login/reset',
    ]) {
      expect(
        safePostLoginRedirect(value),
        `${value} should be rejected`,
      ).toBeNull()
    }
  })

  it('uses the first redirect value and falls back when no safe value exists', () => {
    expect(postLoginRedirectOrFallback(['/issues/1', '/issues/2'])).toBe(
      '/issues/1',
    )
    expect(postLoginRedirectOrFallback('/login', '/portal')).toBe('/portal')
  })
})
