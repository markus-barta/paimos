import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createApp, nextTick, ref } from 'vue'

import { api } from '@/api/client'
import i18n from '@/i18n'
import PortalProjectView from './PortalProjectView.vue'

const routerReplace = vi.fn()
const mockRoute = {
  params: { id: '42' },
  query: {} as Record<string, string>,
}

vi.mock('vue-router', () => ({
  useRoute: () => mockRoute,
  useRouter: () => ({ replace: routerReplace }),
}))

vi.mock('@/api/client', () => ({
  api: {
    get: vi.fn(),
    post: vi.fn(),
  },
  errMsg: (_error: unknown, fallback: string) => fallback,
}))

vi.mock('@/composables/useBranding', () => ({
  useBranding: () => ({
    branding: ref({ logo: '/logo.png' }),
  }),
}))

vi.mock('@/components/IssueList.vue', () => ({
  default: {
    props: ['issues', 'mode'],
    template: `
      <div class="issue-list-stub" :data-mode="mode">
        <span class="row-count">{{ issues.length }}</span>
      </div>
    `,
  },
}))

function makeIssue(id: number) {
  return {
    id,
    issue_key: `PAI-${id}`,
    title: `Issue ${id}`,
    status: 'backlog',
    priority: 'medium',
    type: 'ticket',
    accepted_at: null,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  }
}

async function settle() {
  for (let i = 0; i < 5; i += 1) {
    await Promise.resolve()
    await nextTick()
  }
}

describe('PortalProjectView IssueList v2 query windows', () => {
  beforeEach(() => {
    document.body.innerHTML = '<div id="root"></div>'
    routerReplace.mockReset()
    mockRoute.query = {}
    vi.mocked(api.get).mockReset()
    vi.mocked(api.post).mockReset()
  })

  afterEach(() => {
    document.body.innerHTML = ''
    localStorage.clear()
  })

  it('renders the shared IssueList and preserves show-all window mode across portal filter changes', async () => {
    // IssueList v2 controller path (the only path since PAI-595). Asserts the
    // show-all window (limit=0) is preserved across sort + filter changes.
    const issueUrls: string[] = []
    vi.mocked(api.get).mockImplementation(async (url: string) => {
      if (url === '/portal/projects/42') {
        return {
          id: 42,
          key: 'PAI',
          name: 'PAIMOS',
          description: '',
          status: 'active',
          logo_path: '',
          issue_count: 123,
          done_count: 0,
        }
      }
      if (url.startsWith('/portal/projects/42/issues?')) {
        issueUrls.push(url)
        const showAll = url.includes('limit=0')
        const count = showAll ? 123 : 100
        return {
          issues: Array.from({ length: count }, (_, i) => makeIssue(i + 1)),
          total: 123,
          offset: 0,
          limit: showAll ? 0 : 100,
          returned: count,
          has_more: !showAll,
          fingerprint: showAll ? 'all-window' : 'page-window',
          selection_fingerprint: 'selection',
        }
      }
      throw new Error(`unexpected url ${url}`)
    })

    const app = createApp(PortalProjectView)
    app.use(i18n)
    app.component('RouterLink', { props: ['to'], template: '<a><slot /></a>' })
    app.mount(document.getElementById('root')!)
    await settle()

    expect(issueUrls[0]).toContain('/portal/projects/42/issues?')
    expect(issueUrls[0]).toContain('limit=100')

    document.querySelector<HTMLButtonElement>('.pv__load-more-btn')!.click()
    await settle()
    expect(issueUrls[issueUrls.length - 1]).toContain('limit=0')

    expect(document.querySelector<HTMLElement>('.issue-list-stub')?.dataset.mode).toBe('customer')
    expect(document.querySelector('.row-count')?.textContent).toBe('123')

    const search = document.querySelector<HTMLInputElement>('.pv__filter-input')!
    search.value = 'needle'
    search.dispatchEvent(new Event('input'))
    await settle()
    {
      const afterFilter = issueUrls[issueUrls.length - 1]
      expect(afterFilter).toContain('limit=0') // show-all still preserved
      expect(afterFilter).toContain('q=needle')
    }

    app.unmount()
  })
})
