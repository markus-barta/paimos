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
 * Epic color palette — the 10 swatches used by the epic color picker in
 * both the create modal and the edit sidebar. Single source of truth so
 * adding a swatch (or changing a shade) happens in one place.
 */

export interface EpicColor {
  key: string
  bg: string
  fg: string
}

export const EPIC_COLOR_PALETTE: readonly EpicColor[] = [
  { key: 'red',    bg: '#fee2e2', fg: '#991b1b' },
  { key: 'orange', bg: '#fff7ed', fg: '#9a3412' },
  { key: 'yellow', bg: '#fef9c3', fg: '#854d0e' },
  { key: 'green',  bg: '#dcfce7', fg: '#166534' },
  { key: 'teal',   bg: '#ccfbf1', fg: '#115e59' },
  { key: 'blue',   bg: '#dbeafe', fg: '#1e40af' },
  { key: 'indigo', bg: '#e0e7ff', fg: '#3730a3' },
  { key: 'purple', bg: '#f3e8ff', fg: '#6b21a8' },
  { key: 'pink',   bg: '#fce7f3', fg: '#9d174d' },
  { key: 'gray',   bg: '#f3f4f6', fg: '#374151' },
]
