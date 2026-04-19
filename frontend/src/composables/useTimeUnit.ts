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
 * useTimeUnit — toggle between hours (h) and person-days (PT) for display.
 *
 * DB always stores hours. PT = hours / 8. Conversion is purely client-side.
 * Toggle state persisted in localStorage (key: paimos_time_unit). Default: 'h'.
 *
 * Singleton — all consumers share the same reactive state so the toggle
 * applies globally (issue detail, issue list, cost unit aggregation).
 *
 * Alt-unit display is per-user: show_alt_unit_table / show_alt_unit_detail
 * control whether the "(= Xh)" secondary value shows in each context.
 */

import { ref } from 'vue'
import { useAuthStore } from '@/stores/auth'

export type TimeUnit = 'h' | 'pt'
export type FormatContext = 'table' | 'detail'

const LS_KEY = 'paimos_time_unit'
const PT_FACTOR = 8

function loadUnit(): TimeUnit {
  const stored = localStorage.getItem(LS_KEY)
  return stored === 'pt' ? 'pt' : 'h'
}

const unit = ref<TimeUnit>(loadUnit())

export function useTimeUnit() {
  const auth = useAuthStore()

  function toggle() {
    unit.value = unit.value === 'h' ? 'pt' : 'h'
    localStorage.setItem(LS_KEY, unit.value)
  }

  /** Convert DB hours to display value in current unit. */
  function toDisplay(hours: number | null | undefined): number | null {
    if (hours == null) return null
    return unit.value === 'pt' ? hours / PT_FACTOR : hours
  }

  /** Convert display input back to hours for DB storage. */
  function toHours(displayValue: number | null | undefined): number | null {
    if (displayValue == null) return null
    return unit.value === 'pt' ? displayValue * PT_FACTOR : displayValue
  }

  /** Whether to show the alternative unit in parentheses for the given context. */
  function showAlt(context: FormatContext): boolean {
    const user = auth.user
    if (!user) return true // default: show alt
    return context === 'table' ? user.show_alt_unit_table : user.show_alt_unit_detail
  }

  /** Format a hours value for display with unit label and optional secondary. */
  function formatHours(hours: number | null | undefined, context: FormatContext = 'detail'): string {
    if (hours == null) return '—'
    const alt = showAlt(context)
    if (unit.value === 'pt') {
      const pt = hours / PT_FACTOR
      return alt ? `${fmtNum(pt)} PT (= ${fmtNum(hours)}h)` : `${fmtNum(pt)} PT`
    }
    const pt = hours / PT_FACTOR
    return alt ? `${fmtNum(hours)}h (= ${fmtNum(pt)} PT)` : `${fmtNum(hours)}h`
  }

  /** Current unit label. */
  function label(): string {
    return unit.value === 'pt' ? 'PT' : 'h'
  }

  return { unit, toggle, toDisplay, toHours, formatHours, label }
}

const localeFmt = new Intl.NumberFormat(undefined, { maximumFractionDigits: 2 })

function fmtNum(n: number): string {
  return localeFmt.format(n)
}
