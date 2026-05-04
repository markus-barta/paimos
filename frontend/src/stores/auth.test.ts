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

import { describe, it, expect, beforeEach } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useAuthStore, type User } from './auth'
import { sessionExpired } from '@/api/client'

/**
 * PAI-83 regression guard.
 *
 * Bug: sessionExpired was flipped to true by the 401 interceptor on any
 * stale request, but never reset. A user logging in after a session
 * expiry saw the dashboard with the "Session expired" banner still
 * stuck at the top. Both login paths (password + TOTP) call
 * auth.setUser(), so clearing the flag there covers both.
 */

function fakeUser(): User {
  return {
    id: 1,
    username: 'mba',
    role: 'admin',
    created_at: '2026-01-01T00:00:00Z',
    nickname: '',
    first_name: '',
    last_name: '',
    email: '',
    avatar_path: '',
    markdown_default: true,
    monospace_fields: false,
    recent_projects_limit: 10,
    internal_rate_hourly: null,
    show_alt_unit_table: false,
    show_alt_unit_detail: false,
    locale: 'en',
    recent_timers_limit: 10,
    timezone: 'auto',
    preview_hover_delay: 300,
    issue_auto_refresh_enabled: true,
    issue_auto_refresh_interval_seconds: 60,
    last_login_at: null,
    accruals_stats_enabled: false,
    accruals_extra_statuses: '',
  }
}

describe('auth store — sessionExpired lifecycle (PAI-83)', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    sessionExpired.value = false
  })

  it('setUser clears a stale sessionExpired flag', () => {
    sessionExpired.value = true
    const auth = useAuthStore()
    auth.setUser(fakeUser())
    expect(sessionExpired.value).toBe(false)
  })

  it('setUser also hydrates user + checked', () => {
    const auth = useAuthStore()
    const u = fakeUser()
    auth.setUser(u)
    expect(auth.user).toEqual(u)
    expect(auth.checked).toBe(true)
  })
})
