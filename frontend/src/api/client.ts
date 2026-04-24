/*
 * PAIMOS — Your Professional & Personal AI Project OS
 * Copyright (C) 2026 Markus Barta <markus@barta.com>
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as
 * published by the Free Software Foundation, version 3.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
 * GNU Affero General Public License for more details.
 *
 * You should have received a copy of the GNU Affero General Public
 * License along with this program. If not, see <https://www.gnu.org/licenses/>.
 */

import { ref } from 'vue'

const BASE = '/api'

export class ApiError extends Error {
  constructor(public status: number, message: string) {
    super(message)
  }
}

// ── Session-expired signal ────────────────────────────────────
// Any 401 from a non-auth endpoint flips this ref to true. A top-level
// banner watches it and prompts the user to sign in again. A module-level
// `ref` avoids a circular dep between this file and `stores/auth.ts`: the
// store imports client, never the other way around.
export const sessionExpired = ref(false)

// Paths where a 401 is EXPECTED (wrong password, bad reset token, first
// page load before any session exists) and MUST NOT flip the session-
// expired banner. Anything else that 401s is treated as a session that
// died mid-use.
//
// /auth/me is in the list because the router guard calls it on every
// pristine visit — a 401 there means "not logged in yet", not "session
// died". App.vue does explicit transition detection in its
// visibilitychange heartbeat to catch real session deaths via /auth/me.
const AUTH_ENDPOINT_PREFIXES = [
  '/auth/login',
  '/auth/me',
  '/auth/totp/verify',
  '/auth/forgot',
  '/auth/reset',          // covers /auth/reset and /auth/reset/validate
]

function isAuthEndpoint(path: string): boolean {
  return AUTH_ENDPOINT_PREFIXES.some(p => path.startsWith(p))
}

function maybeMarkSessionExpired(path: string) {
  if (!isAuthEndpoint(path)) {
    sessionExpired.value = true
  }
}

// Hard ceiling on how long any single request can hang. Anything slower
// than this is almost certainly the origin being unreachable (or a
// route/Tailscale issue), not a slow query. Without this, components
// with `loading.value` flags get stuck on "Loading…" indefinitely while
// the browser keeps the underlying fetch open in the background.
const REQUEST_TIMEOUT_MS = 30_000

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const ctrl = new AbortController()
  const timer = setTimeout(() => ctrl.abort(), REQUEST_TIMEOUT_MS)
  let res: Response
  try {
    res = await fetch(`${BASE}${path}`, {
      method,
      headers: body ? { 'Content-Type': 'application/json' } : {},
      body: body ? JSON.stringify(body) : undefined,
      credentials: 'same-origin',
      signal: ctrl.signal,
    })
  } catch (e) {
    // Surface the timeout case as a clean ApiError so callers can
    // render it instead of the raw "AbortError" string.
    if ((e as Error).name === 'AbortError') {
      throw new ApiError(0, `request timed out after ${REQUEST_TIMEOUT_MS / 1000}s`)
    }
    throw e
  } finally {
    clearTimeout(timer)
  }

  if (res.status === 401) {
    maybeMarkSessionExpired(path)
    throw new ApiError(401, 'unauthorized')
  }

  if (res.status === 204) return undefined as T

  const data = await res.json()
  if (!res.ok) throw new ApiError(res.status, data.error ?? 'request failed')
  return data as T
}

async function upload<T>(path: string, formData: FormData, onProgress?: (pct: number) => void): Promise<T> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest()
    xhr.open('POST', `${BASE}${path}`)
    xhr.withCredentials = true
    if (onProgress) {
      xhr.upload.addEventListener('progress', (e) => {
        if (e.lengthComputable) onProgress(Math.round((e.loaded / e.total) * 100))
      })
    }
    xhr.onload = () => {
      if (xhr.status === 401) {
        maybeMarkSessionExpired(path)
        reject(new ApiError(401, 'unauthorized'))
        return
      }
      try {
        const data = JSON.parse(xhr.responseText)
        if (xhr.status >= 400) { reject(new ApiError(xhr.status, data.error ?? 'upload failed')); return }
        resolve(data as T)
      } catch {
        // Non-JSON response (e.g. nginx 413 HTML page) — surface the HTTP status
        reject(new ApiError(xhr.status, `upload failed (HTTP ${xhr.status})`))
      }
    }
    xhr.onerror = () => reject(new ApiError(0, 'network error'))
    xhr.send(formData)
  })
}

/** Extract message from an unknown catch value. */
export function errMsg(e: unknown, fallback = 'An error occurred'): string {
  if (e instanceof Error) return e.message
  if (typeof e === 'string') return e
  return fallback
}

export const api = {
  get: <T>(path: string) => request<T>('GET', path),
  post: <T>(path: string, body: unknown) => request<T>('POST', path, body),
  put: <T>(path: string, body: unknown) => request<T>('PUT', path, body),
  patch: <T>(path: string, body: unknown) => request<T>('PATCH', path, body),
  delete: <T>(path: string, body?: unknown) => request<T>('DELETE', path, body),
  upload: <T>(path: string, formData: FormData, onProgress?: (pct: number) => void) => upload<T>(path, formData, onProgress),
}
