import { afterEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'
import AiOptimizeOverlay from '@/components/ai/AiOptimizeOverlay.vue'
import { mountComponent } from '@/components/ai/testMount'

describe('AiOptimizeOverlay', () => {
  afterEach(() => {
    document.body.innerHTML = ''
  })

  it('PAI-219 applies mixed per-hunk keep/reject decisions', async () => {
    const accept = vi.fn()
    const mounted = await mountComponent(AiOptimizeOverlay, {
      original: 'first paragraph.\n\nsecond paragraph.',
      optimized: 'first paragraph!\n\nsecond paragraph?',
      onAccept: accept,
    })

    const hunks = Array.from(mounted.el.querySelectorAll<HTMLElement>('.ai-hunk'))
    expect(hunks).toHaveLength(2)

    hunks[1].dispatchEvent(new FocusEvent('focusin', { bubbles: true }))
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'r' }))
    await nextTick()

    const acceptButton = Array.from(mounted.el.querySelectorAll<HTMLButtonElement>('button'))
      .find((button) => button.textContent?.includes('Accept & replace'))
    acceptButton?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await nextTick()

    expect(accept).toHaveBeenCalledWith('first paragraph!\n\nsecond paragraph.')
    await mounted.unmount()
  })
})
