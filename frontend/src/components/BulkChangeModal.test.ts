import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createApp, defineComponent, h, nextTick, ref } from 'vue'

import { api } from '@/api/client'
import { provideIssueContext } from '@/composables/useIssueContext'
import type { Issue, User } from '@/types'
import BulkChangeModal from './BulkChangeModal.vue'

vi.mock('@/api/client', async () => {
  const actual = await vi.importActual<typeof import('@/api/client')>('@/api/client')
  return {
    ...actual,
    api: {
      get: vi.fn(),
      post: vi.fn(),
      put: vi.fn(),
      patch: vi.fn(),
      delete: vi.fn(),
    },
  }
})

vi.mock('@/components/AppModal.vue', () => ({
  default: {
    props: ['open', 'title'],
    template: '<div v-if="open" class="modal-stub" :data-title="title"><slot /></div>',
  },
}))

function makeUser(id: number, username: string, role: 'admin' | 'member' = 'member'): User {
  return {
    id,
    username,
    role,
    status: 'active',
  } as unknown as User
}

function makeIssue(id: number, key: string): Issue {
  return {
    id,
    issue_key: key,
    type: 'ticket',
    status: 'backlog',
    priority: 'medium',
    title: key,
    sprint_ids: [],
  } as unknown as Issue
}

interface Mounted {
  el: HTMLElement
  vm: any
  modal: { reset: () => void } | null
  unmount: () => void
}

function mount(opts: {
  selectedIds: Set<number>
  issues: Issue[]
  users: User[]
  sprints?: any[]
}): Mounted {
  const el = document.createElement('div')
  document.body.appendChild(el)

  const sprints = opts.sprints ?? []
  const modalRef = ref<any>(null)
  const Host = defineComponent({
    components: { BulkChangeModal },
    setup(_, { expose }) {
      provideIssueContext({
        users: ref(opts.users),
        allTags: ref([]),
        costUnits: ref([]),
        releases: ref([]),
        projects: ref([]),
        sprints: ref(sprints as any),
      })
      expose({ modalRef })
      return () =>
        h(BulkChangeModal, {
          ref: modalRef,
          open: true,
          selectedIds: opts.selectedIds,
          issues: opts.issues,
          loadedSprints: sprints,
        })
    },
  })

  const app = createApp(Host)
  const vm = app.mount(el)
  return {
    el,
    vm,
    get modal() {
      return modalRef.value as { reset: () => void } | null
    },
    unmount: () => {
      app.unmount()
      el.remove()
    },
  }
}

async function flush() {
  await nextTick()
  await nextTick()
}

describe('BulkChangeModal — atomic batch wiring', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  afterEach(() => {
    document.body.innerHTML = ''
  })

  it('assignee: sends one PATCH /issues with assignee_id per row, never per-issue PUTs', async () => {
    const ids = new Set([10, 20, 30])
    const issues = [10, 20, 30].map(id => makeIssue(id, `PAI-${id}`))
    const users = [makeUser(2, 'mba', 'admin'), makeUser(3, 'dsc')]
    ;(api.patch as any).mockResolvedValue({ issues: issues.map(i => ({ ...i, assignee_id: 2 })) })

    const m = mount({ selectedIds: ids, issues, users })
    const modal = document.querySelector('.modal-stub') as HTMLElement
    expect(modal).toBeTruthy()

    const fieldSelect = modal.querySelector('select.v2-select') as HTMLSelectElement
    fieldSelect.value = 'assignee'
    fieldSelect.dispatchEvent(new Event('change'))
    await flush()

    const selects = modal.querySelectorAll('select.v2-select')
    const valueSelect = selects[1] as HTMLSelectElement
    valueSelect.value = '2'
    valueSelect.dispatchEvent(new Event('change'))
    await flush()

    const applyBtn = Array.from(modal.querySelectorAll('button')).find(b =>
      /Apply to 3 issue/.test(b.textContent ?? ''),
    ) as HTMLButtonElement
    expect(applyBtn).toBeTruthy()
    applyBtn.click()
    await flush()

    expect(api.put).not.toHaveBeenCalled()
    expect(api.patch).toHaveBeenCalledTimes(1)
    const [path, items] = (api.patch as any).mock.calls[0]
    expect(path).toBe('/issues')
    expect(items).toEqual([
      { ref: '10', fields: { assignee_id: 2 } },
      { ref: '20', fields: { assignee_id: 2 } },
      { ref: '30', fields: { assignee_id: 2 } },
    ])

    m.unmount()
  })

  it('status: sends one PATCH with the status field for every selected issue', async () => {
    const ids = new Set([1, 2])
    const issues = [1, 2].map(id => makeIssue(id, `PAI-${id}`))
    ;(api.patch as any).mockResolvedValue({ issues })

    const m = mount({ selectedIds: ids, issues, users: [] })
    const modal = document.querySelector('.modal-stub') as HTMLElement
    const fieldSelect = modal.querySelector('select.v2-select') as HTMLSelectElement
    fieldSelect.value = 'status'
    fieldSelect.dispatchEvent(new Event('change'))
    await flush()

    const valueSelect = modal.querySelectorAll('select.v2-select')[1] as HTMLSelectElement
    valueSelect.value = 'in-progress'
    valueSelect.dispatchEvent(new Event('change'))
    await flush()

    const applyBtn = Array.from(modal.querySelectorAll('button')).find(b =>
      /Apply to 2 issue/.test(b.textContent ?? ''),
    ) as HTMLButtonElement
    applyBtn.click()
    await flush()

    expect(api.patch).toHaveBeenCalledWith(
      '/issues',
      [
        { ref: '1', fields: { status: 'in-progress' } },
        { ref: '2', fields: { status: 'in-progress' } },
      ],
      expect.objectContaining({ signal: expect.any(AbortSignal) }),
    )
    m.unmount()
  })

  it('chunks selections into 50-id PATCH calls and reports completed/total on chunk failure', async () => {
    const total = 120
    const ids = new Set(Array.from({ length: total }, (_, i) => i + 1))
    const issues = Array.from({ length: total }, (_, i) => makeIssue(i + 1, `PAI-${i + 1}`))
    const users = [makeUser(2, 'mba', 'admin')]
    const patchCalls: Array<{ ids: number[] }> = []
    ;(api.patch as any).mockImplementation((_path: string, items: Array<{ ref: string }>) => {
      const callIds = items.map(it => Number(it.ref))
      patchCalls.push({ ids: callIds })
      // Fail the SECOND chunk so we can assert the partial-progress error.
      if (patchCalls.length === 2) {
        return Promise.reject(
          Object.assign(new Error('foreign-key violation'), { status: 400 }),
        )
      }
      return Promise.resolve({ issues: callIds.map(id => ({ ...makeIssue(id, `PAI-${id}`), status: 'in-progress' })) })
    })

    const m = mount({ selectedIds: ids, issues, users })
    const modal = document.querySelector('.modal-stub') as HTMLElement
    const fieldSelect = modal.querySelector('select.v2-select') as HTMLSelectElement
    fieldSelect.value = 'status'
    fieldSelect.dispatchEvent(new Event('change'))
    await flush()
    const valueSelect = modal.querySelectorAll('select.v2-select')[1] as HTMLSelectElement
    valueSelect.value = 'in-progress'
    valueSelect.dispatchEvent(new Event('change'))
    await flush()
    const applyBtn = Array.from(modal.querySelectorAll('button')).find(b =>
      /Apply to 120 issues/.test(b.textContent ?? ''),
    ) as HTMLButtonElement
    applyBtn.click()
    await flush()
    await flush()

    // Two PATCH calls happened — chunk 1 (50) succeeded, chunk 2 (50) failed,
    // chunk 3 never fired because we stopped on first error.
    expect(patchCalls.length).toBe(2)
    expect(patchCalls[0].ids).toHaveLength(50)
    expect(patchCalls[1].ids).toHaveLength(50)
    // Chunks must be disjoint and cover the first 100 ids in order.
    expect(patchCalls[0].ids[0]).toBe(1)
    expect(patchCalls[0].ids[49]).toBe(50)
    expect(patchCalls[1].ids[0]).toBe(51)

    // Error banner reports partial-progress (50 of 120 done, failed at chunk 2/3).
    const err = (modal.querySelector('.form-error') as HTMLElement)?.textContent ?? ''
    expect(err).toMatch(/50\/120/)
    expect(err).toMatch(/chunk 2\/3/)
    expect(err).toMatch(/foreign-key violation/)
    m.unmount()
  })

  it('shows a progress label for >50 selections, hides it for ≤50', async () => {
    // 60 ids → chunked → progress visible after first chunk.
    const ids60 = new Set(Array.from({ length: 60 }, (_, i) => i + 1))
    const issues60 = Array.from({ length: 60 }, (_, i) => makeIssue(i + 1, `PAI-${i + 1}`))

    let resolveFirst: (v: any) => void = () => {}
    let resolveSecond: (v: any) => void = () => {}
    let call = 0
    ;(api.patch as any).mockImplementation((_path: string, items: Array<{ ref: string }>) => {
      call++
      const ids = items.map(it => Number(it.ref))
      const responses = { issues: ids.map(id => ({ ...makeIssue(id, `PAI-${id}`), status: 'in-progress' })) }
      if (call === 1) return new Promise(res => { resolveFirst = () => res(responses) })
      return new Promise(res => { resolveSecond = () => res(responses) })
    })

    const m = mount({ selectedIds: ids60, issues: issues60, users: [] })
    const modal = document.querySelector('.modal-stub') as HTMLElement
    const fieldSelect = modal.querySelector('select.v2-select') as HTMLSelectElement
    fieldSelect.value = 'status'
    fieldSelect.dispatchEvent(new Event('change'))
    await flush()
    const valueSelect = modal.querySelectorAll('select.v2-select')[1] as HTMLSelectElement
    valueSelect.value = 'in-progress'
    valueSelect.dispatchEvent(new Event('change'))
    await flush()
    const applyBtn = Array.from(modal.querySelectorAll('button')).find(b =>
      /Apply to 60 issues/.test(b.textContent ?? ''),
    ) as HTMLButtonElement
    applyBtn.click()
    await flush()

    // First chunk in flight → label shows "Applying 0 of 60"
    let label = (modal.querySelector('.bulk-progress-label') as HTMLElement)?.textContent ?? ''
    expect(label).toMatch(/Applying 0 of 60 issues/)

    resolveFirst({})
    await flush()
    await flush()
    // After first chunk, second chunk in flight → "Applying 50 of 60".
    label = (modal.querySelector('.bulk-progress-label') as HTMLElement)?.textContent ?? ''
    expect(label).toMatch(/Applying 50 of 60 issues/)

    resolveSecond({})
    await flush()
    m.unmount()
  })

  it('sprint mode: surfaces relation failure to user instead of swallowing it (PAI-314)', async () => {
    const ids = new Set([1, 2, 3])
    const issues = [1, 2, 3].map(id => {
      const iss = makeIssue(id, `PAI-${id}`) as Issue
      ;(iss as any).sprint_ids = []
      return iss
    })
    // Sprint Add mode → POST /relations per issue. Reject the FIRST POST
    // so the loop bails on issue #1 and reports 0/3 done.
    const apiPost = api.post as any
    apiPost.mockRejectedValue(
      Object.assign(new Error('forbidden'), { status: 403 }),
    )

    const m = mount({
      selectedIds: ids,
      issues,
      users: [],
      sprints: [{ id: 99, title: 'S99' }],
    })
    const modal = document.querySelector('.modal-stub') as HTMLElement

    // Field = Sprint
    const fieldSelect = modal.querySelector('select.v2-select') as HTMLSelectElement
    fieldSelect.value = 'sprint'
    fieldSelect.dispatchEvent(new Event('change'))
    await flush()

    // Tick the sprint checkbox (drives bulkSprintIds).
    const sprintCheckbox = modal.querySelector('.bulk-sprint-opt input[type="checkbox"]') as HTMLInputElement
    expect(sprintCheckbox).toBeTruthy()
    sprintCheckbox.dispatchEvent(new Event('change'))
    await flush()

    // Apply.
    const applyBtn = Array.from(modal.querySelectorAll('button')).find(b =>
      /Apply to 3 issues/.test(b.textContent ?? ''),
    ) as HTMLButtonElement
    expect(applyBtn).toBeTruthy()
    applyBtn.click()
    await flush()
    await flush()

    // Error banner must surface (no silent swallowing).
    const err = (modal.querySelector('.form-error') as HTMLElement)?.textContent ?? ''
    expect(err).toMatch(/0\/3 updated before failure/)
    expect(err).toMatch(/PAI-1/)
    expect(err).toMatch(/forbidden/)
    m.unmount()
  })

  it('aborts in-flight chunks when reset() is called mid-bulk (PAI-317)', async () => {
    const total = 100
    const ids = new Set(Array.from({ length: total }, (_, i) => i + 1))
    const issues = Array.from({ length: total }, (_, i) => makeIssue(i + 1, `PAI-${i + 1}`))

    let chunkCount = 0
    let firstCallSignal: AbortSignal | null = null
    ;(api.patch as any).mockImplementation(
      (_path: string, _items: Array<{ ref: string }>, opts?: { signal?: AbortSignal }) => {
        chunkCount++
        if (chunkCount === 1) firstCallSignal = opts?.signal ?? null
        // Hold forever unless aborted — like a real network call.
        return new Promise((_res, rej) => {
          if (opts?.signal) {
            opts.signal.addEventListener('abort', () =>
              rej(new DOMException('aborted', 'AbortError')),
            )
          }
        })
      },
    )

    const m = mount({ selectedIds: ids, issues, users: [] })
    const modal = document.querySelector('.modal-stub') as HTMLElement
    const fieldSelect = modal.querySelector('select.v2-select') as HTMLSelectElement
    fieldSelect.value = 'status'
    fieldSelect.dispatchEvent(new Event('change'))
    await flush()
    const valueSelect = modal.querySelectorAll('select.v2-select')[1] as HTMLSelectElement
    valueSelect.value = 'in-progress'
    valueSelect.dispatchEvent(new Event('change'))
    await flush()
    const applyBtn = Array.from(modal.querySelectorAll('button')).find(b =>
      /Apply to 100 issues/.test(b.textContent ?? ''),
    ) as HTMLButtonElement
    applyBtn.click()
    await flush()

    // First chunk in flight, signal wired and not yet aborted.
    expect(chunkCount).toBe(1)
    expect(firstCallSignal).not.toBeNull()
    expect((firstCallSignal as AbortSignal | null)?.aborted).toBe(false)

    // Caller calls reset() — should abort the in-flight chunk and
    // prevent any further chunks from firing.
    expect(m.modal).not.toBeNull()
    m.modal!.reset()
    await flush()
    await flush()

    expect((firstCallSignal as AbortSignal | null)?.aborted).toBe(true)
    // No chunk #2 fired because chunk #1's abort errored out the loop.
    expect(chunkCount).toBe(1)
    // Banner reflects the cancellation, not a generic failure.
    const err = (modal.querySelector('.form-error') as HTMLElement)?.textContent ?? ''
    expect(err).toMatch(/Cancelled/)
    expect(err).toMatch(/0\/100/)
    m.unmount()
  })

  it('Unassigned option fires PATCH with assignee_id:null (PAI-319)', async () => {
    const ids = new Set([10, 20])
    const issues = [10, 20].map(id => makeIssue(id, `PAI-${id}`))
    const users = [makeUser(2, 'mba', 'admin')]
    ;(api.patch as any).mockResolvedValue({
      issues: issues.map(i => ({ ...i, assignee_id: null })),
    })

    const m = mount({ selectedIds: ids, issues, users })
    const modal = document.querySelector('.modal-stub') as HTMLElement
    const fieldSelect = modal.querySelector('select.v2-select') as HTMLSelectElement
    fieldSelect.value = 'assignee'
    fieldSelect.dispatchEvent(new Event('change'))
    await flush()

    // The Unassigned option carries value=''.
    const valueSelect = modal.querySelectorAll('select.v2-select')[1] as HTMLSelectElement
    valueSelect.value = ''
    valueSelect.dispatchEvent(new Event('change'))
    await flush()

    const applyBtn = Array.from(modal.querySelectorAll('button')).find(b =>
      /Apply to 2 issues/.test(b.textContent ?? ''),
    ) as HTMLButtonElement
    applyBtn.click()
    await flush()

    expect(api.patch).toHaveBeenCalledWith(
      '/issues',
      [
        { ref: '10', fields: { assignee_id: null } },
        { ref: '20', fields: { assignee_id: null } },
      ],
      expect.anything(),
    )
    m.unmount()
  })

  it('No-parent (orphan) option fires PATCH with parent_id:null (PAI-319)', async () => {
    // Selected children + an unselected epic (so the parent picker has options).
    const child = makeIssue(10, 'PAI-10')
    ;(child as any).parent_id = 99
    const epic = makeIssue(99, 'PAI-99')
    ;(epic as any).type = 'epic'
    const ids = new Set([10])
    ;(api.patch as any).mockResolvedValue({
      issues: [{ ...child, parent_id: null }],
    })

    const m = mount({ selectedIds: ids, issues: [child, epic], users: [] })
    const modal = document.querySelector('.modal-stub') as HTMLElement
    const fieldSelect = modal.querySelector('select.v2-select') as HTMLSelectElement
    fieldSelect.value = 'parent'
    fieldSelect.dispatchEvent(new Event('change'))
    await flush()

    // The "— No parent (orphan) —" option carries value=''.
    const valueSelect = modal.querySelectorAll('select.v2-select')[1] as HTMLSelectElement
    valueSelect.value = ''
    valueSelect.dispatchEvent(new Event('change'))
    await flush()

    const applyBtn = Array.from(modal.querySelectorAll('button')).find(b =>
      /Apply to 1 issue/.test(b.textContent ?? ''),
    ) as HTMLButtonElement
    applyBtn.click()
    await flush()

    expect(api.patch).toHaveBeenCalledWith(
      '/issues',
      [{ ref: '10', fields: { parent_id: null } }],
      expect.anything(),
    )
    m.unmount()
  })

  it('surfaces backend error summary when PATCH fails (the screenshot scenario)', async () => {
    const ids = new Set([1, 2])
    const issues = [1, 2].map(id => makeIssue(id, `PAI-${id}`))
    const users = [makeUser(2, 'mba', 'admin')]
    ;(api.patch as any).mockRejectedValue(
      Object.assign(new Error('foreign-key violation (e.g. unknown parent_id / assignee_id)'), {
        status: 400,
      }),
    )

    const m = mount({ selectedIds: ids, issues, users })
    const modal = document.querySelector('.modal-stub') as HTMLElement
    const fieldSelect = modal.querySelector('select.v2-select') as HTMLSelectElement
    fieldSelect.value = 'assignee'
    fieldSelect.dispatchEvent(new Event('change'))
    await flush()

    const valueSelect = modal.querySelectorAll('select.v2-select')[1] as HTMLSelectElement
    valueSelect.value = '2'
    valueSelect.dispatchEvent(new Event('change'))
    await flush()

    const applyBtn = Array.from(modal.querySelectorAll('button')).find(b =>
      /Apply to 2 issue/.test(b.textContent ?? ''),
    ) as HTMLButtonElement
    applyBtn.click()
    await flush()

    const err = modal.querySelector('.form-error') as HTMLElement
    expect(err?.textContent ?? '').toContain('foreign-key violation')
    m.unmount()
  })
})
