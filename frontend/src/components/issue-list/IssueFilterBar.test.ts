import { afterEach, describe, expect, it } from 'vitest'
import { createApp, defineComponent, h, nextTick, ref } from 'vue'

import i18n from '@/i18n'
import IssueFilterBar from './IssueFilterBar.vue'
import type {
  EnabledFilter,
  FilterOption,
  SharedFilterState,
  TagOption,
} from './types'

const STATUS_OPTIONS: FilterOption[] = [
  { value: 'in-progress', label: 'In Progress' },
  { value: 'done', label: 'Done' },
  { value: 'delivered', label: 'Delivered' },
]
const TYPE_OPTIONS: FilterOption[] = [
  { value: 'ticket', label: 'Ticket' },
  { value: 'task', label: 'Task' },
]
const TAG_OPTIONS: TagOption[] = [
  { id: 1, name: 'urgent' },
  { id: 2, name: 'frontend' },
]

function mountBar(opts: {
  initial?: Partial<SharedFilterState>
  enabled?: EnabledFilter[]
}) {
  const el = document.createElement('div')
  document.body.appendChild(el)
  const state = ref<SharedFilterState>({
    status: [],
    type: [],
    priority: [],
    tagIds: [],
    q: '',
    ...opts.initial,
  })
  const Host = defineComponent({
    render() {
      return h(IssueFilterBar, {
        modelValue: state.value,
        'onUpdate:modelValue': (next: SharedFilterState) => (state.value = next),
        enabledFilters: opts.enabled ?? ['status', 'type', 'tag', 'q'],
        statusOptions: STATUS_OPTIONS,
        typeOptions: TYPE_OPTIONS,
        tagOptions: TAG_OPTIONS,
        mobileBreakpoint: 0, // force desktop layout in tests
      })
    },
  })
  const app = createApp(Host)
  app.use(i18n)
  app.mount(el)
  return {
    el,
    state,
    unmount() {
      app.unmount()
      el.remove()
    },
  }
}

describe('IssueFilterBar — PAI-469', () => {
  afterEach(() => {
    document.body.innerHTML = ''
  })

  it('renders only the enabled filter pills', async () => {
    const m = mountBar({ enabled: ['status'] })
    await nextTick()
    const pills = m.el.querySelectorAll('.if-bar__pill')
    expect(pills.length).toBe(1)
    expect(pills[0].textContent).toContain('Status')
    m.unmount()
  })

  it('opens a panel on pill click and toggles an option', async () => {
    const m = mountBar({})
    await nextTick()
    const pill = m.el.querySelectorAll('.if-bar__pill')[0] as HTMLElement
    pill.click()
    await nextTick()
    expect(m.el.querySelector('.if-bar__panel')).not.toBeNull()

    const firstOpt = m.el.querySelector('.if-bar__opt') as HTMLElement
    firstOpt.click()
    await nextTick()
    expect(m.state.value.status).toEqual(['in-progress'])
    m.unmount()
  })

  it('emits an active chip for each selected filter', async () => {
    const m = mountBar({
      initial: { status: ['done'], type: ['ticket'], tagIds: [1] },
    })
    await nextTick()
    const chips = m.el.querySelectorAll('.if-bar__chip')
    expect(chips.length).toBe(3)
  })

  it('clear all empties every filter', async () => {
    const m = mountBar({
      initial: { status: ['done'], type: ['ticket'], q: 'hi' },
    })
    await nextTick()
    const clear = m.el.querySelector('.if-bar__clear') as HTMLElement
    clear.click()
    await nextTick()
    expect(m.state.value).toEqual({
      status: [],
      type: [],
      priority: [],
      tagIds: [],
      q: '',
    })
    m.unmount()
  })

  it('removes a single chip via the chip-x button', async () => {
    const m = mountBar({ initial: { status: ['done', 'delivered'] } })
    await nextTick()
    const xBtns = m.el.querySelectorAll('.if-bar__chip-x')
    expect(xBtns.length).toBe(2)
    ;(xBtns[0] as HTMLElement).click()
    await nextTick()
    expect(m.state.value.status.length).toBe(1)
    m.unmount()
  })
})
