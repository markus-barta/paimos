import { describe, expect, it } from 'vitest'

import { useProjectAuxPanels } from './useProjectAuxPanels'
import { buildProjectDisplayTabs, FALLBACK_PROJECT_VIEWS } from '@/config/projectDefaultViews'
import type { SavedView } from '@/types'

function makeView(id: number, title: string, overrides: Partial<SavedView> = {}): SavedView {
  return {
    id,
    user_id: 1,
    owner_username: 'mba',
    title,
    description: '',
    columns_json: '[]',
    filters_json: '{}',
    is_shared: false,
    is_admin_default: false,
    sort_order: 0,
    hidden: false,
    pinned: null,
    created_at: '',
    updated_at: '',
    ...overrides,
  }
}

describe('useProjectAuxPanels', () => {
  it('toggles the same panel closed and different panels open', () => {
    const aux = useProjectAuxPanels()
    aux.toggleAux('docs')
    expect(aux.auxPanel.value).toBe('docs')
    aux.toggleAux('docs')
    expect(aux.auxPanel.value).toBeNull()
    aux.toggleAux('cooperation')
    expect(aux.auxPanel.value).toBe('cooperation')
  })

  it('closes explicitly', () => {
    const aux = useProjectAuxPanels()
    aux.toggleAux('context')
    aux.closeAux()
    expect(aux.auxPanel.value).toBeNull()
  })
})

describe('buildProjectDisplayTabs', () => {
  it('returns fallback tabs when no views exist', () => {
    expect(buildProjectDisplayTabs([])).toEqual(FALLBACK_PROJECT_VIEWS)
  })

  it('keeps visible admin defaults and pinned personal views', () => {
    const tabs = buildProjectDisplayTabs([
      makeView(1, 'Admin hidden', { is_admin_default: true, hidden: true, pinned: null }),
      makeView(2, 'Admin visible', { is_admin_default: true, hidden: false, sort_order: 2 }),
      makeView(3, 'Pinned personal', { pinned: true, title: 'Alpha' }),
      makeView(4, 'Unpinned personal', { pinned: null, title: 'Zulu' }),
      makeView(5, 'Pinned override default', { is_admin_default: true, hidden: true, pinned: true, sort_order: 1 }),
    ])

    expect(tabs.map((tab) => tab.id)).toEqual([5, 2, 3])
  })
})
