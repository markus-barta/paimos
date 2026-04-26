import { api } from '@/api/client'
import type { IssueRelation } from '@/types'

export type IssueRelationType = 'depends_on' | 'impacts' | 'follows_from' | 'blocks' | 'related' | 'sprint' | 'groups'

export function loadIssueRelations(issueId: number): Promise<IssueRelation[]> {
  return api.get<IssueRelation[]>(`/issues/${issueId}/relations`)
}

export function addIssueRelation(issueId: number, targetId: number, type: IssueRelationType): Promise<void> {
  return api.post(`/issues/${issueId}/relations`, { target_id: targetId, type })
}

export function removeIssueRelation(issueId: number, targetId: number, type: IssueRelationType): Promise<void> {
  return api.delete(`/issues/${issueId}/relations`, { target_id: targetId, type })
}
