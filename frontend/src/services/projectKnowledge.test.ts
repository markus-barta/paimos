/*
 * PAIMOS — Your Professional & Personal AI Project OS
 * Copyright (C) 2026 Markus Barta <markus@barta.com>
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as
 * published by the Free Software Foundation, version 3.
 */

// PAI-339 — service-level tests for the knowledge plane convenience
// endpoints. The contract under test is "the path-alias mapping is
// correct" (per category) plus the slug-validation echo. Mocks the
// `api` client so the assertions stay close to the URL the browser
// would have hit.

import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('@/api/client', () => ({
  api: {
    get: vi.fn(),
    post: vi.fn(),
    put: vi.fn(),
    delete: vi.fn(),
  },
}))

import { api } from '@/api/client'
import {
  activeStatusValue,
  archivedStatusValue,
  createKnowledgeEntry,
  deleteKnowledgeEntry,
  getKnowledgeEntry,
  isArchived,
  listKnowledgeEntries,
  suggestSlug,
  updateKnowledgeEntry,
  validateKnowledgeSlug,
} from './projectKnowledge'
import type { KnowledgeEntry } from '@/types'

function makeEntry(over: Partial<KnowledgeEntry> = {}): KnowledgeEntry {
  return {
    id: 1,
    project_id: 7,
    type: 'memory',
    slug: 'feedback_thread_dump',
    title: 'Thread dump on lock signature match',
    body: 'When …',
    status: 'backlog',
    metadata: {},
    created_at: '2026-05-08 10:00:00',
    updated_at: '2026-05-08 10:00:00',
    ...over,
  }
}

describe('projectKnowledge service', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('lists each category at the right convenience endpoint', async () => {
    vi.mocked(api.get).mockResolvedValue([] as never)
    await listKnowledgeEntries(7, 'memory')
    await listKnowledgeEntries(7, 'runbook')
    await listKnowledgeEntries(7, 'external_system')
    await listKnowledgeEntries(7, 'related_project')
    await listKnowledgeEntries(7, 'guideline')
    expect(api.get).toHaveBeenCalledWith('/projects/7/memory')
    expect(api.get).toHaveBeenCalledWith('/projects/7/runbooks')
    expect(api.get).toHaveBeenCalledWith('/projects/7/external-systems')
    expect(api.get).toHaveBeenCalledWith('/projects/7/related-projects')
    expect(api.get).toHaveBeenCalledWith('/projects/7/guidelines')
  })

  it('round-trips a single entry through CRUD', async () => {
    const entry = makeEntry()
    vi.mocked(api.get).mockResolvedValue(entry as never)
    vi.mocked(api.post).mockResolvedValue(entry as never)
    vi.mocked(api.put).mockResolvedValue(entry as never)
    vi.mocked(api.delete).mockResolvedValue(undefined as never)

    await getKnowledgeEntry(7, 'memory', 'feedback_thread_dump')
    expect(api.get).toHaveBeenCalledWith('/projects/7/memory/feedback_thread_dump')

    await createKnowledgeEntry(7, 'memory', {
      slug: 'feedback_thread_dump',
      title: 'Thread dump…',
      body: '',
      metadata: {},
    })
    expect(api.post).toHaveBeenCalledWith('/projects/7/memory', expect.objectContaining({ slug: 'feedback_thread_dump' }))

    await updateKnowledgeEntry(7, 'memory', 'feedback_thread_dump', {
      slug: 'feedback_thread_dump',
      title: 'Thread dump (v2)',
      body: 'updated body',
      metadata: { confidence: 'high' },
    })
    expect(api.put).toHaveBeenCalledWith(
      '/projects/7/memory/feedback_thread_dump',
      expect.objectContaining({ title: 'Thread dump (v2)' }),
    )

    await deleteKnowledgeEntry(7, 'memory', 'feedback_thread_dump')
    expect(api.delete).toHaveBeenCalledWith('/projects/7/memory/feedback_thread_dump')
  })

  it('encodes slugs with reserved URI characters', async () => {
    vi.mocked(api.get).mockResolvedValue(undefined as never)
    await getKnowledgeEntry(7, 'runbook', 'has spaces')
    expect(api.get).toHaveBeenCalledWith('/projects/7/runbooks/has%20spaces')
  })

  describe('validateKnowledgeSlug', () => {
    it('accepts canonical slugs', () => {
      expect(validateKnowledgeSlug('feedback_x')).toBe('')
      expect(validateKnowledgeSlug('a-b-c')).toBe('')
    })
    it('rejects empty / oversize / mis-shaped', () => {
      expect(validateKnowledgeSlug('')).not.toBe('')
      expect(validateKnowledgeSlug('A_invalid')).not.toBe('')
      expect(validateKnowledgeSlug('1leading_digit')).not.toBe('')
      expect(validateKnowledgeSlug('a'.repeat(65))).not.toBe('')
    })
  })

  describe('suggestSlug', () => {
    it('lower-cases and replaces non-slug chars', () => {
      expect(suggestSlug('Hello World!')).toBe('hello_world')
    })
    it('prefixes slugs that start with a digit', () => {
      expect(suggestSlug('1st rule')).toBe('m_1st_rule')
    })
    it('caps at 64 chars', () => {
      expect(suggestSlug('a'.repeat(200)).length).toBeLessThanOrEqual(64)
    })
  })

  describe('archive helpers', () => {
    it('treats cancelled as archived', () => {
      expect(isArchived(makeEntry({ status: 'cancelled' }))).toBe(true)
      expect(isArchived(makeEntry({ status: 'backlog' }))).toBe(false)
    })
    it('exposes status enum values used for the wire', () => {
      expect(archivedStatusValue()).toBe('cancelled')
      expect(activeStatusValue()).toBe('backlog')
    })
  })
})
