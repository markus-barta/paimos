import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('@/api/client', () => ({
  api: { get: vi.fn(), upload: vi.fn(), delete: vi.fn() },
}))

import { api } from '@/api/client'
import { deleteIssueAttachment, loadIssueAttachments, uploadIssueAttachment } from './issueAttachments'

describe('issueAttachments service', () => {
  beforeEach(() => vi.clearAllMocks())

  it('loads attachments', async () => {
    vi.mocked(api.get).mockResolvedValue([{ id: 1 }] as never)
    const attachments = await loadIssueAttachments(9)
    expect(attachments).toHaveLength(1)
    expect(api.get).toHaveBeenCalledWith('/issues/9/attachments')
  })

  it('uploads and deletes attachments', async () => {
    vi.mocked(api.upload).mockResolvedValue({ id: 2 } as never)
    vi.mocked(api.delete).mockResolvedValue(undefined as never)
    const file = new File(['x'], 'x.txt', { type: 'text/plain' })
    await uploadIssueAttachment(9, file)
    await deleteIssueAttachment(2)
    expect(api.upload).toHaveBeenCalled()
    expect(api.delete).toHaveBeenCalledWith('/attachments/2')
  })
})
