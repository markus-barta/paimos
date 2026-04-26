import { api } from '@/api/client'

export interface AICallRow {
  id: number
  request_id: string
  user_id: number | null
  username: string
  action_key: string
  sub_action: string
  surface: string
  issue_id: number | null
  project_id: number | null
  customer_id: number | null
  cooperation_id: number | null
  subject_label: string
  provider: string
  model: string
  prompt_tokens: number
  completion_tokens: number
  total_tokens: number
  cost_micro_usd: number
  outcome: string
  error_class: string
  latency_ms: number
  created_at: string
}

export interface AICallListResponse {
  rows: AICallRow[]
  next_cursor: string
  total_count: number
  total_cost_micro_usd: number
}

export interface IssueAIActivityRow {
  log_id: number
  request_id: string
  action_key: string
  sub_action: string
  surface: string
  user_id: number | null
  user_name: string
  outcome: string
  latency_ms: number
  model: string
  prompt_tokens: number
  completion_tokens: number
  cost_micro_usd: number
  on_user_stack: boolean
  created_at: string
}

export interface IssueAIActivityResponse {
  rows: IssueAIActivityRow[]
  count: number
  last_week_count: number
}

export interface AICallQuery {
  from?: string
  to?: string
  user_id?: number | null
  action_key?: string
  model?: string
  outcome?: string
  surface?: string
  issue_id?: number | null
  limit?: number
  cursor?: string
}

function buildQuery(q: AICallQuery = {}): string {
  const sp = new URLSearchParams()
  if (q.from) sp.set('from', q.from)
  if (q.to) sp.set('to', q.to)
  if (q.user_id) sp.set('user_id', String(q.user_id))
  if (q.action_key) sp.set('action_key', q.action_key)
  if (q.model) sp.set('model', q.model)
  if (q.outcome) sp.set('outcome', q.outcome)
  if (q.surface) sp.set('surface', q.surface)
  if (q.issue_id) sp.set('issue_id', String(q.issue_id))
  if (q.limit) sp.set('limit', String(q.limit))
  if (q.cursor) sp.set('cursor', q.cursor)
  const suffix = sp.toString()
  return suffix ? `?${suffix}` : ''
}

export function loadAICalls(query: AICallQuery = {}): Promise<AICallListResponse> {
  return api.get<AICallListResponse>(`/ai/calls${buildQuery(query)}`)
}

export function loadMyAICalls(query: AICallQuery = {}): Promise<AICallListResponse> {
  return api.get<AICallListResponse>(`/ai/calls/me${buildQuery(query)}`)
}

export function loadIssueAICalls(issueId: number, query: AICallQuery = {}): Promise<AICallListResponse> {
  return api.get<AICallListResponse>(`/issues/${issueId}/ai-calls${buildQuery(query)}`)
}

export function loadIssueAIActivity(issueId: number): Promise<IssueAIActivityResponse> {
  return api.get<IssueAIActivityResponse>(`/issues/${issueId}/ai-activity`)
}

export function undoMutation(logId: number): Promise<{ undone: boolean, log_id: number }> {
  return api.post(`/undo/${logId}`, {})
}

export function undoMutationByRequestId(requestId: string): Promise<{ undone: boolean, log_id: number, request_id: string }> {
  return api.post(`/undo/request/${encodeURIComponent(requestId)}`, {})
}

export function buildAICallsExportUrl(mode: 'admin' | 'self', query: AICallQuery = {}): string {
  return mode === 'admin'
    ? `/api/ai/calls/export.csv${buildQuery(query)}`
    : `/api/ai/calls/me/export.csv${buildQuery(query)}`
}
