import { api } from '@/api/client'
import type { Attachment } from '@/types'

export function loadIssueAttachments(issueId: number): Promise<Attachment[]> {
  return api.get<Attachment[]>(`/issues/${issueId}/attachments`)
}

export function uploadIssueAttachment(
  issueId: number,
  file: File,
  onProgress?: (pct: number) => void,
): Promise<Attachment> {
  const fd = new FormData()
  fd.append('file', file)
  return api.upload<Attachment>(`/issues/${issueId}/attachments`, fd, onProgress)
}

export function deleteIssueAttachment(attachmentId: number): Promise<void> {
  return api.delete(`/attachments/${attachmentId}`)
}
