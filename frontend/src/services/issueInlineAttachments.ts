import { api } from '@/api/client'
import type { Attachment } from '@/types'

export function uploadInlineIssueAttachment(
  issueId: number,
  file: File,
  onProgress?: (pct: number) => void,
): Promise<Attachment> {
  const formData = new FormData()
  formData.append('file', file)
  const endpoint = issueId ? `/issues/${issueId}/attachments` : '/attachments'
  return api.upload<Attachment>(endpoint, formData, onProgress)
}
