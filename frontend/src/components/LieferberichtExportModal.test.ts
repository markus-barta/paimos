import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createApp, nextTick } from 'vue'
import i18n from '@/i18n'
import { OTHER_STATUS_SENTINEL } from '@/composables/useIssueFilter'
import { LS_LIEFERBERICHT_COLS } from '@/constants/storage'
import LieferberichtExportModal from './LieferberichtExportModal.vue'

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({ user: { locale: 'en' } }),
}))

vi.mock('@/components/AppModal.vue', () => ({
  default: {
    props: ['open'],
    emits: ['close'],
    template: '<div v-if="open" class="modal-stub"><slot /></div>',
  },
}))

vi.mock('@/components/MetaSelect.vue', () => ({
  default: {
    props: ['modelValue', 'options'],
    emits: ['update:modelValue'],
    template: `
      <select class="meta-select-stub" :value="modelValue" @change="$emit('update:modelValue', $event.target.value)">
        <option v-for="o in options" :key="o.value" :value="o.value">{{ o.label }}</option>
      </select>
    `,
  },
}))

async function settle() {
  await Promise.resolve()
  await nextTick()
}

const colTestIds = ['lb-col-sp', 'lb-col-h', 'lb-col-ar-sp', 'lb-col-ar-h', 'lb-col-ar-eur'] as const

function mountModal(props: {
  open: boolean
  projectId: number
  filterStatus: string[]
  filterType?: string[]
  filterPriority?: string[]
  filterAssignee?: string[]
  filterCostUnit?: string[]
  filterRelease?: string[]
  filterTags: string[]
  filterSprints: string[]
  dateField?: string
  dateFrom?: string
  dateTo?: string
  unsupportedActive: string[]
}) {
  const el = document.createElement('div')
  document.body.appendChild(el)
  const app = createApp(LieferberichtExportModal, {
    filterType: [],
    filterPriority: [],
    filterAssignee: [],
    filterCostUnit: [],
    filterRelease: [],
    dateField: '',
    dateFrom: '',
    dateTo: '',
    ...props,
  })
  app.use(i18n)
  app.mount(el)
  return {
    el,
    unmount() {
      app.unmount()
      el.remove()
    },
  }
}

function colInput(root: HTMLElement, testId: typeof colTestIds[number]) {
  const input = root.querySelector<HTMLInputElement>(`[data-testid="${testId}"]`)
  if (!input) throw new Error(`missing ${testId}`)
  return input
}

describe('LieferberichtExportModal', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  afterEach(() => {
    document.body.innerHTML = ''
    vi.restoreAllMocks()
  })

  it('serializes negated status and tag filters for the PDF endpoint', async () => {
    const open = vi.spyOn(window, 'open').mockImplementation(() => null)
    const mounted = mountModal({
      open: true,
      projectId: 7,
      filterStatus: ['!done', 'qa', OTHER_STATUS_SENTINEL, `!${OTHER_STATUS_SENTINEL}`],
      filterType: ['ticket', '!epic'],
      filterPriority: ['high'],
      filterAssignee: ['4', '!5'],
      filterCostUnit: ['Ops'],
      filterRelease: ['May'],
      filterTags: ['12', '!34', 'bad', '!0'],
      filterSprints: ['5'],
      dateField: 'completed',
      dateFrom: '2026-05-01',
      dateTo: '2026-05-31',
      unsupportedActive: [],
    })
    await settle()

    mounted.el.querySelector<HTMLButtonElement>('.btn-primary')?.click()
    await settle()

    expect(open).toHaveBeenCalledTimes(1)
    const [rawURL, target] = open.mock.calls[0]
    const url = new URL(String(rawURL), 'http://paimos.test')
    expect(target).toBe('_blank')
    expect(url.pathname).toBe('/api/projects/7/reports/projektbericht/pdf')
    expect(url.searchParams.get('snapshot')).toBe('1')
    expect(url.searchParams.get('scope')).toBe('sprint')
    expect(url.searchParams.get('sprint_ids')).toBe('5')
    expect(url.searchParams.get('statuses')).toBe('!done,qa')
    expect(url.searchParams.get('type')).toBe('ticket,!epic')
    expect(url.searchParams.get('priority')).toBe('high')
    expect(url.searchParams.get('assignee_id')).toBe('4')
    expect(url.searchParams.get('cost_unit')).toBe('Ops')
    expect(url.searchParams.get('release')).toBe('May')
    expect(url.searchParams.get('tag_ids')).toBe('12,!34')
    expect(url.searchParams.get('date_field')).toBe('completed')
    expect(url.searchParams.get('date_from')).toBe('2026-05-01')
    expect(url.searchParams.get('date_to')).toBe('2026-05-31')
    expect(url.searchParams.get('cols')).toBe('sp,h,ar_sp,ar_h,ar_eur')

    mounted.unmount()
  })

  it('does not send negated sprint IDs to the sprint-scoped endpoint', async () => {
    const open = vi.spyOn(window, 'open').mockImplementation(() => null)
    const mounted = mountModal({
      open: true,
      projectId: 7,
      filterStatus: [],
      filterTags: [],
      filterSprints: ['!5'],
      unsupportedActive: ['excluded sprint'],
    })
    await settle()

    mounted.el.querySelector<HTMLButtonElement>('.btn-primary')?.click()
    await settle()

    const [rawURL] = open.mock.calls[0]
    const url = new URL(String(rawURL), 'http://paimos.test')
    expect(url.searchParams.get('scope')).toBe('date_range')
    expect(url.searchParams.has('sprint_ids')).toBe(false)

    mounted.unmount()
  })

  it('persists numeric column visibility and sends the same selection to the PDF endpoint', async () => {
    const open = vi.spyOn(window, 'open').mockImplementation(() => null)
    const mounted = mountModal({
      open: true,
      projectId: 7,
      filterStatus: [],
      filterTags: [],
      filterSprints: [],
      unsupportedActive: [],
    })
    await settle()

    for (const id of colTestIds) {
      const input = colInput(mounted.el, id)
      expect(input.checked).toBe(true)
      input.click()
      await settle()
    }
    expect(JSON.parse(localStorage.getItem(LS_LIEFERBERICHT_COLS) ?? '{}')).toEqual({
      sp: false,
      h: false,
      arSp: false,
      arH: false,
      arEur: false,
      bookedBy: false,
    })

    mounted.unmount()
    const remounted = mountModal({
      open: true,
      projectId: 7,
      filterStatus: [],
      filterTags: [],
      filterSprints: [],
      unsupportedActive: [],
    })
    await settle()

    for (const id of colTestIds) {
      expect(colInput(remounted.el, id).checked).toBe(false)
    }

    colInput(remounted.el, 'lb-col-sp').click()
    colInput(remounted.el, 'lb-col-ar-sp').click()
    colInput(remounted.el, 'lb-col-ar-eur').click()
    await settle()

    remounted.el.querySelector<HTMLButtonElement>('.btn-primary')?.click()
    await settle()

    const [rawURL] = open.mock.calls[0]
    const url = new URL(String(rawURL), 'http://paimos.test')
    expect(url.searchParams.get('cols')).toBe('sp,ar_sp,ar_eur')

    remounted.unmount()
  })

  // PAI-580: the "By month" scope ignores the inherited IssueList filters and
  // emits scope=time_booked with the window (from/to), grouping, and the
  // selected states (default = the completed set).
  it('builds a time_booked request from the month scope, ignoring IssueList filters', async () => {
    const open = vi.spyOn(window, 'open').mockImplementation(() => null)
    const mounted = mountModal({
      open: true,
      projectId: 9,
      filterStatus: ['qa'], // must be ignored in month scope
      filterTags: ['12'],
      filterSprints: ['5'], // must NOT become scope=sprint
      unsupportedActive: ['priority'],
    })
    await settle()

    mounted.el.querySelector<HTMLButtonElement>('[data-testid="lb-scope-month"]')?.click()
    await settle()

    mounted.el.querySelector<HTMLButtonElement>('[data-testid="lb-download"]')?.click()
    await settle()

    expect(open).toHaveBeenCalledTimes(1)
    const url = new URL(String(open.mock.calls[0][0]), 'http://paimos.test')
    expect(url.pathname).toBe('/api/projects/9/reports/projektbericht/pdf')
    expect(url.searchParams.get('scope')).toBe('time_booked')
    expect(url.searchParams.get('group')).toBe('flat')
    expect(url.searchParams.get('sprint_ids')).toBeNull()
    // Window: from/to present, ISO dates, from <= to.
    const from = url.searchParams.get('from') ?? ''
    const to = url.searchParams.get('to') ?? ''
    expect(from).toMatch(/^\d{4}-\d{2}-\d{2}$/)
    expect(to).toMatch(/^\d{4}-\d{2}-\d{2}$/)
    expect(from <= to).toBe(true)
    // Default states = completed set; the IssueList 'qa' filter is ignored.
    const states = (url.searchParams.get('statuses') ?? '').split(',')
    expect(states).toEqual(expect.arrayContaining(['done', 'delivered', 'accepted', 'invoiced']))
    expect(states).not.toContain('qa')

    mounted.unmount()
  })

  it('disables download in month scope when no states are selected', async () => {
    const open = vi.spyOn(window, 'open').mockImplementation(() => null)
    const mounted = mountModal({
      open: true, projectId: 9, filterStatus: [], filterTags: [], filterSprints: [], unsupportedActive: [],
    })
    await settle()
    mounted.el.querySelector<HTMLButtonElement>('[data-testid="lb-scope-month"]')?.click()
    await settle()
    // Uncheck all default states.
    for (const s of ['done', 'delivered', 'accepted', 'invoiced']) {
      const cb = mounted.el.querySelector<HTMLInputElement>(`[data-testid="lb-state-${s}"]`)
      if (cb?.checked) { cb.click() }
    }
    await settle()
    const btn = mounted.el.querySelector<HTMLButtonElement>('[data-testid="lb-download"]')
    expect(btn?.disabled).toBe(true)
    btn?.click()
    await settle()
    expect(open).not.toHaveBeenCalled()
    mounted.unmount()
  })
})
