import type { Customer, Project } from '@/types'

export interface ProjectEditForm {
  name: string
  key: string
  description: string
  status: 'active' | 'archived' | 'deleted'
  product_owner: number | null
  customer_label: string
  customer_id: number | null
  rate_hourly: number | null
  rate_lp: number | null
}

export function emptyProjectEditForm(): ProjectEditForm {
  return {
    name: '',
    key: '',
    description: '',
    status: 'active',
    product_owner: null,
    customer_label: '',
    customer_id: null,
    rate_hourly: null,
    rate_lp: null,
  }
}

export function projectToEditForm(project: Project): ProjectEditForm {
  return {
    name: project.name,
    key: project.key,
    description: project.description,
    status: project.status,
    product_owner: project.product_owner ?? null,
    customer_label: project.customer_label ?? '',
    customer_id: project.customer_id ?? null,
    rate_hourly: project.rate_hourly ?? null,
    rate_lp: project.rate_lp ?? null,
  }
}

export function buildProjectUpdatePayload(form: ProjectEditForm, originalCustomerId: number | null) {
  const nextCustomerId = form.customer_id ?? null
  return {
    ...form,
    clear_customer: originalCustomerId !== null && nextCustomerId === null,
  }
}

export function inheritedProjectRateHint(
  form: ProjectEditForm,
  customers: Customer[],
  kind: 'hourly' | 'lp',
): string {
  const customerId = form.customer_id
  if (!customerId) return ''
  const customer = customers.find((entry) => entry.id === customerId)
  if (!customer) return ''
  const value = kind === 'hourly' ? customer.rate_hourly : customer.rate_lp
  if (value == null) return ''
  const projectRate = kind === 'hourly' ? form.rate_hourly : form.rate_lp
  if (projectRate != null) return ''
  return `Inherits EUR ${value.toFixed(2)} from ${customer.name}`
}
