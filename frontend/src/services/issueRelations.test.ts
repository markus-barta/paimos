import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('@/api/client', () => ({
  api: {
    get: vi.fn(),
    post: vi.fn(),
    delete: vi.fn(),
  },
}))

import { api } from '@/api/client'
import { addIssueRelation, loadIssueRelations, removeIssueRelation } from './issueRelations'

describe('issueRelations service', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('loads relations for an issue', async () => {
    vi.mocked(api.get).mockResolvedValue([{ source_id: 1, target_id: 2, type: 'blocks' }] as never)
    const data = await loadIssueRelations(9)
    expect(data).toHaveLength(1)
    expect(api.get).toHaveBeenCalledWith('/issues/9/relations')
  })

  it('delegates add and remove relation mutations', async () => {
    vi.mocked(api.post).mockResolvedValue(undefined as never)
    vi.mocked(api.delete).mockResolvedValue(undefined as never)
    await addIssueRelation(9, 2, 'blocks')
    await removeIssueRelation(9, 2, 'blocks')
    expect(api.post).toHaveBeenCalledWith('/issues/9/relations', { target_id: 2, type: 'blocks' }, undefined)
    expect(api.delete).toHaveBeenCalledWith('/issues/9/relations', { target_id: 2, type: 'blocks' }, undefined)
  })
})
