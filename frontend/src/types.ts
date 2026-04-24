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

export interface Attachment {
  id: number
  issue_id: number
  object_key: string
  filename: string
  content_type: string
  size_bytes: number
  uploaded_by: number
  uploader: string
  created_at: string
}

export interface Tag {
  id: number
  name: string
  color: string
  description: string
  system?: boolean
  created_at: string
}

export interface Project {
  id: number
  name: string
  key: string
  description: string
  status: 'active' | 'archived' | 'deleted'
  product_owner: number | null
  // PAI-54: customer_label is the freeform legacy text; customer_id is
  // the FK into the customers table (PAI-53). Both nullable.
  customer_label: string
  customer_id: number | null
  customer_name?: string
  created_at: string
  updated_at: string
  issue_count: number
  logo_path: string
  last_activity: string
  open_issue_count: number
  done_issue_count: number
  active_issue_count: number
  tags: Tag[]
  rate_hourly?: number | null
  rate_lp?: number | null
  // PAI-54: effective rates after customer→project cascading; the
  // backend computes these so the UI doesn't have to.
  effective_rate_hourly?: number | null
  effective_rate_lp?: number | null
  rate_inherited?: boolean
}

export interface IssueRelation {
  source_id: number
  target_id: number
  // Convention: source = container/owner, target = member/child.
  // sprint:     source = sprint, target = member issue.
  // groups:     source = epic/cost_unit/release, target = ticket.
  // depends_on:   source = dependent, target = dependency.
  // impacts:      source = impactor, target = impacted.
  // follows_from: source = spin-off, target = predecessor (PAI-89).
  // blocks:       source = blocker, target = blocked (PAI-89).
  // related:      loose "see also" — direction is cosmetic (PAI-89).
  type: 'groups' | 'sprint' | 'depends_on' | 'impacts' | 'follows_from' | 'blocks' | 'related'
  target_key?: string
  target_title?: string
  // "outgoing" when the issue whose /relations endpoint was called is
  // this row's source_id, "incoming" otherwise. Lets the UI render
  // inverse labels without storing a second row. Added in PAI-89.
  direction?: 'outgoing' | 'incoming'
}

export type IssueStatus = 'new' | 'backlog' | 'in-progress' | 'qa' | 'done' | 'delivered' | 'accepted' | 'invoiced' | 'cancelled'
export type IssuePriority = 'low' | 'medium' | 'high'
export type IssueType = 'epic' | 'cost_unit' | 'release' | 'sprint' | 'ticket' | 'task'

export interface Issue {
  id: number
  project_id: number | null
  issue_number: number
  issue_key: string
  type: IssueType
  parent_id: number | null
  title: string
  description: string
  acceptance_criteria: string
  notes: string
  status: IssueStatus
  priority: IssuePriority
  cost_unit: string
  release: string
  // v2 group/sprint fields (nullable)
  billing_type: 'time_and_material' | 'fixed_price' | 'mixed' | null
  total_budget: number | null
  rate_hourly: number | null
  rate_lp: number | null
  estimate_hours: number | null
  estimate_lp: number | null
  ar_hours: number | null
  ar_lp: number | null
  time_override: number | null
  start_date: string | null
  end_date: string | null
  group_state: string | null
  sprint_state: string | null
  jira_id: string | null
  jira_version: string | null
  jira_text: string | null
  // epic color — optional visual accent for epic badges
  color: string | null
  // sprint membership — IDs of sprint issues this issue belongs to
  sprint_ids: number[]
  archived: boolean
  assignee_id: number | null
  assignee: { id: number; username: string; role: string } | null
  children?: Issue[]
  tags: Tag[]
  created_at: string
  updated_at: string
  created_by: number | null
  created_by_name: string
  last_changed_by_name: string
  booked_hours: number
  time_logged: number
  time_rollup: number
  time_total: number
  accepted_at: string | null
  accepted_by: number | null
  invoiced_at: string | null
  invoice_number: string
}

export interface TimeEntry {
  id: number
  issue_id: number   // renamed from ticket_id in migration 32
  user_id: number
  username: string
  started_at: string
  stopped_at: string | null
  override: number | null
  comment: string
  created_at: string
  internal_rate_hourly: number | null
  hours: number | null
  issue_key?: string
  issue_title?: string
  project_id?: number
}

export interface SavedView {
  id: number
  user_id: number
  owner_username: string
  title: string
  description: string
  columns_json: string   // JSON array of hidden column keys
  filters_json: string   // JSON object matching SavedFilters shape
  is_shared: boolean
  is_admin_default: boolean
  sort_order: number
  hidden: boolean
  pinned: boolean | null  // per-user pin state; null = no explicit choice (lazy init)
  created_at: string
  updated_at: string
}

export interface Sprint {
  id: number
  title: string
  start_date: string
  end_date: string
  archived: boolean
  sprint_state: string
  target_ar?: number | null
}

// Canonical User — used everywhere. Local interface stubs are deprecated.
export interface User {
  id: number
  username: string
  role: 'admin' | 'member' | 'external'
  status: 'active' | 'inactive' | 'deleted'
  nickname: string
  first_name: string
  last_name: string
  email: string
  avatar_path: string
  markdown_default: boolean
  monospace_fields: boolean
  recent_projects_limit: number
  internal_rate_hourly: number | null
  show_alt_unit_table: boolean
  show_alt_unit_detail: boolean
  locale: string
  preview_hover_delay: number
  last_login_at: string | null
  created_at: string
  totp_enabled: boolean
}

// Tag color palette — must match backend ValidColors
export const TAG_COLORS = [
  'gray', 'slate', 'blue', 'indigo', 'purple',
  'pink', 'red', 'orange', 'yellow', 'green', 'teal', 'cyan',
] as const

export type TagColor = typeof TAG_COLORS[number]
