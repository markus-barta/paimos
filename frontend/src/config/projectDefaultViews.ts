import type { SavedView } from '@/types'

export const FALLBACK_PROJECT_VIEWS: SavedView[] = [
  {
    id: -100, user_id: 0, owner_username: 'system', title: 'Issues',
    description: 'Tickets and tasks.',
    columns_json: '["billing_type","total_budget","rate_hourly","rate_lp","estimate_hours","estimate_lp","ar_hours","ar_lp","group_state","sprint_state","jira_id","jira_version","jira_text"]',
    filters_json: '{"type":["ticket","task"],"treeView":false}',
    is_shared: true, is_admin_default: true, sort_order: 0, hidden: false, pinned: null, created_at: '', updated_at: '',
  },
  {
    id: -101, user_id: 0, owner_username: 'system', title: 'Epics',
    description: 'Epic planning view.',
    columns_json: '["cost_unit","release","sprint","sprint_state","jira_id","jira_version","jira_text"]',
    filters_json: '{"type":["epic"],"treeView":false}',
    is_shared: true, is_admin_default: true, sort_order: 1, hidden: false, pinned: null, created_at: '', updated_at: '',
  },
  {
    id: -102, user_id: 0, owner_username: 'system', title: 'Cost Units',
    description: 'Cost unit overview.',
    columns_json: '["epic","sprint","sprint_state","jira_id","jira_version","jira_text"]',
    filters_json: '{"type":["cost_unit"],"treeView":false}',
    is_shared: true, is_admin_default: true, sort_order: 2, hidden: false, pinned: null, created_at: '', updated_at: '',
  },
  {
    id: -103, user_id: 0, owner_username: 'system', title: 'Releases',
    description: 'Release planning.',
    columns_json: '["billing_type","total_budget","rate_hourly","rate_lp","estimate_hours","estimate_lp","ar_hours","ar_lp","sprint_state","jira_id","jira_version","jira_text"]',
    filters_json: '{"type":["release"],"treeView":false}',
    is_shared: true, is_admin_default: true, sort_order: 3, hidden: false, pinned: null, created_at: '', updated_at: '',
  },
]

export function buildProjectDisplayTabs(allViews: SavedView[]): SavedView[] {
  const defaults = allViews
    .filter((view) => view.is_admin_default && (!view.hidden || view.pinned === true) && view.pinned !== false)
    .sort((a, b) => a.sort_order - b.sort_order || a.title.localeCompare(b.title))

  const pinnedPersonal = allViews
    .filter((view) => !view.is_admin_default && view.pinned === true)
    .sort((a, b) => a.title.localeCompare(b.title))

  const tabs = [...defaults, ...pinnedPersonal]
  return tabs.length ? tabs : FALLBACK_PROJECT_VIEWS
}
