import { api, type RequestOptions } from '@/api/client'
import type { IssueRelation } from '@/types'

export type IssueRelationType =
  | 'depends_on'
  | 'impacts'
  | 'follows_from'
  | 'blocks'
  | 'related'
  | 'sprint'
  | 'groups'
  // PAI-342: ticket → memory link. Source is the ticket, target is the
  // memory issue. The reverse direction surfaces in the same row when
  // the memory side queries its own /relations endpoint.
  | 'applies_to_memory'

export function loadIssueRelations(issueId: number): Promise<IssueRelation[]> {
  return api.get<IssueRelation[]>(`/issues/${issueId}/relations`)
}

export function addIssueRelation(issueId: number, targetId: number, type: IssueRelationType, opts?: RequestOptions): Promise<void> {
  return api.post(`/issues/${issueId}/relations`, { target_id: targetId, type }, opts)
}

export function removeIssueRelation(issueId: number, targetId: number, type: IssueRelationType, opts?: RequestOptions): Promise<void> {
  return api.delete(`/issues/${issueId}/relations`, { target_id: targetId, type }, opts)
}
