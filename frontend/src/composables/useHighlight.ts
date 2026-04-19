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
 * useHighlight — wraps FTS query matches in <mark> tags for display.
 *
 * Two modes:
 * 1. highlight(text, query) — for plain text. HTML-escapes, then wraps matches in <mark>.
 * 2. highlightDom(el, query) — for rendered HTML (v-html). Walks DOM text nodes and
 *    wraps matches without corrupting the markup.
 */
import { escapeHtml } from '@/utils/html'

export function highlight(text: string, query: string): string {
  if (!text) return ''
  if (!query || query.trim().length < 2) return escapeHtml(text)

  const tokens = query
    .trim()
    .split(/\s+/)
    .filter(t => t.length >= 2)
    .map(t => escapeRegex(t))

  if (!tokens.length) return escapeHtml(text)

  return escapeHtml(text).replace(
    new RegExp(`(${tokens.map(t => escapeRegex(escapeHtml(t))).join('|')})`, 'gi'),
    '<mark class="search-highlight">$1</mark>'
  )
}

/**
 * Apply search highlighting directly to DOM text nodes inside `el`.
 * Safe for use after v-html — only touches text nodes, never raw HTML strings.
 */
export function highlightDom(el: HTMLElement, query: string): void {
  // First remove any existing highlights
  clearHighlights(el)

  if (!query || query.trim().length < 2) return

  const tokens = query.trim().split(/\s+/).filter(t => t.length >= 2).map(t => escapeRegex(t))
  if (!tokens.length) return

  const pattern = new RegExp(`(${tokens.join('|')})`, 'gi')
  walkTextNodes(el, pattern)
}

function clearHighlights(el: HTMLElement): void {
  const marks = el.querySelectorAll('mark.search-highlight')
  marks.forEach(mark => {
    const parent = mark.parentNode
    if (!parent) return
    while (mark.firstChild) parent.insertBefore(mark.firstChild, mark)
    parent.removeChild(mark)
    parent.normalize()
  })
}

function walkTextNodes(node: Node, pattern: RegExp): void {
  if (node.nodeType === Node.TEXT_NODE) {
    const text = node.textContent ?? ''
    if (!text.trim()) return

    pattern.lastIndex = 0
    if (!pattern.test(text)) return

    const frag = document.createDocumentFragment()
    let lastIndex = 0
    pattern.lastIndex = 0
    let match: RegExpExecArray | null

    while ((match = pattern.exec(text)) !== null) {
      if (match.index > lastIndex) {
        frag.appendChild(document.createTextNode(text.slice(lastIndex, match.index)))
      }
      const mark = document.createElement('mark')
      mark.className = 'search-highlight'
      mark.textContent = match[1]
      frag.appendChild(mark)
      lastIndex = pattern.lastIndex
    }

    if (lastIndex < text.length) {
      frag.appendChild(document.createTextNode(text.slice(lastIndex)))
    }

    node.parentNode?.replaceChild(frag, node)
    return
  }

  if (node.nodeType === Node.ELEMENT_NODE) {
    // Skip <mark> elements we just created
    if ((node as Element).tagName === 'MARK' && (node as Element).classList.contains('search-highlight')) return
    // Iterate backwards since we may modify child list
    const children = Array.from(node.childNodes)
    for (const child of children) {
      walkTextNodes(child, pattern)
    }
  }
}


function escapeRegex(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}
