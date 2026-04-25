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

// PAI-148. Line-level LCS diff used by the AI optimize preview overlay.
//
// Extracted from the Vue SFC so it can be unit-tested without mounting
// a component. The component imports lineDiff from here verbatim.
//
// Why LCS-by-line and not a full diff library:
//
//   - The output of the LLM is short (the same field's content,
//     rewritten — typically <2 KB), so the O(m·n) DP is comfortable.
//   - We want a stable column-aligned view, which a Myers-style hunk
//     diff doesn't directly give us. The LCS table makes alignment
//     obvious: deletions go on the left, insertions on the right,
//     and unchanged lines anchor the rows.
//   - Adding a diff library for a single use site bloats the bundle
//     for no real win.

export type DiffLineType =
  /** Unchanged line — present on both sides, same text. */
  | 'eq'
  /** Removed line — present on the left only. */
  | 'del'
  /** Added line — present on the right only. */
  | 'add'
  /** Empty padding row — keeps the two columns visually aligned. */
  | 'pad'

export interface DiffLine {
  type: DiffLineType
  text: string
}

export interface LineDiffResult {
  left: DiffLine[]
  right: DiffLine[]
}

/**
 * lineDiff computes a column-aligned line-level diff using the
 * standard suffix-LCS DP. The two returned arrays always have the
 * same length: each row is one of (eq, eq), (del, pad), or (pad,
 * add). That contract is what lets the SFC render the two columns
 * side-by-side without per-row alignment logic.
 */
export function lineDiff(a: string, b: string): LineDiffResult {
  const aLines = a.split('\n')
  const bLines = b.split('\n')
  const m = aLines.length
  const n = bLines.length

  // dp[i][j] = LCS length for aLines[i..] vs bLines[j..]
  const dp: number[][] = Array.from({ length: m + 1 }, () => new Array<number>(n + 1).fill(0))
  for (let i = m - 1; i >= 0; i--) {
    for (let j = n - 1; j >= 0; j--) {
      if (aLines[i] === bLines[j]) dp[i][j] = dp[i + 1][j + 1] + 1
      else dp[i][j] = Math.max(dp[i + 1][j], dp[i][j + 1])
    }
  }

  const left: DiffLine[] = []
  const right: DiffLine[] = []
  let i = 0
  let j = 0
  while (i < m && j < n) {
    if (aLines[i] === bLines[j]) {
      left.push({ type: 'eq', text: aLines[i] })
      right.push({ type: 'eq', text: bLines[j] })
      i++; j++
    } else if (dp[i + 1][j] >= dp[i][j + 1]) {
      left.push({ type: 'del', text: aLines[i] })
      right.push({ type: 'pad', text: '' })
      i++
    } else {
      left.push({ type: 'pad', text: '' })
      right.push({ type: 'add', text: bLines[j] })
      j++
    }
  }
  while (i < m) {
    left.push({ type: 'del', text: aLines[i++] })
    right.push({ type: 'pad', text: '' })
  }
  while (j < n) {
    left.push({ type: 'pad', text: '' })
    right.push({ type: 'add', text: bLines[j++] })
  }
  return { left, right }
}
