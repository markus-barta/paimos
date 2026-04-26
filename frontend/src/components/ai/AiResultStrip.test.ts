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
})
