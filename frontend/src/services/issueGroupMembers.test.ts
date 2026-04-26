import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('@/api/client', () => ({
  api: { get: vi.fn() },
}))

import { api } from '@/api/client'
import { loadIssueGroupMembers } from './issueGroupMembers'

describe('issueGroupMembers service', () => {
  beforeEach(() => vi.clearAllMocks())

  it('loads grouped issue members for the requested relation type', async () => {
    vi.mocked(api.get).mockResolvedValue([{ id: 1 }] as never)

    const issues = await loadIssueGroupMembers(12, 'groups')

    expect(issues).toHaveLength(1)
    expect(api.get).toHaveBeenCalledWith('/issues/12/members?type=groups')
  })
})
