import { api } from '@/api/client'
import type { IssueAnchor } from '@/types'

export function loadIssueAnchors(issueId: number): Promise<IssueAnchor[]> {
  return api.get<IssueAnchor[]>(`/issues/${issueId}/anchors`)
}
