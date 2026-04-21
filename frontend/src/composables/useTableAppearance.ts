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
 * useTableAppearance — toggle row borders and zebra striping for issue tables.
 *
 * Borders default ON (the existing CSS intended them but border-collapse:separate
 * on <table> prevented tr-level borders from rendering — this composable fixes
 * that by applying borders on <td> via a wrapper class).
 * Stripes default OFF.
 *
 * Singleton: all consumers share the same reactive state.
 */

import { ref, watch } from 'vue'
import {
  LS_TABLE_ROW_BORDERS      as LS_BORDERS,
  LS_TABLE_ROW_STRIPES      as LS_STRIPES,
  LS_TABLE_ROW_BORDER_COLOR as LS_BORDER_COLOR,
  LS_TABLE_ROW_STRIPE_COLOR as LS_STRIPE_COLOR,
} from '@/constants/storage'

const showBorders = ref(localStorage.getItem(LS_BORDERS) !== 'false')
const showStripes = ref(localStorage.getItem(LS_STRIPES) === 'true')

const borderColor = ref(localStorage.getItem(LS_BORDER_COLOR) || '')
const stripeColor = ref(localStorage.getItem(LS_STRIPE_COLOR) || '')

watch(showBorders, v => localStorage.setItem(LS_BORDERS, String(v)))
watch(showStripes, v => localStorage.setItem(LS_STRIPES, String(v)))

watch(borderColor, v => {
  if (v) {
    localStorage.setItem(LS_BORDER_COLOR, v)
    document.documentElement.style.setProperty('--table-row-border', v)
  } else {
    localStorage.removeItem(LS_BORDER_COLOR)
  }
})

watch(stripeColor, v => {
  if (v) {
    localStorage.setItem(LS_STRIPE_COLOR, v)
    document.documentElement.style.setProperty('--table-row-alt', v)
  } else {
    localStorage.removeItem(LS_STRIPE_COLOR)
  }
})

export function useTableAppearance() {
  return { showBorders, showStripes, borderColor, stripeColor }
}

export function resetTableAppearance() {
  localStorage.removeItem(LS_BORDER_COLOR)
  localStorage.removeItem(LS_STRIPE_COLOR)
  borderColor.value = ''
  stripeColor.value = ''
}
