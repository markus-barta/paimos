/*
 * PAIMOS — Your Professional & Personal AI Project OS
 * Copyright (C) 2026 Markus Barta <markus@barta.com>
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as
 * published by the Free Software Foundation, version 3.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
 * GNU Affero General Public License for more details.
 *
 * You should have received a copy of the GNU Affero General Public
 * License along with this program. If not, see <https://www.gnu.org/licenses/>.
 */

import { describe, it, expect } from 'vitest'
import { ref } from 'vue'
import { useMarkdown } from './useMarkdown'

/**
 * Regression guard: the alignment classes emitted by the lightbox's
 * "Copy reference" button (md-img, md-img--left, md-img--md, …) must
 * survive the marked → DOMPurify pipeline. A DOMPurify upgrade that
 * tightened ALLOWED_ATTR could silently strip them and break alignment
 * rendering without any type error.
 */
describe('useMarkdown + DOMPurify', () => {
  it('preserves md-img class attributes on <img> tags', () => {
    const src = ref('<img src="/api/attachments/42" alt="x" class="md-img md-img--left md-img--md" />')
    const enabled = ref(true)
    const { html } = useMarkdown(src, enabled)
    expect(html.value).toContain('md-img')
    expect(html.value).toContain('md-img--left')
    expect(html.value).toContain('md-img--md')
    expect(html.value).toContain('src="/api/attachments/42"')
  })

  it('still sanitises dangerous attributes on <img>', () => {
    const src = ref('<img src="/api/attachments/42" alt="x" onerror="alert(1)" class="md-img" />')
    const enabled = ref(true)
    const { html } = useMarkdown(src, enabled)
    expect(html.value).not.toContain('onerror')
    // Class should still be intact even with the dangerous attr stripped.
    expect(html.value).toContain('md-img')
  })

  it('renders plain markdown images without classes', () => {
    const src = ref('![hi](/api/attachments/1)')
    const enabled = ref(true)
    const { html } = useMarkdown(src, enabled)
    expect(html.value).toContain('<img')
    expect(html.value).toContain('src="/api/attachments/1"')
  })
})
