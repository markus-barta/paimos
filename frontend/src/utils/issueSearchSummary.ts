import { formatInteger } from '@/composables/useNumberFormat'

const SEARCH_ORDER_LABEL = 'best matches first'

function fmt(n: number): string {
  return formatInteger(Math.max(0, n))
}

export function issueSearchSummary(
  loaded: number,
  total: number,
  query: string,
): string {
  const q = query.trim()
  const safeLoaded = Math.max(0, loaded)
  const safeTotal = Math.max(0, total)
  if (safeLoaded < safeTotal) {
    return `Showing first ${fmt(safeLoaded)} of ${fmt(safeTotal)} matches for "${q}" · ${SEARCH_ORDER_LABEL}`
  }
  return `${fmt(safeTotal)} matches for "${q}" · ${SEARCH_ORDER_LABEL}`
}
