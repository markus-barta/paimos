const SEARCH_ORDER_LABEL = 'recently updated first'

function fmt(n: number): string {
  return Math.max(0, n).toLocaleString()
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
