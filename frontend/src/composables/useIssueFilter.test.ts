import { describe, expect, it } from 'vitest'
import { ref } from 'vue'
import { normalizeSavedFilters, normalizeSavedFiltersJSON } from './useIssueFilter'
import { useIssueFilter } from './useIssueFilter'
import type { Issue, Tag } from '@/types'

describe('normalizeSavedFilters', () => {
  it('fills in an explicit flat-mode default when treeView is missing', () => {
    expect(normalizeSavedFilters({ type: ['ticket'] })).toMatchObject({
      type: ['ticket'],
      treeView: false,
    })
  })

  it('preserves explicit tree mode when present', () => {
    expect(normalizeSavedFilters({ type: ['epic'], treeView: true })).toMatchObject({
      type: ['epic'],
      treeView: true,
    })
  })

  it('normalizes persisted column widths alongside filters', () => {
    expect(normalizeSavedFilters({
      type: ['ticket'],
      columnWidths: { title: 480, status: 20, bogus: 100 },
    })).toMatchObject({
      type: ['ticket'],
      columnWidths: { title: 480, status: 92 },
    })
  })
})

describe('normalizeSavedFiltersJSON', () => {
  it('adds treeView to legacy view payloads that do not have it', () => {
    expect(JSON.parse(normalizeSavedFiltersJSON('{"type":["ticket"]}'))).toMatchObject({
      type: ['ticket'],
      treeView: false,
    })
  })

  it('keeps explicit treeView values intact', () => {
    expect(JSON.parse(normalizeSavedFiltersJSON('{"type":["ticket"],"treeView":true}'))).toMatchObject({
      type: ['ticket'],
      treeView: true,
    })
  })

  it('keeps valid columnWidths in saved view payloads', () => {
    expect(JSON.parse(normalizeSavedFiltersJSON('{"columnWidths":{"assignee":142,"bad":200}}'))).toMatchObject({
      columnWidths: { assignee: 142 },
    })
  })
})

function issue(id: number, patch: Partial<Issue>): Issue {
  return {
    id,
    project_id: 1,
    issue_number: id,
    issue_key: `PAI-${id}`,
    type: 'ticket',
    parent_id: null,
    title: `Issue ${id}`,
    description: '',
    acceptance_criteria: '',
    notes: '',
    status: 'new',
    priority: 'medium',
    cost_unit: '',
    release: '',
    billing_type: null,
    total_budget: null,
    rate_hourly: null,
    rate_lp: null,
    estimate_hours: null,
    estimate_lp: null,
    ar_hours: null,
    ar_lp: null,
    time_override: null,
    start_date: null,
    end_date: null,
    group_state: null,
    sprint_state: null,
    jira_id: null,
    jira_version: null,
    jira_text: null,
    color: null,
    sprint_ids: [],
    archived: false,
    assignee_id: null,
    assignee: null,
    tags: [],
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    created_by: null,
    created_by_name: '',
    last_changed_by_name: '',
    booked_hours: 0,
    time_logged: 0,
    time_rollup: 0,
    time_total: 0,
    accepted_at: null,
    accepted_by: null,
    invoiced_at: null,
    invoice_number: '',
    ...patch,
  }
}

function tag(id: number, name: string): Tag {
  return {
    id,
    name,
    color: '',
    description: '',
    created_at: '2026-01-01T00:00:00Z',
  }
}

describe('useIssueFilter complex negation', () => {
  it('applies include and exclude semantics to tag filters', () => {
    const filters = useIssueFilter({
      projectId: ref(1),
      issues: ref([
        issue(1, { tags: [tag(10, 'bug')] }),
        issue(2, { tags: [tag(20, 'ops')] }),
        issue(3, { tags: [] }),
      ]),
      compact: ref(false),
      users: ref([]),
      costUnits: ref([]),
      releases: ref([]),
      toolbarSprintIds: ref([]),
      sortKey: ref(''),
      sortDir: ref('asc'),
    })

    filters.filterTags.value = ['!10']
    expect(filters.filteredIssues.value.map(i => i.id)).toEqual([2, 3])

    filters.filterTags.value = ['20', '!10']
    expect(filters.filteredIssues.value.map(i => i.id)).toEqual([2])
  })

  it('applies exclude semantics to sprint filters', () => {
    const filters = useIssueFilter({
      projectId: ref(1),
      issues: ref([
        issue(1, { sprint_ids: [1] }),
        issue(2, { sprint_ids: [2] }),
        issue(3, { sprint_ids: [] }),
      ]),
      compact: ref(false),
      users: ref([]),
      costUnits: ref([]),
      releases: ref([]),
      toolbarSprintIds: ref([]),
      sortKey: ref(''),
      sortDir: ref('asc'),
    })

    filters.filterSprints.value = ['!1']
    expect(filters.filteredIssues.value.map(i => i.id)).toEqual([2, 3])
  })
})
