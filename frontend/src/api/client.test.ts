/*
 * PAIMOS — Your Professional & Personal AI Project OS
 * Copyright (C) 2026 Markus Barta <markus@barta.com>
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as
 * published by the Free Software Foundation, version 3.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
 * GNU Affero General Public License for more details.
 *
 * You should have received a copy of the GNU Affero General Public
 * License along with this program. If not, see <https://www.gnu.org/licenses/>.
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { ApiError, api, errMsg, mustChangePassword, sessionExpired } from './client'

/**
 * ACME-1 regression guards for the 401 → sessionExpired interceptor.
 *
 * The contract is:
 *   - A 401 on any non-auth endpoint flips sessionExpired.value to true
 *     so the AppLayout banner renders.
 *   - A 401 on /auth/login, /auth/me, /auth/forgot, /auth/reset,
 *     /auth/totp/verify, or /auth/reset/validate does NOT flip the
 *     flag — those 401s are expected (wrong password, pristine load,
 *     bad reset token) and would nag the user on the login page.
 *
 * We stub global.fetch to return canned status codes without spinning
 * up a real server.
 */

type FetchStub = (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>

function stubFetch(impl: FetchStub) {
  globalThis.fetch = vi.fn(impl) as unknown as typeof fetch
}

function makeResponse(status: number, body: unknown = { error: 'unauthorized' }): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

describe('api client 401 interceptor', () => {
  let originalFetch: typeof fetch

  beforeEach(() => {
    originalFetch = globalThis.fetch
    sessionExpired.value = false
    mustChangePassword.value = false
  })

  afterEach(() => {
    globalThis.fetch = originalFetch
  })

  it('flips sessionExpired on 401 from a data endpoint', async () => {
    stubFetch(async () => makeResponse(401))
    await expect(api.get('/projects')).rejects.toThrow()
    expect(sessionExpired.value).toBe(true)
  })

  it('flips sessionExpired on 401 from a non-GET data endpoint', async () => {
    stubFetch(async () => makeResponse(401))
    await expect(api.post('/issues', { title: 'x' })).rejects.toThrow()
    expect(sessionExpired.value).toBe(true)
  })

  it('does NOT flip sessionExpired on 401 from /auth/login (wrong password)', async () => {
    stubFetch(async () => makeResponse(401))
    await expect(api.post('/auth/login', { username: 'a', password: 'b' })).rejects.toThrow()
    expect(sessionExpired.value).toBe(false)
  })

  it('does NOT flip sessionExpired on 401 from /auth/me (pristine page load)', async () => {
    stubFetch(async () => makeResponse(401))
    await expect(api.get('/auth/me')).rejects.toThrow()
    expect(sessionExpired.value).toBe(false)
  })

  it('does NOT flip sessionExpired on 401 from /auth/reset/validate (bad token)', async () => {
    stubFetch(async () => makeResponse(401))
    await expect(api.get('/auth/reset/validate?token=bad')).rejects.toThrow()
    expect(sessionExpired.value).toBe(false)
  })

  it('does NOT flip sessionExpired on a successful request', async () => {
    stubFetch(async () => makeResponse(200, { ok: true }))
    await api.get('/projects')
    expect(sessionExpired.value).toBe(false)
  })

  it('does NOT flip sessionExpired on a non-401 error', async () => {
    stubFetch(async () => makeResponse(500, { error: 'boom' }))
    await expect(api.get('/projects')).rejects.toThrow()
    expect(sessionExpired.value).toBe(false)
  })

  it('salvages JSON objects with stray bytes before the payload', async () => {
    stubFetch(async () => new Response('null{"action":"optimize","body":{"optimized":"Better text"}}', {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    }))

    const payload = await api.get<{ action: string; body: { optimized: string } }>('/ai/action-preview')

    expect(payload.action).toBe('optimize')
    expect(payload.body.optimized).toBe('Better text')
  })

  it('surfaces a clean ApiError when the server returns invalid JSON', async () => {
    stubFetch(async () => new Response('nullboom', {
      status: 502,
      headers: { 'Content-Type': 'application/json' },
    }))

    await expect(api.get('/ai/action')).rejects.toThrow('invalid JSON response: nullboom')
  })

  it('hydrates ApiError from Problem Details bodies', async () => {
    stubFetch(async () => makeResponse(400, {
      type: 'https://paimos.com/errors/enum_violation',
      title: 'Invalid enum value',
      status: 400,
      detail: 'type "Tasky" is not valid',
      code: 'enum_violation',
      field: 'type',
      valid_values: ['ticket', 'task'],
      request_id: 'req-1',
    }))

    try {
      await api.post('/projects/6/issues', { title: 'Bad', type: 'Tasky' })
      throw new Error('expected request to fail')
    } catch (e) {
      expect(e).toBeInstanceOf(ApiError)
      const err = e as ApiError
      expect(err.message).toBe('type "Tasky" is not valid')
      expect(err.code).toBe('enum_violation')
      expect(err.field).toBe('type')
      expect(err.valid_values).toEqual(['ticket', 'task'])
      expect(err.request_id).toBe('req-1')
    }
  })

  it('uses Problem Details code for the must-change-password gate', async () => {
    stubFetch(async () => makeResponse(403, {
      type: 'https://paimos.com/errors/must_change_password',
      title: 'Password change required',
      status: 403,
      detail: 'password change required before continuing',
      code: 'must_change_password',
      error: 'must_change_password',
      request_id: 'req-2',
    }))

    let caught: unknown
    try {
      await api.get('/projects')
    } catch (e) {
      caught = e
    }
    expect(mustChangePassword.value).toBe(true)
    expect(errMsg(caught)).toBe('')
  })
})
