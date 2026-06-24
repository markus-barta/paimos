import { api, type RequestOptions } from '@/api/client'
import type { IssueRelation, RelationType } from '@/types'

// Relation types valid for the relations API. Aliased to the generated
// schema contract (RelationType) so it can never drift from the backend —
// previously a hand-maintained union that fell behind parent/cost_unit/
// release (PAI-490 / the schema:check CI gate now enforces this).
export type IssueRelationType = RelationType

export function loadIssueRelations(issueId: number): Promise<IssueRelation[]> {
  return api.get<IssueRelation[]>(`/issues/${issueId}/relations`)
}

export function addIssueRelation(issueId: number, targetId: number, type: IssueRelationType, opts?: RequestOptions): Promise<void> {
  return api.post(`/issues/${issueId}/relations`, { target_id: targetId, type }, opts)
}

export function removeIssueRelation(issueId: number, targetId: number, type: IssueRelationType, opts?: RequestOptions): Promise<void> {
  return api.delete(`/issues/${issueId}/relations`, { target_id: targetId, type }, opts)
}
