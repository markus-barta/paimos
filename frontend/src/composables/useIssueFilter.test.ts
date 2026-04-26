import { describe, expect, it } from 'vitest'
import { normalizeSavedFilters, normalizeSavedFiltersJSON } from './useIssueFilter'

describe('normalizeSavedFilters', () => {
  it('fills in an explicit flat-mode default when treeView is missing', () => {
    expect(normalizeSavedFilters({ type: ['ticket'] })).toMatchObject({
      type: ['ticket'],
      treeView: false,
    })
  })

  it('preserves explicit tree mode when present', () => {
    expect(normalizeSavedFilters({ type: ['epic'], treeView: true })).toMatchObject({
      type: ['epic'],
      treeView: true,
    })
  })
})

describe('normalizeSavedFiltersJSON', () => {
  it('adds treeView to legacy view payloads that do not have it', () => {
    expect(JSON.parse(normalizeSavedFiltersJSON('{"type":["ticket"]}'))).toMatchObject({
      type: ['ticket'],
      treeView: false,
    })
  })

  it('keeps explicit treeView values intact', () => {
    expect(JSON.parse(normalizeSavedFiltersJSON('{"type":["ticket"],"treeView":true}'))).toMatchObject({
      type: ['ticket'],
      treeView: true,
    })
  })
})
