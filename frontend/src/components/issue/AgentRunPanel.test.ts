import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createApp, defineComponent, h, nextTick } from 'vue'

import { api } from '@/api/client'
import AgentRunPanel from './AgentRunPanel.vue'

vi.mock('@/api/client', () => ({
  api: { get: vi.fn(), post: vi.fn() },
  errMsg: (_e: unknown, fallback: string) => fallback,
}))

vi.mock('@/components/AppIcon.vue', () => ({
  default: { props: ['name'], template: '<span class="icon-stub" />' },
}))

async function settle() {
  for (let i = 0; i < 5; i += 1) {
    await Promise.resolve()
    await nextTick()
  }
}

function mountPanel() {
  const el = document.createElement('div')
  document.body.appendChild(el)
  const Host = defineComponent({
    render() {
      return h(AgentRunPanel, { issueId: 5, issueKey: 'PAI-5', projectId: 9 })
    },
  })
  const app = createApp(Host)
  app.mount(el)
  return {
    el,
    unmount() {
      app.unmount()
      el.remove()
    },
  }
}

function run(status: string, extra: Record<string, unknown> = {}) {
  return {
    id: 1,
    status,
    version: '',
    device_id: 'laptop',
    agent_name: '',
    provider_label: 'Claude Code',
    deploy_target: '',
    tests_summary: null,
    error: '',
    created_at: '2026-06-29 10:00:00',
    started_at: null,
    finished_at: null,
    ...extra,
  }
}

describe('AgentRunPanel — PAI-610', () => {
  beforeEach(() => {
    vi.mocked(api.get).mockReset()
    vi.mocked(api.post).mockReset()
  })
  afterEach(() => {
    document.body.innerHTML = ''
    vi.restoreAllMocks()
  })

  it("renders a run's status and starts a run via the issue key", async () => {
    vi.mocked(api.get).mockImplementation(async (path: string) => {
      if (path === '/issues/5/runs')
        return {
          runs: [
            run('deployed', {
              version: '4.6.0',
              deploy_target: 'ppm',
              tests_summary: 'npm test passed: 2 passed',
              finished_at: '2026-06-29 10:05:00',
            }),
          ],
        }
      if (path === '/projects/9/runners')
        return { runners: [{ user_id: 1, device_id: 'laptop', last_seen: '' }] }
      return {}
    })
    vi.mocked(api.post).mockResolvedValue({})

    const { el, unmount } = mountPanel()
    await settle()

    expect(el.textContent).toContain('Deployed')
    expect(el.textContent).toContain('v4.6.0')
    expect(el.textContent).toContain('npm test passed: 2 passed')
    expect(el.textContent).toContain('#1')
    expect(el.textContent).toContain('Claimed')
    expect(el.textContent).toContain('Tests passed')
    expect(el.querySelector('.arp-device')).toBeNull() // 1 runner → no picker

    const btn = el.querySelector<HTMLButtonElement>('.btn-primary')
    expect(btn?.textContent).toContain('Implement this')
    btn!.click()
    await settle()
    expect(api.post).toHaveBeenCalledWith('/issues/PAI-5/implement', {
      device_id: 'laptop',
      action_key: 'claude_cli.implement',
    })
    unmount()
  })

  it('hints when no runner is online (vs. a runners-endpoint error)', async () => {
    vi.mocked(api.get).mockImplementation(async (path: string) => {
      if (path === '/issues/5/runs') return { runs: [] }
      if (path === '/projects/9/runners') return { runners: [] }
      return {}
    })
    const { el, unmount } = mountPanel()
    await settle()
    expect(el.textContent).toContain('No runner is online')
    expect(el.textContent).toContain('No runs yet')
    unmount()
  })

  it("surfaces a runners-endpoint error distinctly from 'no runners' (M4)", async () => {
    vi.mocked(api.get).mockImplementation(async (path: string) => {
      if (path === '/issues/5/runs') return { runs: [] }
      if (path === '/projects/9/runners') throw new Error('boom')
      return {}
    })
    const { el, unmount } = mountPanel()
    await settle()
    expect(el.textContent).toContain("Couldn't check for runners")
    expect(el.textContent).not.toContain('No runner is online')
    unmount()
  })

  it('renders the device picker with >1 runner and posts the selected device (M5)', async () => {
    vi.mocked(api.get).mockImplementation(async (path: string) => {
      if (path === '/issues/5/runs') return { runs: [] }
      if (path === '/projects/9/runners')
        return {
          runners: [
            { user_id: 1, device_id: 'laptop', last_seen: '' },
            { user_id: 1, device_id: 'desktop', last_seen: '' },
          ],
        }
      return {}
    })
    vi.mocked(api.post).mockResolvedValue({})
    const { el, unmount } = mountPanel()
    await settle()

    const picker = el.querySelector<HTMLSelectElement>('.arp-device')
    expect(picker).toBeTruthy()
    expect(picker!.options.length).toBe(2)
    // Actually change the selection to prove v-model drives the payload (M1).
    picker!.value = 'desktop'
    picker!.dispatchEvent(new Event('change'))
    await settle()

    el.querySelector<HTMLButtonElement>('.btn-primary')!.click()
    await settle()
    expect(api.post).toHaveBeenCalledWith(
      '/issues/PAI-5/implement',
      expect.objectContaining({ device_id: 'desktop', action_key: 'claude_cli.implement' }),
    )
    unmount()
  })

  it('renders provider actions when multiple local CLI actions are available', async () => {
    vi.mocked(api.get).mockImplementation(async (path: string) => {
      if (path === '/issues/5/runs') return { runs: [] }
      if (path === '/projects/9/runners')
        return {
          runners: [
            {
              user_id: 1,
              device_id: 'laptop',
              last_seen: '',
              actions: [
                {
                  action_key: 'claude_cli.implement',
                  provider_kind: 'local_cli',
                  provider_id: 'claude_cli',
                  label: 'Claude Code',
                  run_modes: ['edit'],
                  can_test: true,
                  can_deploy: false,
                },
                {
                  action_key: 'codex_cli.implement',
                  provider_kind: 'local_cli',
                  provider_id: 'codex_cli',
                  label: 'Codex CLI',
                  run_modes: ['edit'],
                  can_test: true,
                  can_deploy: false,
                },
              ],
            },
          ],
        }
      return {}
    })
    vi.mocked(api.post).mockResolvedValue({ id: 33 })
    const { el, unmount } = mountPanel()
    await settle()

    const action = el.querySelector<HTMLSelectElement>('.arp-action')
    expect(action).toBeTruthy()
    expect(action!.options.length).toBe(2)
    action!.value = 'codex_cli.implement'
    action!.dispatchEvent(new Event('change'))
    await settle()

    expect(el.querySelector<HTMLButtonElement>('.btn-primary')?.textContent).toContain(
      'Do this with Codex CLI',
    )
    el.querySelector<HTMLButtonElement>('.btn-primary')!.click()
    await settle()
    expect(api.post).toHaveBeenCalledWith(
      '/issues/PAI-5/implement',
      expect.objectContaining({ device_id: 'laptop', action_key: 'codex_cli.implement' }),
    )
    expect(el.textContent).toContain('Run #33 queued with Codex CLI for laptop')
    unmount()
  })

  it('renders draft provider controls and posts draft options without runner fields', async () => {
    vi.mocked(api.get).mockImplementation(async (path: string) => {
      if (path === '/issues/5/runs') return { runs: [] }
      if (path === '/projects/9/runners') return { runners: [] }
      if (path === '/ai/execution-options?issue_id=5')
        return {
          profiles: [
            {
              id: 'balanced',
              label: 'Balanced',
              provider: 'openrouter',
              model: 'test/draft',
              effort: 'standard',
              speed_label: '',
              cost_label: '',
            },
            {
              id: 'deep',
              label: 'Deep',
              provider: 'openrouter',
              model: 'test/draft',
              effort: 'deep',
              speed_label: '',
              cost_label: '',
            },
          ],
          efforts: ['low', 'standard', 'deep'],
          action_defaults: {
            'openrouter_draft.implement': { profile_id: 'balanced', effort: 'standard' },
          },
          selector_defaults: {
            actions: {},
            runs: {
              'openrouter_draft.implement': {
                action_key: 'openrouter_draft.implement',
                profile_id: 'deep',
                profile_label: 'Deep',
                model: 'test/draft',
                effort: 'low',
                prompt_preset_ref: 'runbook/draft@rev1',
                context_pack: 'knowledge',
                provider_id: 'openrouter',
                provider_label: 'OpenRouter Draft',
                source: 'global',
              },
            },
            row_launch: {
              action_key: 'claude_cli.implement',
              profile_id: 'balanced',
              effort: 'standard',
              prompt_preset_ref: 'default',
              context_pack: 'issue',
              provider_id: 'claude_cli',
              provider_label: 'Claude Code',
              source: 'global',
            },
            workbench: {
              action_key: 'claude_cli.implement',
              profile_id: 'balanced',
              effort: 'standard',
              prompt_preset_ref: 'default',
              context_pack: 'issue',
              provider_id: 'claude_cli',
              provider_label: 'Claude Code',
              source: 'global',
            },
          },
          prompt_presets: [
            {
              ref: 'runbook/draft@rev1',
              label: 'Draft runbook',
              type: 'runbook',
              slug: 'draft',
              status: 'active',
              revision: 'rev1',
              actions: ['openrouter_draft.implement'],
            },
          ],
          knowledge_suggestions: [
            {
              ref: 'kb:runbook:draft',
              type: 'runbook',
              slug: 'draft',
              title: 'Draft Runbook',
              status: 'backlog',
              revision: 'rev1',
              suggested_use: 'prompt',
              prompt_preset: true,
              prompt_preset_ref: 'runbook/draft@rev1',
              prompt_preset_label: 'Draft runbook',
              prompt_preset_status: 'active',
              actions: ['openrouter_draft.implement'],
            },
            {
              ref: 'kb:guideline:review_scope',
              type: 'guideline',
              slug: 'review_scope',
              title: 'Review Scope',
              status: 'backlog',
              revision: 'rev2',
              suggested_use: 'context',
              prompt_preset: false,
            },
          ],
          context_packs: [
            { id: 'issue', label: 'Issue only' },
            { id: 'knowledge', label: 'Project knowledge' },
          ],
          run_providers: [
            {
              action_key: 'openrouter_draft.implement',
              provider_kind: 'hosted_model',
              provider_id: 'openrouter',
              label: 'OpenRouter Draft',
              run_modes: ['draft'],
              can_test: false,
              can_deploy: false,
              available: true,
              requires_runner: false,
              profile_ids: ['balanced', 'deep'],
              efforts: ['low', 'standard', 'deep'],
              models: ['test/draft'],
            },
          ],
        }
      return {}
    })
    vi.mocked(api.post).mockResolvedValue({ id: 77, status: 'drafted' })
    const { el, unmount } = mountPanel()
    await settle()

    const action = el.querySelector<HTMLSelectElement>('.arp-action')
    expect(action).toBeTruthy()
    action!.value = 'openrouter_draft.implement'
    action!.dispatchEvent(new Event('change'))
    await settle()

    const selects = Array.from(el.querySelectorAll<HTMLSelectElement>('.arp-draft-select'))
    expect(selects.length).toBe(4)
    expect(selects.map((select) => select.value)).toEqual([
      'deep',
      'low',
      'runbook/draft@rev1',
      'knowledge',
    ])
    expect(el.textContent).toContain('PPM knowledge')
    expect(el.textContent).toContain('Draft runbook')
    expect(el.textContent).toContain('Review Scope')
    expect(el.textContent).not.toContain('DO NOT RETURN')
    selects[0].value = 'deep'
    selects[0].dispatchEvent(new Event('change'))
    selects[1].value = 'low'
    selects[1].dispatchEvent(new Event('change'))
    selects[2].value = 'runbook/draft@rev1'
    selects[2].dispatchEvent(new Event('change'))
    selects[3].value = 'knowledge'
    selects[3].dispatchEvent(new Event('change'))
    await settle()

    el.querySelector<HTMLButtonElement>('.btn-primary')!.click()
    await settle()
    expect(api.post).toHaveBeenCalledWith('/issues/PAI-5/implement', {
      action_key: 'openrouter_draft.implement',
      options: {
        profile_id: 'deep',
        effort: 'low',
        prompt_preset_ref: 'runbook/draft@rev1',
        context_pack: 'knowledge',
      },
    })
    expect(el.textContent).toContain('Draft #77 ready with OpenRouter Draft')
    unmount()
  })

  it('hands off a drafted run to a trusted runner with the draft linked', async () => {
    vi.mocked(api.get).mockImplementation(async (path: string) => {
      if (path === '/issues/5/runs')
        return {
          runs: [
            run('drafted', {
              id: 77,
              action_key: 'openrouter_draft.implement',
              provider_label: 'OpenRouter Draft',
              run_mode: 'draft',
              profile_id: 'balanced',
              effort: 'standard',
              prompt_preset_ref: 'default',
              context_pack: 'knowledge',
              prompt_tokens: 10,
              completion_tokens: 5,
              tests_summary:
                'AI draft generated; no local tests were run and no deployment was attempted.',
            }),
          ],
        }
      if (path === '/projects/9/runners')
        return { runners: [{ user_id: 1, device_id: 'laptop', last_seen: '' }] }
      return {}
    })
    vi.mocked(api.post).mockResolvedValue({ id: 88, status: 'queued' })
    const { el, unmount } = mountPanel()
    await settle()

    expect(el.textContent).toContain('Draft only')
    expect(el.textContent).toContain('No local tests')
    expect(el.textContent).toContain('Not applied')
    el.querySelector<HTMLButtonElement>('.arp-draft-handoff')!.click()
    await settle()

    expect(api.post).toHaveBeenCalledWith('/issues/PAI-5/implement', {
      action_key: 'claude_cli.implement',
      device_id: 'laptop',
      source_draft_run_id: 77,
    })
    expect(el.textContent).toContain('Follow-up run #88 queued from draft #77')
    unmount()
  })

  it('loads project agents and posts the selected agent name', async () => {
    vi.mocked(api.get).mockImplementation(async (path: string) => {
      if (path === '/issues/5/runs') return { runs: [] }
      if (path === '/projects/9/runners')
        return { runners: [{ user_id: 1, device_id: 'laptop', last_seen: '' }] }
      if (path === '/projects/9/agents')
        return [
          {
            id: 1,
            project_id: 9,
            name: 'codex',
            description: '',
            slash_command_name: '',
            lane_tags: [],
            metadata: {},
            body: '',
            bootstrap_steps: [],
            non_negotiable_rules: [],
          },
          {
            id: 2,
            project_id: 9,
            name: 'docs',
            description: '',
            slash_command_name: '',
            lane_tags: [],
            metadata: {},
            body: '',
            bootstrap_steps: [],
            non_negotiable_rules: [],
          },
        ]
      return {}
    })
    vi.mocked(api.post).mockResolvedValue({ id: 44 })
    const { el, unmount } = mountPanel()
    await settle()

    const agent = el.querySelector<HTMLSelectElement>('.arp-agent')
    expect(agent).toBeTruthy()
    expect(agent!.options.length).toBe(3)
    agent!.value = 'docs'
    agent!.dispatchEvent(new Event('change'))
    await settle()

    el.querySelector<HTMLButtonElement>('.btn-primary')!.click()
    await settle()
    expect(api.post).toHaveBeenCalledWith(
      '/issues/PAI-5/implement',
      expect.objectContaining({
        device_id: 'laptop',
        action_key: 'claude_cli.implement',
        agent_name: 'docs',
      }),
    )
    expect(el.textContent).toContain('Run #44 queued as docs for laptop')
    unmount()
  })

  it('posts an explicit deploy target only when the user sets one', async () => {
    vi.mocked(api.get).mockImplementation(async (path: string) => {
      if (path === '/issues/5/runs') return { runs: [] }
      if (path === '/projects/9/runners')
        return { runners: [{ user_id: 1, device_id: 'laptop', last_seen: '' }] }
      return {}
    })
    vi.mocked(api.post).mockResolvedValue({ id: 22 })
    const { el, unmount } = mountPanel()
    await settle()

    const target = el.querySelector<HTMLSelectElement>('.arp-deploy-target')
    expect(target).toBeTruthy()
    target!.value = 'local-dev'
    target!.dispatchEvent(new Event('change'))
    await settle()
    expect(el.querySelector<HTMLButtonElement>('.btn-primary')?.textContent).toContain(
      'Implement + deploy',
    )

    el.querySelector<HTMLButtonElement>('.btn-primary')!.click()
    await settle()
    expect(api.post).toHaveBeenCalledWith(
      '/issues/PAI-5/implement',
      expect.objectContaining({
        device_id: 'laptop',
        action_key: 'claude_cli.implement',
        deploy_target: 'local-dev',
      }),
    )
    expect(el.textContent).toContain('Run #22 queued for laptop')
    unmount()
  })

  it('renders a timestamp as a valid ISO datetime + a local label (M2/M6)', async () => {
    vi.mocked(api.get).mockImplementation(async (path: string) => {
      if (path === '/issues/5/runs') return { runs: [run('deployed')] }
      if (path === '/projects/9/runners') return { runners: [] }
      return {}
    })
    const { el, unmount } = mountPanel()
    await settle()
    const t = el.querySelector('time')
    expect(t).toBeTruthy()
    const dt = t!.getAttribute('datetime')!
    expect(dt.endsWith('Z')).toBe(true) // UTC, not shifted to local
    expect(Number.isNaN(Date.parse(dt))).toBe(false)
    expect(t!.textContent).not.toContain('Invalid Date')
    expect(t!.textContent!.trim().length).toBeGreaterThan(0)
    unmount()
  })
})

describe('AgentRunPanel — polling lifecycle (H2)', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.mocked(api.get).mockReset()
    vi.mocked(api.post).mockReset()
  })
  afterEach(() => {
    vi.useRealTimers()
    document.body.innerHTML = ''
    vi.restoreAllMocks()
  })

  it('polls an in-flight run every 4s and stops once it reaches a result state', async () => {
    const statuses = ['queued', 'running', 'tests_passed']
    let runsCalls = 0
    vi.mocked(api.get).mockImplementation(async (path: string) => {
      if (path === '/issues/5/runs') {
        const s = statuses[Math.min(runsCalls, statuses.length - 1)]
        runsCalls += 1
        return { runs: [run(s)] }
      }
      if (path === '/projects/9/runners')
        return { runners: [{ user_id: 1, device_id: 'laptop', last_seen: '' }] }
      return {}
    })

    const { el, unmount } = mountPanel()
    await vi.advanceTimersByTimeAsync(0) // flush onMounted fetches
    expect(el.textContent).toContain('Queued')

    await vi.advanceTimersByTimeAsync(4000) // tick 1 → running
    expect(el.textContent).toContain('Running')

    await vi.advanceTimersByTimeAsync(4000) // tick 2 → tests_passed (finished → stop)
    expect(el.textContent).toContain('Tests passed')

    const callsAfterTerminal = runsCalls
    await vi.advanceTimersByTimeAsync(12000) // no further polling once finished
    expect(runsCalls).toBe(callsAfterTerminal)

    unmount()
  })
})

describe('AgentRunPanel — visibility + leak (H1/H2)', () => {
  let hidden = false
  beforeEach(() => {
    vi.useFakeTimers()
    vi.mocked(api.get).mockReset()
    vi.mocked(api.post).mockReset()
    hidden = false
    Object.defineProperty(document, 'hidden', { configurable: true, get: () => hidden })
  })
  afterEach(() => {
    vi.useRealTimers()
    document.body.innerHTML = ''
    vi.restoreAllMocks()
  })

  it('pauses polling while the tab is hidden and catches up on re-show (H2)', async () => {
    let runsCalls = 0
    vi.mocked(api.get).mockImplementation(async (path: string) => {
      if (path === '/issues/5/runs') {
        runsCalls += 1
        return { runs: [run('queued')] }
      }
      if (path === '/projects/9/runners') return { runners: [] }
      return {}
    })
    const { unmount } = mountPanel()
    await vi.advanceTimersByTimeAsync(0)
    const afterMount = runsCalls
    await vi.advanceTimersByTimeAsync(4000)
    expect(runsCalls).toBeGreaterThan(afterMount) // polling while visible

    hidden = true
    document.dispatchEvent(new Event('visibilitychange'))
    const afterHide = runsCalls
    await vi.advanceTimersByTimeAsync(12000)
    expect(runsCalls).toBe(afterHide) // paused while hidden

    hidden = false
    document.dispatchEvent(new Event('visibilitychange'))
    await vi.advanceTimersByTimeAsync(0)
    expect(runsCalls).toBeGreaterThan(afterHide) // caught up on re-show
    unmount()
  })

  it('leaves no polling timer after unmounting mid-fetch (H1)', async () => {
    let landRun: () => void = () => {}
    let runsCalls = 0
    vi.mocked(api.get).mockImplementation((path: string) => {
      if (path === '/issues/5/runs') {
        runsCalls += 1
        return new Promise((r) => {
          landRun = () => r({ runs: [run('queued')] })
        })
      }
      return Promise.resolve({ runners: [] })
    })
    const { unmount } = mountPanel() // onMounted → fetchRuns is pending
    await Promise.resolve()
    unmount() // unmount before the fetch resolves
    landRun() // the fetch now lands on a dead component
    await vi.advanceTimersByTimeAsync(0)
    const settled = runsCalls
    await vi.advanceTimersByTimeAsync(20000) // an orphan interval would poll here
    expect(runsCalls).toBe(settled) // no leak
  })
})
