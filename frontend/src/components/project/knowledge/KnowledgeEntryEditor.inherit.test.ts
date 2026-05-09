/*
 * PAIMOS — Your Professional & Personal AI Project OS
 * Copyright (C) 2026 Markus Barta <markus@barta.com>
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as
 * published by the Free Software Foundation, version 3.
 */

// PAI-348 — frontend coverage for the memory-inheritance editor
// surface. The component test focuses on the round-trip contract:
//
//   - existing entries with no `inherit` field render with the
//     checkbox checked (default = inherit)
//   - unchecking persists `metadata.inherit = false` on save
//   - re-opening an entry with `inherit: false` renders unchecked
//   - non-bool legacy values (string / number) fall back to checked
//
// Done as a unit test against a tiny harness that mounts the
// component with stubbed deps; the heavier integration with
// KnowledgeCategoryPanel.vue rides on the existing Vitest suite.

import { afterEach, describe, expect, it, vi } from 'vitest'
import { createApp, nextTick, ref } from 'vue'
import { createPinia, setActivePinia } from 'pinia'

vi.mock('@/api/client', () => ({
  api: { get: vi.fn(), post: vi.fn(), put: vi.fn(), delete: vi.fn() },
  // The auth store imports these at module load and watches them; use
  // real refs so the watch source is valid (otherwise Vue warns).
  permissionsEpoch: ref(0),
  sessionExpired: ref(false),
  sessionExpiresAt: ref<Date | null>(null),
  sessionReturnPath: ref<string | null>(null),
  announceSessionRestored: vi.fn(),
  announceSessionExpired: vi.fn(),
  isSessionExpiredError: () => false,
}))

vi.mock('@/services/issueRelations', () => ({
  loadIssueRelations: vi.fn().mockResolvedValue([]),
  removeIssueRelation: vi.fn().mockResolvedValue(undefined),
}))

vi.mock('@/composables/useMarkdown', () => ({
  useMarkdown: () => ({ html: { value: '' } }),
}))

vi.mock('@/composables/useConfirm', () => ({
  useConfirm: () => ({ confirm: vi.fn().mockResolvedValue(true) }),
}))

import KnowledgeEntryEditor from './KnowledgeEntryEditor.vue'
import type { KnowledgeEntryInput } from '@/types'

async function mountEditor(initial: KnowledgeEntryInput) {
  setActivePinia(createPinia())
  const el = document.createElement('div')
  document.body.appendChild(el)
  const savedPayloads: KnowledgeEntryInput[] = []
  const app = createApp(KnowledgeEntryEditor, {
    category: 'memory',
    initial,
    currentSlug: initial.slug,
    saving: false,
    saveError: '',
    autosuggestSlug: false,
    onSave: (p: KnowledgeEntryInput) => savedPayloads.push(p),
    onCancel: () => {},
  })
  // Pinia is required because the editor's PAI-342 originating-tickets
  // section pulls the auth store. We don't exercise that path here —
  // entryId is undefined so loadOriginatingTickets exits early.
  app.use(createPinia())
  app.mount(el)
  await nextTick()
  return {
    el,
    saved: savedPayloads,
    cleanup() {
      app.unmount()
      el.remove()
    },
  }
}

function checkbox(el: HTMLElement): HTMLInputElement {
  const node = el.querySelector(
    '[data-testid="memory-inherit-checkbox"]',
  ) as HTMLInputElement | null
  if (!node) throw new Error('inherit checkbox missing from rendered editor')
  return node
}

function clickSave(el: HTMLElement) {
  const buttons = Array.from(el.querySelectorAll('button')) as HTMLButtonElement[]
  const save = buttons.find((b) => b.textContent?.trim() === 'Save' || b.textContent?.trim() === 'Add')
  if (!save) throw new Error('save button missing')
  save.click()
}

describe('KnowledgeEntryEditor — PAI-348 inherit toggle', () => {
  afterEach(() => {
    document.body.innerHTML = ''
    vi.clearAllMocks()
  })

  it('defaults to checked when metadata.inherit is missing', async () => {
    const m = await mountEditor({
      slug: 'rule_one',
      title: 'Rule one',
      body: '',
      status: 'backlog',
      metadata: {},
    })
    expect(checkbox(m.el).checked).toBe(true)
    m.cleanup()
  })

  it('renders unchecked when metadata.inherit === false', async () => {
    const m = await mountEditor({
      slug: 'rule_two',
      title: 'Rule two',
      body: '',
      status: 'backlog',
      metadata: { inherit: false },
    })
    expect(checkbox(m.el).checked).toBe(false)
    m.cleanup()
  })

  it('falls back to checked when metadata.inherit is non-bool', async () => {
    // The server validator rejects non-bool values, but legacy entries
    // that slipped past pre-validation should not render as a
    // confusing tri-state.
    const m = await mountEditor({
      slug: 'rule_three',
      title: 'Rule three',
      body: '',
      status: 'backlog',
      metadata: { inherit: 'true' as unknown as boolean },
    })
    expect(checkbox(m.el).checked).toBe(true)
    m.cleanup()
  })

  it('round-trips inherit=true via save (omitted from payload)', async () => {
    const m = await mountEditor({
      slug: 'rule_four',
      title: 'Rule four',
      body: '',
      status: 'backlog',
      metadata: {},
    })
    clickSave(m.el)
    await nextTick()
    expect(m.saved).toHaveLength(1)
    // Default-true is omitted so the payload doesn't grow on existing
    // entries; the server defaults to true when the flag is absent.
    expect(m.saved[0].metadata).not.toHaveProperty('inherit')
    m.cleanup()
  })

  it('round-trips inherit=false via save (persisted in payload)', async () => {
    const m = await mountEditor({
      slug: 'rule_five',
      title: 'Rule five',
      body: '',
      status: 'backlog',
      metadata: {},
    })
    const cb = checkbox(m.el)
    cb.checked = false
    cb.dispatchEvent(new Event('change'))
    await nextTick()
    clickSave(m.el)
    await nextTick()
    expect(m.saved).toHaveLength(1)
    expect(m.saved[0].metadata.inherit).toBe(false)
    m.cleanup()
  })

  it('flipping back to inherit=true removes the field from the payload', async () => {
    const m = await mountEditor({
      slug: 'rule_six',
      title: 'Rule six',
      body: '',
      status: 'backlog',
      metadata: { inherit: false },
    })
    const cb = checkbox(m.el)
    expect(cb.checked).toBe(false)
    cb.checked = true
    cb.dispatchEvent(new Event('change'))
    await nextTick()
    clickSave(m.el)
    await nextTick()
    expect(m.saved).toHaveLength(1)
    expect(m.saved[0].metadata).not.toHaveProperty('inherit')
    m.cleanup()
  })
})
