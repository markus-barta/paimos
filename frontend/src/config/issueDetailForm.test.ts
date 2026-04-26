import { describe, expect, it } from 'vitest'

import { emptyIssueDetailForm, issueToDetailForm } from './issueDetailForm'
import type { Issue } from '@/types'

function makeIssue(overrides: Partial<Issue> = {}): Issue {
  return {
    id: 1,
    issue_key: 'PAI-1',
    project_id: 1,
    title: 'Title',
    description: 'Desc',
    acceptance_criteria: 'AC',
    notes: 'Notes',
    status: 'new',
    priority: 'medium',
    type: 'ticket',
    issue_number: 1,
    cost_unit: '',
    release: '',
    parent_id: null,
    assignee_id: null,
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
    assignee: null,
    tags: [],
    created_at: '',
    updated_at: '',
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
    ...overrides,
  }
}

describe('issueDetailForm helpers', () => {
  it('creates an empty form shape', () => {
    const form = emptyIssueDetailForm()
    expect(form.title).toBe('')
    expect(form.parent_id).toBeNull()
    expect(form.rate_hourly).toBeNull()
  })

  it('normalizes nullable issue fields into the edit form', () => {
    const form = issueToDetailForm(makeIssue({
      billing_type: 'fixed_price',
      total_budget: 1200,
      jira_id: 'JIRA-1',
      color: '#fff',
    }))
    expect(form.billing_type).toBe('fixed_price')
    expect(form.total_budget).toBe(1200)
    expect(form.jira_id).toBe('JIRA-1')
    expect(form.color).toBe('#fff')
  })
})
