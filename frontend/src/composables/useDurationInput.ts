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
 * useDurationInput — smart duration parser for time tracking fields.
 *
 * Parses human-friendly duration strings into hours (float):
 *   15m → 0.25, 1h → 1, 1h30m → 1.5, 1.5h → 1.5, 90m → 1.5
 *   +10m → current + 0.167, " 10m" (space prefix) → same as +10m
 *   Bare number (e.g. "2") → 2 hours
 */

/** Parse a duration string into hours. Returns null if unparsable. */
export function parseDuration(input: string, currentHours?: number): number | null {
  const s = input.trim()
  if (!s) return null

  // Check for additive prefix: +10m or leading space "  10m"
  const isAdditive = s.startsWith('+') || input.startsWith(' ')
  const raw = isAdditive ? s.replace(/^\+/, '').trim() : s

  const parsed = parseRaw(raw)
  if (parsed === null) return null

  if (isAdditive && currentHours != null) {
    return currentHours + parsed
  }
  return parsed
}

function parseRaw(s: string): number | null {
  // Normalize comma decimal separator to dot (locale support)
  const normalized = s.replace(/,/g, '.')

  // "1h30m" or "1h 30m"
  const hm = normalized.match(/^(\d+(?:\.\d+)?)\s*h\s*(\d+)\s*m$/i)
  if (hm) return parseFloat(hm[1]) + parseInt(hm[2]) / 60

  // "1.5h" or "2h"
  const h = normalized.match(/^(\d+(?:\.\d+)?)\s*h$/i)
  if (h) return parseFloat(h[1])

  // "30m" or "90m"
  const m = normalized.match(/^(\d+(?:\.\d+)?)\s*m$/i)
  if (m) return parseFloat(m[1]) / 60

  // Bare number → hours
  const n = parseFloat(normalized)
  if (!isNaN(n) && isFinite(n) && n >= 0) return n

  return null
}

/** Normalize a numeric string: replace comma with dot, parse to number. Returns null if invalid. */
export function parseLocaleNumber(input: string): number | null {
  const s = input.trim().replace(/,/g, '.')
  if (!s) return null
  const n = parseFloat(s)
  return !isNaN(n) && isFinite(n) ? n : null
}

/** Format hours as a human-readable duration string. */
export function formatDuration(hours: number | null | undefined): string {
  if (hours == null) return '—'
  if (hours === 0) return '0m'
  const totalMinutes = Math.round(hours * 60)
  const h = Math.floor(totalMinutes / 60)
  const m = totalMinutes % 60
  if (h === 0) return `${m}m`
  if (m === 0) return `${h}h`
  return `${h}h ${m}m`
}
