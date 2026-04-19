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

import { computed, type Ref } from 'vue'
import { marked } from 'marked'
import DOMPurify from 'dompurify'

// Configure marked once — gfm (GitHub Flavored Markdown), no async renderer.
marked.setOptions({ gfm: true, breaks: true })

/**
 * useMarkdown — convert text to rendered HTML when enabled.
 *
 * @param text    reactive text source
 * @param enabled reactive flag — if true, parse as Markdown; if false, return escaped plain text
 * @returns       { html: ComputedRef<string> }
 */
export function useMarkdown(text: Ref<string>, enabled: Ref<boolean>) {
  const html = computed(() => {
    const src = text.value ?? ''
    if (!src) return ''
    if (!enabled.value) {
      // Plain text: escape HTML entities and preserve newlines
      return src
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/\n/g, '<br>')
    }
    const rendered = marked.parse(src) as string
    return DOMPurify.sanitize(rendered)
  })

  return { html }
}
