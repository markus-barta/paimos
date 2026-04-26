import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('@/api/client', () => ({
  api: {
    get: vi.fn(),
    delete: vi.fn(),
    post: vi.fn(),
  },
  csrfHeaders: vi.fn(() => ({ 'X-CSRF-Token': 'token' })),
}))

import { api } from '@/api/client'
import {
  buildProjectCsvExportUrl,
  buildProjectIssuesUrl,
  deleteProjectLogo,
  executeProjectTimeEntryPurge,
  loadProjectDetailData,
  loadProjectIssues,
  loadProjectPurgeUsers,
  preflightProjectCsvImport,
  previewProjectTimeEntryPurge,
  runProjectCsvImport,
  uploadProjectLogo,
} from './projectDetail'

describe('projectDetail service', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    globalThis.fetch = vi.fn()
  })

  it('builds issue URLs only when the query is long enough', () => {
    expect(buildProjectIssuesUrl(7, '')).toBe('/projects/7/issues?fields=list')
    expect(buildProjectIssuesUrl(7, 'a')).toBe('/projects/7/issues?fields=list')
    expect(buildProjectIssuesUrl(7, 'ab')).toBe('/projects/7/issues?fields=list&q=ab')
    expect(buildProjectIssuesUrl(7, 'hello world')).toBe('/projects/7/issues?fields=list&q=hello%20world')
  })

  it('builds CSV export URLs with optional selected ids', () => {
    expect(buildProjectCsvExportUrl(7, [])).toBe('/api/projects/7/export/csv')
    expect(buildProjectCsvExportUrl(7, [1, 2])).toBe('/api/projects/7/export/csv?ids=1,2')
  })

  it('loads the project detail aggregate payload', async () => {
    vi.mocked(api.get)
      .mockResolvedValueOnce({ id: 7 } as never)
      .mockResolvedValueOnce([{ id: 1 }] as never)
      .mockResolvedValueOnce([{ id: 2 }] as never)
      .mockResolvedValueOnce(['OPS'] as never)
      .mockResolvedValueOnce(['R1'] as never)
      .mockResolvedValueOnce([{ id: 3 }] as never)
      .mockResolvedValueOnce([{ id: 4 }] as never)
      .mockResolvedValueOnce([{ id: 5 }] as never)

    const data = await loadProjectDetailData(7, 'ab')

    expect(data.project.id).toBe(7)
    expect(data.issues).toHaveLength(1)
    expect(api.get).toHaveBeenCalledWith('/projects/7/issues?fields=list&q=ab')
  })

  it('delegates issue and purge loads to the API layer', async () => {
    vi.mocked(api.get).mockResolvedValue([] as never)
    vi.mocked(api.post).mockResolvedValue({ count: 2, total_hours: 3 } as never)
    vi.mocked(api.delete).mockResolvedValue({ id: 7 } as never)

    await loadProjectIssues(7, '')
    await loadProjectPurgeUsers(7)
    await previewProjectTimeEntryPurge(7, { from: '2026-01-01' })
    await executeProjectTimeEntryPurge(7, { confirmation_key: 'confirm' })
    await deleteProjectLogo(7)

    expect(api.get).toHaveBeenCalledWith('/projects/7/issues?fields=list')
    expect(api.get).toHaveBeenCalledWith('/projects/7/time-entries/users')
    expect(api.post).toHaveBeenCalledWith('/projects/7/time-entries/purge-preview', { from: '2026-01-01' })
    expect(api.post).toHaveBeenCalledWith('/projects/7/time-entries/purge', { confirmation_key: 'confirm' })
    expect(api.delete).toHaveBeenCalledWith('/projects/7/logo')
  })

  it('uploads and imports through fetch-backed endpoints', async () => {
    vi.mocked(globalThis.fetch)
      .mockResolvedValueOnce(new Response(JSON.stringify({ id: 7 }), { status: 200, headers: { 'Content-Type': 'application/json' } }))
      .mockResolvedValueOnce(new Response(JSON.stringify({ collision_count: 0 }), { status: 200, headers: { 'Content-Type': 'application/json' } }))
      .mockResolvedValueOnce(new Response(JSON.stringify({ imported: 1, updated: 0, skipped: 0, errors: [] }), { status: 200, headers: { 'Content-Type': 'application/json' } }))

    const file = new File(['logo'], 'logo.png', { type: 'image/png' })

    const uploaded = await uploadProjectLogo(7, file)
    const preflight = await preflightProjectCsvImport(7, file)
    const imported = await runProjectCsvImport(7, file, 'insert')

    expect(uploaded.id).toBe(7)
    expect(preflight.collision_count).toBe(0)
    expect(imported.imported).toBe(1)
  })
})
