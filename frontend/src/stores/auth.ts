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
import { api } from '@/api/client'
import router from '@/router'
import i18n from '@/i18n'
import { setDisplayTimezone } from '@/utils/formatTime'

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

export const useAuthStore = defineStore('auth', () => {
  const user = ref<User | null>(null)
  const checked = ref(false)
  const totpEnabled = ref(false)
  const totpChecked = ref(false)

  async function fetchMe() {
    try {
      user.value = await api.get<User>('/auth/me')
      if (user.value?.locale) i18n.global.locale.value = user.value.locale as 'en' | 'de'
      setDisplayTimezone(user.value?.timezone)
      await fetchTOTPStatus()
    } catch {
      user.value = null
      totpEnabled.value = false
      totpChecked.value = false
    } finally {
      checked.value = true
    }
  }

  async function login(username: string, password: string) {
    user.value = await api.post<User>('/auth/login', { username, password })
    if (user.value?.locale) i18n.global.locale.value = user.value.locale as 'en' | 'de'
    checked.value = true
    await fetchTOTPStatus()
  }

  function setUser(u: User) {
    user.value = u
    checked.value = true
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
    totpEnabled.value = false
    totpChecked.value = false
    checked.value = true
    router.push('/login')
  }

  // Re-fetch /auth/me and update user state — used after profile/avatar changes.
  async function refreshMe() {
    try {
      user.value = await api.get<User>('/auth/me')
    } catch { /* ignore */ }
  }

  return { user, checked, totpEnabled, totpChecked, fetchMe, login, setUser, fetchTOTPStatus, setTOTPEnabled, logout, refreshMe }
})
