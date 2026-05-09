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
  acceptProposedMemory,
  activeStatusValue,
  archivedStatusValue,
  createKnowledgeEntry,
  deleteKnowledgeEntry,
  getKnowledgeEntry,
  isArchived,
  isProposed,
  listKnowledgeEntries,
  listStaleProposedMemory,
  proposedStatusValue,
  rejectProposedMemory,
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

  // PAI-349 — proposed-memory accept / reject + stale list endpoint.
  describe('proposed memory helpers', () => {
    it('isProposed flips true on status=proposed only', () => {
      expect(isProposed(makeEntry({ status: 'proposed' }))).toBe(true)
      expect(isProposed(makeEntry({ status: 'backlog' }))).toBe(false)
      expect(isProposed(makeEntry({ status: 'cancelled' }))).toBe(false)
      expect(proposedStatusValue()).toBe('proposed')
    })

    it('acceptProposedMemory PUTs status=backlog (active)', async () => {
      const entry = makeEntry({ status: 'proposed', metadata: { type: 'feedback' } })
      vi.mocked(api.put).mockResolvedValue(entry as never)
      await acceptProposedMemory(7, entry)
      expect(api.put).toHaveBeenCalledWith(
        '/projects/7/memory/feedback_thread_dump',
        expect.objectContaining({ status: 'backlog', slug: 'feedback_thread_dump' }),
      )
    })

    it('rejectProposedMemory PUTs status=cancelled and stamps archived_reason', async () => {
      const entry = makeEntry({ status: 'proposed', metadata: { type: 'feedback' } })
      vi.mocked(api.put).mockResolvedValue(entry as never)
      await rejectProposedMemory(7, entry)
      const call = vi.mocked(api.put).mock.calls[0]
      expect(call[0]).toBe('/projects/7/memory/feedback_thread_dump')
      const body = call[1] as { status: string; metadata: { archived_reason?: string; type?: string } }
      expect(body.status).toBe('cancelled')
      expect(body.metadata.archived_reason).toBe('rejected')
      // Existing metadata fields are preserved.
      expect(body.metadata.type).toBe('feedback')
    })

    it('listStaleProposedMemory hits /memory/proposed/stale with optional days', async () => {
      vi.mocked(api.get).mockResolvedValue([] as never)
      await listStaleProposedMemory(7)
      expect(api.get).toHaveBeenCalledWith('/projects/7/memory/proposed/stale')
      await listStaleProposedMemory(7, 14)
      expect(api.get).toHaveBeenCalledWith('/projects/7/memory/proposed/stale?days=14')
    })
  })
})
