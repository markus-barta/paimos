import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('@/api/client', () => ({
  api: { upload: vi.fn() },
}))

import { api } from '@/api/client'
import { uploadInlineIssueAttachment } from './issueInlineAttachments'

describe('issueInlineAttachments service', () => {
  beforeEach(() => vi.clearAllMocks())

  it('uploads inline attachments to the issue endpoint when the issue exists', async () => {
    vi.mocked(api.upload).mockResolvedValue({ id: 5 } as never)

    const file = new File(['hello'], 'hello.txt', { type: 'text/plain' })
    await uploadInlineIssueAttachment(9, file)

    expect(api.upload).toHaveBeenCalledWith('/issues/9/attachments', expect.any(FormData), undefined)
  })

  it('uploads inline attachments to the pending endpoint before the issue exists', async () => {
    vi.mocked(api.upload).mockResolvedValue({ id: 6 } as never)

    const file = new File(['hello'], 'draft.txt', { type: 'text/plain' })
    await uploadInlineIssueAttachment(0, file, expect.any(Function) as never)

    expect(api.upload).toHaveBeenCalledWith('/attachments', expect.any(FormData), expect.any(Function))
  })
})
