import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createApp, nextTick } from 'vue'
import i18n from '@/i18n'
import { OTHER_STATUS_SENTINEL } from '@/composables/useIssueFilter'
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

function mountModal(props: {
  open: boolean
  projectId: number
  filterStatus: string[]
  filterTags: string[]
  filterSprints: string[]
  unsupportedActive: string[]
}) {
  const el = document.createElement('div')
  document.body.appendChild(el)
  const app = createApp(LieferberichtExportModal, props)
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
      filterTags: ['12', '!34', 'bad', '!0'],
      filterSprints: ['5'],
      unsupportedActive: [],
    })
    await settle()

    mounted.el.querySelector<HTMLButtonElement>('.btn-primary')?.click()
    await settle()

    expect(open).toHaveBeenCalledTimes(1)
    const [rawURL, target] = open.mock.calls[0]
    const url = new URL(String(rawURL), 'http://paimos.test')
    expect(target).toBe('_blank')
    expect(url.pathname).toBe('/api/projects/7/reports/lieferbericht/pdf')
    expect(url.searchParams.get('scope')).toBe('sprint')
    expect(url.searchParams.get('sprint_ids')).toBe('5')
    expect(url.searchParams.get('statuses')).toBe('!done,qa')
    expect(url.searchParams.get('tag_ids')).toBe('12,!34')

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
})
