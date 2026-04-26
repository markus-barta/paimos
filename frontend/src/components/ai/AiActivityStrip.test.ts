import { afterEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'
import AiActivityStrip from '@/components/ai/AiActivityStrip.vue'
import { mountComponent } from '@/components/ai/testMount'

describe('AiActivityStrip', () => {
  afterEach(() => {
    vi.useRealTimers()
    document.body.innerHTML = ''
  })

  it('stays hidden during the pending threshold, then shows narration', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-04-26T12:00:00Z'))
    const startedAt = Date.now()
    const mounted = await mountComponent(AiActivityStrip, {
      actionKey: 'find_parent',
      title: 'Parent suggestion',
      startedAt,
    })

    expect(mounted.el.textContent?.trim()).toBe('')

    vi.advanceTimersByTime(300)
    await nextTick()

    expect(mounted.el.textContent).toContain('Reading the issue')
    await mounted.unmount()
  })
})
