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

/**
 * useAttachmentLightbox — singleton state for the global attachment lightbox.
 *
 * `AttachmentLightbox.vue` is mounted once in `AppLayout.vue` and reads from
 * this shared store. Any surface that wants to open an attachment calls
 * `openLightbox(attachments, startIndex)`; non-image attachments are filtered
 * out when the caller builds its list.
 */
import { ref, computed } from 'vue'
import type { Attachment } from '@/types'

export type LightboxAlignment = 'left' | 'center' | 'right' | 'full'
export type LightboxSize      = 'sm'   | 'md'     | 'lg'    | 'full'

const open         = ref(false)
const attachments  = ref<Attachment[]>([])
const currentIndex = ref(0)

const current = computed<Attachment | null>(() => {
  const list = attachments.value
  if (!list.length) return null
  const idx = ((currentIndex.value % list.length) + list.length) % list.length
  return list[idx] ?? null
})
const canStep = computed(() => attachments.value.length > 1)

function openLightbox(list: Attachment[], startIndex = 0) {
  if (!list.length) return
  attachments.value = list
  currentIndex.value = Math.max(0, Math.min(startIndex, list.length - 1))
  open.value = true
}

function close() {
  open.value = false
  attachments.value = []
  currentIndex.value = 0
}

function next() {
  if (!attachments.value.length) return
  currentIndex.value = (currentIndex.value + 1) % attachments.value.length
}

function prev() {
  if (!attachments.value.length) return
  const n = attachments.value.length
  currentIndex.value = (currentIndex.value - 1 + n) % n
}

function jumpTo(index: number) {
  if (!attachments.value.length) return
  const n = attachments.value.length
  currentIndex.value = Math.max(0, Math.min(index, n - 1))
}

/**
 * Build the HTML <img> snippet that users paste into a markdown textarea.
 * Class-based, so the markdown pipeline's DOMPurify pass keeps it intact
 * (default ALLOWED_ATTR permits `class`, `src`, `alt`).
 */
export function buildMarkdownReference(
  attachment: Pick<Attachment, 'id' | 'filename'>,
  align: LightboxAlignment,
  size:  LightboxSize,
): string {
  const safeAlt = attachment.filename.replace(/"/g, '&quot;')
  return `<img src="/api/attachments/${attachment.id}" alt="${safeAlt}" class="md-img md-img--${align} md-img--${size}" />`
}

export function useAttachmentLightbox() {
  return {
    open,
    attachments,
    currentIndex,
    current,
    canStep,
    openLightbox,
    close,
    next,
    prev,
    jumpTo,
  }
}
