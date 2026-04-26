import { afterEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'
import AiSurfaceFeedback from '@/components/ai/AiSurfaceFeedback.vue'
import { mountComponent } from '@/components/ai/testMount'
import { useAiAction } from '@/composables/useAiAction'

describe('AiSurfaceFeedback', () => {
  afterEach(() => {
    const aiAction = useAiAction()
    aiAction.reset()
    aiAction.clearError()
    document.body.innerHTML = ''
  })

  it('sends the expected payload for a parent-apply decision', async () => {
    const aiAction = useAiAction()
    aiAction.result.value = {
      hostKey: 'issue-detail:1:record',
      action: 'find_parent',
      field: 'record',
      fieldLabel: 'Issue record',
      issueId: 1,
      body: {
        candidates: [
          { issue_key: 'PAI-83', title: 'Parent', confidence: 'high', rationale: 'Best fit' },
        ],
      },
      sourceText: '',
      onAccept: () => undefined,
    } as any
    const apply = vi.fn().mockResolvedValue(undefined)
    const mounted = await mountComponent(AiSurfaceFeedback, { hostKey: 'issue-detail:1:record', apply })

    const applyButton = Array.from(mounted.el.querySelectorAll('button')).find((b) => b.textContent?.includes('Apply'))
    expect(applyButton).toBeTruthy()
    applyButton?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await nextTick()

    expect(apply).toHaveBeenCalledWith(expect.objectContaining({
      action: 'find_parent',
      intent: 'move-under',
      values: { issue_key: 'PAI-83' },
    }))
    await mounted.unmount()
  })

  it('shows an error and keeps the result visible when apply fails', async () => {
    const aiAction = useAiAction()
    aiAction.result.value = {
      hostKey: 'issue-detail:1:record',
      action: 'estimate_effort',
      field: 'record',
      fieldLabel: 'Issue record',
      issueId: 1,
      body: { hours: 6, lp: 1 },
      sourceText: '',
      onAccept: () => undefined,
    } as any
    const apply = vi.fn().mockRejectedValue(new Error('Save failed'))
    const mounted = await mountComponent(AiSurfaceFeedback, { hostKey: 'issue-detail:1:record', apply })

    const applyButton = Array.from(mounted.el.querySelectorAll('button')).find((b) => b.textContent?.includes('Apply'))
    applyButton?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await Promise.resolve()
    await nextTick()

    expect(mounted.el.textContent).toContain('Save failed')
    expect(mounted.el.textContent).toContain('6h · 1 LP')
    await mounted.unmount()
  })

  it('applies tone-check rewrites through the host callback', async () => {
    const aiAction = useAiAction()
    aiAction.result.value = {
      hostKey: 'customer-detail:notes',
      action: 'tone_check',
      field: 'customer_notes',
      fieldLabel: 'Customer notes',
      body: {
        optimized: 'Neutralized customer note.',
        counters: { phrases_removed: 2 },
      },
      sourceText: 'You should definitely upgrade now.',
      onAccept: () => undefined,
    } as any
    const apply = vi.fn().mockResolvedValue(undefined)
    const mounted = await mountComponent(AiSurfaceFeedback, { hostKey: 'customer-detail:notes', apply })

    const applyButton = Array.from(mounted.el.querySelectorAll('button')).find((b) => b.textContent?.includes('Apply'))
    applyButton?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await Promise.resolve()
    await nextTick()

    expect(apply).toHaveBeenCalledWith(expect.objectContaining({
      action: 'tone_check',
      intent: 'replace-text',
      values: { text: 'Neutralized customer note.' },
    }))
    await mounted.unmount()
  })
})
