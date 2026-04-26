export interface AiResultSummaryInput {
  action: string
  body: any
  sourceText?: string
  optimizedText?: string
}

function sentenceCount(text: string): number {
  const trimmed = text.trim()
  if (!trimmed) return 0
  return trimmed.split(/[.!?]+/).map(s => s.trim()).filter(Boolean).length
}

export function summarizeAiResult(input: AiResultSummaryInput): string {
  const { action, body, sourceText = '', optimizedText = '' } = input
  switch (action) {
    case 'optimize':
    case 'optimize_customer':
    case 'tone_check':
    case 'translate': {
      const delta = optimizedText.length - sourceText.length
      const sentenceDelta = Math.abs(sentenceCount(optimizedText) - sentenceCount(sourceText))
      if (delta === 0 && sentenceDelta === 0) return 'Rewrite ready'
      if (delta < 0) return `${Math.abs(delta)} chars tightened`
      if (delta > 0) return `${delta} chars expanded`
      return `${sentenceDelta} sentence${sentenceDelta === 1 ? '' : 's'} adjusted`
    }
    case 'suggest_enhancement':
      return `${body?.suggestions?.length ?? 0} improvement ideas proposed`
    case 'spec_out':
      return `${body?.items?.length ?? 0} acceptance items drafted`
    case 'find_parent': {
      const top = body?.candidates?.[0]
      if (!top) return 'No parent candidate found'
      const score = top.score ? ` (${Math.round(top.score * 100)}%)` : ''
      return `Top match: ${top.issue_key}${score}`
    }
    case 'generate_subtasks':
      return `${body?.suggestions?.length ?? 0} sub-tasks proposed`
    case 'estimate_effort':
      return `${body?.hours ?? 0}h · ${body?.lp ?? 0} LP suggested`
    case 'detect_duplicates': {
      const top = body?.matches?.[0]
      if (!top) return 'No close duplicate found'
      const score = top.score ? ` (${Math.round(top.score * 100)}%)` : ''
      return `Top match: ${top.issue_key}${score}`
    }
    case 'ui_generation':
      return `${body?.markdown ? String(body.markdown).length : 0} chars of UI spec generated`
    default:
      return 'AI result ready'
  }
}
