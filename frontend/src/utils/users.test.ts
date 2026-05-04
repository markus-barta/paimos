import { describe, expect, it } from 'vitest'
import { assignableIssueUsers, isAssignableIssueUser } from './users'

describe('issue assignee user helpers', () => {
  it('allows only active internal users to be assigned', () => {
    const users = [
      { id: 1, username: 'admin', role: 'admin', status: 'active' },
      { id: 2, username: 'member', role: 'member', status: 'active' },
      { id: 3, username: 'inactive', role: 'member', status: 'inactive' },
      { id: 4, username: 'deleted', role: 'admin', status: 'deleted' },
      { id: 5, username: 'external', role: 'external', status: 'active' },
    ] as const

    expect(assignableIssueUsers(users).map((user) => user.username)).toEqual([
      'admin',
      'member',
    ])
  })

  it('rejects disabled or external users individually', () => {
    expect(isAssignableIssueUser({ role: 'member', status: 'active' })).toBe(true)
    expect(isAssignableIssueUser({ role: 'member', status: 'inactive' })).toBe(false)
    expect(isAssignableIssueUser({ role: 'admin', status: 'deleted' })).toBe(false)
    expect(isAssignableIssueUser({ role: 'external', status: 'active' })).toBe(false)
  })
})
