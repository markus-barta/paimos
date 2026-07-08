/*
 * PAIMOS — Your Professional & Personal AI Project OS
 * Copyright (C) 2026 Markus Barta <markus@barta.com>
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as
 * published by the Free Software Foundation, version 3.
 */

import { ref, watch } from 'vue'
import type { ComputedRef } from 'vue'
import { api } from '@/api/client'
import { listProjectAgents } from '@/services/projectAgents'
import type { AgentActionCapability, ProjectAgent } from '@/types'
import type { AiExecutionOptionsCatalog, AiSelectorDefault } from '@/composables/useAiAction'

interface UseIssueListRunActionsOptions {
  projectId: ComputedRef<number | undefined>
  disabled: ComputedRef<boolean>
}

function actionAvailable(action: AgentActionCapability): boolean {
  return action.available !== false && !action.unavailable_reason
}

/**
 * PAI-672. IssueList boundary: row-level agent/run quick actions.
 *
 * IssueList renders the toolbar and table. This composable owns the transport
 * and selection policy for "run this row with an agent": project runners,
 * draft-provider actions, project-agent inventory, and row-launch defaults.
 */
export function useIssueListRunActions(options: UseIssueListRunActionsOptions) {
  const agentActions = ref<AgentActionCapability[]>([])
  const projectAgents = ref<ProjectAgent[]>([])
  const selectedAgentName = ref('')
  const rowLaunchDefault = ref<AiSelectorDefault | null>(null)

  function resetActions() {
    agentActions.value = []
    rowLaunchDefault.value = null
  }

  function resetAgents() {
    projectAgents.value = []
    selectedAgentName.value = ''
  }

  function syncSelectedAgentName() {
    if (!projectAgents.value.length) {
      selectedAgentName.value = ''
      return
    }
    if (
      selectedAgentName.value &&
      !projectAgents.value.some((agent) => agent.name === selectedAgentName.value)
    ) {
      selectedAgentName.value = ''
    }
    const defaultAgent = rowLaunchDefault.value?.agent_name ?? ''
    if (
      !selectedAgentName.value &&
      defaultAgent &&
      projectAgents.value.some((agent) => agent.name === defaultAgent)
    ) {
      selectedAgentName.value = defaultAgent
      return
    }
    if (!selectedAgentName.value && projectAgents.value.length === 1) {
      selectedAgentName.value = projectAgents.value[0].name
    }
  }

  async function loadAgentActions() {
    if (options.disabled.value || options.projectId.value === undefined) {
      resetActions()
      return
    }

    const projectId = options.projectId.value
    const byKey = new Map<string, AgentActionCapability>()
    try {
      const data = await api.get<{ runners: { actions?: AgentActionCapability[] }[] }>(
        `/projects/${projectId}/runners`,
      )
      for (const runner of data.runners ?? []) {
        for (const action of runner.actions ?? []) {
          if (!byKey.has(action.action_key) && actionAvailable(action)) byKey.set(action.action_key, action)
        }
      }
    } catch {
      // Keep draft providers below usable even when the runner endpoint flakes.
    }

    try {
      const data = await api.get<AiExecutionOptionsCatalog>(
        `/ai/execution-options?project_id=${projectId}`,
      )
      rowLaunchDefault.value = data.selector_defaults?.row_launch ?? null
      for (const action of data.run_providers ?? []) {
        if (!byKey.has(action.action_key) && actionAvailable(action)) byKey.set(action.action_key, action)
      }
      syncSelectedAgentName()
    } catch {
      rowLaunchDefault.value = null
      // Draft provider availability is optional for row-level quick actions.
    }

    const defaultActionKey = rowLaunchDefault.value?.action_key ?? ''
    agentActions.value = [...byKey.values()].sort((a, b) => {
      if (!defaultActionKey) return 0
      if (a.action_key === defaultActionKey) return -1
      if (b.action_key === defaultActionKey) return 1
      return 0
    })
  }

  async function loadProjectAgentsForRuns() {
    if (options.disabled.value || options.projectId.value === undefined) {
      resetAgents()
      return
    }

    try {
      const data = await listProjectAgents(options.projectId.value)
      projectAgents.value = Array.isArray(data) ? data : []
      syncSelectedAgentName()
    } catch {
      resetAgents()
    }
  }

  watch([options.projectId, options.disabled], () => {
    void loadAgentActions()
    void loadProjectAgentsForRuns()
  }, { immediate: true })

  return {
    agentActions,
    projectAgents,
    selectedAgentName,
    rowLaunchDefault,
    loadAgentActions,
    loadProjectAgentsForRuns,
    syncSelectedAgentName,
  }
}
