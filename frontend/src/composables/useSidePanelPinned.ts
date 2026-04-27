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

/**
 * useSidePanelPinned — singleton owner of the issue side-panel pinned + visible state.
 *
 * Lifted out of IssueList so AppLayout can also consume it: when the panel is
 * pinned AND an issue is open, AppLayout applies a right inset on `.main`
 * so the AppHeader and main-content shrink together, instead of the header
 * being covered by the fixed-position panel.
 */

import { ref } from 'vue'
import { LS_SIDEBAR_PINNED } from '@/constants/storage'

const pinned  = ref(localStorage.getItem(LS_SIDEBAR_PINNED) === '1')
const visible = ref(false)

export function useSidePanelPinned() {
  return { pinned, visible }
}

export function setSidePanelPinned(v: boolean) {
  pinned.value = v
  localStorage.setItem(LS_SIDEBAR_PINNED, v ? '1' : '0')
}

export function setSidePanelVisible(v: boolean) {
  visible.value = v
}
