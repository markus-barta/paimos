import { afterEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'
import AiActionMenu from '@/components/ai/AiActionMenu.vue'
import { mountComponent } from '@/components/ai/testMount'
import { api } from '@/api/client'
import { useAiAction } from '@/composables/useAiAction'
import { useAiOptimize } from '@/composables/useAiOptimize'

const actions = [
  {
    key: 'spec_out',
    label: 'Spec out',
    surface: 'issue',
    placement: 'text',
    implemented: true,
    default_profile_id: 'balanced',
    default_effort: 'standard',
  },
] as const

const executionOptions = {
  profiles: [
    {
      id: 'balanced',
      label: 'Balanced',
      provider: 'test',
      model: 'test/model',
      effort: 'standard',
      speed_label: 'Normal',
      cost_label: 'Normal',
    },
    {
      id: 'deep',
      label: 'Deep',
      provider: 'test',
      model: 'test/model',
      effort: 'deep',
      speed_label: 'Slower',
      cost_label: 'Higher',
    },
  ],
  efforts: ['standard', 'deep'],
  action_defaults: {
    spec_out: { profile_id: 'balanced', effort: 'standard' },
  },
  prompt_presets: [
    {
      ref: 'kb:memory:spec_writer',
      label: 'Spec Writer',
      type: 'memory',
      slug: 'spec_writer',
      status: 'active',
      revision: 'rev123',
      actions: ['spec_out'],
    },
  ],
  context_packs: [
    { id: 'issue', label: 'Issue only' },
    { id: 'knowledge', label: 'Project knowledge' },
  ],
}

async function flush() {
  await Promise.resolve()
  await nextTick()
}

describe('AiActionMenu', () => {
  afterEach(() => {
    const aiAction = useAiAction()
    aiAction.reset()
    aiAction.clearError()
    aiAction.actions.value = []
    aiAction.executionOptions.value = null
    useAiOptimize().available.value = false
    vi.restoreAllMocks()
    document.body.innerHTML = ''
  })

  it('posts selected execution profile and effort overrides', async () => {
    vi.spyOn(api, 'get').mockImplementation(async (path: string) => {
      if (path === '/ai/actions') return { actions }
      if (path.startsWith('/ai/execution-options')) return executionOptions
      return { available: true }
    })
    const post = vi.spyOn(api, 'post').mockResolvedValue({
      request_id: 'req-1',
      action: 'spec_out',
      body: { items: [] },
      options: {
        profile_id: 'deep',
        model: 'test/model',
        effort: 'deep',
        prompt_preset_ref: 'kb:memory:spec_writer@rev123',
        prompt_preset_label: 'Spec Writer',
        context_pack: 'knowledge',
        context_pack_label: 'Project knowledge',
      },
    } as never)

    const aiAction = useAiAction()
    const aiOptimize = useAiOptimize()
    aiOptimize.available.value = true
    await aiAction.refreshActions()
    await aiAction.refreshExecutionOptions()

    const mounted = await mountComponent(AiActionMenu, {
      field: 'description',
      fieldLabel: 'Description',
      issueId: 7,
      surface: 'issue',
      text: () => 'Draft AC',
      onAccept: () => undefined,
    })

    mounted.el.querySelector<HTMLButtonElement>('.ai-menu-chip-chev')?.click()
    await flush()

    const profile = mounted.el.querySelector<HTMLSelectElement>('select[aria-label="AI profile"]')
    const effort = mounted.el.querySelector<HTMLSelectElement>('select[aria-label="AI effort"]')
    const prompt = mounted.el.querySelector<HTMLSelectElement>('select[aria-label="AI prompt preset"]')
    const context = mounted.el.querySelector<HTMLSelectElement>('select[aria-label="AI context pack"]')
    expect(profile).toBeTruthy()
    expect(effort).toBeTruthy()
    expect(prompt).toBeTruthy()
    expect(context).toBeTruthy()

    profile!.value = 'deep'
    profile!.dispatchEvent(new Event('change', { bubbles: true }))
    effort!.value = 'deep'
    effort!.dispatchEvent(new Event('change', { bubbles: true }))
    prompt!.value = 'kb:memory:spec_writer'
    prompt!.dispatchEvent(new Event('change', { bubbles: true }))
    context!.value = 'knowledge'
    context!.dispatchEvent(new Event('change', { bubbles: true }))
    await flush()

    const specButton = Array.from(mounted.el.querySelectorAll('button')).find((b) => b.textContent?.includes('Spec out'))
    specButton?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await flush()

    expect(post).toHaveBeenCalledWith('/ai/action', expect.objectContaining({
      action: 'spec_out',
      options: { profile_id: 'deep', effort: 'deep', prompt_preset_ref: 'kb:memory:spec_writer', context_pack: 'knowledge' },
    }), { timeoutMs: 90_000 })

    await mounted.unmount()
  })

  it('omits controls when there is nothing meaningful to choose', async () => {
    const singleChoiceOptions = {
      profiles: [executionOptions.profiles[0]],
      efforts: ['standard'],
      action_defaults: executionOptions.action_defaults,
      prompt_presets: [],
      context_packs: [{ id: 'issue', label: 'Issue only' }],
    }
    vi.spyOn(api, 'get').mockImplementation(async (path: string) => {
      if (path === '/ai/actions') return { actions }
      if (path.startsWith('/ai/execution-options')) return singleChoiceOptions
      return { available: true }
    })

    const aiAction = useAiAction()
    useAiOptimize().available.value = true
    await aiAction.refreshActions()
    await aiAction.refreshExecutionOptions()

    const mounted = await mountComponent(AiActionMenu, {
      field: 'description',
      issueId: 7,
      surface: 'issue',
      text: () => 'Draft AC',
      onAccept: () => undefined,
    })

    mounted.el.querySelector<HTMLButtonElement>('.ai-menu-chip-chev')?.click()
    await flush()

    expect(mounted.el.querySelector('select[aria-label="AI profile"]')).toBeNull()
    expect(mounted.el.querySelector('select[aria-label="AI effort"]')).toBeNull()
    expect(mounted.el.querySelector('select[aria-label="AI prompt preset"]')).toBeNull()
    expect(mounted.el.querySelector('select[aria-label="AI context pack"]')).toBeNull()
    expect(mounted.el.textContent).toContain('Balanced · Standard')

    await mounted.unmount()
  })
})
