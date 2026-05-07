import { api } from '@/api/client'
import type { TimeEntry } from '@/types'

export interface CreateTimeEntryPayload {
  comment: string
  override: number
  started_at: string
  stopped_at: string
  // PAI-335: super-admin only — log time on behalf of another user.
  // Backend rejects with 403 when this is set by a non-super-admin.
  // Omit (or send caller's id) for the normal self-write case.
  user_id?: number
}

export function loadIssueTimeEntries(issueId: number): Promise<TimeEntry[]> {
  return api.get<TimeEntry[]>(`/issues/${issueId}/time-entries`)
}

export function createIssueTimeEntry(issueId: number, body: CreateTimeEntryPayload): Promise<void> {
  return api.post(`/issues/${issueId}/time-entries`, body)
}

export function updateTimeEntry(entryId: number, payload: Record<string, unknown>): Promise<void> {
  return api.put(`/time-entries/${entryId}`, payload)
}

export function deleteTimeEntryById(entryId: number): Promise<void> {
  return api.delete(`/time-entries/${entryId}`)
}
