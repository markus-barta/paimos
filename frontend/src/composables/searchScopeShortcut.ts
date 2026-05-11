/*
 * PAIMOS — Your Professional & Personal AI Project OS
 * Copyright (C) 2026 Markus Barta <markus@barta.com>
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as
 * published by the Free Software Foundation, version 3.
 */

// PAI-368: per-user search-scope shortcut. Replaces the hard-coded
// Ctrl+^ binding (PAI-364) which was unreachable on some layout/OS
// combos. The chord is recorded live in Settings → Account; we store
// modifier flags + KeyboardEvent.code so matching is layout-stable for
// the user who recorded it (the `code` is the physical key position).
// `key` and `label` are kept for display only.

export interface ShortcutChord {
  ctrl: boolean
  shift: boolean
  alt: boolean
  meta: boolean
  /** KeyboardEvent.code — physical key, layout-stable. */
  code: string
  /** KeyboardEvent.key at capture time — for display. */
  key: string
  /** Pre-computed human label (e.g. "Ctrl+Shift+^"). */
  label: string
}

/** Parse the stored string. Empty / malformed → null (disabled). */
export function parseShortcut(raw: string | null | undefined): ShortcutChord | null {
  if (!raw) return null
  try {
    const c = JSON.parse(raw) as Partial<ShortcutChord>
    if (!c || typeof c.code !== 'string' || !c.code) return null
    if (!(c.ctrl || c.alt || c.meta)) return null
    return {
      ctrl: !!c.ctrl,
      shift: !!c.shift,
      alt: !!c.alt,
      meta: !!c.meta,
      code: c.code,
      key: typeof c.key === 'string' ? c.key : '',
      label: typeof c.label === 'string' && c.label ? c.label : formatChordLabel(c as ShortcutChord),
    }
  } catch {
    return null
  }
}

/** Serialize for storage. */
export function serializeShortcut(c: ShortcutChord): string {
  return JSON.stringify(c)
}

/** True when this event matches the stored chord exactly. */
export function matchesShortcut(e: KeyboardEvent, c: ShortcutChord | null): boolean {
  if (!c) return false
  return (
    e.ctrlKey === c.ctrl &&
    e.shiftKey === c.shift &&
    e.altKey === c.alt &&
    e.metaKey === c.meta &&
    e.code === c.code
  )
}

/** Capture a KeyboardEvent into a chord. Returns null when only a
 *  modifier was pressed (no real key yet) or when no real modifier is
 *  held (shift alone collides with normal typing). */
export function captureChord(e: KeyboardEvent): ShortcutChord | null {
  // Ignore lone modifier keydowns — they fire as the user assembles
  // the chord. We want the moment a real key is pressed *with* mods.
  if (e.key === 'Control' || e.key === 'Shift' || e.key === 'Alt' || e.key === 'Meta') {
    return null
  }
  if (!(e.ctrlKey || e.altKey || e.metaKey)) {
    // Shift-only and no-mod chords are rejected — they'd trigger on
    // normal typing inside the search input.
    return null
  }
  const c: ShortcutChord = {
    ctrl: e.ctrlKey,
    shift: e.shiftKey,
    alt: e.altKey,
    meta: e.metaKey,
    code: e.code,
    key: e.key === 'Dead' ? '' : e.key,
    label: '',
  }
  c.label = formatChordLabel(c)
  return c
}

/** Build a human label like "Ctrl+Shift+^" from a chord. */
export function formatChordLabel(c: ShortcutChord): string {
  const parts: string[] = []
  if (c.meta) parts.push(isMac() ? 'Cmd' : 'Meta')
  if (c.ctrl) parts.push('Ctrl')
  if (c.alt) parts.push(isMac() ? 'Option' : 'Alt')
  if (c.shift) parts.push('Shift')
  parts.push(prettyKey(c))
  return parts.join('+')
}

function prettyKey(c: ShortcutChord): string {
  // Prefer the printable `key` if present and short. Falls back to
  // stripping the standard `code` prefixes (KeyA → A, Digit6 → 6,
  // Backquote → `, etc.) so users see something recognisable even
  // when the OS swallowed the dead-key composition.
  if (c.key && c.key.length === 1) return c.key.toUpperCase()
  if (c.key && /^[A-Z][a-z]+$/.test(c.key)) return c.key
  if (c.code.startsWith('Key')) return c.code.slice(3)
  if (c.code.startsWith('Digit')) return c.code.slice(5)
  if (c.code === 'Backquote') return '`'
  if (c.code === 'Minus') return '-'
  if (c.code === 'Equal') return '='
  if (c.code === 'Slash') return '/'
  if (c.code === 'Backslash') return '\\'
  if (c.code === 'BracketLeft') return '['
  if (c.code === 'BracketRight') return ']'
  if (c.code === 'Semicolon') return ';'
  if (c.code === 'Quote') return "'"
  if (c.code === 'Comma') return ','
  if (c.code === 'Period') return '.'
  if (c.code === 'Space') return 'Space'
  return c.code
}

function isMac(): boolean {
  // navigator.platform is deprecated; userAgent is the supported probe.
  return typeof navigator !== 'undefined' && /Mac|iPhone|iPad|iPod/i.test(navigator.userAgent)
}
