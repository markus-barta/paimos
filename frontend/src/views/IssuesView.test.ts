import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createApp, nextTick } from 'vue'
import { createPinia, setActivePinia } from 'pinia'

import { api } from '@/api/client'
import { useSearchStore } from '@/stores/search'
import type { Issue } from '@/types'
import IssuesView from './IssuesView.vue'

vi.mock('@/api/client', () => ({
  api: {
    get: vi.fn(),
    getWithMeta: vi.fn(),
  },
  errMsg: (_error: unknown, fallback: string) => fallback,
}))

vi.mock('@/components/AppIcon.vue', () => ({
  default: {
    props: ['name'],
    template: '<span class="icon-stub" :data-icon="name"></span>',
  },
}))

vi.mock('@/components/IssueList.vue', () => ({
  default: {
    props: ['issues'],
    emits: ['created', 'updated', 'deleted', 'server-sort-change'],
    setup(_props: unknown, { expose }: { expose: (exposed: unknown) => void }) {
      expose({
        activeFilterCount: 0,
        applyView: vi.fn(),
        filteredIssues: [],
      })
      return {}
    },
    template: '<button class="sort-stub" @click="$emit(\'server-sort-change\', \'title\', \'asc\')">sort</button>',
  },
}))

class MockIntersectionObserver {
  observe = vi.fn()
  disconnect = vi.fn()
}

function makeIssue(id: number): Issue {
  return {
    id,
    project_id: 1,
    issue_number: id,
    issue_key: `PAI-${id}`,
    type: 'ticket',
    parent_id: null,
    title: `Issue ${id}`,
    description: '',
    acceptance_criteria: '',
    notes: '',
    report_summary: '',
    status: 'new',
    priority: 'medium',
    cost_unit: '',
    release: '',
    billing_type: null,
    total_budget: null,
    rate_hourly: null,
    rate_lp: null,
    estimate_hours: null,
    estimate_lp: null,
    ar_hours: null,
    ar_lp: null,
    time_override: null,
    start_date: null,
    end_date: null,
    group_state: null,
    sprint_state: null,
    jira_id: null,
    jira_version: null,
    jira_text: null,
    color: null,
    sprint_ids: [],
    archived: false,
    assignee_id: null,
    assignee: null,
    tags: [],
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    created_by: null,
    created_by_name: '',
    last_changed_by_name: '',
    booked_hours: 0,
    time_logged: 0,
    time_rollup: 0,
    time_total: 0,
    accepted_at: null,
    accepted_by: null,
    invoiced_at: null,
    invoice_number: '',
  }
}

async function settle() {
  for (let i = 0; i < 5; i += 1) {
    await Promise.resolve()
    await nextTick()
  }
}

describe('IssuesView search summary', () => {
  beforeEach(() => {
    vi.mocked(api.get).mockReset()
    vi.mocked(api.getWithMeta).mockReset()
    vi.mocked(api.getWithMeta).mockResolvedValue({ status: 304, etag: '', data: null } as never)
    Object.defineProperty(globalThis, 'IntersectionObserver', {
      configurable: true,
      value: MockIntersectionObserver,
    })
  })

  afterEach(() => {
    document.body.innerHTML = ''
    localStorage.clear()
    vi.restoreAllMocks()
  })

  it('states capped search results and can load the remaining matches', async () => {
    // v1 fallback path (asserts exact request URLs). v2 (the default) is
    // covered by issueListV2Matrix.test.ts + runtime QA; opt this off the flag.
    localStorage.setItem('ff_issuelist_v2', '0')
    const issueUrls: string[] = []
    vi.mocked(api.get).mockImplementation(async (url: string) => {
      if (url.startsWith('/issues?')) {
        issueUrls.push(url)
        if (url.includes('limit=0')) {
          return {
            issues: Array.from({ length: 123 }, (_, i) => makeIssue(i + 1)),
            total: 123,
            offset: 0,
            limit: 0,
            returned: 123,
            has_more: false,
          }
        }
        if (url.includes('offset=0')) {
          return {
            issues: Array.from({ length: 100 }, (_, i) => makeIssue(i + 1)),
            total: 123,
            offset: 0,
            limit: 100,
            returned: 100,
            has_more: true,
          }
        }
        return {
          issues: Array.from({ length: 23 }, (_, i) => makeIssue(i + 101)),
          total: 123,
          offset: 100,
          limit: 23,
          returned: 23,
          has_more: false,
        }
      }
      return []
    })

    document.body.innerHTML = '<div id="app-header-left"></div><div id="root"></div>'
    const pinia = createPinia()
    setActivePinia(pinia)
    useSearchStore(pinia).setQuery('ma')

    const app = createApp(IssuesView)
    app.use(pinia)
    app.mount(document.getElementById('root')!)
    await settle()

    const header = document.getElementById('app-header-left')!
    expect(header.textContent).toContain(
      'Showing first 100 of 123 matches for "ma" · best matches first',
    )
    expect(issueUrls).toEqual(['/issues?fields=list&limit=100&offset=0&q=ma'])

    const loadAll = header.querySelector<HTMLButtonElement>('.load-all-link')
    expect(loadAll).toBeTruthy()
    loadAll!.click()
    await settle()

    expect(issueUrls).toEqual([
      '/issues?fields=list&limit=100&offset=0&q=ma',
      '/issues?fields=list&limit=0&offset=0&q=ma',
    ])
    expect(header.textContent).toContain('123 matches for "ma" · best matches first')

    document.querySelector<HTMLButtonElement>('.sort-stub')!.click()
    await settle()

    expect(issueUrls[issueUrls.length - 1]).toBe('/issues?fields=list&limit=0&offset=0&sort=title&order=asc&q=ma')

    app.unmount()
  })
})
