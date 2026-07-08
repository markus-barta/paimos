/*
 * PAIMOS — Your Professional & Personal AI Project OS
 * Copyright (C) 2026 Markus Barta <markus@barta.com>
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as
 * published by the Free Software Foundation, version 3.
 */

import { computed, nextTick, ref } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/api/client'
import { listProjectAgents } from '@/services/projectAgents'
import { useIssueListRunActions } from './useIssueListRunActions'
import type { AgentActionCapability, ProjectAgent } from '@/types'

vi.mock('@/api/client', () => ({
  api: { get: vi.fn() },
}))

vi.mock('@/services/projectAgents', () => ({
  listProjectAgents: vi.fn(),
}))

async function settle() {
  for (let i = 0; i < 5; i += 1) {
    await Promise.resolve()
    await nextTick()
  }
}

function action(actionKey: string, extra: Partial<AgentActionCapability> = {}): AgentActionCapability {
  return {
    action_key: actionKey,
    provider_kind: 'local_cli',
    provider_id: actionKey,
    label: actionKey,
    can_test: true,
    can_deploy: true,
    ...extra,
  }
}

function agent(name: string): ProjectAgent {
  return {
    id: 1,
    project_id: 7,
    name,
    description: '',
    slash_command_name: '',
    lane_tags: [],
    metadata: {},
    body: '',
    bootstrap_steps: [],
    non_negotiable_rules: [],
  }
}

describe('useIssueListRunActions', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('merges runners with draft providers, filters unavailable actions, and applies row defaults', async () => {
    vi.mocked(api.get).mockImplementation(async (path: string) => {
      if (path === '/projects/7/runners') {
        return {
          runners: [
            {
              actions: [
                action('claude_cli.implement'),
                action('offline.runner', { available: false }),
              ],
            },
          ],
        } as never
      }
      if (path === '/ai/execution-options?project_id=7') {
        return {
          selector_defaults: {
            row_launch: { action_key: 'openrouter_draft.implement', agent_name: 'ops' },
          },
          run_providers: [
            action('openrouter_draft.implement', { provider_kind: 'hosted_model' }),
            action('claude_cli.implement', { provider_kind: 'local_cli' }),
          ],
        } as never
      }
      return {} as never
    })
    vi.mocked(listProjectAgents).mockResolvedValue([agent('ops'), agent('qa')])

    const projectId = ref<number | undefined>(7)
    const disabled = ref(false)
    const state = useIssueListRunActions({
      projectId: computed(() => projectId.value),
      disabled: computed(() => disabled.value),
    })
    await settle()

    expect(state.agentActions.value.map((a) => a.action_key)).toEqual([
      'openrouter_draft.implement',
      'claude_cli.implement',
    ])
    expect(state.projectAgents.value.map((a) => a.name)).toEqual(['ops', 'qa'])
    expect(state.selectedAgentName.value).toBe('ops')
  })

  it('keeps draft provider actions usable when runner discovery fails', async () => {
    vi.mocked(api.get).mockImplementation(async (path: string) => {
      if (path === '/projects/7/runners') throw new Error('runner endpoint down')
      if (path === '/ai/execution-options?project_id=7') {
        return {
          selector_defaults: { row_launch: { action_key: 'openrouter_draft.implement' } },
          run_providers: [action('openrouter_draft.implement', { provider_kind: 'hosted_model' })],
        } as never
      }
      return {} as never
    })
    vi.mocked(listProjectAgents).mockResolvedValue([agent('solo')])

    const state = useIssueListRunActions({
      projectId: computed(() => 7),
      disabled: computed(() => false),
    })
    await settle()

    expect(state.agentActions.value.map((a) => a.action_key)).toEqual(['openrouter_draft.implement'])
    expect(state.selectedAgentName.value).toBe('solo')
  })

  it('resets actions and agent selection while disabled', async () => {
    vi.mocked(api.get).mockResolvedValue({
      selector_defaults: {},
      run_providers: [action('openrouter_draft.implement')],
    } as never)
    vi.mocked(listProjectAgents).mockResolvedValue([agent('ops')])

    const projectId = ref<number | undefined>(7)
    const disabled = ref(false)
    const state = useIssueListRunActions({
      projectId: computed(() => projectId.value),
      disabled: computed(() => disabled.value),
    })
    await settle()
    expect(state.projectAgents.value).toHaveLength(1)

    disabled.value = true
    await settle()

    expect(state.agentActions.value).toEqual([])
    expect(state.projectAgents.value).toEqual([])
    expect(state.selectedAgentName.value).toBe('')
  })
})
