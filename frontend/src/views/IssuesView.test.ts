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
    emits: ['created', 'updated', 'deleted'],
    setup(_props: unknown, { expose }: { expose: (exposed: unknown) => void }) {
      expose({
        activeFilterCount: 0,
        applyView: vi.fn(),
        filteredIssues: [],
      })
      return {}
    },
    template: '<div class="issue-list-stub"></div>',
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
    const issueUrls: string[] = []
    vi.mocked(api.get).mockImplementation(async (url: string) => {
      if (url.startsWith('/issues?')) {
        issueUrls.push(url)
        if (url.includes('offset=0')) {
          return {
            issues: Array.from({ length: 100 }, (_, i) => makeIssue(i + 1)),
            total: 123,
            offset: 0,
            limit: 100,
          }
        }
        return {
          issues: Array.from({ length: 23 }, (_, i) => makeIssue(i + 101)),
          total: 123,
          offset: 100,
          limit: 23,
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
      'Showing first 100 of 123 matches for "ma" · recently updated first',
    )
    expect(issueUrls).toEqual(['/issues?fields=list&limit=100&offset=0&q=ma'])

    const loadAll = header.querySelector<HTMLButtonElement>('.load-all-link')
    expect(loadAll).toBeTruthy()
    loadAll!.click()
    await settle()

    expect(issueUrls).toEqual([
      '/issues?fields=list&limit=100&offset=0&q=ma',
      '/issues?fields=list&limit=23&offset=100&q=ma',
    ])
    expect(header.textContent).toContain('123 matches for "ma" · recently updated first')

    app.unmount()
  })
})
