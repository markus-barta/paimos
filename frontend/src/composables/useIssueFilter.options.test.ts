import { describe, expect, it } from 'vitest'

import { PRIORITY_OPTIONS, STATUS_OPTIONS, TYPE_OPTIONS } from '@/composables/useIssueFilter'
import { ISSUE_PRIORITIES, ISSUE_STATUSES, ISSUE_TYPES } from '@/types/generated/schema'

// PAI-489 / PAI-490 — canonical-value guard.
//
// The backend now strictly rejects non-canonical enum values with a 400
// (PAI-484), so the frontend must submit the canonical lowercase WIRE value,
// never the proper-case display label. The select option lists carry both:
// `value` (submitted) + `label` (shown). These options are hand-built, so the
// schema:check CI gate — which only covers src/types/generated/schema.ts —
// does NOT catch drift here. These guards do: they fail if a backend enum
// changes and an option list falls behind, or if any option ever binds a
// non-canonical value.

const sorted = (a: readonly string[]) => [...a].sort()

describe('enum option lists stay canonical + in sync with the schema (PAI-489)', () => {
  it('STATUS_OPTIONS values exactly match the canonical statuses', () => {
    expect(sorted(STATUS_OPTIONS.map((o) => o.value))).toEqual(sorted(ISSUE_STATUSES))
  })

  it('TYPE_OPTIONS values exactly match the canonical types', () => {
    expect(sorted(TYPE_OPTIONS.map((o) => o.value))).toEqual(sorted(ISSUE_TYPES))
  })

  it('PRIORITY_OPTIONS values exactly match the canonical priorities', () => {
    expect(sorted(PRIORITY_OPTIONS.map((o) => o.value))).toEqual(sorted(ISSUE_PRIORITIES))
  })

  it('every option submits the canonical wire value, not the display label', () => {
    for (const opt of [...STATUS_OPTIONS, ...TYPE_OPTIONS, ...PRIORITY_OPTIONS]) {
      // canonical wire values are lowercase; labels are proper-case display text
      expect(opt.value).toBe(opt.value.toLowerCase())
      expect(opt.value).not.toBe(opt.label)
    }
  })
})
