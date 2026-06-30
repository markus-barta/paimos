/*
 * PAIMOS — Your Professional & Personal AI Project OS
 * Copyright (C) 2026 Markus Barta <markus@barta.com>
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as
 * published by the Free Software Foundation, version 3.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
 * GNU Affero General Public License for more details.
 *
 * You should have received a copy of the GNU Affero General Public
 * License along with this program. If not, see <https://www.gnu.org/licenses/>.
 */

/**
 * issueQueryFetchers — PAI-563.
 *
 * Maps a canonical IssueQuery onto the existing issue-list API, byte-for-byte
 * matching the v1 param encoding (IssueList.vue `serverFilterQuery` +
 * IssuesView/ProjectDetailView path builders) so migrating hosts onto
 * useIssueQuery preserves behavior. One fetcher serves all three modes; only
 * the base path differs, which is what lets internal and portal lists share
 * the same engine (PAI-570/474).
 */

import { api } from '@/api/client'
import { isNeg, posOf } from './useIssueFilter'
import type { Issue, IssueListEnvelope } from '@/types'
import type { IssueQuery, IssueFetcher } from './useIssueQuery'

function appendSignedList(
  params: URLSearchParams,
  key: string,
  values: string[],
  opts: { numeric?: boolean } = {},
) {
  const out: string[] = []
  for (const raw of values) {
    const neg = isNeg(raw)
    const value = posOf(raw)
    if (opts.numeric) {
      const n = Number(value)
      if (!Number.isInteger(n) || n <= 0) continue
    }
    out.push(neg ? `!${value}` : value)
  }
  if (out.length > 0) params.set(key, out.join(','))
}

/** Encode a query as the URLSearchParams the issue-list endpoints expect. */
export function buildIssueQueryParams(q: IssueQuery): URLSearchParams {
  const p = new URLSearchParams()
  const f = q.filters
  appendSignedList(p, 'status', f.status)
  appendSignedList(p, 'priority', f.priority)
  appendSignedList(p, 'type', f.type)
  appendSignedList(p, 'cost_unit', f.costUnit)
  appendSignedList(p, 'release', f.release)
  appendSignedList(p, 'ai_status', f.aiStatus)
  appendSignedList(p, 'tags', f.tags, { numeric: true })
  appendSignedList(p, 'sprints', f.sprints, { numeric: true })
  appendSignedList(p, 'assignee_id', f.assignee)
  appendSignedList(p, 'project_ids', f.projects, { numeric: true })
  appendSignedList(p, 'parent_id', f.epic, { numeric: true })
  if (f.dateFrom || f.dateTo) {
    p.set('date_field', f.dateField || 'completed')
    if (f.dateFrom) p.set('date_from', f.dateFrom)
    if (f.dateTo) p.set('date_to', f.dateTo)
  }
  p.set('fields', 'list')
  p.set('limit', String(q.window.mode === 'all' ? 0 : q.window.limit))
  p.set('offset', String(q.window.offset))
  if (q.sort.key) {
    p.set('sort', q.sort.key)
    p.set('order', q.sort.dir)
  }
  const search = q.search.trim()
  if (search.length >= 2) p.set('q', search)
  return p
}

/**
 * Internal-host param builder: starts from the pre-encoded filter string that
 * IssueList emits (`rawFilter`) and layers fields/limit/offset/sort/q exactly
 * as v1's fetchIssues did, so the controller is a drop-in for those hosts.
 */
export function buildInternalParams(q: IssueQuery): URLSearchParams {
  const p = new URLSearchParams(q.rawFilter || '')
  p.set('fields', 'list')
  // The project endpoint returns a plain array unless envelope=1; the global
  // /issues endpoint always returns the envelope. Match v1 buildProjectIssuesUrl.
  if (q.mode === 'internal-project') p.set('envelope', '1')
  p.set('limit', String(q.window.mode === 'all' ? 0 : q.window.limit))
  p.set('offset', String(q.window.offset))
  if (q.sort.key) {
    p.set('sort', q.sort.key)
    p.set('order', q.sort.dir)
  }
  const search = q.search.trim()
  if (search.length >= 2) p.set('q', search)
  return p
}

export function internalIssuePath(q: IssueQuery): string {
  const qs = buildInternalParams(q).toString()
  return q.mode === 'internal-project' && q.projectId != null
    ? `/projects/${q.projectId}/issues?${qs}`
    : `/issues?${qs}`
}

/**
 * Path a freshness poll should hit to re-fetch the *current loaded window* at
 * offset 0 (limit grows with what's loaded, mirroring v1's freshnessLimit) so
 * the poll compares like-for-like against what the user sees.
 */
export function controllerFreshnessPath(q: IssueQuery, loaded: number, pageSize: number): string {
  const limit = q.window.mode === 'all' ? 0 : Math.max(pageSize, loaded)
  return internalIssuePath({ ...q, window: { mode: q.window.mode, limit, offset: 0 } })
}

/** Fetcher for the internal global/project lists (IssueList-driven). */
export function createInternalFetcher(): IssueFetcher<Issue> {
  return async (q, signal) => {
    const env = await api.get<IssueListEnvelope<Issue>>(internalIssuePath(q), { signal })
    const issues = env.issues ?? []
    const total = env.total ?? issues.length
    const hasMore = env.has_more ?? total > issues.length
    return {
      issues, total, hasMore,
      revision: env.revision,
      fingerprint: env.fingerprint,
      selectionFingerprint: env.selection_fingerprint,
    }
  }
}

/**
 * Portal param builder (PAI-570 + PAI-461 contract): the portal endpoint takes
 * status/type/priority/tag_ids + q (no assignee/cost_unit/etc), envelope=1, and
 * does not gate q on length. Maps the controller's structured filters onto it.
 */
export function buildPortalParams(q: IssueQuery): URLSearchParams {
  const p = new URLSearchParams()
  const f = q.filters
  if (f.status.length) p.set('status', f.status.join(','))
  if (f.type.length) p.set('type', f.type.join(','))
  if (f.priority.length) p.set('priority', f.priority.join(','))
  if (f.tags.length) p.set('tag_ids', f.tags.join(','))
  p.set('fields', 'list')
  p.set('envelope', '1')
  p.set('limit', String(q.window.mode === 'all' ? 0 : q.window.limit))
  p.set('offset', String(q.window.offset))
  if (q.sort.key) {
    p.set('sort', q.sort.key)
    p.set('order', q.sort.dir)
  }
  const search = q.search.trim()
  if (search) p.set('q', search)
  return p
}

/** Fetcher for the customer-portal list (PAI-570: portal on the shared core). */
export function createPortalFetcher<T extends { id: number }>(): IssueFetcher<T> {
  return async (q, signal) => {
    const url = `/portal/projects/${q.projectId}/issues?${buildPortalParams(q).toString()}`
    const env = await api.get<IssueListEnvelope<T>>(url, { signal })
    const issues = env.issues ?? []
    const total = env.total ?? issues.length
    const hasMore = env.has_more ?? total > issues.length
    return {
      issues, total, hasMore,
      revision: env.revision,
      fingerprint: env.fingerprint,
      selectionFingerprint: env.selection_fingerprint,
    }
  }
}

/** Endpoint for a query's mode (structured filters). */
export function issuePath(q: IssueQuery): string {
  const qs = buildIssueQueryParams(q).toString()
  switch (q.mode) {
    case 'internal-project':
      return `/projects/${q.projectId}/issues?${qs}`
    case 'portal':
      return `/portal/projects/${q.projectId}/issues?${qs}`
    case 'internal-global':
    default:
      return `/issues?${qs}`
  }
}

/**
 * Real fetcher backed by the HTTP API. The AbortSignal from useIssueQuery is
 * forwarded to api.get so a superseded request is actually cancelled, not just
 * ignored on arrival.
 */
export function createIssueFetcher(): IssueFetcher<Issue> {
  return async (q, signal) => {
    const env = await api.get<IssueListEnvelope<Issue>>(issuePath(q), { signal })
    const issues = env.issues ?? []
    const total = env.total ?? issues.length
    const hasMore = env.has_more ?? total > issues.length
    return {
      issues, total, hasMore,
      revision: env.revision,
      fingerprint: env.fingerprint,
      selectionFingerprint: env.selection_fingerprint,
    }
  }
}
