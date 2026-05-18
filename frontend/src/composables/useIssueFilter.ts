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
 * useIssueFilter — all filter state, persistence, negation helpers, chip groups.
 */
import { ref, computed, watch } from 'vue'
import type { Ref, ComputedRef } from 'vue'
import type { Issue, Tag, Sprint, User } from '@/types'
import type { Project } from '@/types'
import type { MetaOption } from '@/components/MetaSelect.vue'
import {
  useIssueDisplay,
  TYPE_SVGS,
  STATUS_DOT_STYLE, STATUS_LABEL,
  PRIORITY_ICON, PRIORITY_COLOR, PRIORITY_LABEL,
} from '@/composables/useIssueDisplay'
import { lsFiltersKey } from '@/constants/storage'
import { assignableIssueUsers } from '@/utils/users'
import { normalizeColumnWidths, type ColumnWidths } from '@/composables/useColumnWidths'

// ── Negation helpers ────────────────────────────────────────────────────────
export const NEG = '!'
export function isNeg(v: string) { return v.startsWith(NEG) }
export function negOf(v: string) { return isNeg(v) ? v : NEG + v }
export function posOf(v: string) { return isNeg(v) ? v.slice(1) : v }

export function swapInPlace(arr: string[], oldVal: string, newVal: string): string[] {
  return arr.map(v => v === oldVal ? newVal : v)
}

export function toggleFilter(arr: string[], val: string): string[] {
  const pos = posOf(val)
  const hasPos = arr.includes(pos)
  const hasNeg = arr.includes(NEG + pos)
  if (!hasPos && !hasNeg) return [...arr, pos]
  if (hasPos) return swapInPlace(arr, pos, NEG + pos)
  return swapInPlace(arr, NEG + pos, pos)
}

export function toggleFilterCheckbox(arr: string[], val: string): string[] {
  const pos = posOf(val)
  const hasPos = arr.includes(pos)
  const hasNeg = arr.includes(NEG + pos)
  if (!hasPos && !hasNeg) return [...arr, pos]
  return arr.filter(v => v !== pos && v !== NEG + pos)
}

// Known status values — anything else is "other"
export const KNOWN_STATUSES = new Set(['backlog', 'in-progress', 'done', 'delivered', 'accepted', 'invoiced', 'cancelled'])
export const OTHER_STATUS_SENTINEL = '__other__'

export interface FilterChip { label: string; group: string; value: string; negated: boolean }
export interface ChipGroup   { group: string; chips: FilterChip[] }

export interface SavedFilters {
  status:   string[]
  priority: string[]
  type:     string[]
  costUnit: string[]
  release:  string[]
  assignee: string[]
  tags:     string[]
  projects: string[]
  sprints:  string[]
  epic:     string[]
  dateField: string
  dateFrom:  string
  dateTo:    string
  treeView: boolean
  columnWidths: ColumnWidths
  sortKey?: string
  sortDir?: string
}

export interface RawSavedFilters {
  status:   string | string[]
  priority: string | string[]
  type:     string | string[]
  costUnit: string | string[]
  release:  string | string[]
  assignee: string | string[]
  tags:     string[]
  projects: string[]
  sprints:  string[]
  epic:     string[]
  dateField?: string
  dateFrom?:  string
  dateTo?:    string
  treeView: boolean
  columnWidths?: unknown
  sortKey?: string
  sortDir?: string
}

const EMPTY_FILTERS: SavedFilters = {
  status: [],
  priority: [],
  type: [],
  costUnit: [],
  release: [],
  assignee: [],
  tags: [],
  projects: [],
  sprints: [],
  epic: [],
  dateField: '',
  dateFrom: '',
  dateTo: '',
  treeView: false,
  columnWidths: {},
}

export function normalizeSavedFilters(input: Partial<RawSavedFilters> | null | undefined): SavedFilters {
  return {
    status: toArr(input?.status),
    priority: toArr(input?.priority),
    type: toArr(input?.type),
    costUnit: toArr(input?.costUnit),
    release: toArr(input?.release),
    assignee: toArr(input?.assignee),
    tags: toArr(input?.tags),
    projects: toArr(input?.projects),
    sprints: toArr(input?.sprints),
    epic: toArr(input?.epic),
    dateField: normalizeDateField(input?.dateField),
    dateFrom: normalizeDateInput(input?.dateFrom),
    dateTo: normalizeDateInput(input?.dateTo),
    treeView: input?.treeView === true,
    columnWidths: normalizeColumnWidths(input?.columnWidths),
    sortKey: input?.sortKey || undefined,
    sortDir: input?.sortKey ? (input?.sortDir === 'desc' ? 'desc' : 'asc') : undefined,
  }
}

export function normalizeSavedFiltersJSON(raw: string | null | undefined): string {
  try {
    const parsed = raw ? JSON.parse(raw) : {}
    return JSON.stringify(normalizeSavedFilters(parsed))
  } catch {
    return JSON.stringify(EMPTY_FILTERS)
  }
}

export const TYPE_OPTIONS: MetaOption[] = [
  { value: 'epic',      label: 'Epic',      icon: TYPE_SVGS.epic      },
  { value: 'ticket',    label: 'Ticket',    icon: TYPE_SVGS.ticket    },
  { value: 'task',      label: 'Task',      icon: TYPE_SVGS.task      },
  { value: 'cost_unit', label: 'Cost Unit', icon: TYPE_SVGS.cost_unit },
  { value: 'release',   label: 'Release',   icon: TYPE_SVGS.release   },
  { value: 'sprint',    label: 'Sprint',    icon: TYPE_SVGS.sprint    },
]

export const STATUS_OPTIONS: MetaOption[] = [
  { value: 'new',          label: STATUS_LABEL.new,             dotColor: STATUS_DOT_STYLE.new.color,             dotOutline: STATUS_DOT_STYLE.new.outline },
  { value: 'backlog',      label: STATUS_LABEL.backlog,         dotColor: STATUS_DOT_STYLE.backlog.color,         dotOutline: STATUS_DOT_STYLE.backlog.outline },
  { value: 'in-progress',  label: STATUS_LABEL['in-progress'],  dotColor: STATUS_DOT_STYLE['in-progress'].color,  dotOutline: STATUS_DOT_STYLE['in-progress'].outline },
  { value: 'qa',           label: STATUS_LABEL.qa,              dotColor: STATUS_DOT_STYLE.qa.color,              dotOutline: STATUS_DOT_STYLE.qa.outline },
  { value: 'done',         label: STATUS_LABEL.done,            dotColor: STATUS_DOT_STYLE.done.color,            dotOutline: STATUS_DOT_STYLE.done.outline },
  { value: 'delivered',    label: STATUS_LABEL.delivered,       dotColor: STATUS_DOT_STYLE.delivered.color,       dotOutline: STATUS_DOT_STYLE.delivered.outline },
  { value: 'accepted',     label: STATUS_LABEL.accepted,        dotColor: STATUS_DOT_STYLE.accepted.color,        dotOutline: STATUS_DOT_STYLE.accepted.outline },
  { value: 'invoiced',     label: STATUS_LABEL.invoiced,        dotColor: STATUS_DOT_STYLE.invoiced.color,        dotOutline: STATUS_DOT_STYLE.invoiced.outline },
  { value: 'cancelled',    label: STATUS_LABEL.cancelled,       dotColor: STATUS_DOT_STYLE.cancelled.color,       dotOutline: STATUS_DOT_STYLE.cancelled.outline },
]

export const PRIORITY_OPTIONS: MetaOption[] = [
  { value: 'high',   label: PRIORITY_LABEL.high,   iconName: PRIORITY_ICON.high,   arrowColor: PRIORITY_COLOR.high   },
  { value: 'medium', label: PRIORITY_LABEL.medium, iconName: PRIORITY_ICON.medium, arrowColor: PRIORITY_COLOR.medium },
  { value: 'low',    label: PRIORITY_LABEL.low,    iconName: PRIORITY_ICON.low,    arrowColor: PRIORITY_COLOR.low    },
]

export const DATE_FIELD_OPTIONS: MetaOption[] = [
  { value: 'completed', label: 'Completed' },
  { value: 'created',   label: 'Created' },
  { value: 'updated',   label: 'Updated' },
  { value: 'accepted',  label: 'Accepted' },
  { value: 'invoiced',  label: 'Invoiced' },
  { value: 'start',     label: 'Start date' },
  { value: 'end',       label: 'End date' },
]

function toArr(v: string | string[] | undefined): string[] {
  if (!v) return []
  return Array.isArray(v) ? v : [v]
}

function normalizeDateField(v: string | undefined): string {
  return typeof v === 'string' && DATE_FIELD_OPTIONS.some(o => o.value === v) ? v : ''
}

function normalizeDateInput(v: string | undefined): string {
  return typeof v === 'string' && /^\d{4}-\d{2}-\d{2}$/.test(v) ? v : ''
}

function splitFilter(arr: string[]) {
  return {
    pos: arr.filter(v => !isNeg(v)),
    neg: arr.filter(v =>  isNeg(v)).map(posOf),
  }
}

function statusMatches(issueStatus: string, val: string): boolean {
  if (val === OTHER_STATUS_SENTINEL) return !KNOWN_STATUSES.has(issueStatus)
  return issueStatus === val
}

function issueDateValue(issue: Issue, field: string): string {
  switch (field) {
    case 'created': return issue.created_at ?? ''
    case 'updated': return issue.updated_at ?? ''
    case 'accepted': return issue.accepted_at ?? ''
    case 'invoiced': return issue.invoiced_at ?? ''
    case 'start': return issue.start_date ?? ''
    case 'end': return issue.end_date ?? ''
    case 'completed':
      if (!['done', 'delivered', 'accepted', 'invoiced'].includes(issue.status)) return ''
      return issue.invoiced_at ?? issue.accepted_at ?? issue.updated_at ?? ''
    default: return ''
  }
}

function issueDateMatches(issue: Issue, field: string, from: string, to: string): boolean {
  const value = issueDateValue(issue, field).slice(0, 10)
  if (!value) return false
  if (from && value < from) return false
  if (to && value > to) return false
  return true
}

export interface UseIssueFilterOptions {
  projectId: Ref<number | undefined>
  issues: Ref<Issue[]>
  compact: Ref<boolean>
  projects?: Ref<Project[] | undefined>
  users: Ref<User[]>
  allTags?: Ref<Tag[] | undefined>
  costUnits: Ref<string[]>
  releases: Ref<string[]>
  sprints?: Ref<Sprint[] | undefined>
  toolbarSprintIds: Ref<number[]>
  sortKey: Ref<string>
  sortDir: Ref<string>
}

export type ComplexTabKey = 'project' | 'assignee' | 'tags' | 'costunit' | 'release' | 'sprint' | 'epic'

export function useIssueFilter(opts: UseIssueFilterOptions) {
  const storageKey = computed(() => lsFiltersKey(opts.projectId.value))

  // Filter refs
  const filterStatus   = ref<string[]>([])
  const filterPriority = ref<string[]>([])
  const filterType     = ref<string[]>([])
  const filterCostUnit = ref<string[]>([])
  const filterRelease  = ref<string[]>([])
  const filterAssignee = ref<string[]>([])
  const filterTags     = ref<string[]>([])
  const filterProjects = ref<string[]>([])
  const filterSprints  = ref<string[]>([])
  const filterEpic     = ref<string[]>([])
  const filterDateField = ref('')
  const filterDateFrom  = ref('')
  const filterDateTo    = ref('')
  const showArchivedSprints = ref(false)
  const treeView = ref(false)

  const filterPanelOpen = ref(false)

  // Complex-tier tab state
  const complexTab       = ref<ComplexTabKey>('assignee')
  const complexTabSearch = ref('')

  const assignableUsers = computed(() => assignableIssueUsers(opts.users.value))

  const availableTags = computed(() => {
    if (opts.allTags?.value?.length) return [...opts.allTags.value].sort((a, b) => a.name.localeCompare(b.name))
    const seen = new Map<number, Tag>()
    for (const issue of opts.issues.value) {
      for (const t of (issue.tags ?? [])) seen.set(t.id, t)
    }
    return [...seen.values()].sort((a, b) => a.name.localeCompare(b.name))
  })

  // Available complex tabs
  const complexTabs = computed<{ key: ComplexTabKey; label: string }[]>(() => {
    const tabs: { key: ComplexTabKey; label: string }[] = []
    if (opts.projects?.value?.length)                                    tabs.push({ key: 'project',  label: 'Project'   })
    tabs.push({ key: 'assignee', label: 'Assignee' })
    if (availableTags.value.length)                                      tabs.push({ key: 'tags',     label: 'Tags'      })
    if (opts.costUnits.value.length)                                     tabs.push({ key: 'costunit', label: 'Cost Unit' })
    if (opts.releases.value.length)                                      tabs.push({ key: 'release',  label: 'Release'   })
    if (opts.sprints?.value?.length)                                     tabs.push({ key: 'sprint',   label: 'Sprint'    })
    if (opts.issues.value.some(i => i.type === 'epic'))                  tabs.push({ key: 'epic',     label: 'Epic'      })
    return tabs
  })

  watch(complexTabs, (tabs) => {
    if (!tabs.find(t => t.key === complexTab.value)) {
      complexTab.value = tabs[0]?.key ?? 'assignee'
    }
  }, { immediate: true })

  function switchComplexTab(key: ComplexTabKey) {
    complexTab.value = key
    complexTabSearch.value = ''
  }

  const complexBadge = computed(() => ({
    project:  filterProjects.value.length,
    assignee: filterAssignee.value.length,
    tags:     filterTags.value.length,
    costunit: filterCostUnit.value.length,
    release:  filterRelease.value.length,
    sprint:   filterSprints.value.length,
    epic:     filterEpic.value.length,
  }))

  const activeFilterCount = computed(() =>
    filterStatus.value.length + filterPriority.value.length + filterType.value.length +
    filterCostUnit.value.length + filterRelease.value.length + filterAssignee.value.length +
    filterTags.value.length + filterProjects.value.length + filterSprints.value.length + filterEpic.value.length +
    (dateFilterActive.value ? 1 : 0)
  )

  function clearAllFilters() {
    filterStatus.value = []; filterPriority.value = []; filterType.value = []
    filterCostUnit.value = []; filterRelease.value = []; filterAssignee.value = []
    filterTags.value = []; filterProjects.value = []; filterSprints.value = []; filterEpic.value = []
    filterDateField.value = ''; filterDateFrom.value = ''; filterDateTo.value = ''
  }

  const dateFilterActive = computed(() => !!(filterDateField.value || filterDateFrom.value || filterDateTo.value))
  const effectiveDateField = computed(() => filterDateField.value || (dateFilterActive.value ? 'completed' : ''))

  // ── Filter chip groups ──────────────────────────────────────────────────
  const filterChipGroups = computed<ChipGroup[]>(() => {
    const groups: ChipGroup[] = []

    function pushGroup(groupKey: string, values: string[], labelFn: (v: string) => string) {
      if (!values.length) return
      const chips: FilterChip[] = values.map(raw => {
        const neg = isNeg(raw)
        const v   = posOf(raw)
        const baseLabel = v === OTHER_STATUS_SENTINEL ? 'Other / unknown' : (labelFn(v) || v)
        return { label: baseLabel, group: groupKey, value: raw, negated: neg }
      })
      groups.push({ group: groupKey, chips })
    }

    pushGroup('type',    filterType.value,     v => TYPE_OPTIONS.find(o => o.value === v)?.label ?? v)
    pushGroup('status',  filterStatus.value,   v => STATUS_OPTIONS.find(o => o.value === v)?.label ?? v)
    pushGroup('priority',filterPriority.value, v => PRIORITY_OPTIONS.find(o => o.value === v)?.label ?? v)
    pushGroup('project', filterProjects.value, v => {
      const p = opts.projects?.value?.find(p => String(p.id) === v)
      return p ? `Project: ${p.key}` : `Project: ${v}`
    })
    pushGroup('assignee', filterAssignee.value, v => {
      if (v === 'unassigned') return 'Unassigned'
      const u = opts.users.value.find(u => String(u.id) === v)
      return u ? `Assignee: ${u.username}` : `Assignee: ${v}`
    })
    pushGroup('tags', filterTags.value, v => {
      const t = availableTags.value.find(t => String(t.id) === v)
      return t ? `Tag: ${t.name}` : `Tag: ${v}`
    })
    pushGroup('costunit', filterCostUnit.value, v => `Cost Unit: ${v}`)
    pushGroup('release',  filterRelease.value,  v => `Release: ${v}`)
    pushGroup('sprint',   filterSprints.value,  v => {
      const s = opts.sprints?.value?.find(s => String(s.id) === v)
      return s ? `Sprint: ${s.title}` : `Sprint: ${v}`
    })
    pushGroup('epic',     filterEpic.value,     v => {
      const e = opts.issues.value.find(i => String(i.id) === v)
      return e ? `Epic: ${e.issue_key} ${e.title}` : `Epic: ${v}`
    })
    if (dateFilterActive.value) {
      const field = DATE_FIELD_OPTIONS.find(o => o.value === effectiveDateField.value)?.label ?? 'Date'
      const range = filterDateFrom.value && filterDateTo.value
        ? `${filterDateFrom.value}..${filterDateTo.value}`
        : filterDateFrom.value
          ? `from ${filterDateFrom.value}`
          : filterDateTo.value
            ? `to ${filterDateTo.value}`
            : 'any date'
      groups.push({
        group: 'date',
        chips: [{ label: `${field}: ${range}`, group: 'date', value: 'date', negated: false }],
      })
    }

    return groups
  })

  function removeChip(group: string, value: string) {
    switch (group) {
      case 'type':     filterType.value     = filterType.value.filter(v => v !== value);     break
      case 'status':   filterStatus.value   = filterStatus.value.filter(v => v !== value);   break
      case 'priority': filterPriority.value = filterPriority.value.filter(v => v !== value); break
      case 'project':  filterProjects.value = filterProjects.value.filter(v => v !== value); break
      case 'assignee': filterAssignee.value = filterAssignee.value.filter(v => v !== value); break
      case 'tags':     filterTags.value     = filterTags.value.filter(v => v !== value);     break
      case 'costunit': filterCostUnit.value = filterCostUnit.value.filter(v => v !== value); break
      case 'release':  filterRelease.value  = filterRelease.value.filter(v => v !== value);  break
      case 'sprint':   filterSprints.value  = filterSprints.value.filter(v => v !== value);  break
      case 'epic':     filterEpic.value     = filterEpic.value.filter(v => v !== value);     break
      case 'date':     filterDateField.value = ''; filterDateFrom.value = ''; filterDateTo.value = ''; break
    }
  }

  function clearChipGroup(group: string) {
    switch (group) {
      case 'type':     filterType.value     = []; break
      case 'status':   filterStatus.value   = []; break
      case 'priority': filterPriority.value = []; break
      case 'project':  filterProjects.value = []; break
      case 'assignee': filterAssignee.value = []; break
      case 'tags':     filterTags.value     = []; break
      case 'costunit': filterCostUnit.value = []; break
      case 'release':  filterRelease.value  = []; break
      case 'sprint':   filterSprints.value  = []; break
      case 'epic':     filterEpic.value     = []; break
      case 'date':     filterDateField.value = ''; filterDateFrom.value = ''; filterDateTo.value = ''; break
    }
  }

  function toggleChipNegation(group: string, value: string) {
    const r = filterRefForGroup(group)
    if (!r) return
    const pos = posOf(value)
    const neg = NEG + pos
    const hasPos = r.value.includes(pos)
    const hasNeg = r.value.includes(neg)
    if (hasPos) r.value = swapInPlace(r.value, pos, neg)
    else if (hasNeg) r.value = swapInPlace(r.value, neg, pos)
  }

  function filterRefForGroup(group: string): Ref<string[]> | null {
    switch (group) {
      case 'type':     return filterType
      case 'status':   return filterStatus
      case 'priority': return filterPriority
      case 'project':  return filterProjects
      case 'assignee': return filterAssignee
      case 'tags':     return filterTags
      case 'costunit': return filterCostUnit
      case 'release':  return filterRelease
      case 'sprint':   return filterSprints
      case 'epic':     return filterEpic
      default: return null
    }
  }

  // ── Persistence ─────────────────────────────────────────────────────────
  function loadFilters() {
    try {
      const raw = localStorage.getItem(storageKey.value)
      if (!raw) return
      const f = normalizeSavedFilters(JSON.parse(raw))
      filterStatus.value   = f.status
      filterPriority.value = f.priority
      filterType.value     = f.type
      filterCostUnit.value = f.costUnit
      filterRelease.value  = f.release
      filterAssignee.value = f.assignee
      filterTags.value     = f.tags
      filterProjects.value = f.projects
      filterSprints.value  = f.sprints
      filterEpic.value     = f.epic
      filterDateField.value = f.dateField
      filterDateFrom.value  = f.dateFrom
      filterDateTo.value    = f.dateTo
      treeView.value       = f.treeView
      if (f.sortKey) {
        opts.sortKey.value = f.sortKey
        opts.sortDir.value = f.sortDir === 'desc' ? 'desc' : 'asc'
      } else {
        opts.sortKey.value = ''
        opts.sortDir.value = 'asc'
      }
    } catch { /* ignore */ }
  }

  function saveFilters() {
    const stored = readStoredColumnWidths()
    const f = normalizeSavedFilters({
      status:   filterStatus.value,
      priority: filterPriority.value,
      type:     filterType.value,
      costUnit: filterCostUnit.value,
      release:  filterRelease.value,
      assignee: filterAssignee.value,
      tags:     filterTags.value,
      projects: filterProjects.value,
      sprints:  filterSprints.value,
      epic:     filterEpic.value,
      dateField: filterDateField.value,
      dateFrom:  filterDateFrom.value,
      dateTo:    filterDateTo.value,
      treeView: treeView.value,
      columnWidths: stored,
      sortKey:  opts.sortKey.value || undefined,
      sortDir:  opts.sortKey.value ? opts.sortDir.value : undefined,
    })
    localStorage.setItem(storageKey.value, JSON.stringify(f))
  }

  function currentFiltersJSON(): string {
    try {
      const stored = readStoredColumnWidths()
      const f = normalizeSavedFilters({
        status:   filterStatus.value,
        priority: filterPriority.value,
        type:     filterType.value,
        costUnit: filterCostUnit.value,
        release:  filterRelease.value,
        assignee: filterAssignee.value,
        tags:     filterTags.value,
        projects: filterProjects.value,
        sprints:  filterSprints.value,
        epic:     filterEpic.value,
        dateField: filterDateField.value,
        dateFrom:  filterDateFrom.value,
        dateTo:    filterDateTo.value,
        treeView: treeView.value,
        columnWidths: stored,
        sortKey:  opts.sortKey.value || undefined,
        sortDir:  opts.sortKey.value ? opts.sortDir.value : undefined,
      })
      return JSON.stringify(f)
    } catch { return '{}' }
  }

  function readStoredColumnWidths(): ColumnWidths {
    try {
      const raw = localStorage.getItem(storageKey.value)
      if (!raw) return {}
      return normalizeSavedFilters(JSON.parse(raw)).columnWidths
    } catch {
      return {}
    }
  }

  // ── filteredIssues computed ─────────────────────────────────────────────
  const filteredIssues = computed(() => {
    if (opts.compact.value) return opts.issues.value
    return opts.issues.value.filter(i => {
      if (filterStatus.value.length) {
        const { pos, neg } = splitFilter(filterStatus.value)
        if (pos.length && !pos.some(v => statusMatches(i.status, v)))   return false
        if (neg.some(v => statusMatches(i.status, v)))                   return false
      }
      if (filterPriority.value.length) {
        const { pos, neg } = splitFilter(filterPriority.value)
        if (pos.length && !pos.includes(i.priority))   return false
        if (neg.includes(i.priority))                   return false
      }
      if (filterType.value.length) {
        const { pos, neg } = splitFilter(filterType.value)
        if (pos.length && !pos.includes(i.type))   return false
        if (neg.includes(i.type))                   return false
      }
      if (filterCostUnit.value.length) {
        const { pos, neg } = splitFilter(filterCostUnit.value)
        const costUnit = i.cost_unit ?? ''
        if (pos.length && !pos.includes(costUnit)) return false
        if (neg.includes(costUnit)) return false
      }
      if (filterRelease.value.length) {
        const { pos, neg } = splitFilter(filterRelease.value)
        const release = i.release ?? ''
        if (pos.length && !pos.includes(release)) return false
        if (neg.includes(release)) return false
      }
      if (filterAssignee.value.length) {
        const { pos, neg } = splitFilter(filterAssignee.value)
        const matchesAssignee = (v: string) => v === 'unassigned'
          ? i.assignee_id === null
          : String(i.assignee_id) === v
        if (pos.length && !pos.some(matchesAssignee)) return false
        if (neg.some(matchesAssignee)) return false
      }
      if (filterTags.value.length) {
        const issueTags = (i.tags ?? []).map(t => String(t.id))
        const { pos, neg } = splitFilter(filterTags.value)
        if (pos.length && !pos.some(tid => issueTags.includes(tid))) return false
        if (neg.some(tid => issueTags.includes(tid))) return false
      }
      if (filterProjects.value.length) {
        const { pos, neg } = splitFilter(filterProjects.value)
        const projectID = String(i.project_id)
        if (pos.length && !pos.includes(projectID)) return false
        if (neg.includes(projectID)) return false
      }
      if (filterSprints.value.length) {
        const ids = i.sprint_ids ?? []
        const { pos, neg } = splitFilter(filterSprints.value)
        if (pos.length && !pos.some(sid => ids.includes(Number(sid)))) return false
        if (neg.some(sid => ids.includes(Number(sid)))) return false
      }
      // Toolbar sprint navigator (AND with all other filters)
      if (opts.toolbarSprintIds.value.length) {
        const ids = i.sprint_ids ?? []
        if (ids.length > 0 && !opts.toolbarSprintIds.value.some(sid => ids.includes(sid))) return false
      }
      if (filterEpic.value.length) {
        const { pos, neg } = splitFilter(filterEpic.value)
        const parentID = String(i.parent_id)
        if (pos.length && !pos.includes(parentID)) return false
        if (neg.includes(parentID)) return false
      }
      if (dateFilterActive.value && !issueDateMatches(i, effectiveDateField.value, filterDateFrom.value, filterDateTo.value)) {
        return false
      }
      return true
    })
  })

  // ── Picker helpers ──────────────────────────────────────────────────────
  function pickerItems<T>(
    allItems: T[],
    selectedVals: string[],
    keyFn: (item: T) => string,
    labelFn: (item: T) => string,
    search: string,
  ): T[] {
    const q = search.trim().toLowerCase()
    const filtered = q ? allItems.filter(i => labelFn(i).toLowerCase().includes(q)) : allItems
    const selected = new Set(selectedVals.map(posOf))
    return [...filtered].sort((a, b) => {
      const asel = selected.has(keyFn(a))
      const bsel = selected.has(keyFn(b))
      if (asel !== bsel) return asel ? -1 : 1
      return labelFn(a).localeCompare(labelFn(b))
    })
  }

  const assigneeIsAny = computed(() => filterAssignee.value.length === 0)
  function setAssigneeAny() { filterAssignee.value = [] }

  const pickerProjects = computed(() =>
    pickerItems(opts.projects?.value ?? [], filterProjects.value,
      p => String(p.id), p => p.key + ' ' + p.name, complexTabSearch.value)
  )
  const pickerUsers = computed(() => {
    const q = complexTabSearch.value.trim().toLowerCase()
    const base = assignableUsers.value
    const filtered = q ? base.filter(u => u.username.toLowerCase().includes(q)) : base
    const selectedNamed = filterAssignee.value.filter(v => v !== 'unassigned')
    return [...filtered].sort((a, b) => {
      const asel = selectedNamed.includes(String(a.id))
      const bsel = selectedNamed.includes(String(b.id))
      if (asel !== bsel) return asel ? -1 : 1
      return a.username.localeCompare(b.username)
    })
  })
  const pickerTags = computed(() =>
    pickerItems(availableTags.value, filterTags.value,
      t => String(t.id), t => t.name, complexTabSearch.value)
  )
  const pickerCostUnits = computed(() =>
    pickerItems(opts.costUnits.value, filterCostUnit.value,
      v => v, v => v, complexTabSearch.value)
  )
  const pickerReleases = computed(() =>
    pickerItems(opts.releases.value, filterRelease.value,
      v => v, v => v, complexTabSearch.value)
  )
  const pickerSprints = computed(() =>
    pickerItems(
      (opts.sprints?.value ?? []).filter(s => !s.archived || showArchivedSprints.value),
      filterSprints.value,
      s => String(s.id),
      s => s.title,
      complexTabSearch.value,
    )
  )

  // Watchers array for parent to register
  const filterWatchSources = [filterStatus, filterPriority, filterType, filterCostUnit, filterRelease, filterAssignee, filterTags, filterProjects, filterSprints, filterEpic, filterDateField, filterDateFrom, filterDateTo, treeView] as const

  return {
    // Refs
    filterStatus, filterPriority, filterType, filterCostUnit, filterRelease,
    filterAssignee, filterTags, filterProjects, filterSprints, filterEpic,
    filterDateField, filterDateFrom, filterDateTo,
    showArchivedSprints, treeView, filterPanelOpen,
    complexTab, complexTabSearch,

    // Computed
    availableTags, assignableUsers, complexTabs, complexBadge,
    activeFilterCount, filterChipGroups, filteredIssues,
    dateFilterActive, effectiveDateField,
    assigneeIsAny,
    pickerProjects, pickerUsers, pickerTags, pickerCostUnits, pickerReleases, pickerSprints,

    // Functions
    clearAllFilters, removeChip, clearChipGroup,
    toggleChipNegation, filterRefForGroup,
    loadFilters, saveFilters, currentFiltersJSON,
    switchComplexTab, setAssigneeAny,

    // For watcher registration
    filterWatchSources,
    storageKey,
  }
}
