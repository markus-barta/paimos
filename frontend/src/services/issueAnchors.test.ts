import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('@/api/client', () => ({
  api: { get: vi.fn() },
}))

import { api } from '@/api/client'
import { loadIssueAnchors } from './issueAnchors'

describe('issueAnchors service', () => {
  beforeEach(() => vi.clearAllMocks())

  it('loads anchors', async () => {
    vi.mocked(api.get).mockResolvedValue([{ id: 1 }] as never)
    const anchors = await loadIssueAnchors(9)
    expect(anchors).toHaveLength(1)
    expect(api.get).toHaveBeenCalledWith('/issues/9/anchors')
  })
})
