import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('@/api/client', () => ({
  api: {
    get: vi.fn(),
    post: vi.fn(),
    put: vi.fn(),
    delete: vi.fn(),
  },
}))

import { api } from '@/api/client'
import { createIssueTimeEntry, deleteTimeEntryById, loadIssueTimeEntries, updateTimeEntry } from './issueTimeEntries'

describe('issueTimeEntries service', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('loads time entries for an issue', async () => {
    vi.mocked(api.get).mockResolvedValue([{ id: 1 }] as never)
    const data = await loadIssueTimeEntries(9)
    expect(data).toHaveLength(1)
    expect(api.get).toHaveBeenCalledWith('/issues/9/time-entries')
  })

  it('delegates create, update, and delete mutations', async () => {
    vi.mocked(api.post).mockResolvedValue(undefined as never)
    vi.mocked(api.put).mockResolvedValue(undefined as never)
    vi.mocked(api.delete).mockResolvedValue(undefined as never)
    await createIssueTimeEntry(9, { comment: 'work', override: 1.5, started_at: 'a', stopped_at: 'b' })
    await updateTimeEntry(3, { comment: 'edited' })
    await deleteTimeEntryById(3)
    expect(api.post).toHaveBeenCalledWith('/issues/9/time-entries', { comment: 'work', override: 1.5, started_at: 'a', stopped_at: 'b' })
    expect(api.put).toHaveBeenCalledWith('/time-entries/3', { comment: 'edited' })
    expect(api.delete).toHaveBeenCalledWith('/time-entries/3')
  })
})
