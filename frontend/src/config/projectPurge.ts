export interface ProjectPurgeForm {
  source: string
  from_date: string
  to_date: string
  user_id: number | null
}

export function emptyProjectPurgeForm(): ProjectPurgeForm {
  return { source: 'all', from_date: '', to_date: '', user_id: null }
}

export function buildProjectPurgePayload(form: ProjectPurgeForm): Record<string, unknown> {
  const payload: Record<string, unknown> = { source: form.source }
  if (form.from_date) payload.from_date = form.from_date
  if (form.to_date) payload.to_date = form.to_date
  if (form.user_id != null) payload.user_id = form.user_id
  return payload
}
