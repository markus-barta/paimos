import { describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'

import { api } from '@/api/client'
import type { Issue } from '@/types'
import { useInlineEdit } from './useInlineEdit'

vi.mock('@/api/client', () => ({
  api: {
    put: vi.fn(),
    post: vi.fn(),
    delete: vi.fn(),
    get: vi.fn(),
  },
}))

function makeIssue(): Issue {
  return {
    id: 1,
    project_id: 1,
    issue_number: 1,
    issue_key: 'PAI-1',
    type: 'ticket',
    parent_id: null,
    title: 'Old title',
    description: '',
    acceptance_criteria: '',
    notes: '',
    report_summary: '',
    status: 'backlog',
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
    sprint_ids: [2],
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
  }
}

function mountInline(issue: Issue) {
  const emitted: Issue[] = []
  const inline = useInlineEdit({
    issues: ref([issue]),
    users: ref([]),
    loadedSprints: ref([]),
    childrenOf: () => [],
    emit: { updated: (updated) => emitted.push({ ...updated, sprint_ids: [...updated.sprint_ids] }) },
  })
  return { inline, emitted }
}

describe('useInlineEdit optimistic reconciliation', () => {
  it('emits an optimistic row update, then reconciles with the server response', async () => {
    const issue = makeIssue()
    const server = { ...issue, title: 'Server title', updated_at: '2026-01-02T00:00:00Z' }
    vi.mocked(api.put).mockResolvedValueOnce(server as never)
    const { inline, emitted } = mountInline(issue)

    await inline.saveCellEdit(issue, 'title', 'Draft title')

    expect(emitted[0].title).toBe('Draft title')
    expect(emitted[emitted.length - 1]?.title).toBe('Server title')
  })

  it('restores the previous row if the save fails', async () => {
    const issue = makeIssue()
    vi.mocked(api.put).mockRejectedValueOnce(new Error('nope'))
    const { inline, emitted } = mountInline(issue)

    await inline.saveCellEdit(issue, 'title', 'Draft title')

    expect(emitted[0].title).toBe('Draft title')
    expect(emitted[emitted.length - 1]?.title).toBe('Old title')
    expect(issue.title).toBe('Old title')
  })

  it('optimistically toggles sprint membership and rolls back on failure', async () => {
    const issue = makeIssue()
    vi.mocked(api.post).mockRejectedValueOnce(new Error('nope'))
    const { inline, emitted } = mountInline(issue)

    await inline.toggleSprint(issue, 3)

    expect(emitted[0].sprint_ids).toEqual([2, 3])
    expect(emitted[emitted.length - 1]?.sprint_ids).toEqual([2])
    expect(issue.sprint_ids).toEqual([2])
  })
})
