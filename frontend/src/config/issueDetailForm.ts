import type { Issue } from '@/types'

export interface IssueDetailForm {
  title: string
  description: string
  acceptance_criteria: string
  notes: string
  type: string
  status: string
  priority: string
  cost_unit: string
  release: string
  parent_id: number | null
  assignee_id: number | null
  billing_type: string | null
  total_budget: number | null
  rate_hourly: number | null
  rate_lp: number | null
  estimate_hours: number | null
  estimate_lp: number | null
  ar_hours: number | null
  ar_lp: number | null
  time_override: number | null
  start_date: string | null
  end_date: string | null
  group_state: string | null
  sprint_state: string | null
  jira_id: string | null
  jira_version: string | null
  jira_text: string | null
  color: string | null
}

export function emptyIssueDetailForm(): IssueDetailForm {
  return {
    title: '',
    description: '',
    acceptance_criteria: '',
    notes: '',
    type: '',
    status: '',
    priority: '',
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
  }
}

export function issueToDetailForm(issue: Issue): IssueDetailForm {
  return {
    title: issue.title,
    description: issue.description,
    acceptance_criteria: issue.acceptance_criteria,
    notes: issue.notes,
    type: issue.type,
    status: issue.status,
    priority: issue.priority,
    cost_unit: issue.cost_unit,
    release: issue.release,
    parent_id: issue.parent_id,
    assignee_id: issue.assignee_id,
    billing_type: issue.billing_type ?? null,
    total_budget: issue.total_budget ?? null,
    rate_hourly: issue.rate_hourly ?? null,
    rate_lp: issue.rate_lp ?? null,
    estimate_hours: issue.estimate_hours ?? null,
    estimate_lp: issue.estimate_lp ?? null,
    ar_hours: issue.ar_hours ?? null,
    ar_lp: issue.ar_lp ?? null,
    time_override: issue.time_override ?? null,
    start_date: issue.start_date ?? null,
    end_date: issue.end_date ?? null,
    group_state: issue.group_state ?? null,
    sprint_state: issue.sprint_state ?? null,
    jira_id: issue.jira_id ?? null,
    jira_version: issue.jira_version ?? null,
    jira_text: issue.jira_text ?? null,
    color: issue.color ?? null,
  }
}
