import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('@/api/client', () => ({
  api: { get: vi.fn(), post: vi.fn() },
}))

import { api } from '@/api/client'
import { completeEpic, loadIssueChildren } from './issueEpicCompletion'

describe('issueEpicCompletion service', () => {
  beforeEach(() => vi.clearAllMocks())

  it('completes an epic with and without force', async () => {
    vi.mocked(api.post).mockResolvedValue({ id: 7 } as never)

    await completeEpic(7)
    await completeEpic(7, true)

    expect(api.post).toHaveBeenCalledWith('/issues/7/complete-epic', {})
    expect(api.post).toHaveBeenCalledWith('/issues/7/complete-epic?force=true', {})
  })

  it('loads issue children', async () => {
    vi.mocked(api.get).mockResolvedValue([{ id: 9 }] as never)

    const children = await loadIssueChildren(7)

    expect(children).toHaveLength(1)
    expect(api.get).toHaveBeenCalledWith('/issues/7/children')
  })
})
