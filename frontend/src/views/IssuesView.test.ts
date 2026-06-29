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
    cost_unit: null,
    release: null,
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
    // IssueList v2 controller path (the only path since PAI-595). Asserts the
    // controller's request URLs. v2 applies its search via the post-mount
    // watch (not at mount) and carries a default created_at sort.
    const issueUrls: string[] = []
    vi.mocked(api.get).mockImplementation(async (url: string) => {
      if (url.startsWith('/issues?')) {
        issueUrls.push(url)
        const issues = url.includes('limit=0')
          ? Array.from({ length: 123 }, (_, i) => makeIssue(i + 1))
          : Array.from({ length: 100 }, (_, i) => makeIssue(i + 1))
        return {
          issues,
          total: 123,
          offset: 0,
          limit: url.includes('limit=0') ? 0 : 100,
          returned: issues.length,
          has_more: issues.length < 123,
        }
      }
      return []
    })

    document.body.innerHTML = '<div id="app-header-left"></div><div id="root"></div>'
    const pinia = createPinia()
    setActivePinia(pinia)
    const searchStore = useSearchStore(pinia)

    const app = createApp(IssuesView)
    app.use(pinia)
    app.mount(document.getElementById('root')!)
    await settle()

    // Initial browse load: capped page of 100 / 123, default created_at sort.
    const header = document.getElementById('app-header-left')!
    expect(issueUrls[0]).toBe('/issues?fields=list&limit=100&offset=0&sort=created_at&order=desc')
    expect(header.textContent).toContain('123 issues · 100 loaded')

    // Search summary is view-level reactive (the controller search itself is
    // debounced and covered by issueListV2Matrix.test.ts).
    searchStore.setQuery('ma')
    await settle()
    expect(header.textContent).toContain('Showing first 100 of 123 matches for "ma"')

    // Load-all widens the controller window to limit=0.
    const loadAll = header.querySelector<HTMLButtonElement>('.load-all-link')
    expect(loadAll).toBeTruthy()
    loadAll!.click()
    await settle()
    expect(issueUrls[issueUrls.length - 1]).toContain('limit=0')
    expect(header.textContent).toContain('123 matches for "ma"')

    // Sort routes through the controller too.
    document.querySelector<HTMLButtonElement>('.sort-stub')!.click()
    await settle()
    expect(issueUrls[issueUrls.length - 1]).toContain('sort=title&order=asc')

    app.unmount()
  })
})
