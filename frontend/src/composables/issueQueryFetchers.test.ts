import { describe, it, expect, vi, beforeEach } from 'vitest'

vi.mock('@/api/client', () => ({ api: { get: vi.fn() } }))

import { api } from '@/api/client'
import {
  buildIssueQueryParams, issuePath, createIssueFetcher,
  buildInternalParams, internalIssuePath, controllerFreshnessPath,
  buildPortalParams,
} from './issueQueryFetchers'
import { emptyFilters, type IssueQuery } from './useIssueQuery'

function q(overrides: Partial<IssueQuery> = {}): IssueQuery {
  return {
    mode: 'internal-global',
    projectId: null,
    filters: { ...emptyFilters(), ...(overrides.filters ?? {}) },
    search: overrides.search ?? '',
    sort: overrides.sort ?? { key: 'created_at', dir: 'desc' },
    window: overrides.window ?? { mode: 'page', limit: 100, offset: 0 },
    viewId: overrides.viewId ?? null,
    tab: overrides.tab ?? null,
    rawFilter: overrides.rawFilter ?? '',
    ...('mode' in overrides ? { mode: overrides.mode! } : {}),
    ...('projectId' in overrides ? { projectId: overrides.projectId! } : {}),
  }
}

describe('buildIssueQueryParams', () => {
  it('encodes signed lists and validates numeric ones (v1 parity)', () => {
    const p = buildIssueQueryParams(q({
      filters: {
        ...emptyFilters(),
        status: ['done', '!cancelled'],
        aiStatus: ['running', '!failed', 'none'],
        tags: ['5', '!7', '0', 'x'], // 0 and non-numeric dropped
        assignee: ['12'],
        projects: ['3'],
        epic: ['!9'],
      },
    }))
    expect(p.get('status')).toBe('done,!cancelled')
    expect(p.get('ai_status')).toBe('running,!failed,none')
    expect(p.get('tags')).toBe('5,!7')
    expect(p.get('assignee_id')).toBe('12')
    expect(p.get('project_ids')).toBe('3')
    expect(p.get('parent_id')).toBe('!9')
    expect(p.get('fields')).toBe('list')
  })

  it('omits empty lists entirely', () => {
    const p = buildIssueQueryParams(q())
    expect(p.has('status')).toBe(false)
    expect(p.has('ai_status')).toBe(false)
    expect(p.has('tags')).toBe(false)
  })

  it('only sends q when the trimmed term is >= 2 chars', () => {
    expect(buildIssueQueryParams(q({ search: ' a ' })).has('q')).toBe(false)
    expect(buildIssueQueryParams(q({ search: '  ab ' })).get('q')).toBe('ab')
  })

  it('sets sort/order and uses limit 0 for the all window', () => {
    const paged = buildIssueQueryParams(q({ sort: { key: 'priority', dir: 'asc' } }))
    expect(paged.get('sort')).toBe('priority')
    expect(paged.get('order')).toBe('asc')
    expect(paged.get('limit')).toBe('100')
    const all = buildIssueQueryParams(q({ window: { mode: 'all', limit: 0, offset: 0 } }))
    expect(all.get('limit')).toBe('0')
  })

  it('adds date params only when a bound is present', () => {
    expect(buildIssueQueryParams(q()).has('date_field')).toBe(false)
    const p = buildIssueQueryParams(q({
      filters: { ...emptyFilters(), dateFrom: '2026-01-01' },
    }))
    expect(p.get('date_field')).toBe('completed') // default field
    expect(p.get('date_from')).toBe('2026-01-01')
  })
})

describe('buildInternalParams / internalIssuePath', () => {
  it('layers fields/limit/offset/sort/q onto the raw filter string', () => {
    const p = buildInternalParams(q({
      rawFilter: 'status=open&priority=high',
      sort: { key: 'priority', dir: 'asc' },
      search: 'login',
    }))
    expect(p.get('status')).toBe('open')
    expect(p.get('priority')).toBe('high')
    expect(p.get('fields')).toBe('list')
    expect(p.get('limit')).toBe('100')
    expect(p.get('offset')).toBe('0')
    expect(p.get('sort')).toBe('priority')
    expect(p.get('order')).toBe('asc')
    expect(p.get('q')).toBe('login')
  })

  it('routes global vs project and uses limit 0 for show-all', () => {
    expect(internalIssuePath(q())).toMatch(/^\/issues\?/)
    expect(internalIssuePath(q({ mode: 'internal-project', projectId: 5 }))).toMatch(/^\/projects\/5\/issues\?/)
    expect(buildInternalParams(q({ window: { mode: 'all', limit: 0, offset: 0 } })).get('limit')).toBe('0')
  })

  it('sets envelope=1 only for project mode', () => {
    expect(buildInternalParams(q()).has('envelope')).toBe(false)
    expect(buildInternalParams(q({ mode: 'internal-project', projectId: 5 })).get('envelope')).toBe('1')
  })

  it('controllerFreshnessPath polls offset 0 with limit growing to loaded', () => {
    const p1 = new URL('http://x' + controllerFreshnessPath(q(), 30, 100))
    expect(p1.searchParams.get('offset')).toBe('0')
    expect(p1.searchParams.get('limit')).toBe('100') // max(100, 30)
    const p2 = new URL('http://x' + controllerFreshnessPath(q(), 250, 100))
    expect(p2.searchParams.get('limit')).toBe('250') // grows with loaded
  })
})

describe('buildPortalParams (PAI-570/461)', () => {
  it('maps structured filters to the portal contract (tag_ids, envelope, q ungated, no assignee/cost_unit)', () => {
    const p = buildPortalParams(q({
      mode: 'portal', projectId: 9,
      filters: { ...emptyFilters(), status: ['done'], type: ['ticket'], priority: ['high'], tags: ['5', '7'], assignee: ['12'], costUnit: ['x'] },
      search: 'x', // 1 char — portal does not gate q length
    }))
    expect(p.get('status')).toBe('done')
    expect(p.get('type')).toBe('ticket')
    expect(p.get('priority')).toBe('high')
    expect(p.get('tag_ids')).toBe('5,7')
    expect(p.get('envelope')).toBe('1')
    expect(p.get('q')).toBe('x')
    expect(p.has('assignee_id')).toBe(false)
    expect(p.has('cost_unit')).toBe(false)
  })
})

describe('issuePath', () => {
  it('routes per mode', () => {
    expect(issuePath(q())).toMatch(/^\/issues\?/)
    expect(issuePath(q({ mode: 'internal-project', projectId: 7 }))).toMatch(/^\/projects\/7\/issues\?/)
    expect(issuePath(q({ mode: 'portal', projectId: 9 }))).toMatch(/^\/portal\/projects\/9\/issues\?/)
  })
})

describe('createIssueFetcher', () => {
  beforeEach(() => { vi.clearAllMocks() })

  it('forwards the abort signal and maps the envelope', async () => {
    ;(api.get as ReturnType<typeof vi.fn>).mockResolvedValue({
      issues: [{ id: 1 }, { id: 2 }],
      total: 5,
      has_more: true,
    })
    const fetcher = createIssueFetcher()
    const signal = new AbortController().signal
    const res = await fetcher(q({ mode: 'internal-project', projectId: 4 }), signal)

    expect(res).toEqual({ issues: [{ id: 1 }, { id: 2 }], total: 5, hasMore: true })
    expect(api.get).toHaveBeenCalledWith(
      expect.stringMatching(/^\/projects\/4\/issues\?/),
      { signal },
    )
  })

  it('falls back has_more from total vs loaded count', async () => {
    ;(api.get as ReturnType<typeof vi.fn>).mockResolvedValue({ issues: [{ id: 1 }], total: 3 })
    const res = await createIssueFetcher()(q(), new AbortController().signal)
    expect(res.hasMore).toBe(true)
  })
})
