import { api } from '@/api/client'
import type { Issue } from '@/types'

export function completeEpic(issueId: number, force = false): Promise<Issue> {
  const url = force ? `/issues/${issueId}/complete-epic?force=true` : `/issues/${issueId}/complete-epic`
  return api.post<Issue>(url, {})
}

export function loadIssueChildren(issueId: number): Promise<Issue[]> {
  return api.get<Issue[]>(`/issues/${issueId}/children`)
}
