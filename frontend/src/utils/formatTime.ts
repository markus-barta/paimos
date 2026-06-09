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

import {
  formatDateTimeWithLocale,
  formatDateWithLocale,
  formatRelativeTimeWithLocale,
  formatShortDateTimeWithLocale,
  formatTimeWithLocale,
  setDisplayTimezone,
} from '@/composables/useDateFormat'

export { setDisplayTimezone }

/** Format as date only: "23 Mar 2026" */
export function fmtDate(utc: string): string {
  return formatDateWithLocale(utc)
}

/** Format as date + time: "23 Mar 2026, 12:30" */
export function fmtDateTime(utc: string): string {
  return formatDateTimeWithLocale(utc)
}

/** Format as time only: "12:30" */
export function fmtTime(utc: string): string {
  return formatTimeWithLocale(utc)
}

/** Format as short date + time for compact displays: "23 Mar, 12:30" */
export function fmtShortDateTime(utc: string): string {
  return formatShortDateTimeWithLocale(utc)
}

/** Relative time: "2m ago", "3h ago", "yesterday", "23 Mar" */
export function fmtRelative(utc: string): string {
  return formatRelativeTimeWithLocale(utc)
}
