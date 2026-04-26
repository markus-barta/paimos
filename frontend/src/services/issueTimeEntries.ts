import { api } from '@/api/client'
import type { TimeEntry } from '@/types'

export interface CreateTimeEntryPayload {
  comment: string
  override: number
  started_at: string
  stopped_at: string
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
