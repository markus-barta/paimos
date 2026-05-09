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
import { addProjectContextRepo, loadProjectContext, removeProjectContextRepo } from './projectContext'

describe('projectContext service', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('loads repos', async () => {
    vi.mocked(api.get).mockResolvedValueOnce([{ id: 1 }] as never)

    const data = await loadProjectContext(7)

    expect(data.repos).toHaveLength(1)
    expect(api.get).toHaveBeenCalledWith('/projects/7/repos')
  })

  it('delegates repo mutations', async () => {
    vi.mocked(api.post).mockResolvedValue(undefined as never)
    vi.mocked(api.delete).mockResolvedValue(undefined as never)

    await addProjectContextRepo(7, { url: 'https://github.com/acme/repo', default_branch: 'main', label: 'repo' })
    await removeProjectContextRepo(7, 1)

    expect(api.post).toHaveBeenCalledWith('/projects/7/repos', { url: 'https://github.com/acme/repo', default_branch: 'main', label: 'repo' })
    expect(api.delete).toHaveBeenCalledWith('/projects/7/repos/1')
  })
})
