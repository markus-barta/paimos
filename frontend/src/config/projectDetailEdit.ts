import { formatCurrency } from '@/composables/useNumberFormat'
import type { Customer, Project, ProjectAIDefaults, ProjectAIDefaultSet } from '@/types'

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
  ai_default_profile_id: string
  ai_default_effort: string
  ai_default_prompt_preset_ref: string
  ai_default_context_pack: string
  ai_preferred_provider_class: string
  ai_scoped_defaults_json: string
  ai_disable_hosted_draft: boolean
  ai_disable_local_model_draft: boolean
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
    ai_default_profile_id: '',
    ai_default_effort: '',
    ai_default_prompt_preset_ref: '',
    ai_default_context_pack: '',
    ai_preferred_provider_class: '',
    ai_scoped_defaults_json: '',
    ai_disable_hosted_draft: false,
    ai_disable_local_model_draft: false,
  }
}

export function projectToEditForm(project: Project): ProjectEditForm {
  const global = project.ai_defaults?.global ?? {}
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
    ai_default_profile_id: global.profile_id ?? '',
    ai_default_effort: global.effort ?? '',
    ai_default_prompt_preset_ref: global.prompt_preset_ref ?? '',
    ai_default_context_pack: global.context_pack ?? '',
    ai_preferred_provider_class: global.preferred_provider_class ?? '',
    ai_scoped_defaults_json: scopedDefaultsJSON(project.ai_defaults),
    ai_disable_hosted_draft: !!project.ai_policy?.disable_hosted_draft,
    ai_disable_local_model_draft: !!project.ai_policy?.disable_local_model_draft,
  }
}

export function buildProjectUpdatePayload(
  form: ProjectEditForm,
  originalCustomerId: number | null,
) {
  const nextCustomerId = form.customer_id ?? null
  const scopedDefaults = parseScopedDefaults(form.ai_scoped_defaults_json)
  const globalDefaults = compactDefaultSet({
    profile_id: form.ai_default_profile_id,
    effort: form.ai_default_effort,
    prompt_preset_ref: form.ai_default_prompt_preset_ref,
    context_pack: form.ai_default_context_pack,
    preferred_provider_class: form.ai_preferred_provider_class,
  })
  return {
    ...form,
    clear_customer: originalCustomerId !== null && nextCustomerId === null,
    ai_defaults: {
      ...scopedDefaults,
      global: globalDefaults,
    },
    ai_policy: {
      disable_hosted_draft: form.ai_disable_hosted_draft,
      disable_local_model_draft: form.ai_disable_local_model_draft,
    },
  }
}

function compactDefaultSet(set: ProjectAIDefaultSet): ProjectAIDefaultSet {
  return Object.fromEntries(
    Object.entries(set)
      .map(([key, value]) => [key, String(value ?? '').trim()])
      .filter(([, value]) => value !== ''),
  ) as ProjectAIDefaultSet
}

function scopedDefaultsJSON(defaults?: ProjectAIDefaults): string {
  if (!defaults) return ''
  const scoped: ProjectAIDefaults = {}
  if (defaults.actions && Object.keys(defaults.actions).length) scoped.actions = defaults.actions
  if (defaults.runs && Object.keys(defaults.runs).length) scoped.runs = defaults.runs
  if (defaults.agents && Object.keys(defaults.agents).length) scoped.agents = defaults.agents
  return Object.keys(scoped).length ? JSON.stringify(scoped, null, 2) : ''
}

function parseScopedDefaults(raw: string): ProjectAIDefaults {
  const trimmed = raw.trim()
  if (!trimmed) return {}
  const parsed = JSON.parse(trimmed)
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
    throw new Error('AI scoped defaults must be a JSON object.')
  }
  return parsed as ProjectAIDefaults
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
  return `Inherits ${formatCurrency(value, 'EUR')} from ${customer.name}`
}
