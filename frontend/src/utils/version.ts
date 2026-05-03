export function formatDisplayVersion(version: string): string {
  const trimmed = version.trim()
  const match = /^v?(\d+(?:\.\d+){1,3})(?=$|[-+])/.exec(trimmed)
  return match?.[1] ?? trimmed
}
