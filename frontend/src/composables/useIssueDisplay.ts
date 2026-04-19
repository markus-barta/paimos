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
 * useIssueDisplay — centralized display logic for issue metadata.
 *
 * Type display (icon / text) is user-configurable via Settings → Issue Display.
 * Rules:
 *   - At least one of showTypeIcon / showTypeText must always be true.
 *   - Enforced in the Settings UI (JS) and here as a read-time fallback.
 */

import { ref, watch, computed } from 'vue'
// ─── localStorage keys ───────────────────────────────────────────────────────
const LS_TYPE_ICON = 'issue-display-type-icon'
const LS_TYPE_TEXT = 'issue-display-type-text'

// ─── Defaults ─────────────────────────────────────────────────────────────────
export const DEFAULT_TYPE_ICON = true
export const DEFAULT_TYPE_TEXT = true

// ─── Singletons (module-level, shared across all component instances) ─────────
const _rawIcon = ref(localStorage.getItem(LS_TYPE_ICON) !== 'false')
const _rawText = ref(localStorage.getItem(LS_TYPE_TEXT) !== 'false')

// Defensive fallback: if someone hacks localStorage to both=false, restore defaults
const showTypeIcon = computed(() => {
  if (!_rawIcon.value && !_rawText.value) return DEFAULT_TYPE_ICON
  return _rawIcon.value
})
const showTypeText = computed(() => {
  if (!_rawIcon.value && !_rawText.value) return DEFAULT_TYPE_TEXT
  return _rawText.value
})

watch(_rawIcon, v => localStorage.setItem(LS_TYPE_ICON, String(v)))
watch(_rawText, v => localStorage.setItem(LS_TYPE_TEXT, String(v)))

export function useIssueDisplay() {
  return { showTypeIcon, showTypeText, _rawIcon, _rawText }
}

// ─── Type SVG icons ───────────────────────────────────────────────────────────
// Inline SVG strings, sized at 16×16 for better legibility.

export const TYPE_SVGS: Record<string, string> = {
  epic: `<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
    <polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2"/>
  </svg>`,
  ticket: `<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
    <rect x="3" y="3" width="18" height="18" rx="2" ry="2"/>
    <line x1="9" y1="9" x2="15" y2="9"/>
    <line x1="9" y1="12" x2="15" y2="12"/>
    <line x1="9" y1="15" x2="13" y2="15"/>
  </svg>`,
  task: `<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
    <polyline points="9 11 12 14 22 4"/>
    <path d="M21 12v7a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11"/>
  </svg>`,
  cost_unit: `<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
    <circle cx="12" cy="12" r="10"/>
    <path d="M16 8h-6a2 2 0 0 0 0 4h4a2 2 0 0 1 0 4H8"/>
    <line x1="12" y1="6" x2="12" y2="8"/>
    <line x1="12" y1="16" x2="12" y2="18"/>
  </svg>`,
  release: `<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
    <path d="M12 2L2 7l10 5 10-5-10-5z"/>
    <path d="M2 17l10 5 10-5"/>
    <path d="M2 12l10 5 10-5"/>
  </svg>`,
  sprint: `<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
    <path d="M5 3l14 9-14 9V3z"/>
  </svg>`,
}

// ─── Status display ───────────────────────────────────────────────────────────
// `outline: true` → render as ring (no fill), `outline: false` → filled dot.

export interface StatusDotStyle {
  color: string
  outline: boolean
}

export const STATUS_DOT_STYLE: Record<string, StatusDotStyle> = {
  new:           { color: '#f59e0b', outline: true  },  // yellow outline ring — untriaged
  backlog:       { color: '#4b5563', outline: true  },  // dark gray outline ring — triaged, not started
  'in-progress': { color: '#3b82f6', outline: false },  // blue filled — active
  qa:            { color: '#a855f7', outline: false },  // purple filled — in QA
  done:          { color: '#22c55e', outline: false },  // green filled — done
  delivered:     { color: '#10b981', outline: false },  // emerald filled — delivered to customer
  accepted:      { color: '#8b5cf6', outline: false },  // purple filled — accepted by customer
  invoiced:      { color: '#6366f1', outline: false },  // indigo filled — invoiced
  cancelled:     { color: '#9ca3af', outline: true  },  // light gray outline — cancelled (strikethrough via CSS)
}

export const STATUS_LABEL: Record<string, string> = {
  new:           'New',
  backlog:       'Backlog',
  'in-progress': 'In Progress',
  qa:            'QA',
  done:          'Done',
  delivered:     'Delivered',
  accepted:      'Accepted',
  invoiced:      'Invoiced',
  cancelled:     'Cancelled',
}

// ─── Priority display ─────────────────────────────────────────────────────────
/** Lucide icon name for each priority */
export const PRIORITY_ICON: Record<string, string> = {
  high:   'arrow-up',
  medium: 'arrow-right',
  low:    'arrow-down',
}

export const PRIORITY_COLOR: Record<string, string> = {
  high:   '#b94040',  // darker red — serious
  medium: '#637383',  // muted — default text-muted
  low:    '#7a9db5',  // muted blue-grey — light
}

export const PRIORITY_LABEL: Record<string, string> = {
  high:   'High',
  medium: 'Medium',
  low:    'Low',
}
