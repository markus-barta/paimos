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

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    canEdit: () => true,
  }),
}))

vi.mock('@/composables/useBranding', () => ({
  useBranding: () => ({
    branding: ref({ logo: '/logo.png' }),
  }),
}))

vi.mock('@/composables/useSidebarSelectionUrl', () => ({
  useSidebarSelectionUrl: vi.fn(),
}))

vi.mock('@/composables/useSidePanelPinned', () => ({
  useSidePanelPinned: () => ({ pinned: ref(false), visible: ref(false) }),
  setSidePanelPinned: vi.fn(),
  setSidePanelVisible: vi.fn(),
}))

vi.mock('@/components/issue-list/IssueFilterBar.vue', () => ({
  default: {
    props: ['modelValue'],
    emits: ['update:modelValue'],
    setup(props: { modelValue: Record<string, unknown> }, { emit }: { emit: (event: string, value: unknown) => void }) {
      function applyFilter() {
        emit('update:modelValue', { ...props.modelValue, q: 'needle' })
      }
      return { applyFilter }
    },
    template: '<button type="button" class="filter-stub" @click="applyFilter">filter</button>',
  },
}))

vi.mock('@/components/issue-list/IssueTable.vue', () => ({
  default: {
    props: ['issues'],
    emits: ['sort', 'row-click'],
    template: `
      <div class="table-stub">
        <button type="button" class="sort-title" @click="$emit('sort', 'title')">sort</button>
        <span class="row-count">{{ issues.length }}</span>
      </div>
    `,
  },
}))

vi.mock('@/components/portal/PortalIssueSidePanel.vue', () => ({
  default: { template: '<div class="portal-panel-stub"></div>' },
}))

vi.mock('@/components/AppIcon.vue', () => ({
  default: { props: ['name'], template: '<span class="icon-stub" :data-icon="name"></span>' },
}))

vi.mock('@/components/StatusDot.vue', () => ({
  default: { props: ['status'], template: '<span class="status-dot-stub">{{ status }}</span>' },
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

  it('preserves show-all window mode across portal sort and filter changes', async () => {
    // v1 fallback path (asserts exact request URLs). The same show-all
    // preservation under the v2 controller is covered by
    // issueListV2Matrix.test.ts + runtime QA; opt this off the flag.
    localStorage.setItem('ff_issuelist_v2', '0')
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

    expect(issueUrls[0]).toBe('/portal/projects/42/issues?envelope=1&limit=100&offset=0&sort=updated_at&order=desc')

    document.querySelector<HTMLButtonElement>('.pv__load-more-btn')!.click()
    await settle()
    expect(issueUrls[issueUrls.length - 1]).toBe('/portal/projects/42/issues?envelope=1&limit=0&offset=0&sort=updated_at&order=desc')

    document.querySelector<HTMLButtonElement>('.sort-title')!.click()
    await settle()
    expect(issueUrls[issueUrls.length - 1]).toBe('/portal/projects/42/issues?envelope=1&limit=0&offset=0&sort=title&order=asc')

    document.querySelector<HTMLButtonElement>('.filter-stub')!.click()
    await settle()
    expect(issueUrls[issueUrls.length - 1]).toBe('/portal/projects/42/issues?envelope=1&limit=0&offset=0&q=needle&sort=title&order=asc')

    app.unmount()
  })
})
