import { afterEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'
import AiResultStrip from '@/components/ai/AiResultStrip.vue'
import { mountComponent } from '@/components/ai/testMount'

describe('AiResultStrip', () => {
  afterEach(() => {
    vi.useRealTimers()
    document.body.innerHTML = ''
  })

  it('auto-dismisses after the configured timeout', async () => {
    vi.useFakeTimers()
    const dismiss = vi.fn()
    const mounted = await mountComponent(AiResultStrip, {
      actionKey: 'estimate_effort',
      title: 'Estimate effort',
      summary: '6h · 1 LP suggested',
      dismissable: true,
      autoDismissMs: 200,
      onDismiss: dismiss,
    })

    vi.advanceTimersByTime(220)
    await nextTick()

    expect(dismiss).toHaveBeenCalledTimes(1)
    await mounted.unmount()
  })

  it('emits details instead of toggling inline content in modal mode', async () => {
    const details = vi.fn()
    const mounted = await mountComponent(AiResultStrip, {
      actionKey: 'spec_out',
      title: 'Spec out',
      summary: '6 acceptance items drafted',
      detailsLabel: 'Details',
      detailsMode: 'modal',
      onDetails: details,
    })

    const button = Array.from(mounted.el.querySelectorAll('button')).find((b) => b.textContent?.includes('Details'))
    button?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await nextTick()

    expect(details).toHaveBeenCalledTimes(1)
    expect(mounted.el.textContent).not.toContain('Inline detail body')
    await mounted.unmount()
  })
})
