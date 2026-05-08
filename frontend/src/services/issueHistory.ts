import { api } from '@/api/client'

export interface IssueHistoryEntry {
  id: number
  issue_id: number
  changed_by: number | null
  changed_by_name: string
  snapshot: Record<string, any>
  changed_at: string
  // PAI-324 — agent + session attribution. Both columns are nullable
  // on the server: rows written before PAI-324 (and rows written by
  // callers that don't send the X-Paimos-* headers) read null here.
  agent_name: string | null
  session_id: string | null
}

export function loadIssueHistory(issueId: number): Promise<IssueHistoryEntry[]> {
  return api.get<IssueHistoryEntry[]>(`/issues/${issueId}/history`)
}
