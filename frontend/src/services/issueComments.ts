import { api } from '@/api/client'

export interface IssueComment {
  id: number
  issue_id: number
  author_id: number | null
  author: string | null
  avatar_path: string | null
  body: string
  created_at: string
}

export function loadIssueComments(issueId: number): Promise<IssueComment[]> {
  return api.get<IssueComment[]>(`/issues/${issueId}/comments`)
}

export function createIssueComment(issueId: number, body: string): Promise<IssueComment> {
  return api.post<IssueComment>(`/issues/${issueId}/comments`, { body })
}

export function deleteIssueComment(commentId: number): Promise<void> {
  return api.delete(`/comments/${commentId}`)
}
