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
 * Shared time formatting utilities.
 *
 * All API timestamps are UTC. These functions parse them as UTC and
 * format in the user's display timezone (browser local by default,
 * overridable via setDisplayTimezone).
 */

let displayTimezone: string | undefined // undefined = browser local

/** Set the display timezone. 'auto' or empty = browser local; otherwise IANA tz or 'UTC'. */
export function setDisplayTimezone(tz: string | undefined) {
  displayTimezone = (!tz || tz === 'auto') ? undefined : tz
}

/** Ensure a UTC string is parseable — append Z if no timezone indicator. */
function ensureUTC(s: string): string {
  if (!s) return s
  if (s.endsWith('Z') || /[+-]\d{2}:\d{2}$/.test(s)) return s
  // "2026-03-23 10:00:00" → "2026-03-23T10:00:00Z"
  return s.replace(' ', 'T') + 'Z'
}

function opts(extra?: Intl.DateTimeFormatOptions): Intl.DateTimeFormatOptions {
  const base: Intl.DateTimeFormatOptions = {}
  if (displayTimezone) base.timeZone = displayTimezone
  return { ...base, ...extra }
}

/** Format as date only: "23 Mar 2026" */
export function fmtDate(utc: string): string {
  if (!utc) return '—'
  return new Date(ensureUTC(utc)).toLocaleDateString(undefined, opts({
    day: 'numeric', month: 'short', year: 'numeric',
  }))
}

/** Format as date + time: "23 Mar 2026, 12:30" */
export function fmtDateTime(utc: string): string {
  if (!utc) return '—'
  return new Date(ensureUTC(utc)).toLocaleString(undefined, opts({
    day: 'numeric', month: 'short', year: 'numeric',
    hour: '2-digit', minute: '2-digit',
  }))
}

/** Format as time only: "12:30" */
export function fmtTime(utc: string): string {
  if (!utc) return '—'
  return new Date(ensureUTC(utc)).toLocaleTimeString(undefined, opts({
    hour: '2-digit', minute: '2-digit',
  }))
}

/** Format as short date + time for compact displays: "23 Mar, 12:30" */
export function fmtShortDateTime(utc: string): string {
  if (!utc) return '—'
  return new Date(ensureUTC(utc)).toLocaleString(undefined, opts({
    day: 'numeric', month: 'short',
    hour: '2-digit', minute: '2-digit',
  }))
}

/** Relative time: "2m ago", "3h ago", "yesterday", "23 Mar" */
export function fmtRelative(utc: string): string {
  if (!utc) return '—'
  const d = new Date(ensureUTC(utc))
  const now = Date.now()
  const diffMs = now - d.getTime()
  if (diffMs < 0) return 'just now'
  const diffMin = Math.floor(diffMs / 60000)
  if (diffMin < 1) return 'just now'
  if (diffMin < 60) return `${diffMin}m ago`
  const diffH = Math.floor(diffMin / 60)
  if (diffH < 24) return `${diffH}h ago`
  const diffD = Math.floor(diffH / 24)
  if (diffD === 1) return 'yesterday'
  if (diffD < 7) return `${diffD}d ago`
  return d.toLocaleDateString(undefined, opts({ day: 'numeric', month: 'short' }))
}
