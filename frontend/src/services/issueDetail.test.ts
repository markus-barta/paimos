import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('@/api/client', () => ({
  api: {
    get: vi.fn(),
    put: vi.fn(),
    delete: vi.fn(),
    post: vi.fn(),
  },
}))

import { api } from '@/api/client'
import {
  addIssueTag,
  assignIssueSprint,
  cloneIssueDetail,
  deleteIssueDetail,
  loadIssueAggregation,
  loadIssueDetailData,
  loadIssueParent,
  removeIssueSprint,
  removeIssueTag,
  saveIssueDetail,
} from './issueDetail'

describe('issueDetail service', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('loads issue detail dependencies and parent issue', async () => {
    vi.mocked(api.get)
      .mockResolvedValueOnce({ id: 9, parent_id: 4 } as never)
      .mockResolvedValueOnce([{ id: 1 }] as never)
      .mockResolvedValueOnce(['OPS'] as never)
      .mockResolvedValueOnce(['R1'] as never)
      .mockResolvedValueOnce([{ id: 11 }] as never)
      .mockResolvedValueOnce([{ id: 2 }] as never)
      .mockResolvedValueOnce([{ id: 12 }] as never)
      .mockResolvedValueOnce({ id: 7 } as never)
      .mockResolvedValueOnce([{ id: 3 }] as never)
      .mockResolvedValueOnce({ id: 4 } as never)

    const data = await loadIssueDetailData(9, 7)

    expect(data.issue.id).toBe(9)
    expect(data.project?.id).toBe(7)
    expect(data.parentIssue?.id).toBe(4)
    expect(api.get).toHaveBeenCalledWith('/issues/9')
    expect(api.get).toHaveBeenCalledWith('/issues/4')
  })

  it('loads issue keys and uses the canonical numeric id for detail dependencies', async () => {
    vi.mocked(api.get)
      .mockResolvedValueOnce({ id: 42, project_id: 6, parent_id: null } as never)
      .mockResolvedValueOnce([{ id: 1 }] as never)
      .mockResolvedValueOnce(['OPS'] as never)
      .mockResolvedValueOnce(['R1'] as never)
      .mockResolvedValueOnce([{ id: 11 }] as never)
      .mockResolvedValueOnce([{ id: 2 }] as never)
      .mockResolvedValueOnce([{ id: 12 }] as never)
      .mockResolvedValueOnce({ id: 6 } as never)
      .mockResolvedValueOnce([{ id: 3 }] as never)

    const data = await loadIssueDetailData('PAI-265')

    expect(data.issue.id).toBe(42)
    expect(data.project?.id).toBe(6)
    expect(api.get).toHaveBeenNthCalledWith(1, '/issues/PAI-265')
    expect(api.get).toHaveBeenCalledWith('/issues/42/children')
    expect(api.get).toHaveBeenCalledWith('/projects/6/issues?fields=list')
    expect(api.get).not.toHaveBeenCalledWith('/issues/PAI-265/children')
  })

  it('skips parent lookup when issue has no parent', async () => {
    vi.mocked(api.get)
      .mockResolvedValueOnce({ id: 9, parent_id: null } as never)
      .mockResolvedValueOnce([] as never)
      .mockResolvedValueOnce([] as never)
      .mockResolvedValueOnce([] as never)
      .mockResolvedValueOnce([] as never)
      .mockResolvedValueOnce([] as never)
      .mockResolvedValueOnce([] as never)
      .mockResolvedValueOnce(null as never)
      .mockResolvedValueOnce([] as never)

    const data = await loadIssueDetailData(9, 7)

    expect(data.parentIssue).toBeNull()
    expect(vi.mocked(api.get).mock.calls.some(([url]) => url === '/issues/null')).toBe(false)
  })

  it('avoids project-scoped dependency calls when no project is available', async () => {
    vi.mocked(api.get)
      .mockResolvedValueOnce({ id: 9, project_id: null, parent_id: null } as never)
      .mockResolvedValueOnce([] as never)
      .mockResolvedValueOnce([] as never)
      .mockResolvedValueOnce([] as never)
      .mockResolvedValueOnce([] as never)

    const data = await loadIssueDetailData(9)

    expect(data.project).toBeNull()
    expect(data.projectIssues).toEqual([])
    expect(vi.mocked(api.get).mock.calls.map(([url]) => url)).toEqual([
      '/issues/9',
      '/users',
      '/issues/9/children',
      '/tags',
      '/sprints',
    ])
    expect(
      vi.mocked(api.get).mock.calls.some(([url]) => String(url).startsWith('/projects/')),
    ).toBe(false)
  })

  it('delegates save/delete/clone/aggregation and mutations to the API layer', async () => {
    vi.mocked(api.put).mockResolvedValue({ id: 9 } as never)
    vi.mocked(api.delete).mockResolvedValue(undefined as never)
    vi.mocked(api.post).mockResolvedValue({ id: 10 } as never)
    vi.mocked(api.get).mockResolvedValue({ member_count: 1 } as never)

    await saveIssueDetail(9, { title: 'x' } as never)
    await deleteIssueDetail(9)
    await cloneIssueDetail(9)
    await loadIssueAggregation(9)
    await loadIssueParent(4)
    await addIssueTag(9, 2)
    await removeIssueTag(9, 2)
    await assignIssueSprint(9, 3)
    await removeIssueSprint(9, 3)

    expect(api.put).toHaveBeenCalledWith('/issues/9', { title: 'x' })
    expect(api.delete).toHaveBeenCalledWith('/issues/9')
    expect(api.delete).toHaveBeenCalledWith('/issues/9/tags/2')
    expect(api.delete).toHaveBeenCalledWith('/issues/9/relations', { target_id: 3, type: 'sprint' })
    expect(api.post).toHaveBeenCalledWith('/issues/9/clone', {})
    expect(api.post).toHaveBeenCalledWith('/issues/9/tags', { tag_id: 2 })
    expect(api.post).toHaveBeenCalledWith('/issues/9/relations', { target_id: 3, type: 'sprint' })
    expect(api.get).toHaveBeenCalledWith('/issues/9/aggregation')
    expect(api.get).toHaveBeenCalledWith('/issues/4')
  })
})
