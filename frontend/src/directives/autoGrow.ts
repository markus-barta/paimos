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
 * v-auto-grow directive — makes a textarea grow with its content.
 * Min-height: 2 lines (~48px). Max-height: ~30 lines (~720px), then scrolls.
 */
import type { Directive } from 'vue'

const LINE_HEIGHT = 24  // px — matches 14px base font at 1.5 line-height (approx)
const MIN_LINES   = 2
const MAX_LINES   = 30

const MIN_H = LINE_HEIGHT * MIN_LINES   //  48px
const MAX_H = LINE_HEIGHT * MAX_LINES   // 720px

function resize(el: HTMLTextAreaElement) {
  el.style.height = 'auto'
  const h = Math.min(Math.max(el.scrollHeight, MIN_H), MAX_H)
  el.style.height = `${h}px`
  el.style.overflowY = el.scrollHeight > MAX_H ? 'auto' : 'hidden'
}

export const vAutoGrow: Directive<HTMLTextAreaElement> = {
  mounted(el) {
    el.style.resize = 'none'
    el.style.minHeight = `${MIN_H}px`
    // Defer initial resize to after browser layout — scrollHeight is not yet
    // correct when Vue mounts the textarea (content injected via v-model).
    requestAnimationFrame(() => resize(el))
    el.addEventListener('input', () => resize(el))
  },
  updated(el) {
    resize(el)
  },
  unmounted(el) {
    el.removeEventListener('input', () => resize(el))
  },
}
