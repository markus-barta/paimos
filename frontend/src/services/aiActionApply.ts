export interface AiApplyInfo {
  requestId?: string
  action: string
  subAction?: string
  field: string
  fieldLabel: string
  issueId?: number
  body: any
  intent?: string
  selection?: number[]
  values?: Record<string, unknown>
}

export function aiMutationHeaders(info: AiApplyInfo): Record<string, string> {
  const headers: Record<string, string> = {}
  if (info.requestId) headers['X-PAIMOS-AI-Request-Id'] = info.requestId
  if (info.action) headers['X-PAIMOS-AI-Action'] = info.action
  if (info.subAction) headers['X-PAIMOS-AI-Sub-Action'] = info.subAction
  return headers
}

function appendBlock(base: string, addition: string): string {
  const trimmedBase = (base ?? '').trim()
  const trimmedAddition = (addition ?? '').trim()
  if (!trimmedAddition) return base ?? ''
  return trimmedBase ? `${trimmedBase}\n\n${trimmedAddition}` : trimmedAddition
}

export function applyIssueTextMutations(
  info: AiApplyInfo,
  current: { description: string; acceptance_criteria: string; notes: string },
): { description: string; acceptance_criteria: string; notes: string } {
  const next = { ...current }
  if (info.action === 'suggest_enhancement') {
    for (const idx of info.selection ?? []) {
      const item = info.body?.suggestions?.[idx]
      if (!item) continue
      const line = `${item.title}: ${item.body} (suggested by AI)`.trim()
      if (item.target_field === 'ac') next.acceptance_criteria = appendBlock(next.acceptance_criteria, `- ${line}`)
      else next.notes = appendBlock(next.notes, `- ${line}`)
    }
    return next
  }
  if (info.action === 'spec_out') {
    const lines = (info.selection ?? []).map(idx => info.body?.items?.[idx]?.text).filter(Boolean).map((text: string) => `- ${text}`)
    next.acceptance_criteria = appendBlock(next.acceptance_criteria, lines.join('\n'))
    return next
  }
  if (info.action === 'ui_generation') {
    const markdown = String(info.body?.spec_markdown ?? '')
    if (info.intent === 'replace-description') next.description = markdown
    else next.notes = appendBlock(next.notes, markdown)
    return next
  }
  return next
}
