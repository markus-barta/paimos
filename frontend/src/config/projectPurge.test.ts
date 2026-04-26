import { describe, expect, it } from 'vitest'

import { buildProjectPurgePayload, emptyProjectPurgeForm } from './projectPurge'

describe('projectPurge helpers', () => {
  it('creates a blank purge form', () => {
    expect(emptyProjectPurgeForm()).toEqual({
      source: 'all',
      from_date: '',
      to_date: '',
      user_id: null,
    })
  })

  it('builds a sparse payload from filled fields only', () => {
    expect(buildProjectPurgePayload({
      source: 'filtered',
      from_date: '2026-01-01',
      to_date: '',
      user_id: 7,
    })).toEqual({
      source: 'filtered',
      from_date: '2026-01-01',
      user_id: 7,
    })
  })
})
