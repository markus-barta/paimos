import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('@/api/client', () => ({
  api: { get: vi.fn(), post: vi.fn(), delete: vi.fn() },
}))

import { api } from '@/api/client'
import { createIssueComment, deleteIssueComment, loadIssueComments } from './issueComments'

describe('issueComments service', () => {
  beforeEach(() => vi.clearAllMocks())

  it('loads comments', async () => {
    vi.mocked(api.get).mockResolvedValue([{ id: 1 }] as never)
    const comments = await loadIssueComments(9)
    expect(comments).toHaveLength(1)
    expect(api.get).toHaveBeenCalledWith('/issues/9/comments')
  })

  it('creates and deletes comments', async () => {
    vi.mocked(api.post).mockResolvedValue({ id: 2 } as never)
    vi.mocked(api.delete).mockResolvedValue(undefined as never)
    await createIssueComment(9, 'hello')
    await deleteIssueComment(2)
    expect(api.post).toHaveBeenCalledWith('/issues/9/comments', { body: 'hello' })
    expect(api.delete).toHaveBeenCalledWith('/comments/2')
  })
})
