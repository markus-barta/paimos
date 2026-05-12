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
import { computed, ref, watch } from 'vue'
import { api, announceSessionRestored, permissionsEpoch } from '@/api/client'
import router from '@/router'
import i18n from '@/i18n'
import { setDisplayTimezone } from '@/utils/formatTime'
import { useSearchStore } from '@/stores/search'

export interface User {
  id: number
  username: string
  role: 'admin' | 'member' | 'external' | 'super_admin'
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
  // Issue list auto-refresh preferences (migration 88)
  issue_auto_refresh_enabled: boolean
  issue_auto_refresh_interval_seconds: number
  // PAI-368 / M103: search-scope shortcut (JSON or '' = disabled).
  // See useSearchScopeShortcut for parse + matcher.
  search_scope_shortcut: string
  // Last login timestamp (migration 54)
  last_login_at: string | null
  // Accruals report preferences (migration 62) — admin-only feature
  accruals_stats_enabled: boolean
  accruals_extra_statuses: string
  // PAI-336: compatibility flag. The canonical public role is now
  // `super_admin`; this remains for older clients and cautious UI gates.
  is_super_admin: boolean
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

  const isAdmin = computed(() => user.value?.role === 'admin' || user.value?.role === 'super_admin')
  const isSuperAdmin = computed(() => user.value?.role === 'super_admin' || !!user.value?.is_super_admin)

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
    // PAI-320: a fresh login means the next epoch we see is the new
    // baseline — don't let a leftover from before logout retrigger
    // refreshMe.
    resetEpochBaseline()
    // PAI-322: broadcast session-restored so sibling tabs dismiss their
    // session-expired modals. Local ref clear is included in the helper.
    announceSessionRestored()
    await fetchTOTPStatus()
  }

  // PAI-83: installing a fresh authenticated user proves the session is
  // valid again; clear the banner flag here so both password and TOTP
  // login paths (which converge on setUser) recover from a stale 401.
  // PAI-322: also broadcast across tabs.
  function setUser(u: User) {
    user.value = u
    checked.value = true
    announceSessionRestored()
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
    resetEpochBaseline() // PAI-320
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

  // PAI-320: server-pushed permissions invalidation. The middleware
  // emits X-Permissions-Epoch on every authed response, captured into
  // the module-level `permissionsEpoch` ref by client.ts. We track the
  // last epoch we hydrated from /auth/me; when the next observed value
  // differs (admin promoted/demoted/membership change), refetch /auth/me
  // and re-hydrate access. Soft refresh, no re-login.
  //
  // We compare against `lastSyncedEpoch` rather than the previous
  // ref value because the very first observation just sets the
  // baseline — there's no "previous" hydration to invalidate yet.
  let lastSyncedEpoch: number | null = null
  watch(permissionsEpoch, (n) => {
    if (n < 0) return // sentinel: not yet observed
    if (!user.value) return // not logged in here — login flow will hydrate
    if (lastSyncedEpoch === null) {
      // First sighting after login / page load — record and don't refetch.
      lastSyncedEpoch = n
      return
    }
    if (n !== lastSyncedEpoch) {
      lastSyncedEpoch = n
      void refreshMe()
    }
  })

  // Reset the epoch baseline on auth transitions so a fresh login
  // doesn't immediately retrigger refreshMe based on a stale sentinel.
  function resetEpochBaseline() {
    lastSyncedEpoch = null
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
    isAdmin,
    isSuperAdmin,
    fetchTOTPStatus,
    setTOTPEnabled,
    logout,
    refreshMe,
  }
})
