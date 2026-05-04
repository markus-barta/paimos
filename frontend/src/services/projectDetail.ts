import { api, csrfHeaders } from '@/api/client'
import type { CollisionStrategy, PreflightResult } from '@/components/ImportCollisionModal.vue'
import type { Customer, Issue, Project, SavedView, Tag, User } from '@/types'

export interface ProjectDetailData {
  project: Project
  issues: Issue[]
  users: User[]
  allTags: Tag[]
  costUnits: string[]
  releases: string[]
  allViews: SavedView[]
  customers: Customer[]
}

export interface ProjectPurgeStats {
  count: number
  total_hours: number
}

export interface ProjectImportResult {
  imported: number
  updated: number
  skipped: number
  errors: string[]
}

export interface ProjectPurgeUser {
  id: number
  username: string
}

export function buildProjectIssuesUrl(projectId: number, query: string): string {
  const q = query.trim()
  return `/projects/${projectId}/issues?fields=list${q.length >= 2 ? `&q=${encodeURIComponent(q)}` : ''}`
}

export function buildProjectCsvExportUrl(projectId: number, selectedIds: number[]): string {
  let url = `/api/projects/${projectId}/export/csv`
  if (selectedIds.length > 0) {
    url += `?ids=${selectedIds.join(',')}`
  }
  return url
}

export async function loadProjectDetailData(projectId: number, query: string): Promise<ProjectDetailData> {
  const [project, issues, users, costUnits, releases, allTags, allViews, customers] = await Promise.all([
    api.get<Project>(`/projects/${projectId}`),
    api.get<Issue[]>(buildProjectIssuesUrl(projectId, query)),
    api.get<User[]>('/users'),
    api.get<string[]>(`/projects/${projectId}/cost-units`).catch(() => []),
    api.get<string[]>(`/projects/${projectId}/releases`).catch(() => []),
    api.get<Tag[]>('/tags'),
    api.get<SavedView[]>('/views').catch(() => []),
    api.get<Customer[]>('/customers').catch(() => [] as Customer[]),
  ])

  return {
    project,
    issues,
    users,
    allTags,
    costUnits,
    releases,
    allViews,
    customers,
  }
}

export function loadProjectIssues(projectId: number, query: string): Promise<Issue[]> {
  return api.get<Issue[]>(buildProjectIssuesUrl(projectId, query))
}

export async function uploadProjectLogo(projectId: number, file: File): Promise<Project> {
  const fd = new FormData()
  fd.append('logo', file)
  const resp = await fetch(`/api/projects/${projectId}/logo`, {
    method: 'POST',
    body: fd,
    credentials: 'same-origin',
    headers: csrfHeaders(),
  })
  const data = await resp.json()
  if (!resp.ok) {
    throw new Error(data.error ?? 'Upload failed.')
  }
  return data as Project
}

export function deleteProjectLogo(projectId: number): Promise<Project> {
  return api.delete<Project>(`/projects/${projectId}/logo`)
}

export function loadProjectPurgeUsers(projectId: number): Promise<ProjectPurgeUser[]> {
  return api.get<ProjectPurgeUser[]>(`/projects/${projectId}/time-entries/users`)
}

export function previewProjectTimeEntryPurge(projectId: number, payload: Record<string, unknown>): Promise<ProjectPurgeStats> {
  return api.post<ProjectPurgeStats>(`/projects/${projectId}/time-entries/purge-preview`, payload)
}

export function executeProjectTimeEntryPurge(projectId: number, payload: Record<string, unknown>): Promise<ProjectPurgeStats> {
  return api.post<ProjectPurgeStats>(`/projects/${projectId}/time-entries/purge`, payload)
}

export async function preflightProjectCsvImport(projectId: number, file: File): Promise<PreflightResult> {
  const fd = new FormData()
  fd.append('file', file)
  const resp = await fetch(`/api/projects/${projectId}/import/csv/preflight`, {
    method: 'POST',
    credentials: 'include',
    headers: csrfHeaders(),
    body: fd,
  })
  const data = await resp.json()
  if (!resp.ok) {
    throw new Error(data.error ?? 'Preflight failed.')
  }
  return data as PreflightResult
}

export async function runProjectCsvImport(
  projectId: number,
  file: File,
  strategy: CollisionStrategy,
): Promise<ProjectImportResult> {
  const fd = new FormData()
  fd.append('file', file)
  fd.append('strategy', strategy)
  const resp = await fetch(`/api/projects/${projectId}/import/csv`, {
    method: 'POST',
    credentials: 'include',
    headers: csrfHeaders(),
    body: fd,
  })
  const data = await resp.json()
  if (!resp.ok) {
    throw new Error(data.error ?? 'Import failed.')
  }
  return data as ProjectImportResult
}

export function refreshProjectViews(): Promise<SavedView[]> {
  return api.get<SavedView[]>('/views')
}

export function saveProjectDetail(projectId: number, payload: Record<string, unknown>): Promise<Project> {
  return api.put<Project>(`/projects/${projectId}`, payload)
}

export function setProjectStatus(projectId: number, status: 'active' | 'archived'): Promise<Project> {
  return api.put<Project>(`/projects/${projectId}`, { status })
}

export function deleteProjectDetail(projectId: number): Promise<void> {
  return api.delete(`/projects/${projectId}`)
}

export function addProjectTag(projectId: number, tagId: number): Promise<void> {
  return api.post(`/projects/${projectId}/tags`, { tag_id: tagId })
}

export function removeProjectTag(projectId: number, tagId: number): Promise<void> {
  return api.delete(`/projects/${projectId}/tags/${tagId}`)
}
