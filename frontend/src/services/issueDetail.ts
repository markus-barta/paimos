import { api } from '@/api/client'
import type { IssueDetailForm } from '@/config/issueDetailForm'
import type { Issue, Project, Sprint, Tag, User } from '@/types'

export interface IssueAggregation {
  member_count: number
  estimate_hours: number | null
  estimate_lp: number | null
  estimate_eur: number | null
  ar_hours: number | null
  ar_lp: number | null
  ar_eur: number | null
  actual_hours: number | null
  actual_internal_cost: number | null
  margin_eur: number | null
}

export interface IssueDetailData {
  issue: Issue
  project: Project | null
  parentIssue: Issue | null
  children: Issue[]
  projectIssues: Issue[]
  users: User[]
  allTags: Tag[]
  allSprints: Sprint[]
  costUnits: string[]
  releases: string[]
}

export async function loadIssueDetailData(issueId: number, projectId: number): Promise<IssueDetailData> {
  const [issue, users, costUnits, releases, children, allTags, projectIssues, project, allSprints] = await Promise.all([
    api.get<Issue>(`/issues/${issueId}`),
    api.get<User[]>('/users'),
    api.get<string[]>(`/projects/${projectId}/cost-units`).catch(() => []),
    api.get<string[]>(`/projects/${projectId}/releases`).catch(() => []),
    api.get<Issue[]>(`/issues/${issueId}/children`).catch(() => []),
    api.get<Tag[]>('/tags'),
    api.get<Issue[]>(`/projects/${projectId}/issues?fields=list`).catch(() => []),
    api.get<Project>(`/projects/${projectId}`).catch(() => null),
    api.get<Sprint[]>('/sprints').catch(() => []),
  ])

  const parentIssue = issue.parent_id
    ? await api.get<Issue>(`/issues/${issue.parent_id}`).catch(() => null)
    : null

  return {
    issue,
    project,
    parentIssue,
    children,
    projectIssues,
    users,
    allTags,
    allSprints,
    costUnits,
    releases,
  }
}

export function saveIssueDetail(issueId: number, payload: IssueDetailForm): Promise<Issue> {
  return api.put<Issue>(`/issues/${issueId}`, payload)
}

export function deleteIssueDetail(issueId: number): Promise<void> {
  return api.delete(`/issues/${issueId}`)
}

export function cloneIssueDetail(issueId: number): Promise<Issue> {
  return api.post<Issue>(`/issues/${issueId}/clone`, {})
}

export function loadIssueAggregation(issueId: number): Promise<IssueAggregation> {
  return api.get<IssueAggregation>(`/issues/${issueId}/aggregation`)
}

export function loadIssueParent(issueId: number): Promise<Issue | null> {
  return api.get<Issue>(`/issues/${issueId}`).catch(() => null)
}

export function addIssueTag(issueId: number, tagId: number): Promise<void> {
  return api.post(`/issues/${issueId}/tags`, { tag_id: tagId })
}

export function removeIssueTag(issueId: number, tagId: number): Promise<void> {
  return api.delete(`/issues/${issueId}/tags/${tagId}`)
}

export function assignIssueSprint(issueId: number, sprintId: number): Promise<void> {
  return api.post(`/issues/${issueId}/relations`, { target_id: sprintId, type: 'sprint' })
}

export function removeIssueSprint(issueId: number, sprintId: number): Promise<void> {
  return api.delete(`/issues/${issueId}/relations`, { target_id: sprintId, type: 'sprint' })
}
