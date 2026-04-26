import { api } from '@/api/client'

export interface IssueHistoryEntry {
  id: number
  issue_id: number
  changed_by: number | null
  changed_by_name: string
  snapshot: Record<string, any>
  changed_at: string
}

export function loadIssueHistory(issueId: number): Promise<IssueHistoryEntry[]> {
  return api.get<IssueHistoryEntry[]>(`/issues/${issueId}/history`)
}
