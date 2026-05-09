/*
 * PAIMOS — Your Professional & Personal AI Project OS
 * Copyright (C) 2026 Markus Barta <markus@barta.com>
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as
 * published by the Free Software Foundation, version 3.
 */

// PAI-342 — service-level tests for the applicable-memories convenience
// endpoint. Asserts URL shape (manual list vs ?suggest=1) and pass-
// through return-shape since the heavy logic lives server-side.

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
  type ApplicableMemory,
  listApplicableMemories,
  suggestApplicableMemories,
} from './applicableMemories'

describe('applicableMemories service', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('lists curated set without ?suggest', async () => {
    vi.mocked(api.get).mockResolvedValue([] as never)
    await listApplicableMemories(42)
    expect(api.get).toHaveBeenCalledWith('/issues/42/applicable-memories')
  })

  it('passes ?suggest=1 for the auto-suggest path', async () => {
    vi.mocked(api.get).mockResolvedValue([] as never)
    await suggestApplicableMemories(42)
    expect(api.get).toHaveBeenCalledWith('/issues/42/applicable-memories?suggest=1')
  })

  it('passes the API response straight through', async () => {
    const fixture: ApplicableMemory[] = [
      {
        id: 7,
        project_id: 1,
        project_key: 'PAI',
        slug: 'feedback_lock_signature',
        title: 'Lock signature feedback',
        preview: 'When two threads…',
        score: 5,
        matched: ['tag:bug', 'env:prod'],
      },
    ]
    vi.mocked(api.get).mockResolvedValue(fixture as never)
    const got = await suggestApplicableMemories(42)
    expect(got).toEqual(fixture)
  })
})
