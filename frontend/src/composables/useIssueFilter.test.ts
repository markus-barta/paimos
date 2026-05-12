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

  it('normalizes persisted column widths alongside filters', () => {
    expect(normalizeSavedFilters({
      type: ['ticket'],
      columnWidths: { title: 480, status: 20, bogus: 100 },
    })).toMatchObject({
      type: ['ticket'],
      columnWidths: { title: 480, status: 92 },
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

  it('keeps valid columnWidths in saved view payloads', () => {
    expect(JSON.parse(normalizeSavedFiltersJSON('{"columnWidths":{"assignee":142,"bad":200}}'))).toMatchObject({
      columnWidths: { assignee: 142 },
    })
  })
})
