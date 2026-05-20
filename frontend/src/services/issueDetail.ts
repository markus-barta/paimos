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

// PAI-463: backs the IssueDetailView visibility toggle's audit line. The
// payload is intentionally compact — the toggle's checked state and the
// most recent CUSTOMERPORTAL attach/detach event, no historical list.
export type PortalVisibilityEventType =
  | 'auto_tag'
  | 'migration_backfill'
  | 'toggle_add'
  | 'toggle_remove'

export interface PortalVisibilityEvent {
  actor: string
  at: string
  type: PortalVisibilityEventType | string
}

export interface PortalVisibilityResponse {
  visible: boolean
  last_event: PortalVisibilityEvent | null
}

export function loadIssuePortalVisibility(
  issueId: number,
): Promise<PortalVisibilityResponse> {
  return api.get<PortalVisibilityResponse>(`/issues/${issueId}/portal-visibility`)
}

// PAI-467: admin Customer Portal Visibility report. JSON shape matches
// the backend adminVisibilityResponse — kept colocated with the
// per-issue helper because both serve the same conceptual view.
export interface AdminVisibilityIssue {
  id: number
  issue_key: string
  title: string
  status: string
  last_actor?: string
  last_at?: string
  last_event_type?: string
}

export interface AdminVisibilityAuditRow {
  at: string
  actor?: string
  event_type: string
  issue_id: number
  issue_key: string
  title: string
}

export interface AdminVisibilityReport {
  project_id: number
  visible_count: number
  issues: AdminVisibilityIssue[]
  audit: AdminVisibilityAuditRow[]
  total_audit: number
  audit_offset: number
  audit_limit: number
}

export function loadAdminPortalVisibility(
  projectId: number,
  opts?: { auditOffset?: number; auditLimit?: number },
): Promise<AdminVisibilityReport> {
  const params = new URLSearchParams()
  if (opts?.auditOffset != null) params.set('audit_offset', String(opts.auditOffset))
  if (opts?.auditLimit != null) params.set('audit_limit', String(opts.auditLimit))
  const qs = params.toString()
  return api.get<AdminVisibilityReport>(
    `/admin/projects/${projectId}/portal-visibility${qs ? '?' + qs : ''}`,
  )
}

export function adminPortalVisibilityCsvUrl(
  projectId: number,
  section: 'current' | 'audit',
): string {
  return `/api/admin/projects/${projectId}/portal-visibility.csv?section=${section}`
}

export function assignIssueSprint(issueId: number, sprintId: number): Promise<void> {
  return api.post(`/issues/${issueId}/relations`, { target_id: sprintId, type: 'sprint' })
}

export function removeIssueSprint(issueId: number, sprintId: number): Promise<void> {
  return api.delete(`/issues/${issueId}/relations`, { target_id: sprintId, type: 'sprint' })
}
