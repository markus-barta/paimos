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

import { describe, it, expect, beforeEach } from 'vitest'
import { useAttachmentLightbox, buildMarkdownReference } from './useAttachmentLightbox'
import type { Attachment } from '@/types'

function makeAttachment(id: number, filename = `img-${id}.png`): Attachment {
  return {
    id,
    issue_id: 1,
    object_key: `key-${id}`,
    filename,
    content_type: 'image/png',
    size_bytes: 100,
    uploaded_by: 1,
    uploader: 'mba',
    created_at: '2026-04-14',
  }
}

describe('useAttachmentLightbox', () => {
  beforeEach(() => {
    // Reset singleton state between tests.
    const { close } = useAttachmentLightbox()
    close()
  })

  it('opens with the given list and start index', () => {
    const lb = useAttachmentLightbox()
    const atts = [makeAttachment(1), makeAttachment(2), makeAttachment(3)]
    lb.openLightbox(atts, 1)
    expect(lb.open.value).toBe(true)
    expect(lb.currentIndex.value).toBe(1)
    expect(lb.current.value?.id).toBe(2)
    expect(lb.canStep.value).toBe(true)
  })

  it('clamps start index into range', () => {
    const lb = useAttachmentLightbox()
    lb.openLightbox([makeAttachment(1), makeAttachment(2)], 99)
    expect(lb.currentIndex.value).toBe(1)
    lb.close()
    lb.openLightbox([makeAttachment(1), makeAttachment(2)], -10)
    expect(lb.currentIndex.value).toBe(0)
  })

  it('refuses to open with an empty list', () => {
    const lb = useAttachmentLightbox()
    lb.openLightbox([], 0)
    expect(lb.open.value).toBe(false)
  })

  it('next wraps to 0 past the end', () => {
    const lb = useAttachmentLightbox()
    const atts = [makeAttachment(1), makeAttachment(2), makeAttachment(3)]
    lb.openLightbox(atts, 2)
    lb.next()
    expect(lb.currentIndex.value).toBe(0)
    expect(lb.current.value?.id).toBe(1)
  })

  it('prev wraps to last from 0', () => {
    const lb = useAttachmentLightbox()
    const atts = [makeAttachment(1), makeAttachment(2), makeAttachment(3)]
    lb.openLightbox(atts, 0)
    lb.prev()
    expect(lb.currentIndex.value).toBe(2)
    expect(lb.current.value?.id).toBe(3)
  })

  it('canStep is false for single-image lists', () => {
    const lb = useAttachmentLightbox()
    lb.openLightbox([makeAttachment(1)], 0)
    expect(lb.canStep.value).toBe(false)
  })

  it('close resets state', () => {
    const lb = useAttachmentLightbox()
    lb.openLightbox([makeAttachment(1), makeAttachment(2)], 1)
    lb.close()
    expect(lb.open.value).toBe(false)
    expect(lb.attachments.value).toHaveLength(0)
    expect(lb.currentIndex.value).toBe(0)
    expect(lb.current.value).toBeNull()
  })

  it('jumpTo clamps index into range', () => {
    const lb = useAttachmentLightbox()
    lb.openLightbox([makeAttachment(1), makeAttachment(2), makeAttachment(3)], 0)
    lb.jumpTo(99)
    expect(lb.currentIndex.value).toBe(2)
    lb.jumpTo(-5)
    expect(lb.currentIndex.value).toBe(0)
  })
})

describe('buildMarkdownReference', () => {
  const att = { id: 42, filename: 'screenshot.png' }

  it('builds a left-aligned medium reference', () => {
    expect(buildMarkdownReference(att, 'left', 'md'))
      .toBe('<img src="/api/attachments/42" alt="screenshot.png" class="md-img md-img--left md-img--md" />')
  })

  it('builds a full-width reference', () => {
    expect(buildMarkdownReference(att, 'full', 'full'))
      .toBe('<img src="/api/attachments/42" alt="screenshot.png" class="md-img md-img--full md-img--full" />')
  })

  it('escapes double quotes in filenames so the alt attribute stays valid', () => {
    expect(buildMarkdownReference({ id: 7, filename: 'my "quoted" file.png' }, 'center', 'lg'))
      .toBe('<img src="/api/attachments/7" alt="my &quot;quoted&quot; file.png" class="md-img md-img--center md-img--lg" />')
  })

  it('supports every alignment × size combination without throwing', () => {
    const aligns: Array<'left'|'center'|'right'|'full'> = ['left','center','right','full']
    const sizes: Array<'sm'|'md'|'lg'|'full'> = ['sm','md','lg','full']
    for (const a of aligns) {
      for (const s of sizes) {
        const html = buildMarkdownReference(att, a, s)
        expect(html).toContain(`md-img--${a}`)
        expect(html).toContain(`md-img--${s}`)
        expect(html).toContain('src="/api/attachments/42"')
      }
    }
  })
})
