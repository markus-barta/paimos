import { api } from '@/api/client'
import type { Issue } from '@/types'

export function loadIssueGroupMembers(issueId: number, relationType: 'groups' | 'sprint'): Promise<Issue[]> {
  return api.get<Issue[]>(`/issues/${issueId}/members?type=${relationType}`)
}
