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

import { defineStore } from 'pinia'
import { ref } from 'vue'
import { api, sessionExpired } from '@/api/client'
import router from '@/router'
import i18n from '@/i18n'
import { setDisplayTimezone } from '@/utils/formatTime'
import { useSearchStore } from '@/stores/search'

export interface User {
  id: number
  username: string
  role: 'admin' | 'member' | 'external'
  created_at: string
  // Profile fields (migration 25)
  nickname:    string
  first_name:  string
  last_name:   string
  email:       string
  avatar_path: string  // relative path e.g. /avatars/1.jpg — served by Go
  // Editor preferences (migration 29)
  markdown_default: boolean
  monospace_fields: boolean
  // Recent projects limit (migration 38)
  recent_projects_limit: number
  // Internal hourly rate (migration 39)
  internal_rate_hourly: number | null
  // Alt-unit display preferences (migration 44)
  show_alt_unit_table: boolean
  show_alt_unit_detail: boolean
  // Locale (migration 47)
  locale: string
  // Recent timers limit (migration 49)
  recent_timers_limit: number
  // Display timezone (migration 50) — 'auto' = browser local
  timezone: string
  // Preview hover delay in ms (migration 53)
  preview_hover_delay: number
  // Last login timestamp (migration 54)
  last_login_at: string | null
  // Accruals report preferences (migration 62) — admin-only feature
  accruals_stats_enabled: boolean
  accruals_extra_statuses: string
}

// AccessLevel mirrors backend auth.AccessLevel.
export type AccessLevel = 'viewer' | 'editor'

// AccessResponse is the `access` field on the login / totp / me responses.
// AllProjects=true is the admin shortcut; otherwise `levels` lists every
// project the user has at least viewer access on.
export interface AccessResponse {
  all_projects: boolean
  levels: Record<string, AccessLevel>
}

// MeResponse is the envelope returned by /auth/login, /auth/totp/verify,
// and /auth/me. Parsed once to hydrate the access Map.
//
// PAI-267: via_dev_login is true iff the current request authenticated
// via the dev-login route. Only set in development builds with the
// dev_login backend tag — production /auth/me always omits it.
export interface MeResponse {
  user: User
  access: AccessResponse
  via_dev_login?: boolean
}

export const useAuthStore = defineStore('auth', () => {
  const user = ref<User | null>(null)
  const checked = ref(false)
  const totpEnabled = ref(false)
  const totpChecked = ref(false)

  // accessibleProjects maps project ID (number) → access level. Admins
  // get an empty map and `allProjects=true` — the helpers below handle
  // both branches.
  const accessibleProjects = ref<Map<number, AccessLevel>>(new Map())
  const allProjects = ref(false)

  // PAI-267: true iff the current session was created via the
  // dev-login route. Drives the non-dismissable AppDevLoginBanner in
  // AppLayout. Always false in production frontend builds talking to a
  // production backend — the backend simply never sets the field.
  const viaDevLogin = ref(false)

  function hydrateAccess(access: AccessResponse | undefined | null) {
    const m = new Map<number, AccessLevel>()
    allProjects.value = !!access?.all_projects
    if (access?.levels) {
      for (const [k, v] of Object.entries(access.levels)) {
        const id = Number(k)
        if (!Number.isNaN(id)) m.set(id, v)
      }
    }
    accessibleProjects.value = m
  }

  function canView(projectId: number | null | undefined): boolean {
    if (projectId == null) return true // orphan / no project — show
    if (allProjects.value) return true
    return accessibleProjects.value.has(projectId)
  }

  function canEdit(projectId: number | null | undefined): boolean {
    if (projectId == null) return true
    if (allProjects.value) return true
    return accessibleProjects.value.get(projectId) === 'editor'
  }

  async function fetchMe() {
    try {
      const resp = await api.get<MeResponse>('/auth/me')
      user.value = resp.user
      hydrateAccess(resp.access)
      viaDevLogin.value = !!resp.via_dev_login
      if (user.value?.locale) i18n.global.locale.value = user.value.locale as 'en' | 'de'
      setDisplayTimezone(user.value?.timezone)
      await fetchTOTPStatus()
    } catch {
      user.value = null
      allProjects.value = false
      accessibleProjects.value = new Map()
      viaDevLogin.value = false
      totpEnabled.value = false
      totpChecked.value = false
    } finally {
      checked.value = true
    }
  }

  async function login(username: string, password: string) {
    const resp = await api.post<MeResponse>('/auth/login', { username, password })
    user.value = resp.user
    hydrateAccess(resp.access)
    viaDevLogin.value = !!resp.via_dev_login
    if (user.value?.locale) i18n.global.locale.value = user.value.locale as 'en' | 'de'
    checked.value = true
    sessionExpired.value = false
    await fetchTOTPStatus()
  }

  // PAI-83: installing a fresh authenticated user proves the session is
  // valid again; clear the banner flag here so both password and TOTP
  // login paths (which converge on setUser) recover from a stale 401.
  function setUser(u: User) {
    user.value = u
    checked.value = true
    sessionExpired.value = false
  }

  async function fetchTOTPStatus(force = false) {
    if (!user.value) {
      totpEnabled.value = false
      totpChecked.value = false
      return false
    }
    if (totpChecked.value && !force) return totpEnabled.value
    try {
      totpEnabled.value = (await api.get<{ enabled: boolean }>('/auth/totp/status')).enabled
      totpChecked.value = true
    } catch {
      totpEnabled.value = false
      totpChecked.value = false
    }
    return totpEnabled.value
  }

  function setTOTPEnabled(enabled: boolean) {
    totpEnabled.value = enabled
    totpChecked.value = true
  }

  async function logout() {
    try { await api.post('/auth/logout', {}) } catch { /* ignore */ }
    user.value = null
    allProjects.value = false
    accessibleProjects.value = new Map()
    viaDevLogin.value = false
    totpEnabled.value = false
    totpChecked.value = false
    checked.value = true
    // PAI-242: search query persists across users via localStorage; reset
    // it on logout so the next login doesn't pre-fill the prior session's
    // sidebar search input.
    useSearchStore().clear()
    router.push('/login')
  }

  // Re-fetch /auth/me and update user + access state — used after
  // profile/avatar changes and after permission edits that should apply
  // immediately without a page reload.
  async function refreshMe() {
    try {
      const resp = await api.get<MeResponse>('/auth/me')
      user.value = resp.user
      hydrateAccess(resp.access)
      viaDevLogin.value = !!resp.via_dev_login
    } catch { /* ignore */ }
  }

  return {
    user,
    checked,
    totpEnabled,
    totpChecked,
    accessibleProjects,
    allProjects,
    viaDevLogin,
    fetchMe,
    login,
    setUser,
    hydrateAccess,
    canView,
    canEdit,
    fetchTOTPStatus,
    setTOTPEnabled,
    logout,
    refreshMe,
  }
})
