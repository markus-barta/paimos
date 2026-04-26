import { describe, expect, it } from 'vitest'
import { phaseFor } from '@/composables/useAiPhases'

describe('phaseFor', () => {
  it('uses action-specific narration at boundaries', () => {
    expect(phaseFor('find_parent', 0).phase).toBe('reading')
    expect(phaseFor('find_parent', 1200).phase).toBe('scanning')
    expect(phaseFor('find_parent', 3000).phase).toBe('ranking')
  })

  it('falls back to optimize script for unknown actions', () => {
    expect(phaseFor('unknown_action', 0).label).toBeTruthy()
    expect(phaseFor('unknown_action', 4000).phase).toBe('refining')
  })
})
