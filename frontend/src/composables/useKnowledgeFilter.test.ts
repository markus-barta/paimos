/*
 * PAIMOS — Your Professional & Personal AI Project OS
 * Copyright (C) 2026 Markus Barta <markus@barta.com>
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as
 * published by the Free Software Foundation, version 3.
 */

// PAI-339 — filter pipeline tests. Covers the search + filter +
// sort contract every KnowledgeCategoryPanel relies on.

import { describe, expect, it } from 'vitest'
import type { KnowledgeEntry } from '@/types'
import { filterKnowledge } from './useKnowledgeFilter'

function entry(over: Partial<KnowledgeEntry> = {}): KnowledgeEntry {
  return {
    id: 1,
    project_id: 1,
    type: 'memory',
    slug: 'a',
    title: '',
    body: '',
    status: 'backlog',
    metadata: {},
    created_at: '2026-05-01 10:00:00',
    updated_at: '2026-05-01 10:00:00',
    ...over,
  }
}

describe('filterKnowledge', () => {
  it('hides archived by default and shows them when opted in', () => {
    const items = [
      entry({ slug: 'live', status: 'backlog' }),
      entry({ slug: 'old', status: 'cancelled' }),
    ]
    expect(filterKnowledge(items, { category: 'memory' }).map((e) => e.slug)).toEqual(['live'])
    expect(filterKnowledge(items, { category: 'memory', showArchived: true }).map((e) => e.slug)).toEqual(['live', 'old'])
  })

  it('filters memory by metadata.type and ignores the filter for other categories', () => {
    const items = [
      entry({ slug: 'a', metadata: { type: 'feedback' } }),
      entry({ slug: 'b', metadata: { type: 'project' } }),
    ]
    expect(filterKnowledge(items, { category: 'memory', memoryType: 'feedback' }).map((e) => e.slug)).toEqual(['a'])
    // For non-memory categories the metadata.type filter is ignored.
    expect(filterKnowledge(items, { category: 'runbook', memoryType: 'feedback' }).map((e) => e.slug)).toEqual(['a', 'b'])
  })

  it('filters by environment list membership (case-insensitive)', () => {
    const items = [
      entry({ slug: 'p', metadata: { applies_to_environments: ['prod'] } }),
      entry({ slug: 's', metadata: { applies_to_environments: ['staging'] } }),
      entry({ slug: 'b', metadata: { applies_to_environments: ['Prod', 'staging'] } }),
    ]
    expect(filterKnowledge(items, { category: 'memory', environment: 'prod' }).map((e) => e.slug)).toEqual(['p', 'b'])
  })

  it('searches across title, slug, and body case-insensitively', () => {
    const items = [
      entry({ slug: 'feedback_lock', title: 'Lock signature' }),
      entry({ slug: 'unrelated', title: 'Other', body: 'mentions LOCK in body' }),
      entry({ slug: 'noise', title: 'unrelated' }),
    ]
    expect(filterKnowledge(items, { category: 'memory', search: 'lock' }).map((e) => e.slug).sort()).toEqual(['feedback_lock', 'unrelated'])
  })

  it('sorts by recency by default', () => {
    const items = [
      entry({ slug: 'old', updated_at: '2026-04-01 00:00:00' }),
      entry({ slug: 'new', updated_at: '2026-05-08 00:00:00' }),
      entry({ slug: 'mid', updated_at: '2026-04-15 00:00:00' }),
    ]
    expect(filterKnowledge(items, { category: 'memory' }).map((e) => e.slug)).toEqual(['new', 'mid', 'old'])
  })

  it('sorts alphabetically by slug', () => {
    const items = [
      entry({ slug: 'z' }),
      entry({ slug: 'a' }),
      entry({ slug: 'm' }),
    ]
    expect(filterKnowledge(items, { category: 'memory', sort: 'alpha' }).map((e) => e.slug)).toEqual(['a', 'm', 'z'])
  })

  it('sorts memory by confidence (high → medium → low), missing = medium', () => {
    const items = [
      entry({ slug: 'low_one', metadata: { confidence: 'low' } }),
      entry({ slug: 'no_meta' }),
      entry({ slug: 'high_one', metadata: { confidence: 'high' } }),
      entry({ slug: 'medium_one', metadata: { confidence: 'medium' } }),
    ]
    const order = filterKnowledge(items, { category: 'memory', sort: 'confidence' }).map((e) => e.slug)
    expect(order[0]).toBe('high_one')
    expect(order[order.length - 1]).toBe('low_one')
    // Missing-meta and explicit-medium are tied (both rank 1) — both
    // sit between high and low.
    expect(order.slice(1, 3).sort()).toEqual(['medium_one', 'no_meta'])
  })

  it('falls through to recency for confidence sort on non-memory', () => {
    const items = [
      entry({ slug: 'old', updated_at: '2026-04-01' }),
      entry({ slug: 'new', updated_at: '2026-05-08' }),
    ]
    // Non-memory + confidence sort → recency.
    expect(filterKnowledge(items, { category: 'runbook', sort: 'confidence' }).map((e) => e.slug)).toEqual(['new', 'old'])
  })

  it('combines filters: archived hidden + memoryType + search + sort', () => {
    const items = [
      entry({ slug: 'a', title: 'Lock signature', status: 'backlog', metadata: { type: 'feedback' }, updated_at: '2026-05-01' }),
      entry({ slug: 'b', title: 'Lock signature copy', status: 'cancelled', metadata: { type: 'feedback' }, updated_at: '2026-05-08' }),
      entry({ slug: 'c', title: 'Lock signature later', status: 'backlog', metadata: { type: 'feedback' }, updated_at: '2026-05-05' }),
      entry({ slug: 'd', title: 'Other', status: 'backlog', metadata: { type: 'project' }, updated_at: '2026-05-08' }),
    ]
    const result = filterKnowledge(items, {
      category: 'memory',
      search: 'lock',
      memoryType: 'feedback',
      sort: 'recency',
    })
    expect(result.map((e) => e.slug)).toEqual(['c', 'a'])
  })

  it('handles 500 entries comfortably (smoke perf test)', () => {
    const items: KnowledgeEntry[] = []
    for (let i = 0; i < 500; i += 1) {
      items.push(entry({ id: i, slug: `s_${i}`, title: `entry ${i}`, body: `body ${i % 7}`, updated_at: `2026-05-${String((i % 30) + 1).padStart(2, '0')} 00:00:00` }))
    }
    const start = performance.now()
    const result = filterKnowledge(items, { category: 'memory', search: 'body 3' })
    const elapsed = performance.now() - start
    // The contract is "renders in <500ms"; a pure-array filter
    // should be orders of magnitude faster (<50ms is plenty).
    expect(elapsed).toBeLessThan(100)
    expect(result.length).toBeGreaterThan(0)
  })
})
