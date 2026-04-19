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
 * Issue field registry — single source of truth for how fields render across
 * the five issue UIs.
 *
 * This file lands the registry + helpers only; no runtime consumers yet.
 * Follow-ups ACME-1 through ACME-1 refactor each surface to read from
 * here so field coverage, labels, and ordering stop drifting.
 *
 * Surfaces:
 *   create      — components/CreateIssueModal.vue + components/GlobalNewIssueModal.vue
 *   edit        — views/IssueDetailView.vue (edit mode) + components/issue/IssueEditSidebar.vue
 *   tableInline — components/IssueTable.vue + composables/useInlineEdit.ts
 *   view        — views/IssueDetailView.vue (view mode) + components/issue/IssueMetaGrid.vue
 *   quickPanel  — components/IssueSidePanel.vue
 *
 * Field coverage matrix (as of v1.1.12; * = gap to be filled by a follow-up ticket):
 *
 *   field                   create   edit   tblInline   view   quickPanel
 *   ─────────────────────── ──────── ────── ─────────── ────── ──────────
 *   title                   E        E      R  *E       R      E
 *   description             E        E      —           R      E
 *   acceptance_criteria     E        E      —           R      E
 *   notes                   E        E      —           R      E
 *   type                    E        E      R           R      R
 *   status                  E        E      E           R      E
 *   priority                E        E      E           R      E
 *   parent_id               E        E      —           R      —  *E
 *   assignee_id             E        E      E           R      E
 *   tags                    E        E      —           R      E
 *   cost_unit               E        E      E           R      E
 *   release                 E        E      E           R      E
 *   billing_type            E        E      —           R      —
 *   total_budget            E        E      —           R      —
 *   rate_hourly             E        E      —           R      —
 *   rate_lp                 E        E      —           R      —
 *   estimate_hours          —  *E    E      —  *E       R      E
 *   estimate_lp             —  *E    E      —  *E       R      E
 *   ar_hours                —        E      —           R      E
 *   ar_lp                   —        E      —           R      E
 *   time_override           —        E      —           R      E
 *   sprint_ids              —  *E    E      E           R      E
 *   start_date              E        E      —           R      —
 *   end_date                E        E      —           R      —
 *   jira_id                 E        E      —           R      —
 *   jira_version            E        E      —           R      —
 *   jira_text               E        E      —           R      —
 *   color                   —  *E    E      —           R      —
 *   group_state             E        E      —           R      —
 *   sprint_state            E        E      —           R      —
 *   created_at              —        —      —           R      R
 *   updated_at              —        —      —           R      R
 *   accepted_at             —        —      —           R      —
 *   invoiced_at             —        —      —           R      —
 *   booked_hours            —        —      —           R      R
 *
 *   E = editable, R = read-only, — = not shown.
 *   Entries with "X  *Y" mean "currently X, should become Y per ACME-1 audit".
 */

import type { Issue } from '@/types'

// ── Types ─────────────────────────────────────────────────────────────────

export type IssueSurface = 'create' | 'edit' | 'tableInline' | 'view' | 'quickPanel'

export type IssueFieldGroup =
  | 'core' | 'meta' | 'money' | 'time' | 'sprint' | 'dates' | 'jira' | 'audit'

export type IssueFieldComponent =
  | 'text' | 'textarea'
  | 'meta-select' | 'tag-selector'
  | 'sprint-picker' | 'parent-picker'
  | 'date' | 'money' | 'duration'
  | 'autocomplete' | 'color' | 'computed'

export interface IssueFieldDef {
  id: keyof Issue
  label: string
  group: IssueFieldGroup
  component: IssueFieldComponent
  visibleIn: IssueSurface[]
  editableIn: IssueSurface[]
  /** Optional type gate — return true when the field applies to the given issue. */
  gatedBy?: (issue: Partial<Issue>) => boolean
}

// ── Gate predicates ───────────────────────────────────────────────────────
// Encodes the existing visibility rules from CreateIssueModal / IssueEditSidebar.

const inTypes = (...types: Issue['type'][]) => (i: Partial<Issue>) =>
  !!i.type && types.includes(i.type)

const billingGated     = inTypes('epic', 'cost_unit')
const estimateGated    = inTypes('ticket', 'task', 'epic', 'cost_unit')
const datesGated       = inTypes('release', 'sprint')
const jiraIdGated      = inTypes('ticket', 'task')
const jiraVersionGated = inTypes('release')
const jiraTextGated    = inTypes('sprint')
const colorGated       = inTypes('epic')
const sprintableGated  = inTypes('ticket', 'task', 'epic')
const acceptanceGated  = inTypes('epic', 'cost_unit', 'ticket')
const groupStateGated  = inTypes('release')
const sprintStateGated = inTypes('sprint')

// ── Surface shortcuts ─────────────────────────────────────────────────────

const ALL: IssueSurface[]         = ['create', 'edit', 'tableInline', 'view', 'quickPanel']
const ALL_VIEWS: IssueSurface[]   = ['create', 'edit', 'view', 'quickPanel']
const FULL_FORMS: IssueSurface[]  = ['create', 'edit']
const DETAIL_ONLY: IssueSurface[] = ['edit', 'view']
const RICH_SURFACES: IssueSurface[] = ['edit', 'view', 'quickPanel']

// ── Registry ──────────────────────────────────────────────────────────────

export const ISSUE_FIELDS: IssueFieldDef[] = [
  // ── Core ────────────────────────────────────────────────────────────────
  { id: 'title',               label: 'Title',               group: 'core', component: 'text',          visibleIn: ALL,         editableIn: ['create', 'edit', 'quickPanel'] },
  { id: 'description',         label: 'Description',         group: 'core', component: 'textarea',      visibleIn: ALL_VIEWS,   editableIn: ['create', 'edit', 'quickPanel'] },
  { id: 'acceptance_criteria', label: 'Acceptance Criteria', group: 'core', component: 'textarea',      visibleIn: ALL_VIEWS,   editableIn: ['create', 'edit', 'quickPanel'], gatedBy: acceptanceGated },
  { id: 'notes',               label: 'Notes',               group: 'core', component: 'textarea',      visibleIn: ALL_VIEWS,   editableIn: ['create', 'edit', 'quickPanel'] },
  { id: 'type',                label: 'Type',                group: 'core', component: 'meta-select',   visibleIn: ALL,         editableIn: ['create', 'edit'] },
  { id: 'status',              label: 'Status',              group: 'core', component: 'meta-select',   visibleIn: ALL,         editableIn: ['create', 'edit', 'tableInline', 'quickPanel'] },
  { id: 'priority',            label: 'Priority',            group: 'core', component: 'meta-select',   visibleIn: ALL,         editableIn: ['create', 'edit', 'tableInline', 'quickPanel'] },
  { id: 'parent_id',           label: 'Parent',              group: 'core', component: 'parent-picker', visibleIn: ['create', 'edit', 'view'], editableIn: ['create', 'edit'] },
  { id: 'assignee_id',         label: 'Assignee',            group: 'core', component: 'meta-select',   visibleIn: ALL,         editableIn: ['create', 'edit', 'tableInline', 'quickPanel'] },
  { id: 'tags',                label: 'Tags',                group: 'core', component: 'tag-selector',  visibleIn: ALL_VIEWS,   editableIn: ['create', 'edit', 'quickPanel'] },

  // ── Meta ────────────────────────────────────────────────────────────────
  { id: 'cost_unit', label: 'Cost Unit', group: 'meta', component: 'autocomplete', visibleIn: ALL, editableIn: ['create', 'edit', 'tableInline', 'quickPanel'] },
  { id: 'release',   label: 'Release',   group: 'meta', component: 'autocomplete', visibleIn: ALL, editableIn: ['create', 'edit', 'tableInline', 'quickPanel'] },

  // ── Money (epic / cost_unit only) ───────────────────────────────────────
  { id: 'billing_type', label: 'Billing Type', group: 'money', component: 'meta-select', visibleIn: FULL_FORMS.concat('view'), editableIn: FULL_FORMS, gatedBy: billingGated },
  { id: 'total_budget', label: 'Budget',       group: 'money', component: 'money',       visibleIn: FULL_FORMS.concat('view'), editableIn: FULL_FORMS, gatedBy: billingGated },
  { id: 'rate_hourly',  label: 'Rate / Hour',  group: 'money', component: 'money',       visibleIn: FULL_FORMS.concat('view'), editableIn: FULL_FORMS, gatedBy: billingGated },
  { id: 'rate_lp',      label: 'Rate / LP',    group: 'money', component: 'money',       visibleIn: FULL_FORMS.concat('view'), editableIn: FULL_FORMS, gatedBy: billingGated },

  // ── Time (ticket / task / epic / cost_unit) ─────────────────────────────
  { id: 'estimate_hours', label: 'Estimate (h)',  group: 'time', component: 'duration', visibleIn: RICH_SURFACES, editableIn: ['edit', 'quickPanel'], gatedBy: estimateGated },
  { id: 'estimate_lp',    label: 'Estimate (LP)', group: 'time', component: 'duration', visibleIn: RICH_SURFACES, editableIn: ['edit', 'quickPanel'], gatedBy: estimateGated },
  { id: 'ar_hours',       label: 'AR (h)',        group: 'time', component: 'duration', visibleIn: RICH_SURFACES, editableIn: ['edit', 'quickPanel'], gatedBy: estimateGated },
  { id: 'ar_lp',          label: 'AR (LP)',       group: 'time', component: 'duration', visibleIn: RICH_SURFACES, editableIn: ['edit', 'quickPanel'], gatedBy: estimateGated },
  { id: 'time_override',  label: 'Time Override', group: 'time', component: 'duration', visibleIn: RICH_SURFACES, editableIn: ['edit', 'quickPanel'], gatedBy: estimateGated },

  // ── Sprint membership ───────────────────────────────────────────────────
  { id: 'sprint_ids', label: 'Sprints', group: 'sprint', component: 'sprint-picker', visibleIn: ['edit', 'tableInline', 'view', 'quickPanel'], editableIn: ['edit', 'tableInline', 'quickPanel'], gatedBy: sprintableGated },

  // ── Dates (release / sprint only) ───────────────────────────────────────
  { id: 'start_date', label: 'Start Date', group: 'dates', component: 'date', visibleIn: FULL_FORMS.concat('view'), editableIn: FULL_FORMS, gatedBy: datesGated },
  { id: 'end_date',   label: 'End Date',   group: 'dates', component: 'date', visibleIn: FULL_FORMS.concat('view'), editableIn: FULL_FORMS, gatedBy: datesGated },

  // ── Jira integration ────────────────────────────────────────────────────
  { id: 'jira_id',      label: 'Jira ID',      group: 'jira', component: 'text', visibleIn: FULL_FORMS.concat('view'), editableIn: FULL_FORMS, gatedBy: jiraIdGated },
  { id: 'jira_version', label: 'Jira Version', group: 'jira', component: 'text', visibleIn: FULL_FORMS.concat('view'), editableIn: FULL_FORMS, gatedBy: jiraVersionGated },
  { id: 'jira_text',    label: 'Jira Text',    group: 'jira', component: 'text', visibleIn: FULL_FORMS.concat('view'), editableIn: FULL_FORMS, gatedBy: jiraTextGated },

  // ── Visual accents + group/sprint state ─────────────────────────────────
  { id: 'color',        label: 'Color',        group: 'meta', component: 'color',       visibleIn: DETAIL_ONLY,              editableIn: ['edit'],   gatedBy: colorGated },
  { id: 'group_state',  label: 'Group State',  group: 'meta', component: 'meta-select', visibleIn: FULL_FORMS.concat('view'), editableIn: FULL_FORMS, gatedBy: groupStateGated },
  { id: 'sprint_state', label: 'Sprint State', group: 'meta', component: 'meta-select', visibleIn: FULL_FORMS.concat('view'), editableIn: FULL_FORMS, gatedBy: sprintStateGated },

  // ── Audit (read-only) ───────────────────────────────────────────────────
  { id: 'created_at',           label: 'Created',         group: 'audit', component: 'computed', visibleIn: ['view', 'quickPanel'], editableIn: [] },
  { id: 'updated_at',           label: 'Updated',         group: 'audit', component: 'computed', visibleIn: ['view', 'quickPanel'], editableIn: [] },
  { id: 'created_by_name',      label: 'Created By',      group: 'audit', component: 'computed', visibleIn: ['view'],               editableIn: [] },
  { id: 'last_changed_by_name', label: 'Last Changed By', group: 'audit', component: 'computed', visibleIn: ['view'],               editableIn: [] },
  { id: 'accepted_at',          label: 'Accepted',        group: 'audit', component: 'computed', visibleIn: ['view'],               editableIn: [] },
  { id: 'invoiced_at',          label: 'Invoiced',        group: 'audit', component: 'computed', visibleIn: ['view'],               editableIn: [] },
  { id: 'booked_hours',         label: 'Booked',          group: 'audit', component: 'computed', visibleIn: ['view', 'quickPanel'], editableIn: [] },
]

// ── Helpers ───────────────────────────────────────────────────────────────

/**
 * Fields that should be rendered in the given surface for the given issue.
 * Honors both `visibleIn` and any `gatedBy` predicate.
 */
export function fieldsFor(surface: IssueSurface, issue: Partial<Issue>): IssueFieldDef[] {
  return ISSUE_FIELDS.filter(f =>
    f.visibleIn.includes(surface) && (!f.gatedBy || f.gatedBy(issue)),
  )
}

/**
 * Whether the given field is editable in the given surface for the given issue.
 * Returns false if the gate predicate rejects the issue, even if `editableIn`
 * includes the surface.
 */
export function isEditable(field: IssueFieldDef, surface: IssueSurface, issue: Partial<Issue>): boolean {
  if (!field.editableIn.includes(surface)) return false
  if (field.gatedBy && !field.gatedBy(issue)) return false
  return true
}

/**
 * Canonical label for a field — the one source of truth.
 * Surfaces must read labels through this helper instead of hard-coding strings,
 * so a change here propagates to all five UIs at once.
 */
export function fieldLabel(id: keyof Issue): string {
  return ISSUE_FIELDS.find(f => f.id === id)?.label ?? String(id)
}

/**
 * Look up a registry entry by id. Undefined if unknown.
 */
export function fieldDef(id: keyof Issue): IssueFieldDef | undefined {
  return ISSUE_FIELDS.find(f => f.id === id)
}

// Compile-time assertion: every registered id is a real key of Issue.
// IssueFieldDef.id is already typed `keyof Issue`, but this line documents the
// intent and gives a second safety net if the interface is refactored.
const _schemaCheck: readonly (keyof Issue)[] = ISSUE_FIELDS.map(f => f.id)
void _schemaCheck
