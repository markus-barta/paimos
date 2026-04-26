import { afterEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'
import AiDecisionRow from '@/components/ai/AiDecisionRow.vue'
import { mountComponent } from '@/components/ai/testMount'

describe('AiDecisionRow', () => {
  afterEach(() => {
    document.body.innerHTML = ''
  })

  it('dispatches keyboard shortcuts', async () => {
    const primary = vi.fn()
    const secondary = vi.fn()
    const mounted = await mountComponent(AiDecisionRow, {
      primary: { label: 'Apply', shortcut: 'A', action: primary },
      secondary: [{ label: 'Dismiss', shortcut: 'D', action: secondary }],
    })

    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'a' }))
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await nextTick()

    expect(primary).toHaveBeenCalledTimes(1)
    expect(secondary).toHaveBeenCalledTimes(1)
    await mounted.unmount()
  })
})
