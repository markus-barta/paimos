import { api } from '@/api/client'
import type { IssueDetailForm } from '@/config/issueDetailForm'
import type { Issue, Project, Sprint, Tag, User } from '@/types'

export type IssueRef = number | string

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

function issuePath(issueRef: IssueRef): string {
  return `/issues/${encodeURIComponent(String(issueRef))}`
}

function effectiveProjectId(
  issue: Issue,
  projectId?: number | null,
): number | null {
  return projectId && Number.isFinite(projectId) && projectId > 0
    ? projectId
    : issue.project_id
}

export async function loadIssueDetailData(
  issueRef: IssueRef,
  projectId?: number | null,
): Promise<IssueDetailData> {
  const issue = await api.get<Issue>(issuePath(issueRef))
  const issueId = issue.id
  const pid = effectiveProjectId(issue, projectId)

  const [
    users,
    costUnits,
    releases,
    children,
    allTags,
    projectIssues,
    project,
    allSprints,
  ] = await Promise.all([
    api.get<User[]>('/users'),
    pid
      ? api.get<string[]>(`/projects/${pid}/cost-units`).catch(() => [])
      : Promise.resolve([]),
    pid
      ? api.get<string[]>(`/projects/${pid}/releases`).catch(() => [])
      : Promise.resolve([]),
    api.get<Issue[]>(`/issues/${issueId}/children`).catch(() => []),
    api.get<Tag[]>('/tags'),
    pid
      ? api.get<Issue[]>(`/projects/${pid}/issues?fields=list`).catch(() => [])
      : Promise.resolve([]),
    pid
      ? api.get<Project>(`/projects/${pid}`).catch(() => null)
      : Promise.resolve(null),
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
