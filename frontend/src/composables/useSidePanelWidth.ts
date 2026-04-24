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
 * useSidePanelWidth — singleton owner of the issue side-panel width.
 *
 * IssueSidePanel writes the committed width on drag-end / reset; IssueList
 * reads it for the layout offset when the panel is pinned. Both go through
 * this composable instead of touching `LS_SIDEBAR_WIDTH` directly.
 *
 * Note: IssueSidePanel uses a local "draft" ref during a drag so the
 * IssueList offset doesn't reflow on every mousemove — the committed value
 * lands here only at drag-end.
 */

import { ref, watch } from 'vue'
import { LS_SIDEBAR_WIDTH } from '@/constants/storage'

export const SIDE_PANEL_DEFAULT_WIDTH = 520
export const SIDE_PANEL_MIN_WIDTH = 300
export const SIDE_PANEL_MAX_WIDTH_RATIO = 0.6

const width = ref(parseInt(localStorage.getItem(LS_SIDEBAR_WIDTH) || String(SIDE_PANEL_DEFAULT_WIDTH), 10))

watch(width, v => localStorage.setItem(LS_SIDEBAR_WIDTH, String(v)))

export function useSidePanelWidth() {
  return { width }
}

export function resetSidePanelWidth() {
  width.value = SIDE_PANEL_DEFAULT_WIDTH
}
