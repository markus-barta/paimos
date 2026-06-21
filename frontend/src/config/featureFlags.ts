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
 * Lightweight client feature flags (PAI-575 rollout). Read once at setup.
 *
 * `flagDefaultOn` reads a flag that is ON by default: it stays on unless the
 * operator explicitly opts out with `localStorage.setItem('ff_<name>', '0')`.
 */
function flagDefaultOn(name: string): boolean {
  try {
    return localStorage.getItem(`ff_${name}`) !== '0'
  } catch {
    return true
  }
}

/**
 * PAI-575: IssueList v2 is now the default. The v1 paths remain as a fallback,
 * reachable via the off-switch `localStorage.setItem('ff_issuelist_v2','0')`
 * (no redeploy needed) until they are deleted in a later cleanup.
 */
export function isIssueListV2(): boolean {
  return flagDefaultOn('issuelist_v2')
}
