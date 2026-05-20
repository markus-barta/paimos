import { afterEach, describe, expect, it } from 'vitest'
import { createApp, defineComponent, h, nextTick } from 'vue'

import i18n from '@/i18n'
import IssueTable from './IssueTable.vue'
import type { ColumnDef, RowAction } from './types'
import type { Issue } from '@/types'

function makeIssue(id: number, overrides: Partial<Issue> = {}): Issue {
  return {
    id,
    issue_key: `KEY-${id}`,
    type: 'ticket',
    status: 'in-progress',
    priority: 'medium',
    title: `Issue ${id}`,
    ...overrides,
  } as unknown as Issue
}

const COLUMNS: ColumnDef[] = [
  { key: 'key', label: 'Key', sortable: true, render: (i: Issue) => i.issue_key },
  { key: 'title', label: 'Title', render: (i: Issue) => i.title },
  {
    key: 'status',
    label: 'Status',
    render: (i: Issue) => h('span', { class: 'status-badge' }, i.status),
  },
  { key: 'priority', label: 'Priority', render: (i: Issue) => i.priority },
]

function mountTable(opts: {
  issues: Issue[]
  rowActions?: (issue: Issue) => RowAction[]
  sort?: { col: string; dir: 'asc' | 'desc' }
}) {
  const el = document.createElement('div')
  document.body.appendChild(el)
  const events: { type: string; payload: unknown }[] = []
  const Host = defineComponent({
    render() {
      return h(IssueTable, {
        issues: opts.issues,
        columns: COLUMNS,
        rowActions: opts.rowActions,
        sort: opts.sort,
        onSort: (col: string) => events.push({ type: 'sort', payload: col }),
        onRowClick: (issue: Issue) =>
          events.push({ type: 'row-click', payload: issue.id }),
      })
    },
  })
  const app = createApp(Host)
  app.use(i18n)
  app.mount(el)
  return {
    el,
    events,
    unmount() {
      app.unmount()
      el.remove()
    },
  }
}

describe('IssueTable — PAI-468', () => {
  afterEach(() => {
    document.body.innerHTML = ''
  })

  it('renders a row per issue', async () => {
    const m = mountTable({ issues: [makeIssue(1), makeIssue(2), makeIssue(3)] })
    await nextTick()
    expect(m.el.querySelectorAll('.it-row').length).toBe(3)
    m.unmount()
  })

  it('renders status column via VNode (h() output)', async () => {
    const m = mountTable({ issues: [makeIssue(1)] })
    await nextTick()
    expect(m.el.querySelector('.status-badge')?.textContent).toBe('in-progress')
    m.unmount()
  })

  it('renders primitive columns as text', async () => {
    const m = mountTable({ issues: [makeIssue(1, { title: 'My ticket' })] })
    await nextTick()
    const cells = m.el.querySelectorAll('.it-cell')
    expect([...cells].some((c) => c.textContent?.includes('My ticket'))).toBe(true)
    m.unmount()
  })

  it('emits sort on a sortable header click', async () => {
    const m = mountTable({ issues: [makeIssue(1)] })
    await nextTick()
    const sortable = m.el.querySelector('.it-th--sortable') as HTMLElement
    sortable.click()
    await nextTick()
    expect(m.events).toEqual([{ type: 'sort', payload: 'key' }])
    m.unmount()
  })

  it('emits row-click on row click', async () => {
    const m = mountTable({ issues: [makeIssue(42)] })
    await nextTick()
    const row = m.el.querySelector('.it-row') as HTMLElement
    row.click()
    await nextTick()
    expect(m.events).toEqual([{ type: 'row-click', payload: 42 }])
    m.unmount()
  })

  it('row-click does not fire when an action button is the target', async () => {
    const m = mountTable({
      issues: [makeIssue(7)],
      rowActions: (_issue) => [
        { key: 'accept', label: 'Accept', onClick: () => {} },
      ],
    })
    await nextTick()
    const btn = m.el.querySelector('.it-action') as HTMLButtonElement
    btn.click()
    await nextTick()
    expect(m.events).toEqual([]) // no row-click, no sort, just the action
    m.unmount()
  })

  it('renders the empty state when issues is empty', async () => {
    const m = mountTable({ issues: [] })
    await nextTick()
    expect(m.el.querySelector('.it-empty')).not.toBeNull()
    expect(m.el.querySelectorAll('.it-row').length).toBe(0)
    m.unmount()
  })
})
