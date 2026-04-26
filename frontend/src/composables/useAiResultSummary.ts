export interface AiResultSummaryInput {
  action: string
  body: any
  sourceText?: string
  optimizedText?: string
}

function wordCount(text: string): number {
  return text.trim().split(/\s+/).filter(Boolean).length
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
    case 'optimize_customer': {
      const delta = optimizedText.length - sourceText.length
      const sentenceDelta = Math.abs(sentenceCount(optimizedText) - sentenceCount(sourceText))
      if (delta === 0 && sentenceDelta === 0) return 'Rewrite ready'
      if (delta < 0) return `${Math.abs(delta)} chars tightened`
      if (delta > 0) return `${delta} chars expanded`
      return `${sentenceDelta} sentence${sentenceDelta === 1 ? '' : 's'} adjusted`
    }
    case 'translate': {
      const words = wordCount(optimizedText)
      const before = wordCount(sourceText)
      return `Translated copy · ${words || before} words`
    }
    case 'tone_check': {
      const removed = Number(body?.counters?.phrases_removed ?? 0)
      if (removed > 0) return `${removed} persuasive phrase${removed === 1 ? '' : 's'} removed`
      return 'Tone rewrite ready'
    }
    case 'suggest_enhancement':
      return `${body?.counters?.items ?? body?.suggestions?.length ?? 0} improvement ideas proposed`
    case 'spec_out':
      return `${body?.counters?.items ?? body?.items?.length ?? 0} acceptance items drafted`
    case 'find_parent': {
      const top = body?.candidates?.[0]
      if (!top) return 'No parent candidate found'
      const score = top.score ? ` (${Math.round(top.score * 100)}%)` : ''
      return `Top match: ${top.issue_key}${score}`
    }
    case 'generate_subtasks':
      return `${body?.counters?.items ?? body?.suggestions?.length ?? 0} sub-tasks proposed`
    case 'estimate_effort': {
      const confidence = body?.confidence ? ` · ${String(body.confidence)}` : ' suggested'
      return `${body?.hours ?? 0}h · ${body?.lp ?? 0} LP${confidence}`
    }
    case 'detect_duplicates': {
      const top = body?.matches?.[0]
      if (!top) return 'No close duplicate found'
      const similarity = top.score
        ? ` (${Math.round(top.score * 100)}%)`
        : top.similarity ? ` (${String(top.similarity)})` : ''
      const count = body?.counters?.matches ?? body?.matches?.length ?? 0
      return `${count} likely duplicate${count === 1 ? '' : 's'} · top: ${top.issue_key}${similarity}`
    }
    case 'ui_generation':
      return `${wordCount(String(body?.spec_markdown ?? ''))} words generated`
    default:
      return 'AI result ready'
  }
}
