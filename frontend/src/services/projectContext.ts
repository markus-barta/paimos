import { api } from '@/api/client'
import type { ProjectManifest, ProjectRepo } from '@/types'

export interface ProjectContextData {
  repos: ProjectRepo[]
  manifest: ProjectManifest
}

export async function loadProjectContext(projectId: number): Promise<ProjectContextData> {
  const [repos, manifest] = await Promise.all([
    api.get<ProjectRepo[]>(`/projects/${projectId}/repos`),
    api.get<ProjectManifest>(`/projects/${projectId}/manifest`),
  ])
  return { repos, manifest }
}

export function addProjectContextRepo(projectId: number, payload: { url: string; default_branch: string; label: string }): Promise<void> {
  return api.post(`/projects/${projectId}/repos`, payload)
}

export function removeProjectContextRepo(projectId: number, repoId: number): Promise<void> {
  return api.delete(`/projects/${projectId}/repos/${repoId}`)
}

export function saveProjectContextManifest(projectId: number, data: Record<string, unknown>): Promise<ProjectManifest> {
  return api.put<ProjectManifest>(`/projects/${projectId}/manifest`, { data })
}
