<script setup lang="ts">
/*
 * PAIMOS — Your Professional & Personal AI Project OS
 * Copyright (C) 2026 Markus Barta <markus@barta.com>
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as
 * published by the Free Software Foundation, version 3.
 */

// PAI-610 (epic PAI-605): the "Implement this" button + live run-status card.
// Clicking the button creates a queued agent run (PAI-606); the developer's
// local runner (PAI-608) picks it up over SSE and reports progress back, which
// this panel surfaces by polling while a run is in flight.
import { ref, computed, onMounted, onUnmounted } from 'vue'
import AppIcon from '@/components/AppIcon.vue'
import { api, errMsg } from '@/api/client'
import { listProjectAgents } from '@/services/projectAgents'
import type { AgentActionCapability, ProjectAgent } from '@/types'
import type {
  AiActionOptions,
  AiExecutionOptionsCatalog,
  AiKnowledgeSuggestion,
  AiSelectorDefault,
} from '@/composables/useAiAction'

const props = withDefaults(defineProps<{
  issueId: number
  issueKey: string
  projectId: number
  canEdit?: boolean
}>(), {
  canEdit: true,
})

interface AgentRun {
  id: number
  status: string
  version: string
  device_id: string
  action_key: string
  provider_label: string
  run_mode: string
  profile_id: string
  effort: string
  prompt_preset_ref: string
  context_pack: string
  context_truncated?: boolean
  context_sources_json?: string
  prompt_tokens?: number
  completion_tokens?: number
  finish_reason?: string
  agent_name: string
  deploy_target: string
  tests_summary: string | null
  error: string
  created_at: string
  started_at: string | null
  finished_at: string | null
  source_draft_run_id?: number
  followup_run_id?: number
}

interface ProjectRunner {
  user_id: number
  device_id: string
  last_seen: string
  actions?: AgentActionCapability[]
}

const runs = ref<AgentRun[]>([])
const runners = ref<ProjectRunner[]>([])
const executionOptions = ref<AiExecutionOptionsCatalog | null>(null)
const projectAgents = ref<ProjectAgent[]>([])
const selectedActionKey = ref('claude_cli.implement')
const selectedDevice = ref('')
const selectedAgentName = ref('')
const deployTarget = ref('')
const selectedProfileId = ref('')
const selectedEffort = ref('')
const selectedPromptPresetRef = ref('')
const selectedContextPack = ref('')
const loading = ref(true)
const busy = ref(false)
const error = ref('')
const notice = ref('')
const runnersError = ref('') // distinct from "no runners online" (M4)
const executionOptionsError = ref('')
const agentsError = ref('')
const canStartRun = computed(() => props.canEdit !== false)

const IN_FLIGHT = new Set(['queued', 'running'])
const hasActiveRun = computed(() => runs.value.some((r) => IN_FLIGHT.has(r.status)))

let pollTimer: ReturnType<typeof setInterval> | null = null
// Monotonic tokens so an out-of-order response can't overwrite newer state (M2).
let runsSeq = 0
let runnersSeq = 0
let executionOptionsSeq = 0
let agentsSeq = 0
// Guards against a fetch that resolves AFTER unmount re-arming the poll timer (H1).
let alive = true

const STATUS_LABEL: Record<string, string> = {
  queued: 'Queued',
  running: 'Running',
  tests_passed: 'Tests passed',
  tests_failed: 'Tests failed',
  deployed: 'Deployed',
  drafted: 'Draft ready',
  failed: 'Failed',
  cancelled: 'Cancelled',
}

function statusLabel(s: string): string {
  return STATUS_LABEL[s] ?? s
}

const DEPLOY_TARGETS = [
  { value: '', label: 'No deploy' },
  { value: 'local-dev', label: 'local-dev' },
]

const DEFAULT_ACTION: AgentActionCapability = {
  action_key: 'claude_cli.implement',
  provider_kind: 'local_cli',
  provider_id: 'claude_cli',
  label: 'Claude Code',
  run_modes: ['edit'],
  can_test: true,
  can_deploy: false,
  available: true,
  requires_runner: true,
}

function runnerActions(runner: ProjectRunner): AgentActionCapability[] {
  return runner.actions?.length ? runner.actions : [DEFAULT_ACTION]
}

function actionAvailable(action: AgentActionCapability): boolean {
  return action.available !== false && !action.unavailable_reason
}

function actionRequiresRunner(action: AgentActionCapability | undefined): boolean {
  return action?.requires_runner ?? action?.provider_kind === 'local_cli'
}

function actionIsDraft(action: AgentActionCapability | undefined): boolean {
  return !!action && (action.run_modes?.includes('draft') || !actionRequiresRunner(action))
}

const draftProviderActions = computed(() => executionOptions.value?.run_providers ?? [])
const availableDraftProviderActions = computed(() =>
  draftProviderActions.value.filter(actionAvailable),
)
const unavailableDraftProviderActions = computed(() =>
  draftProviderActions.value.filter((action) => !actionAvailable(action)),
)

const availableActions = computed(() => {
  const byKey = new Map<string, AgentActionCapability>()
  let hasRunnerBackedAction = false
  for (const runner of runners.value) {
    for (const action of runnerActions(runner)) {
      if (!byKey.has(action.action_key) && actionAvailable(action)) {
        byKey.set(action.action_key, action)
        if (actionRequiresRunner(action)) hasRunnerBackedAction = true
      }
    }
  }
  if (!hasRunnerBackedAction) byKey.set(DEFAULT_ACTION.action_key, DEFAULT_ACTION)
  for (const action of availableDraftProviderActions.value) {
    if (!byKey.has(action.action_key)) byKey.set(action.action_key, action)
  }
  return [...byKey.values()]
})

const selectedAction = computed(
  () =>
    availableActions.value.find((a) => a.action_key === selectedActionKey.value) ??
    availableActions.value[0],
)
const workbenchSelectorDefault = computed(
  () => executionOptions.value?.selector_defaults?.workbench ?? null,
)

function runnerSupportsAction(runner: ProjectRunner, actionKey: string): boolean {
  return runnerActions(runner).some((action) => action.action_key === actionKey)
}

const actionRunners = computed(() =>
  selectedActionRequiresRunner.value
    ? runners.value.filter((runner) => runnerSupportsAction(runner, selectedActionKey.value))
    : [],
)

const selectedProjectAgent = computed(
  () => projectAgents.value.find((agent) => agent.name === selectedAgentName.value) ?? null,
)

const selectedActionRequiresRunner = computed(() => actionRequiresRunner(selectedAction.value))
const selectedActionIsDraft = computed(() => actionIsDraft(selectedAction.value))
const selectedActionCanDeploy = computed(() => selectedActionRequiresRunner.value)
const onlineAdapterLabels = computed(() => {
  const labels = new Set<string>()
  for (const runner of runners.value) {
    for (const action of runnerActions(runner)) {
      if (actionRequiresRunner(action) && actionAvailable(action)) labels.add(action.label)
    }
  }
  return [...labels]
})

const profileChoices = computed(() => {
  const ids = selectedAction.value?.profile_ids
  const profiles = executionOptions.value?.profiles ?? []
  if (!ids?.length) return profiles
  const allowed = new Set(ids)
  return profiles.filter((profile) => allowed.has(profile.id))
})

const effortChoices = computed(() => {
  const efforts = selectedAction.value?.efforts?.length
    ? selectedAction.value.efforts
    : (executionOptions.value?.efforts ?? [])
  return efforts
})

const promptPresetChoices = computed(() => [
  { ref: 'default', label: 'Default prompt' },
  ...(executionOptions.value?.prompt_presets ?? []).map((preset) => ({
    ref: preset.ref,
    label: preset.label,
  })),
])

const contextPackChoices = computed(() => executionOptions.value?.context_packs ?? [])
const knowledgeSuggestions = computed(() => executionOptions.value?.knowledge_suggestions ?? [])
const hasKnowledgeContextPack = computed(() =>
  contextPackChoices.value.some((pack) => pack.id === 'knowledge'),
)
const selectedDefaultSourceLabel = computed(() => {
  const source = selectedActionIsDraft.value
    ? runSelectorDefault(selectedActionKey.value)?.source
    : workbenchSelectorDefault.value?.source
  return source === 'project' ? 'Project defaults' : 'Inherited defaults'
})

function suggestionAppliesToAction(suggestion: AiKnowledgeSuggestion): boolean {
  const actions = suggestion.actions ?? []
  return !actions.length || actions.includes('*') || actions.includes(selectedActionKey.value)
}

function canUseKnowledgePrompt(suggestion: AiKnowledgeSuggestion): boolean {
  return !!(
    suggestion.prompt_preset &&
    suggestion.suggested_use === 'prompt' &&
    suggestion.prompt_preset_ref &&
    promptPresetChoices.value.some((preset) => preset.ref === suggestion.prompt_preset_ref)
  )
}

const visibleKnowledgeSuggestions = computed(() =>
  knowledgeSuggestions.value
    .filter((suggestion) => !suggestion.prompt_preset || suggestionAppliesToAction(suggestion))
    .slice(0, 6),
)

function useKnowledgeSuggestion(suggestion: AiKnowledgeSuggestion) {
  if (!canStartRun.value) return
  if (canUseKnowledgePrompt(suggestion) && suggestion.prompt_preset_ref) {
    selectedPromptPresetRef.value = suggestion.prompt_preset_ref
    return
  }
  if (hasKnowledgeContextPack.value) selectedContextPack.value = 'knowledge'
}

function knowledgeSuggestionTitle(suggestion: AiKnowledgeSuggestion): string {
  return suggestion.prompt_preset_label || suggestion.title
}

function knowledgeSuggestionMeta(suggestion: AiKnowledgeSuggestion): string {
  const parts = [suggestion.type, suggestion.slug]
  if (suggestion.revision) parts.push(suggestion.revision)
  if (suggestion.prompt_preset_status && suggestion.prompt_preset_status !== 'active') {
    parts.push(suggestion.prompt_preset_status)
  }
  return parts.filter(Boolean).join(' · ')
}

function agentArtifactStatus(agent: ProjectAgent): string {
  return agent.body || agent.bootstrap_steps?.length || agent.non_negotiable_rules?.length
    ? 'Artifact ready'
    : 'Artifact shell'
}

const selectedAgentReadiness = computed(() => {
  const agent = selectedProjectAgent.value
  if (!agent) return []
  return [
    agent.name,
    agentArtifactStatus(agent),
    onlineAdapterLabels.value.length ? onlineAdapterLabels.value.join(', ') : 'No runner online',
  ]
})

function selectedAgentCommands(agent: ProjectAgent): string[] {
  return [
    `paimos skill render ${agent.name}`,
    `paimos run-agent watch --project ${props.projectId} --repo-root .`,
  ]
}

const selectedDraftProviderMeta = computed(() => {
  const action = selectedAction.value
  if (!action) return ''
  const parts: string[] = []
  if (action.models?.length) parts.push(action.models[0])
  if (action.endpoint_label) parts.push(action.endpoint_label)
  return parts.join(' · ')
})
const selectedActionModels = computed(() => selectedAction.value?.models?.join(', ') ?? '')
const selectedPromptPresetLabel = computed(
  () =>
    promptPresetChoices.value.find((preset) => preset.ref === selectedPromptPresetRef.value)?.label ??
    selectedPromptPresetRef.value,
)
const selectedContextPackLabel = computed(
  () =>
    contextPackChoices.value.find((pack) => pack.id === selectedContextPack.value)?.label ??
    selectedContextPack.value,
)
const selectorSummary = computed(() => {
  const parts = [
    selectedDefaultSourceLabel.value,
    selectedAction.value?.label,
    selectedActionModels.value ? `Model ${selectedActionModels.value}` : selectedDraftProviderMeta.value,
    selectedActionIsDraft.value && selectedProfileId.value ? `Profile ${selectedProfileId.value}` : '',
    selectedActionIsDraft.value && selectedEffort.value ? `Effort ${selectedEffort.value}` : '',
    selectedActionIsDraft.value && selectedPromptPresetLabel.value ? `Prompt ${selectedPromptPresetLabel.value}` : '',
    selectedActionIsDraft.value && selectedContextPackLabel.value ? `Context ${selectedContextPackLabel.value}` : '',
    selectedProjectAgent.value ? `Agent ${selectedProjectAgent.value.name}` : '',
  ]
  return parts.filter(Boolean)
})

function syncActionSelection() {
  if (!availableActions.value.some((action) => action.action_key === selectedActionKey.value)) {
    const defaultActionKey = workbenchSelectorDefault.value?.action_key ?? DEFAULT_ACTION.action_key
    selectedActionKey.value = availableActions.value.some(
      (action) => action.action_key === defaultActionKey,
    )
      ? defaultActionKey
      : (availableActions.value[0]?.action_key ?? DEFAULT_ACTION.action_key)
  }
  if (!selectedActionCanDeploy.value) deployTarget.value = ''
  syncDraftOptions()
}

function syncDeviceSelection() {
  if (!selectedActionRequiresRunner.value) {
    selectedDevice.value = ''
    return
  }
  if (!actionRunners.value.length) {
    selectedDevice.value = ''
    return
  }
  if (
    !selectedDevice.value ||
    !actionRunners.value.some((runner) => runner.device_id === selectedDevice.value)
  ) {
    selectedDevice.value = actionRunners.value[0].device_id
  }
}

function syncAgentSelection() {
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
  const defaultAgent = workbenchSelectorDefault.value?.agent_name ?? ''
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

function runSelectorDefault(actionKey: string): AiSelectorDefault | null {
  return executionOptions.value?.selector_defaults?.runs?.[actionKey] ?? null
}

function syncDraftOptions() {
  if (!selectedActionIsDraft.value || !executionOptions.value) return
  const defaults = runSelectorDefault(selectedActionKey.value)
  const legacyDefaults = executionOptions.value.action_defaults?.[selectedActionKey.value]
  const profiles = profileChoices.value
  if (
    !selectedProfileId.value ||
    !profiles.some((profile) => profile.id === selectedProfileId.value)
  ) {
    const profileID = defaults?.profile_id || legacyDefaults?.profile_id
    selectedProfileId.value =
      (profileID && profiles.some((profile) => profile.id === profileID)
        ? profileID
        : profiles[0]?.id) ?? ''
  }
  const efforts = effortChoices.value
  if (!selectedEffort.value || !efforts.includes(selectedEffort.value)) {
    const effort = defaults?.effort || legacyDefaults?.effort
    selectedEffort.value = (effort && efforts.includes(effort) ? effort : efforts[0]) ?? ''
  }
  const presets = promptPresetChoices.value
  if (
    !selectedPromptPresetRef.value ||
    !presets.some((preset) => preset.ref === selectedPromptPresetRef.value)
  ) {
    const prompt = defaults?.prompt_preset_ref || 'default'
    selectedPromptPresetRef.value = presets.some((preset) => preset.ref === prompt)
      ? prompt
      : 'default'
  }
  const packs = contextPackChoices.value
  if (!selectedContextPack.value || !packs.some((pack) => pack.id === selectedContextPack.value)) {
    const pack = defaults?.context_pack || 'issue'
    selectedContextPack.value = packs.some((candidate) => candidate.id === pack)
      ? pack
      : packs.some((candidate) => candidate.id === 'issue')
        ? 'issue'
        : (packs[0]?.id ?? 'issue')
  }
}

function onActionChange() {
  syncDeviceSelection()
  syncDraftOptions()
  if (!selectedActionCanDeploy.value) deployTarget.value = ''
}

const buttonLabel = computed(() => {
  if (busy.value) return selectedActionIsDraft.value ? 'Drafting...' : 'Starting...'
  if (selectedActionIsDraft.value) return `Draft with ${selectedAction.value.label}`
  if (availableActions.value.length > 1) {
    return deployTarget.value
      ? `Do this with ${selectedAction.value.label} + deploy`
      : `Do this with ${selectedAction.value.label}`
  }
  return deployTarget.value ? 'Implement + deploy' : 'Implement this'
})

// The API emits SQLite's "YYYY-MM-DD HH:MM:SS" (UTC). Parse it to a real Date
// for a localized display string and a valid ISO `datetime` attribute (M6).
function toDate(ts: string | null): Date | null {
  if (!ts) return null
  let s = ts.trim().replace(' ', 'T')
  // Treat a zone-less timestamp as UTC (the API emits UTC). A present zone is a
  // trailing Z or ±HH:MM — only append Z when neither is there (L1).
  if (!/[Zz]$|[+-]\d{2}:?\d{2}$/.test(s)) s += 'Z'
  const d = new Date(s)
  return Number.isNaN(d.getTime()) ? null : d
}
function isoAttr(ts: string): string {
  return toDate(ts)?.toISOString() ?? ts
}
function localTime(ts: string): string {
  return toDate(ts)?.toLocaleString() ?? ts
}
function runTimestamp(run: AgentRun): string {
  return run.finished_at || run.started_at || run.created_at
}

type StageState = 'pending' | 'active' | 'complete' | 'failed'
interface RunStage {
  key: string
  label: string
  state: StageState
}

function isFinished(run: AgentRun): boolean {
  return ['tests_passed', 'tests_failed', 'deployed', 'drafted', 'failed', 'cancelled'].includes(
    run.status,
  )
}

function runStages(run: AgentRun): RunStage[] {
  const isDraft = run.run_mode === 'draft' || run.status === 'drafted'
  if (isDraft) {
    const isRunning = run.status === 'running'
    const isDrafted = run.status === 'drafted'
    const isFailed = run.status === 'failed' || run.status === 'cancelled'
    return [
      { key: 'requested', label: 'Requested', state: 'complete' },
      {
        key: 'drafting',
        label: 'Drafting',
        state: isRunning ? 'active' : isFailed ? 'failed' : 'complete',
      },
      {
        key: 'drafted',
        label: isDrafted ? 'Draft ready' : 'Draft',
        state: isDrafted ? 'complete' : isFailed ? 'failed' : 'pending',
      },
    ]
  }
  const isQueued = run.status === 'queued'
  const isRunning = run.status === 'running'
  const isDeployed = run.status === 'deployed'
  const isTestsPassed = run.status === 'tests_passed' || isDeployed
  const isTestsFailed = run.status === 'tests_failed'
  const isFailed = run.status === 'failed' || run.status === 'cancelled'
  const started = !!run.started_at || isRunning || isFinished(run)
  const hasDeploy = !!run.deploy_target

  return [
    { key: 'queued', label: 'Queued', state: isQueued ? 'active' : 'complete' },
    { key: 'claimed', label: 'Claimed', state: started ? 'complete' : 'pending' },
    {
      key: 'editing',
      label: 'Editing',
      state: isRunning ? 'active' : started ? (isFailed ? 'failed' : 'complete') : 'pending',
    },
    {
      key: 'tests',
      label: isTestsPassed ? 'Tests passed' : isTestsFailed ? 'Tests failed' : 'Tests',
      state: isTestsPassed
        ? 'complete'
        : isTestsFailed
          ? 'failed'
          : started && !isFailed
            ? 'active'
            : 'pending',
    },
    {
      key: hasDeploy ? 'deploy' : 'report',
      label: hasDeploy ? (isDeployed ? 'Deployed' : 'Deploy') : 'Reported',
      state: hasDeploy
        ? isDeployed
          ? 'complete'
          : isTestsFailed || isFailed
            ? 'failed'
            : isTestsPassed
              ? 'active'
              : 'pending'
        : isTestsPassed
          ? 'complete'
          : isTestsFailed || isFailed
            ? 'failed'
            : 'pending',
    },
  ]
}

function trustedRunnerActionForDraft(): AgentActionCapability {
  if (selectedAction.value && actionRequiresRunner(selectedAction.value))
    return selectedAction.value
  return availableActions.value.find((action) => actionRequiresRunner(action)) ?? DEFAULT_ACTION
}

function firstRunnerForAction(actionKey: string): ProjectRunner | null {
  return runners.value.find((runner) => runnerSupportsAction(runner, actionKey)) ?? null
}

function draftReviewMeta(run: AgentRun): string[] {
  const parts = ['Draft only', 'No local tests', 'Not applied']
  if (run.profile_id && run.effort) parts.push(`${run.profile_id}/${run.effort}`)
  if (run.prompt_preset_ref) parts.push(run.prompt_preset_ref)
  if (run.context_pack) parts.push(run.context_pack)
  if (run.context_truncated) parts.push('context truncated')
  const tokens = (run.prompt_tokens ?? 0) + (run.completion_tokens ?? 0)
  if (tokens > 0) parts.push(`${tokens} tokens`)
  return parts
}

async function handoffDraft(run: AgentRun) {
  if (!canStartRun.value) return
  const action = trustedRunnerActionForDraft()
  busy.value = true
  error.value = ''
  notice.value = ''
  try {
    const payload: {
      action_key: string
      device_id?: string
      agent_name?: string
      source_draft_run_id: number
    } = {
      action_key: action.action_key,
      source_draft_run_id: run.id,
    }
    if (actionRequiresRunner(action)) {
      const selectedRunner = runners.value.find(
        (runner) =>
          runner.device_id === selectedDevice.value &&
          runnerSupportsAction(runner, action.action_key),
      )
      const runner = selectedRunner ?? firstRunnerForAction(action.action_key)
      if (runner?.device_id) payload.device_id = runner.device_id
    }
    const agentName = (run.agent_name || selectedAgentName.value).trim()
    if (agentName) payload.agent_name = agentName
    const followup = await api.post<AgentRun>(`/issues/${props.issueKey}/implement`, payload)
    notice.value = followup?.id
      ? `Follow-up run #${followup.id} queued from draft #${run.id}`
      : `Follow-up run queued from draft #${run.id}`
    await Promise.all([fetchRuns(), fetchRunners()])
  } catch (e: unknown) {
    error.value = errMsg(e, 'Could not hand off the draft.')
  } finally {
    busy.value = false
  }
}

async function fetchRuns() {
  const seq = ++runsSeq
  try {
    const data = await api.get<{ runs: AgentRun[] }>(`/issues/${props.issueId}/runs`)
    if (!alive || seq !== runsSeq) return // unmounted, or a newer fetch landed
    runs.value = data.runs ?? []
    error.value = ''
  } catch (e: unknown) {
    if (!alive || seq !== runsSeq) return
    error.value = errMsg(e, 'Could not load runs.')
  } finally {
    if (alive) loading.value = false
  }
  syncPolling()
}

async function fetchAgents() {
  const seq = ++agentsSeq
  try {
    const data = await listProjectAgents(props.projectId)
    if (!alive || seq !== agentsSeq) return
    projectAgents.value = Array.isArray(data) ? data : []
    agentsError.value = ''
    syncAgentSelection()
  } catch (e: unknown) {
    if (!alive || seq !== agentsSeq) return
    projectAgents.value = []
    selectedAgentName.value = ''
    agentsError.value = errMsg(e, 'Could not load project agents.')
  }
}

async function fetchExecutionOptions() {
  const seq = ++executionOptionsSeq
  try {
    const data = await api.get<AiExecutionOptionsCatalog>(
      `/ai/execution-options?issue_id=${props.issueId}`,
    )
    if (!alive || seq !== executionOptionsSeq) return
    executionOptions.value = data
    executionOptionsError.value = ''
    syncActionSelection()
    syncAgentSelection()
    syncDeviceSelection()
    syncDraftOptions()
  } catch (e: unknown) {
    if (!alive || seq !== executionOptionsSeq) return
    executionOptions.value = null
    executionOptionsError.value = errMsg(e, 'Could not load AI execution options.')
  }
}

async function fetchRunners() {
  const seq = ++runnersSeq
  try {
    const data = await api.get<{ runners: ProjectRunner[] }>(`/projects/${props.projectId}/runners`)
    if (!alive || seq !== runnersSeq) return
    runners.value = data.runners ?? []
    runnersError.value = ''
    syncActionSelection()
    syncDeviceSelection()
  } catch (e: unknown) {
    if (!alive || seq !== runnersSeq) return
    runners.value = []
    runnersError.value = errMsg(e, 'Could not load runners.') // M4: don't masquerade as "none online"
  }
}

async function implement() {
  if (!canStartRun.value) return
  busy.value = true
  error.value = ''
  notice.value = ''
  try {
    const payload: {
      device_id?: string
      action_key: string
      deploy_target?: string
      agent_name?: string
      options?: AiActionOptions
    } = {
      action_key: selectedActionKey.value,
    }
    if (selectedActionRequiresRunner.value) payload.device_id = selectedDevice.value
    const target = deployTarget.value.trim()
    if (target && selectedActionCanDeploy.value) payload.deploy_target = target
    const agentName = selectedAgentName.value.trim()
    if (agentName) payload.agent_name = agentName
    if (selectedActionIsDraft.value) {
      payload.options = {
        profile_id: selectedProfileId.value,
        effort: selectedEffort.value,
        prompt_preset_ref: selectedPromptPresetRef.value,
        context_pack: selectedContextPack.value,
      }
    }
    const run = await api.post<AgentRun>(`/issues/${props.issueKey}/implement`, payload)
    const actor = availableActions.value.length > 1 ? ` with ${selectedAction.value.label}` : ''
    const agent = selectedProjectAgent.value ? ` as ${selectedProjectAgent.value.name}` : ''
    const runner =
      selectedActionRequiresRunner.value && selectedDevice.value
        ? ` for ${selectedDevice.value}`
        : ''
    if (run?.status === 'drafted') {
      notice.value = run?.id
        ? `Draft #${run.id} ready${actor}${agent}`
        : `Draft ready${actor}${agent}`
    } else {
      notice.value = run?.id
        ? `Run #${run.id} queued${actor}${agent}${runner}`
        : `Run queued${actor}${agent}${runner}`
    }
    await Promise.all([fetchRuns(), fetchRunners()]) // M5: refresh the picker too
  } catch (e: unknown) {
    error.value = errMsg(e, 'Could not start the run.')
  } finally {
    busy.value = false
  }
}

// Poll only while a run is in flight AND the tab is visible — a backgrounded
// tab with a stuck queued run must not heartbeat forever (M1). Each tick also
// refreshes runners so one that connects after load appears in the picker (M5).
function pollTick() {
  void fetchRuns()
  void fetchRunners()
}
function syncPolling() {
  if (!alive) {
    if (pollTimer) {
      clearInterval(pollTimer)
      pollTimer = null
    }
    return
  }
  const shouldPoll = hasActiveRun.value && !document.hidden
  if (shouldPoll && !pollTimer) {
    pollTimer = setInterval(pollTick, 4000)
  } else if (!shouldPoll && pollTimer) {
    clearInterval(pollTimer)
    pollTimer = null
  }
}

function onVisibility() {
  if (!document.hidden && hasActiveRun.value) void fetchRuns()
  syncPolling()
}

onMounted(() => {
  void fetchRuns()
  void fetchAgents()
  void fetchExecutionOptions()
  void fetchRunners()
  document.addEventListener('visibilitychange', onVisibility)
})

onUnmounted(() => {
  alive = false
  if (pollTimer) {
    clearInterval(pollTimer)
    pollTimer = null
  }
  document.removeEventListener('visibilitychange', onVisibility)
})
</script>

<template>
  <section class="agent-run-panel">
    <div class="arp-head">
      <h3 class="arp-title">
        <AppIcon name="zap" :size="14" />
        Implement
      </h3>
      <div class="arp-actions">
        <select
          v-if="availableActions.length > 1"
          v-model="selectedActionKey"
          class="arp-action"
          aria-label="Agent action"
          :disabled="busy || !canStartRun"
          @change="onActionChange"
        >
          <option
            v-for="action in availableActions"
            :key="action.action_key"
            :value="action.action_key"
          >
            {{ action.label }}
          </option>
        </select>
        <select
          v-if="actionRunners.length > 1"
          v-model="selectedDevice"
          class="arp-device"
          aria-label="Target runner"
          :disabled="busy || !canStartRun"
        >
          <option v-for="r in actionRunners" :key="r.device_id" :value="r.device_id">
            {{ r.device_id }}
          </option>
        </select>
        <select
          v-if="projectAgents.length"
          v-model="selectedAgentName"
          class="arp-agent"
          aria-label="Project agent"
          :disabled="busy || !canStartRun"
        >
          <option value="">No project agent</option>
          <option v-for="agent in projectAgents" :key="agent.name" :value="agent.name">
            {{ agent.name }}
          </option>
        </select>
        <select
          v-if="selectedActionCanDeploy"
          v-model="deployTarget"
          class="arp-deploy-target"
          aria-label="Deploy target"
          :disabled="busy || !canStartRun"
        >
          <option v-for="target in DEPLOY_TARGETS" :key="target.value" :value="target.value">
            {{ target.label }}
          </option>
        </select>
        <button v-if="canStartRun" class="btn btn-primary btn-sm" type="button" :disabled="busy" @click="implement">
          {{ buttonLabel }}
        </button>
      </div>
    </div>

    <div v-if="selectorSummary.length" class="arp-defaults" aria-label="AI defaults">
      <span class="arp-defaults-label">Defaults</span>
      <span v-for="item in selectorSummary" :key="item" class="arp-defaults-pill">{{ item }}</span>
    </div>

    <div v-if="selectedActionIsDraft" class="arp-draft-controls">
      <select
        v-model="selectedProfileId"
        class="arp-draft-select"
        aria-label="Draft profile"
        :disabled="busy || !canStartRun || !profileChoices.length"
      >
        <option v-for="profile in profileChoices" :key="profile.id" :value="profile.id">
          {{ profile.label }}
        </option>
      </select>
      <select
        v-model="selectedEffort"
        class="arp-draft-select"
        aria-label="Draft effort"
        :disabled="busy || !canStartRun || !effortChoices.length"
      >
        <option v-for="effort in effortChoices" :key="effort" :value="effort">
          {{ effort }}
        </option>
      </select>
      <select
        v-model="selectedPromptPresetRef"
        class="arp-draft-select arp-draft-select--wide"
        aria-label="Draft prompt preset"
        :disabled="busy || !canStartRun || !promptPresetChoices.length"
      >
        <option v-for="preset in promptPresetChoices" :key="preset.ref" :value="preset.ref">
          {{ preset.label }}
        </option>
      </select>
      <select
        v-model="selectedContextPack"
        class="arp-draft-select arp-draft-select--wide"
        aria-label="Draft context pack"
        :disabled="busy || !canStartRun || !contextPackChoices.length"
      >
        <option v-for="pack in contextPackChoices" :key="pack.id" :value="pack.id">
          {{ pack.label }}
        </option>
      </select>
      <span v-if="selectedDraftProviderMeta" class="arp-draft-meta">{{
        selectedDraftProviderMeta
      }}</span>
      <span class="arp-draft-source">{{ selectedDefaultSourceLabel }}</span>
    </div>

    <div v-if="selectedActionIsDraft" class="arp-knowledge" aria-label="PPM knowledge">
      <div class="arp-knowledge-head">
        <AppIcon name="book-open" :size="13" />
        <span>PPM knowledge</span>
      </div>
      <div v-if="visibleKnowledgeSuggestions.length" class="arp-knowledge-list">
        <article
          v-for="suggestion in visibleKnowledgeSuggestions"
          :key="suggestion.ref"
          class="arp-knowledge-item"
        >
          <div class="arp-knowledge-copy">
            <strong>{{ knowledgeSuggestionTitle(suggestion) }}</strong>
            <span>{{ knowledgeSuggestionMeta(suggestion) }}</span>
          </div>
          <button
            class="arp-knowledge-button"
            type="button"
            :disabled="!canStartRun || busy || (!canUseKnowledgePrompt(suggestion) && !hasKnowledgeContextPack)"
            @click="useKnowledgeSuggestion(suggestion)"
          >
            {{ canUseKnowledgePrompt(suggestion) ? 'Use prompt' : 'Use context' }}
          </button>
        </article>
      </div>
      <p v-else class="arp-knowledge-empty">No prompt-ready PPM knowledge yet.</p>
    </div>

    <div v-if="selectedProjectAgent" class="arp-agent-ready" aria-label="Agent readiness">
      <div class="arp-agent-ready-row">
        <span v-for="item in selectedAgentReadiness" :key="item" class="arp-agent-ready-pill">
          {{ item }}
        </span>
      </div>
      <div class="arp-agent-commands">
        <code v-for="command in selectedAgentCommands(selectedProjectAgent)" :key="command">{{
          command
        }}</code>
      </div>
    </div>

    <p v-if="runnersError" class="arp-error" role="alert">
      Couldn't check for runners: {{ runnersError }}
    </p>
    <p v-else-if="!runners.length && !availableDraftProviderActions.length" class="arp-hint">
      No runner is online for this project. The run will queue until a
      <code>paimos run-agent watch</code> picks it up.
    </p>
    <p v-else-if="!runners.length && availableDraftProviderActions.length" class="arp-hint">
      No local runner is online. Draft providers are available.
    </p>
    <p v-if="executionOptionsError" class="arp-error" role="alert">
      {{ executionOptionsError }}
    </p>
    <p
      v-for="provider in unavailableDraftProviderActions"
      :key="provider.action_key"
      class="arp-hint"
    >
      {{ provider.label }} unavailable: {{ provider.unavailable_reason }}
    </p>
    <p v-if="agentsError" class="arp-error" role="alert">
      Couldn't load project agents: {{ agentsError }}
    </p>

    <p v-if="error" class="arp-error" role="alert">{{ error }}</p>
    <p v-if="notice" class="arp-notice" role="status">{{ notice }}</p>

    <p v-if="!loading && !runs.length && canStartRun" class="arp-empty">
      No runs yet. Click <strong>Implement this</strong> to hand {{ issueKey }} to your local agent.
    </p>
    <p v-else-if="!loading && !runs.length" class="arp-empty">
      No runs yet.
    </p>

    <ul v-if="runs.length" class="arp-runs" aria-live="polite" aria-label="Agent runs">
      <li v-for="run in runs" :key="run.id" class="arp-run">
        <div class="arp-run-main">
          <span class="arp-pill" :class="`arp-pill--${run.status}`">
            {{ statusLabel(run.status) }}
          </span>
          <span class="arp-run-meta">
            <span class="arp-run-id">#{{ run.id }}</span>
            <span v-if="run.version" class="arp-ver">v{{ run.version }}</span>
            <span v-if="run.provider_label" class="arp-provider">{{ run.provider_label }}</span>
            <span v-if="run.run_mode === 'draft' && run.profile_id" class="arp-provider"
              >{{ run.profile_id }} / {{ run.effort }}</span
            >
            <span v-if="run.run_mode === 'draft' && run.context_pack" class="arp-provider">{{
              run.context_pack
            }}</span>
            <span v-if="run.agent_name" class="arp-agent-name">{{ run.agent_name }}</span>
            <span v-if="run.device_id" class="arp-dev">{{ run.device_id }}</span>
            <span v-if="run.deploy_target" class="arp-target">→ {{ run.deploy_target }}</span>
            <time :datetime="isoAttr(runTimestamp(run))">{{ localTime(runTimestamp(run)) }}</time>
          </span>
        </div>
        <ol class="arp-timeline" :aria-label="`Run #${run.id} timeline`">
          <li
            v-for="stage in runStages(run)"
            :key="stage.key"
            class="arp-stage"
            :class="`arp-stage--${stage.state}`"
          >
            <span class="arp-stage-dot" aria-hidden="true" />
            <span>{{ stage.label }}</span>
          </li>
        </ol>
        <div v-if="run.run_mode === 'draft'" class="arp-draft-review">
          <span v-for="part in draftReviewMeta(run)" :key="part" class="arp-draft-review-pill">
            {{ part }}
          </span>
          <button
            v-if="canStartRun && run.status === 'drafted' && !run.followup_run_id"
            type="button"
            class="arp-draft-handoff"
            :disabled="busy"
            @click="handoffDraft(run)"
          >
            Handoff to runner
          </button>
          <span v-else-if="run.followup_run_id" class="arp-draft-followup">
            Follow-up #{{ run.followup_run_id }}
          </span>
        </div>
        <span v-if="run.source_draft_run_id" class="arp-tests">
          From draft #{{ run.source_draft_run_id }}
        </span>
        <span v-if="run.tests_summary" class="arp-tests">{{ run.tests_summary }}</span>
        <span v-if="run.error" class="arp-run-err">{{ run.error }}</span>
      </li>
    </ul>
  </section>
</template>

<style scoped>
.agent-run-panel {
  margin-top: 1.25rem;
  padding: 1rem;
  border: 1px solid var(--border);
  border-radius: 10px;
  background: var(--bg-card);
}
.arp-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
  flex-wrap: wrap;
}
.arp-title {
  display: inline-flex;
  align-items: center;
  gap: 0.4rem;
  margin: 0;
  font-size: 14px;
  font-weight: 600;
  color: var(--text);
}
.arp-actions {
  display: inline-flex;
  align-items: center;
  gap: 0.5rem;
  flex-wrap: wrap;
  justify-content: flex-end;
}
.arp-defaults {
  display: flex;
  align-items: center;
  gap: 0.35rem;
  flex-wrap: wrap;
  margin-top: 0.65rem;
  font-size: 11px;
}
.arp-defaults-label {
  font-weight: 800;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: .04em;
}
.arp-defaults-pill {
  max-width: 16rem;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  border: 1px solid var(--border);
  border-radius: 6px;
  padding: 0.16rem 0.45rem;
  background: color-mix(in srgb, var(--bg) 78%, transparent);
  color: var(--text);
}
.arp-action,
.arp-device,
.arp-agent,
.arp-deploy-target,
.arp-draft-select {
  font: inherit;
  font-size: 12px;
  padding: 0.25rem 0.4rem;
  border: 1px solid var(--border);
  border-radius: 6px;
  background: var(--bg);
  color: var(--text);
}
.arp-action {
  width: 11rem;
}
.arp-agent {
  width: 9.5rem;
}
.arp-deploy-target {
  width: 10rem;
}
.arp-draft-controls {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 0.45rem;
  flex-wrap: wrap;
  margin-top: 0.65rem;
}
.arp-draft-select {
  width: 7.5rem;
}
.arp-draft-select--wide {
  width: 11rem;
}
.arp-draft-meta {
  font-size: 11px;
  color: var(--text-muted);
  max-width: 16rem;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.arp-draft-source {
  font-size: 11px;
  font-weight: 700;
  color: var(--text-muted);
  border: 1px solid var(--border);
  border-radius: 6px;
  padding: 0.18rem 0.45rem;
  background: var(--bg-card);
}
.arp-knowledge {
  margin-top: 0.65rem;
  padding-top: 0.65rem;
  border-top: 1px solid color-mix(in srgb, var(--border) 72%, transparent);
}
.arp-knowledge-head {
  display: inline-flex;
  align-items: center;
  gap: 0.35rem;
  font-size: 12px;
  font-weight: 700;
  color: var(--text);
}
.arp-knowledge-list {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(13rem, 1fr));
  gap: 0.45rem;
  margin-top: 0.45rem;
}
.arp-knowledge-item {
  min-width: 0;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.6rem;
  padding: 0.45rem 0.5rem;
  border: 1px solid color-mix(in srgb, var(--border) 80%, transparent);
  border-radius: 7px;
  background: color-mix(in srgb, var(--bg) 76%, transparent);
}
.arp-knowledge-copy {
  min-width: 0;
  display: grid;
  gap: 0.1rem;
}
.arp-knowledge-copy strong {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 12px;
  color: var(--text);
}
.arp-knowledge-copy span,
.arp-knowledge-empty {
  font-size: 11px;
  color: var(--text-muted);
}
.arp-knowledge-empty {
  margin: 0.35rem 0 0;
}
.arp-knowledge-button {
  flex: 0 0 auto;
  font: inherit;
  font-size: 11px;
  font-weight: 700;
  padding: 0.22rem 0.45rem;
  border: 1px solid var(--border);
  border-radius: 6px;
  color: var(--text);
  background: var(--bg-card);
  cursor: pointer;
}
.arp-knowledge-button:disabled {
  cursor: not-allowed;
  opacity: 0.55;
}
.arp-agent-ready {
  margin-top: 0.65rem;
  display: grid;
  gap: 0.4rem;
  padding-top: 0.65rem;
  border-top: 1px solid color-mix(in srgb, var(--border) 72%, transparent);
}
.arp-agent-ready-row {
  display: flex;
  align-items: center;
  gap: 0.35rem;
  flex-wrap: wrap;
}
.arp-agent-ready-pill {
  display: inline-flex;
  align-items: center;
  min-height: 22px;
  border: 1px solid var(--border);
  border-radius: 6px;
  padding: 0.1rem 0.45rem;
  font-size: 11px;
  font-weight: 700;
  color: var(--text);
  background: var(--bg);
}
.arp-agent-commands {
  display: grid;
  gap: 0.3rem;
}
.arp-agent-commands code {
  display: block;
  overflow-x: auto;
  white-space: nowrap;
  border: 1px solid var(--border);
  border-radius: 5px;
  padding: 0.3rem 0.45rem;
  color: var(--text-muted);
  background: var(--bg);
  font-size: 11px;
}
.arp-hint,
.arp-empty {
  margin: 0.6rem 0 0;
  font-size: 12px;
  color: var(--text-muted);
}
.arp-error {
  margin: 0.6rem 0 0;
  font-size: 12px;
  color: #c0392b;
}
.arp-notice {
  margin: 0.6rem 0 0;
  font-size: 12px;
  color: #0f7355;
}
.arp-hint code {
  font-size: 11px;
  background: color-mix(in srgb, var(--text-muted) 12%, transparent);
  padding: 0.05rem 0.3rem;
  border-radius: 4px;
}
.arp-runs {
  list-style: none;
  margin: 0.75rem 0 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 0.4rem;
}
.arp-run {
  display: grid;
  gap: 0.45rem;
  font-size: 12px;
  padding: 0.55rem 0;
  border-top: 1px solid color-mix(in srgb, var(--border) 72%, transparent);
}
.arp-run:first-child {
  border-top: 0;
  padding-top: 0;
}
.arp-run-main {
  display: flex;
  align-items: center;
  gap: 0.6rem;
  flex-wrap: wrap;
}
.arp-pill {
  display: inline-block;
  padding: 0.1rem 0.5rem;
  border-radius: 999px;
  font-weight: 600;
  font-size: 11px;
  white-space: nowrap;
  background: color-mix(in srgb, var(--text-muted) 18%, transparent);
  color: var(--text);
}
.arp-pill--running {
  background: color-mix(in srgb, var(--brand-blue) 20%, transparent);
  color: var(--brand-blue);
}
.arp-pill--tests_passed {
  background: color-mix(in srgb, #1aa179 24%, transparent);
  color: #0f7355;
}
.arp-pill--deployed {
  background: color-mix(in srgb, #2ecc71 24%, transparent);
  color: #1e8449;
}
.arp-pill--drafted {
  background: color-mix(in srgb, #8e7cc3 22%, transparent);
  color: #5d4b93;
}
.arp-pill--tests_failed,
.arp-pill--failed {
  background: color-mix(in srgb, #e74c3c 22%, transparent);
  color: #c0392b;
}
.arp-run-meta {
  display: inline-flex;
  align-items: center;
  gap: 0.5rem;
  color: var(--text-muted);
  flex-wrap: wrap;
}
.arp-run-id {
  font-weight: 700;
  color: var(--text);
}
.arp-ver {
  font-weight: 600;
  color: var(--text);
}
.arp-run-err {
  color: #c0392b;
  flex-basis: 100%;
}
.arp-tests {
  color: var(--text-muted);
  flex-basis: 100%;
}
.arp-draft-review {
  display: flex;
  align-items: center;
  gap: 0.35rem;
  flex-wrap: wrap;
}
.arp-draft-review-pill,
.arp-draft-followup {
  display: inline-flex;
  align-items: center;
  min-height: 22px;
  border-radius: 999px;
  padding: 0.08rem 0.45rem;
  font-size: 11px;
  color: var(--text-muted);
  background: color-mix(in srgb, var(--text-muted) 10%, transparent);
}
.arp-draft-handoff {
  font: inherit;
  font-size: 11px;
  font-weight: 700;
  border: 1px solid var(--border);
  border-radius: 6px;
  padding: 0.18rem 0.5rem;
  color: var(--text);
  background: var(--bg-card);
  cursor: pointer;
}
.arp-draft-handoff:disabled {
  cursor: wait;
  opacity: 0.65;
}
.arp-timeline {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  align-items: center;
  gap: 0.35rem;
  flex-wrap: wrap;
}
.arp-stage {
  display: inline-flex;
  align-items: center;
  gap: 0.25rem;
  color: var(--text-muted);
  font-size: 11px;
}
.arp-stage:not(:last-child)::after {
  content: '';
  width: 18px;
  height: 1px;
  margin-left: 0.35rem;
  background: var(--border);
}
.arp-stage-dot {
  width: 7px;
  height: 7px;
  border-radius: 999px;
  background: var(--border);
}
.arp-stage--complete {
  color: #1e8449;
}
.arp-stage--complete .arp-stage-dot {
  background: #2ecc71;
}
.arp-stage--active {
  color: var(--brand-blue);
  font-weight: 700;
}
.arp-stage--active .arp-stage-dot {
  background: var(--brand-blue);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--brand-blue) 16%, transparent);
}
.arp-stage--failed {
  color: #c0392b;
}
.arp-stage--failed .arp-stage-dot {
  background: #c0392b;
}
</style>
