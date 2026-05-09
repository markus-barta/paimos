import { api } from '@/api/client'
import type { ProjectRepo } from '@/types'

// PAI-358: ProjectManifest, loadProjectContext (manifest fetch),
// saveProjectContextManifest, and migrateManifestToKnowledge deleted
// with the legacy manifest editor surface. ProjectContextSection now
// only manages project_repos.

export interface ProjectContextData {
  repos: ProjectRepo[]
}

export async function loadProjectContext(projectId: number): Promise<ProjectContextData> {
  const repos = await api.get<ProjectRepo[]>(`/projects/${projectId}/repos`)
  return { repos }
}

export function addProjectContextRepo(projectId: number, payload: { url: string; default_branch: string; label: string }): Promise<void> {
  return api.post(`/projects/${projectId}/repos`, payload)
}

export function removeProjectContextRepo(projectId: number, repoId: number): Promise<void> {
  return api.delete(`/projects/${projectId}/repos/${repoId}`)
}
