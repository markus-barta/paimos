export function safePostLoginRedirect(raw: unknown): string | null {
  const value = Array.isArray(raw) ? raw[0] : raw
  if (typeof value !== 'string') return null
  if (!value.startsWith('/') || value.startsWith('//')) return null
  if (
    value === '/login' ||
    value.startsWith('/login?') ||
    value.startsWith('/login#') ||
    value.startsWith('/login/')
  ) {
    return null
  }
  return value
}

export function postLoginRedirectOrFallback(raw: unknown, fallback = '/'): string {
  return safePostLoginRedirect(raw) ?? fallback
}
