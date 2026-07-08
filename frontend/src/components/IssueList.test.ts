import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createApp, defineComponent, nextTick, ref } from 'vue'
import { createPinia, setActivePinia } from 'pinia'

import { api } from '@/api/client'
import i18n from '@/i18n'
import { provideIssueContext } from '@/composables/useIssueContext'
import type { Issue, SavedView } from '@/types'
import IssueList from './IssueList.vue'

vi.mock('@/api/client', () => ({
  api: {
    get: vi.fn(),
    post: vi.fn(),
    put: vi.fn(),
    patch: vi.fn(),
    delete: vi.fn(),
  },
  // Refs touched by the auth store on import — provide stubs so the
  // module loads inside this test's mock without pulling the full
  // client.ts (which would drag in BroadcastChannel, fetch, etc.).
  permissionsEpoch: ref(-1),
  sessionExpired: ref(false),
  sessionExpiresAt: ref(null),
  sessionReturnPath: ref(null),
  announceSessionRestored: vi.fn(),
  announceSessionExpired: vi.fn(),
  isSessionExpiredError: () => false,
}))

vi.mock('vue-router', () => ({
  createRouter: () => ({
    beforeEach: vi.fn(),
    onError: vi.fn(),
    push: vi.fn(),
    replace: vi.fn(),
  }),
  createWebHistory: vi.fn(),
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
  useRoute: () => ({ path: '/issues', query: {} }),
}))

vi.mock('@/components/AppIcon.vue', () => ({
  default: { props: ['name'], template: '<span class="icon-stub" :data-icon="name"></span>' },
}))

vi.mock('@/components/LoadingText.vue', () => ({
  default: { props: ['label'], template: '<span class="loading-stub">{{ label }}</span>' },
}))

vi.mock('@/components/AppModal.vue', () => ({
  default: { props: ['open'], template: '<div v-if="open" class="modal-stub"><slot /></div>' },
}))

vi.mock('@/components/IssueTable.vue', () => ({
  default: {
    props: ['issues', 'columnWidths', 'readonly', 'isVisible', 'statusLabels', 'typeLabels'],
    emits: ['resize-column', 'reset-column-width'],
    template: `
      <div
        class="issue-table-stub"
        :data-rendered-count="issues.length"
        :data-column-widths="JSON.stringify(columnWidths)"
        :data-readonly="readonly ? 'true' : 'false'"
        :data-accepted-visible="isVisible('accepted_at') ? 'true' : 'false'"
        :data-actions-visible="isVisible('actions') ? 'true' : 'false'"
        :data-status-new="statusLabels?.new || ''"
        :data-type-ticket="typeLabels?.ticket || ''"
      >
        <button class="resize-status-stub" @click="$emit('resize-column', 'status', 124)">resize</button>
        <button class="reset-status-stub" @click="$emit('reset-column-width', 'status')">reset</button>
      </div>
    `,
  },
}))

vi.mock('@/components/IssueTreeView.vue', () => ({
  default: { template: '<div class="issue-tree-stub"></div>' },
}))

vi.mock('@/components/CreateIssueModal.vue', () => ({
  default: {
    props: ['open'],
    setup(_props: unknown, { expose }: { expose: (exposed: unknown) => void }) {
      expose({ openCreate: vi.fn() })
      return {}
    },
    template: '<div v-if="open" class="create-modal-stub"></div>',
  },
}))

vi.mock('@/components/BulkChangeModal.vue', () => ({
  default: {
    props: ['open'],
    setup(_props: unknown, { expose }: { expose: (exposed: unknown) => void }) {
      expose({ reset: vi.fn() })
      return {}
    },
    template: '<div v-if="open" class="bulk-modal-stub"></div>',
  },
}))

vi.mock('@/components/IssueSidePanel.vue', () => ({
  default: { template: '<div class="side-panel-stub"></div>' },
}))

vi.mock('@/components/portal/PortalIssueSidePanel.vue', () => ({
  default: { template: '<div class="portal-side-panel-stub"></div>' },
}))

vi.mock('@/components/IssueFilterPanel.vue', () => ({
  default: { template: '<div class="filter-panel-stub"></div>' },
}))

vi.mock('@/components/IssueViewsPanel.vue', () => ({
  default: { template: '<div class="views-panel-stub"></div>' },
}))

vi.mock('@/components/EpicCascadeDialog.vue', () => ({
  default: { template: '<div class="cascade-dialog-stub"></div>' },
}))

class MockIntersectionObserver {
  observe = vi.fn()
  unobserve = vi.fn()
  disconnect = vi.fn()
}

class MockResizeObserver {
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

function makeSavedView(id: number, title: string): SavedView {
  return {
    id,
    user_id: 0,
    owner_username: 'system',
    title,
    description: '',
    columns_json: '[]',
    filters_json: '{}',
    is_shared: true,
    is_admin_default: true,
    sort_order: 0,
    hidden: false,
    pinned: null,
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

function mountIssueList(
  issues: Issue[],
  props: {
    projectId?: number
    resultTotal?: number
    resultHasMore?: boolean
    selectionFingerprint?: string
    mode?: 'internal' | 'customer'
    statusLabels?: Record<string, string>
    typeLabels?: Record<string, string>
  } = {},
) {
  const el = document.createElement('div')
  document.body.appendChild(el)
  const listRef = ref<InstanceType<typeof IssueList> | null>(null)

  const Harness = defineComponent({
    setup() {
      provideIssueContext({
        users: ref([]),
        allTags: ref([]),
        costUnits: ref([]),
        releases: ref([]),
        projects: ref([]),
        sprints: ref([]),
      })
      return { issues, listRef, props }
    },
    components: { IssueList },
    template: `
      <IssueList
        ref="listRef"
        :issues="issues"
        :project-id="props.projectId"
        :result-total="props.resultTotal"
        :result-has-more="props.resultHasMore"
        :selection-fingerprint="props.selectionFingerprint"
        :mode="props.mode"
        :status-labels="props.statusLabels"
        :type-labels="props.typeLabels"
      />
    `,
  })

  const app = createApp(Harness)
  app.use(createPinia())
  app.use(i18n)
  app.mount(el)

  return {
    el,
    listRef,
    unmount() {
      app.unmount()
      el.remove()
    },
  }
}

function selectedIdSet(exposed: unknown): Set<number> {
  const value = (exposed as { selectedIds?: Set<number> | { value: Set<number> } }).selectedIds
  return value instanceof Set ? value : value?.value ?? new Set<number>()
}

function setSelectedIds(exposed: unknown, ids: number[]) {
  const target = exposed as { selectedIds?: Set<number> | { value: Set<number> } }
  if (target.selectedIds && !(target.selectedIds instanceof Set) && 'value' in target.selectedIds) {
    target.selectedIds.value = new Set(ids)
  } else {
    target.selectedIds = new Set(ids)
  }
}

describe('IssueList progressive rendering', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.mocked(api.get).mockResolvedValue([])
    Object.defineProperty(globalThis, 'IntersectionObserver', {
      configurable: true,
      value: MockIntersectionObserver,
    })
    Object.defineProperty(globalThis, 'ResizeObserver', {
      configurable: true,
      value: MockResizeObserver,
    })
  })

  afterEach(() => {
    document.body.innerHTML = ''
    localStorage.clear()
    vi.restoreAllMocks()
  })

  it('offers an inline show-all action next to the rendered count', async () => {
    const mounted = mountIssueList(Array.from({ length: 443 }, (_, i) => makeIssue(i + 1)))
    await settle()

    expect(mounted.el.querySelector('.issue-count')?.textContent).toContain('443 issues')
    expect(mounted.el.querySelector('.issue-count')?.textContent).toContain('showing 100')
    expect(mounted.el.querySelector('.issue-table-stub')?.getAttribute('data-rendered-count')).toBe('100')

    const showAll = mounted.el.querySelector<HTMLButtonElement>('.issue-count-link')
    expect(showAll).toBeTruthy()
    expect(showAll?.textContent).toBe('show all')

    showAll!.click()
    await settle()

    expect(mounted.el.querySelector('.issue-table-stub')?.getAttribute('data-rendered-count')).toBe('443')
    expect(mounted.el.querySelector('.issue-count')?.textContent).toContain('443 issues')
    expect(mounted.el.querySelector('.issue-count')?.textContent).not.toContain('showing')
    expect(mounted.el.querySelector('.issue-count-link')).toBeNull()

    mounted.unmount()
  })

  it('summarizes server-loaded results without repeating the issue count', async () => {
    const mounted = mountIssueList(Array.from({ length: 100 }, (_, i) => makeIssue(i + 1)), {
      resultTotal: 596,
      resultHasMore: true,
    })
    await settle()

    const countText = mounted.el.querySelector('.issue-count')?.textContent ?? ''
    expect(countText).toContain('100 loaded · 596 total')
    expect(countText).toContain('load all')
    expect(countText).not.toContain('100 issues')

    mounted.unmount()
  })

  it('uses the IssueList view picker as the single saved-view control', async () => {
    vi.mocked(api.get).mockImplementation((url: string) => {
      if (url === '/views') return Promise.resolve([makeSavedView(10, 'Default')]) as never
      return Promise.resolve([]) as never
    })

    const mounted = mountIssueList([makeIssue(1)])
    await settle()

    const viewButtons = mounted.el.querySelectorAll('.views-btn')
    expect(viewButtons).toHaveLength(1)
    expect(viewButtons[0]?.textContent).toContain('View')
    expect(viewButtons[0]?.textContent).toContain('Default')
    expect(mounted.el.querySelector('.view-tabs')).toBeNull()

    mounted.unmount()
  })

  it('persists resized column widths in the issue-list filter snapshot', async () => {
    const mounted = mountIssueList([makeIssue(1)])
    await settle()

    mounted.el.querySelector<HTMLButtonElement>('.resize-status-stub')!.click()
    await settle()

    expect(JSON.parse(localStorage.getItem('paimos:filters:global') ?? '{}')).toMatchObject({
      columnWidths: { status: 124 },
    })
    expect(mounted.el.querySelector('.issue-table-stub')?.getAttribute('data-column-widths')).toContain('"status":124')

    mounted.el.querySelector<HTMLButtonElement>('.reset-status-stub')!.click()
    await settle()

    expect(JSON.parse(localStorage.getItem('paimos:filters:global') ?? '{}')).toMatchObject({
      columnWidths: {},
    })

    mounted.unmount()
  })

  it('expands project selections through the project-scoped ids endpoint', async () => {
    vi.mocked(api.get).mockImplementation((url: string) => {
      if (url.startsWith('/projects/42/issues?')) {
        return Promise.resolve({
          ids: [1, 2],
          total: 2,
          truncated: false,
          cap: 5000,
          fingerprint: 'select-a',
        }) as never
      }
      return Promise.resolve([]) as never
    })

    const mounted = mountIssueList([makeIssue(1)], {
      projectId: 42,
      resultTotal: 2,
      selectionFingerprint: 'select-a',
    })
    await settle()

    const exposed = mounted.listRef.value as unknown as {
      toggleSelectionMode: () => void
    }
    exposed.toggleSelectionMode()
    setSelectedIds(exposed, [1])
    await settle()

    mounted.el.querySelector<HTMLButtonElement>('.select-all-matching')!.click()
    await settle()

    const idsOnlyCall = vi.mocked(api.get).mock.calls
      .map(([url]) => String(url))
      .find((url) => url.startsWith('/projects/42/issues?'))
    expect(idsOnlyCall).toContain('ids_only=1')
    expect(selectedIdSet(exposed).has(2)).toBe(true)

    mounted.unmount()
  })

  it('renders customer mode through the read-only shared table and portal panel', async () => {
    const mounted = mountIssueList([makeIssue(1)], {
      projectId: 42,
      mode: 'customer',
      statusLabels: { new: 'Planned' },
      typeLabels: { ticket: 'Request' },
    })
    await settle()

    const table = mounted.el.querySelector<HTMLElement>('.issue-table-stub')
    expect(table?.dataset.readonly).toBe('true')
    expect(table?.dataset.acceptedVisible).toBe('true')
    expect(table?.dataset.actionsVisible).toBe('false')
    expect(table?.dataset.statusNew).toBe('Planned')
    expect(table?.dataset.typeTicket).toBe('Request')
    expect(mounted.el.querySelector('.filters')).toBeNull()
    expect(mounted.el.querySelector('.side-panel-stub')).toBeNull()
    expect(mounted.el.querySelector('.portal-side-panel-stub')).toBeTruthy()
    expect(vi.mocked(api.get).mock.calls.map(([url]) => String(url))).not.toContain('/sprints')

    mounted.unmount()
  })
})
