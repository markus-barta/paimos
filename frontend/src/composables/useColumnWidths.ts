import { ALL_COLUMNS } from '@/composables/useColumnConfig'

export type ColumnWidths = Record<string, number>

const KNOWN_COLUMN_KEYS = new Set(ALL_COLUMNS.map(c => c.key))

const DEFAULT_WIDTHS: Record<string, number> = {
  key: 92,
  type: 118,
  title: 360,
  status: 112,
  priority: 118,
  cost_unit: 128,
  release: 128,
  assignee: 150,
  tags: 150,
  epic: 140,
  sprint: 170,
  billing_type: 128,
  total_budget: 118,
  rate_hourly: 104,
  rate_lp: 104,
  estimate_hours: 104,
  estimate_lp: 104,
  ar_hours: 104,
  ar_lp: 104,
  start_date: 116,
  end_date: 116,
  group_state: 136,
  sprint_state: 136,
  jira_id: 116,
  jira_version: 128,
  jira_text: 220,
  booked_hours: 116,
  // PAI-447. Customer-facing report summary. Wider than `jira_text`
  // because content is full sentences (Apple-style or exec-style),
  // not key/value snippets — needs room to render legibly when
  // selected.
  report_summary: 320,
  ai_status: 132,
  actions: 118,
}

const MIN_WIDTHS: Record<string, number> = {
  key: 78,
  type: 88,
  title: 220,
  status: 92,
  priority: 94,
  assignee: 116,
  ai_status: 96,
  actions: 86,
  jira_text: 140,
}

const MAX_WIDTHS: Record<string, number> = {
  title: 760,
  tags: 360,
  epic: 360,
  sprint: 420,
  jira_text: 520,
  report_summary: 680,
  ai_status: 220,
  actions: 180,
}

export function defaultColumnWidth(key: string): number {
  return DEFAULT_WIDTHS[key] ?? 128
}

export function minColumnWidth(key: string): number {
  return MIN_WIDTHS[key] ?? 72
}

export function maxColumnWidth(key: string): number {
  return MAX_WIDTHS[key] ?? 320
}

export function clampColumnWidth(key: string, width: number): number {
  const finite = Number.isFinite(width) ? width : defaultColumnWidth(key)
  return Math.round(Math.min(maxColumnWidth(key), Math.max(minColumnWidth(key), finite)))
}

export function normalizeColumnWidths(input: unknown): ColumnWidths {
  if (!input || typeof input !== 'object' || Array.isArray(input)) return {}
  const result: ColumnWidths = {}
  for (const [key, raw] of Object.entries(input as Record<string, unknown>)) {
    if (!KNOWN_COLUMN_KEYS.has(key)) continue
    const numeric = typeof raw === 'number' ? raw : Number(raw)
    if (!Number.isFinite(numeric)) continue
    result[key] = clampColumnWidth(key, numeric)
  }
  return result
}
