import { afterEach, describe, expect, it } from 'vitest'
import { createApp, defineComponent, h, nextTick } from 'vue'

import i18n from '@/i18n'
import IssueVisibilityNudge from './IssueVisibilityNudge.vue'

function mountNudge(props: {
  status: string
  visible: boolean
  canEdit?: boolean
}) {
  const el = document.createElement('div')
  document.body.appendChild(el)

  const events: string[] = []

  const Host = defineComponent({
    render() {
      return h(IssueVisibilityNudge, {
        status: props.status,
        visible: props.visible,
        canEdit: props.canEdit ?? true,
        onMakeVisible: () => {
          events.push('make-visible')
        },
      })
    },
  })

  const app = createApp(Host)
  app.use(i18n)
  app.mount(el)
  return {
    el,
    events,
    unmount() {
      app.unmount()
      el.remove()
    },
  }
}

describe('IssueVisibilityNudge — PAI-464', () => {
  afterEach(() => {
    document.body.innerHTML = ''
  })

  it('renders when status is delivered and tag is absent', async () => {
    const m = mountNudge({ status: 'delivered', visible: false })
    await nextTick()
    expect(m.el.querySelector('.vis-nudge')).not.toBeNull()
    expect(m.el.querySelector('.vis-nudge__action')).not.toBeNull()
    m.unmount()
  })

  it('renders when status is done and tag is absent', async () => {
    const m = mountNudge({ status: 'done', visible: false })
    await nextTick()
    expect(m.el.querySelector('.vis-nudge')).not.toBeNull()
    m.unmount()
  })

  it('does not render when the tag is already attached', async () => {
    const m = mountNudge({ status: 'delivered', visible: true })
    await nextTick()
    expect(m.el.querySelector('.vis-nudge')).toBeNull()
    m.unmount()
  })

  it('does not render in non-terminal status (in-progress)', async () => {
    const m = mountNudge({ status: 'in-progress', visible: false })
    await nextTick()
    expect(m.el.querySelector('.vis-nudge')).toBeNull()
    m.unmount()
  })

  it('does not render when issue is cancelled', async () => {
    const m = mountNudge({ status: 'cancelled', visible: false })
    await nextTick()
    expect(m.el.querySelector('.vis-nudge')).toBeNull()
    m.unmount()
  })

  it('renders but disables the action when canEdit is false', async () => {
    const m = mountNudge({ status: 'done', visible: false, canEdit: false })
    await nextTick()
    const btn = m.el.querySelector('.vis-nudge__action') as HTMLButtonElement | null
    expect(btn).not.toBeNull()
    expect(btn?.disabled).toBe(true)
    m.unmount()
  })

  it('has no close-without-action button (dismiss only via the toggle)', async () => {
    const m = mountNudge({ status: 'done', visible: false })
    await nextTick()
    // The whole banner has exactly one button — the action — never a close.
    const buttons = m.el.querySelectorAll('.vis-nudge button')
    expect(buttons.length).toBe(1)
    m.unmount()
  })

  it('clicking the action emits make-visible', async () => {
    const m = mountNudge({ status: 'delivered', visible: false })
    await nextTick()
    const btn = m.el.querySelector('.vis-nudge__action') as HTMLButtonElement
    btn.click()
    await nextTick()
    expect(m.events).toEqual(['make-visible'])
    m.unmount()
  })
})
