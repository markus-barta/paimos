import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('@/api/client', () => ({
  api: { get: vi.fn() },
}))

import { api } from '@/api/client'
import { loadIssueHistory } from './issueHistory'

describe('issueHistory service', () => {
  beforeEach(() => vi.clearAllMocks())

  it('loads history', async () => {
    vi.mocked(api.get).mockResolvedValue([{ id: 1 }] as never)
    const history = await loadIssueHistory(9)
    expect(history).toHaveLength(1)
    expect(api.get).toHaveBeenCalledWith('/issues/9/history')
  })
})
