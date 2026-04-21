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

import { ref, watch } from 'vue'
import { useBranding } from './useBranding'
import {
  LS_SIDEBAR_BG_COLOR      as LS_BG,
  LS_SIDEBAR_PATTERN_COLOR as LS_PAT,
} from '@/constants/storage'

// Legacy defaults (used if branding not loaded yet)
export const DEFAULT_BG      = '#002b41'
export const DEFAULT_PATTERN = '#203a56'

const bgColor      = ref<string>(localStorage.getItem(LS_BG)  ?? DEFAULT_BG)
const patternColor = ref<string>(localStorage.getItem(LS_PAT) ?? DEFAULT_PATTERN)

watch(bgColor, v => {
  localStorage.setItem(LS_BG, v)
})
watch(patternColor, v => {
  localStorage.setItem(LS_PAT, v)
})

/** Apply branding defaults if user hasn't customized sidebar colors */
export function syncSidebarWithBranding() {
  const { branding } = useBranding()
  // Only override if user hasn't set a custom value in localStorage
  if (!localStorage.getItem(LS_BG)) {
    bgColor.value = branding.value.colors.sidebarBg
  }
  if (!localStorage.getItem(LS_PAT)) {
    patternColor.value = branding.value.colors.loginPattern
  }
}

export function resetSidebarToDefaults() {
  const { branding } = useBranding()
  localStorage.removeItem(LS_BG)
  localStorage.removeItem(LS_PAT)
  bgColor.value = branding.value.colors.sidebarBg
  patternColor.value = branding.value.colors.loginPattern
}

export function useSidebarColors() {
  return { bgColor, patternColor }
}
