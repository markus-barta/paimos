import { describe, expect, it } from 'vitest'

import {
  buildProjectUpdatePayload,
  emptyProjectEditForm,
  inheritedProjectRateHint,
  projectToEditForm,
} from './projectDetailEdit'
import type { Customer, Project } from '@/types'

function makeProject(overrides: Partial<Project> = {}): Project {
  return {
    id: 1,
    name: 'Project',
    key: 'PAI',
    description: 'Desc',
    status: 'active',
    product_owner: null,
    customer_label: '',
    customer_id: null,
    created_at: '',
    updated_at: '',
    issue_count: 0,
    logo_path: '',
    last_activity: '',
    open_issue_count: 0,
    done_issue_count: 0,
    active_issue_count: 0,
    tags: [],
    ...overrides,
  }
}

function makeCustomer(overrides: Partial<Customer> = {}): Customer {
  return {
    id: 1,
    name: 'ACME',
    external_id: null,
    external_url: null,
    external_provider: null,
    synced_at: null,
    contact_name: '',
    contact_email: '',
    address: '',
    country: '',
    industry: '',
    rate_hourly: null,
    rate_lp: null,
    notes: '',
    created_at: '',
    updated_at: '',
    ...overrides,
  }
}

describe('projectDetailEdit helpers', () => {
  it('creates a blank edit form', () => {
    const form = emptyProjectEditForm()
    expect(form.status).toBe('active')
    expect(form.customer_id).toBeNull()
  })

  it('maps a project into the edit form shape', () => {
    const form = projectToEditForm(makeProject({ customer_id: 3, rate_hourly: 120 }))
    expect(form.customer_id).toBe(3)
    expect(form.rate_hourly).toBe(120)
  })

  it('sets clear_customer only when detaching an existing customer', () => {
    const payload = buildProjectUpdatePayload(
      { ...emptyProjectEditForm(), customer_id: null },
      42,
    )
    expect(payload.clear_customer).toBe(true)
  })

  it('builds inherited rate hints only when project rate is empty', () => {
    const customers = [makeCustomer({ id: 7, name: 'Umbrella', rate_hourly: 135 })]
    const form = { ...emptyProjectEditForm(), customer_id: 7, rate_hourly: null }
    expect(inheritedProjectRateHint(form, customers, 'hourly')).toBe('Inherits EUR 135.00 from Umbrella')
    expect(inheritedProjectRateHint({ ...form, rate_hourly: 120 }, customers, 'hourly')).toBe('')
  })
})
