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
 * Lightweight client feature flags (PAI-575 rollout). Read once at setup; flip
 * via `localStorage.setItem('ff_<name>', '1')`. The IssueList v2 engine ships
 * behind `issuelist_v2` so it can be exercised in the running app before it
 * becomes the default and the v1 paths are deleted.
 */
function flag(name: string): boolean {
  try {
    return localStorage.getItem(`ff_${name}`) === '1'
  } catch {
    return false
  }
}

export function isIssueListV2(): boolean {
  return flag('issuelist_v2')
}
