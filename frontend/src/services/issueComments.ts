import { api } from '@/api/client'

// PAI-475: every comment is either 'internal' (team-only) or 'external'
// (also visible on the Customer Portal sidebar). NEW comments default to
// 'internal' — explicit opt-in is required for customer visibility.
export type CommentVisibility = 'internal' | 'external'

export interface IssueComment {
  id: number
  issue_id: number
  author_id: number | null
  author: string | null
  avatar_path: string | null
  body: string
  visibility: CommentVisibility
  created_at: string
}

export function loadIssueComments(issueId: number): Promise<IssueComment[]> {
  return api.get<IssueComment[]>(`/issues/${issueId}/comments`)
}

export function createIssueComment(
  issueId: number,
  body: string,
  visibility: CommentVisibility = 'internal',
): Promise<IssueComment> {
  return api.post<IssueComment>(`/issues/${issueId}/comments`, { body, visibility })
}

export function updateIssueCommentVisibility(
  commentId: number,
  visibility: CommentVisibility,
): Promise<{ id: number; visibility: CommentVisibility }> {
  return api.patch(`/comments/${commentId}`, { visibility })
}

export function deleteIssueComment(commentId: number): Promise<void> {
  return api.delete(`/comments/${commentId}`)
}
