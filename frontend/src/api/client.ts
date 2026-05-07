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

import { ref } from "vue";

const BASE = "/api";

export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message);
  }
}

// ── Session-expired signal ────────────────────────────────────
// PAI-322: any 401 from a non-auth endpoint flips this ref to true.
// SessionExpiredModal watches it and prompts the user to sign in
// again. Module-level `ref` avoids a circular dep between this file
// and `stores/auth.ts`: the store imports client, never the other
// way around. Cross-tab sync is provided by a BroadcastChannel below
// so all open tabs converge on the same modal.
export const sessionExpired = ref(false);

// PAI-322: latest X-Session-Expires-At observed on any authenticated
// response, parsed to a Date. The pre-expiry toast component reads
// this and shows itself when the value is within 5 minutes. Sliding
// renewal keeps this far in the future for active users; the toast
// only appears as the 90-day absolute cap approaches.
export const sessionExpiresAt = ref<Date | null>(null);

// PAI-322: the route the user was on when 401 first hit. Captured so
// the post-login flow can deep-link them back. Cleared on successful
// login. Used by the login page reading ?next=… as well — this is
// the in-memory mirror so single-tab flows don't need URL bouncing.
export const sessionReturnPath = ref<string | null>(null);

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
  "/auth/login",
  "/auth/me",
  "/auth/totp/verify",
  "/auth/forgot",
  "/auth/reset", // covers /auth/reset and /auth/reset/validate
];

function isAuthEndpoint(path: string): boolean {
  return AUTH_ENDPOINT_PREFIXES.some((p) => path.startsWith(p));
}

// PAI-322: cross-tab sync. Modern browsers all support BroadcastChannel;
// when one tab learns the session is dead, it posts a message and every
// other tab opens the same modal — no waiting for each tab's next
// request to discover the truth on its own.
type AuthBroadcast =
  | { type: "session-expired" }
  | { type: "session-restored" };

const authChannel: BroadcastChannel | null =
  typeof BroadcastChannel !== "undefined"
    ? new BroadcastChannel("paimos-auth")
    : null;

if (authChannel) {
  authChannel.addEventListener("message", (ev: MessageEvent<AuthBroadcast>) => {
    if (ev.data?.type === "session-expired") {
      sessionExpired.value = true;
    } else if (ev.data?.type === "session-restored") {
      sessionExpired.value = false;
      sessionReturnPath.value = null;
    }
  });
}

function broadcastAuth(msg: AuthBroadcast) {
  authChannel?.postMessage(msg);
}

// Public — call from auth store when login/TOTP succeeds, so sibling
// tabs dismiss their session-expired modals.
export function announceSessionRestored() {
  sessionExpired.value = false;
  sessionReturnPath.value = null;
  broadcastAuth({ type: "session-restored" });
}

// Public — call from App.vue's visibility/heartbeat path when it
// detects a logged-in→logged-out transition. Broadcasts to sibling
// tabs and captures the return path so re-login deep-links the user
// back to where they were.
export function announceSessionExpired() {
  markSessionExpired();
}

function markSessionExpired() {
  if (!sessionExpired.value) {
    // Capture the current location ONCE, on the first 401. Subsequent
    // 401s during the same dead-session episode would otherwise
    // overwrite this with /login or whatever the app navigated to in
    // the meantime.
    if (!sessionReturnPath.value) {
      const path = window.location.pathname + window.location.search;
      if (!path.startsWith("/login")) sessionReturnPath.value = path;
    }
    sessionExpired.value = true;
    broadcastAuth({ type: "session-expired" });
  }
}

// PAI-322: thrown for 401s on non-auth endpoints. Distinct from
// ApiError so component-level catch blocks can be told to ignore it
// — the global SessionExpiredModal is the sole user-facing surface
// for the dead-session condition. Callers that render `errMsg(e)`
// must not render this one's message, or the user will see a toast
// and a modal at the same time.
export class SessionExpiredError extends Error {
  // Discriminator so `e instanceof SessionExpiredError` is unambiguous
  // even after bundling / minification mangles class names. Used by
  // the helper below.
  readonly isSessionExpired = true as const;
  constructor() {
    super("session expired");
  }
}

// isSessionExpiredError narrows an unknown caught value. Component
// error handlers should call this and skip toast/banner rendering
// when it returns true.
export function isSessionExpiredError(e: unknown): e is SessionExpiredError {
  return (
    e instanceof SessionExpiredError ||
    (typeof e === "object" &&
      e !== null &&
      (e as { isSessionExpired?: boolean }).isSessionExpired === true)
  );
}

// Hard ceiling on how long any single request can hang. Anything slower
// than this is almost certainly the origin being unreachable (or a
// route/Tailscale issue), not a slow query. Without this, components
// with `loading.value` flags get stuck on "Loading…" indefinitely while
// the browser keeps the underlying fetch open in the background.
const REQUEST_TIMEOUT_MS = 30_000;

// PAI-113: read the per-session CSRF token from the non-HttpOnly cookie
// the backend sets at login, so we can echo it back on every mutating
// request. Empty string when not yet authenticated — the backend does
// not enforce CSRF on the public auth endpoints.
export function readCsrfToken(): string {
  const m = document.cookie.match(/(?:^|;\s*)csrf_token=([^;]+)/);
  return m ? decodeURIComponent(m[1]) : "";
}

// csrfHeaders returns a headers object pre-populated with the CSRF header
// when a token is available. Use from any code that hits the backend
// directly with fetch() instead of the `api` wrapper.
export function csrfHeaders(
  extra: Record<string, string> = {},
): Record<string, string> {
  const tok = readCsrfToken();
  return tok ? { ...extra, "X-CSRF-Token": tok } : { ...extra };
}

const SAFE_METHODS = new Set(["GET", "HEAD", "OPTIONS"]);

function withCsrfHeader(
  method: string,
  headers: Record<string, string>,
): Record<string, string> {
  if (SAFE_METHODS.has(method)) return headers;
  const tok = readCsrfToken();
  if (tok) headers["X-CSRF-Token"] = tok;
  return headers;
}

/**
 * RequestOptions lets a small number of long-running endpoints opt in
 * to a custom timeout. Default is REQUEST_TIMEOUT_MS (30s); the AI
 * optimize endpoint (PAI-146) sets 90s because the backend itself
 * waits up to 60s on the LLM call. Using fetch's signal here means
 * cancellation still works the same way as the default path.
 */
export interface RequestOptions {
  timeoutMs?: number;
  headers?: Record<string, string>;
  /**
   * External AbortSignal — caller-driven cancellation. Linked to the
   * internal timeout controller so either source cancels the fetch.
   * Used by BulkChangeModal to abort in-flight chunked PATCH calls
   * when the user closes the modal mid-bulk (PAI-317).
   */
  signal?: AbortSignal;
}

export interface ApiMetaResponse<T> {
  data: T;
  etag: string | null;
  lastModified: string | null;
  status: number;
}

function extractLikelyJSON(raw: string): string {
  const trimmed = raw.trim();
  if (trimmed === "") return trimmed;
  const firstObject = trimmed.indexOf("{");
  const lastObject = trimmed.lastIndexOf("}");
  if (firstObject >= 0 && lastObject > firstObject) {
    return trimmed.slice(firstObject, lastObject + 1);
  }
  const firstArray = trimmed.indexOf("[");
  const lastArray = trimmed.lastIndexOf("]");
  if (firstArray >= 0 && lastArray > firstArray) {
    return trimmed.slice(firstArray, lastArray + 1);
  }
  return trimmed;
}

function responseSnippet(raw: string): string {
  const singleLine = raw.replace(/\s+/g, " ").trim();
  if (singleLine === "") return "empty response body";
  return singleLine.length > 180
    ? `${singleLine.slice(0, 177)}...`
    : singleLine;
}

async function readJSON<T>(res: Response): Promise<T> {
  const raw = await res.text();
  if (raw.trim() === "") return undefined as T;
  try {
    return JSON.parse(raw) as T;
  } catch {
    const extracted = extractLikelyJSON(raw);
    if (extracted !== raw.trim()) {
      try {
        return JSON.parse(extracted) as T;
      } catch {
        // Fall through to the user-facing ApiError below.
      }
    }
    throw new ApiError(
      res.status,
      `invalid JSON response: ${responseSnippet(raw)}`,
    );
  }
}

async function fetchResponse(
  method: string,
  path: string,
  body?: unknown,
  opts?: RequestOptions,
): Promise<Response> {
  const ctrl = new AbortController();
  const timeoutMs = opts?.timeoutMs ?? REQUEST_TIMEOUT_MS;
  const timer = setTimeout(() => ctrl.abort(), timeoutMs);
  // Wire the caller's external signal into our local controller so a
  // user-initiated cancel (e.g. closing the bulk modal) tears down the
  // pending fetch the same way a timeout does.
  if (opts?.signal) {
    if (opts.signal.aborted) {
      ctrl.abort();
    } else {
      opts.signal.addEventListener("abort", () => ctrl.abort(), { once: true });
    }
  }
  try {
    const headers: Record<string, string> = {
      ...(body ? { "Content-Type": "application/json" } : {}),
      ...(opts?.headers ?? {}),
    };
    const res = await fetch(`${BASE}${path}`, {
      method,
      headers: withCsrfHeader(method, headers),
      body: body ? JSON.stringify(body) : undefined,
      credentials: "same-origin",
      signal: ctrl.signal,
    });
    // PAI-322: surface the server-side session expiry so the toast
    // component can show a low-key warning as the absolute cap
    // approaches. Always present on authed responses; absent on
    // public auth endpoints like /auth/login.
    const expHdr = res.headers.get("X-Session-Expires-At");
    if (expHdr) {
      const t = new Date(expHdr);
      if (!Number.isNaN(t.valueOf())) sessionExpiresAt.value = t;
    }
    if (res.status === 401) {
      // Auth-endpoint 401s (wrong password, bad reset token, first
      // pristine /auth/me) bubble as ApiError so the login form can
      // render "invalid credentials". Only mid-session 401s become
      // SessionExpiredError, which the global handler suppresses
      // from the per-action toast layer.
      if (isAuthEndpoint(path)) {
        throw new ApiError(401, "unauthorized");
      }
      markSessionExpired();
      throw new SessionExpiredError();
    }
    return res;
  } catch (e) {
    if ((e as Error).name === "AbortError") {
      throw new ApiError(0, `request timed out after ${timeoutMs / 1000}s`);
    }
    throw e;
  } finally {
    clearTimeout(timer);
  }
}

async function request<T>(
  method: string,
  path: string,
  body?: unknown,
  opts?: RequestOptions,
): Promise<T> {
  const res = await fetchResponse(method, path, body, opts);

  if (res.status === 204) return undefined as T;

  const data = await readJSON<any>(res);
  if (!res.ok) {
    const err = new ApiError(res.status, data.error ?? "request failed");
    if (data && typeof data === "object") Object.assign(err, data);
    throw err;
  }
  return data as T;
}

async function requestWithMeta<T>(
  method: string,
  path: string,
  body?: unknown,
  opts?: RequestOptions,
): Promise<ApiMetaResponse<T>> {
  const res = await fetchResponse(method, path, body, opts);
  const etag = res.headers.get("ETag");
  const lastModified = res.headers.get("Last-Modified");

  if (res.status === 304) {
    return {
      data: null as T,
      etag,
      lastModified,
      status: res.status,
    };
  }

  if (res.status === 204) {
    return {
      data: undefined as T,
      etag,
      lastModified,
      status: res.status,
    };
  }

  const data = await readJSON<any>(res);
  if (!res.ok) {
    const err = new ApiError(res.status, data.error ?? "request failed");
    if (data && typeof data === "object") Object.assign(err, data);
    throw err;
  }
  return {
    data: data as T,
    etag,
    lastModified,
    status: res.status,
  };
}

async function upload<T>(
  path: string,
  formData: FormData,
  onProgress?: (pct: number) => void,
): Promise<T> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open("POST", `${BASE}${path}`);
    xhr.withCredentials = true;
    // PAI-113: echo CSRF token on multipart uploads too. Cookie path
    // doesn't trip Origin/Referer issues because the SPA is same-origin.
    const csrf = readCsrfToken();
    if (csrf) xhr.setRequestHeader("X-CSRF-Token", csrf);
    if (onProgress) {
      xhr.upload.addEventListener("progress", (e) => {
        if (e.lengthComputable)
          onProgress(Math.round((e.loaded / e.total) * 100));
      });
    }
    xhr.onload = () => {
      // PAI-322: mirror the fetch path for header capture + error type.
      const expHdr = xhr.getResponseHeader("X-Session-Expires-At");
      if (expHdr) {
        const t = new Date(expHdr);
        if (!Number.isNaN(t.valueOf())) sessionExpiresAt.value = t;
      }
      if (xhr.status === 401) {
        if (isAuthEndpoint(path)) {
          reject(new ApiError(401, "unauthorized"));
          return;
        }
        markSessionExpired();
        reject(new SessionExpiredError());
        return;
      }
      try {
        const data = JSON.parse(xhr.responseText);
        if (xhr.status >= 400) {
          reject(new ApiError(xhr.status, data.error ?? "upload failed"));
          return;
        }
        resolve(data as T);
      } catch {
        // Non-JSON response (e.g. nginx 413 HTML page) — surface the HTTP status
        reject(new ApiError(xhr.status, `upload failed (HTTP ${xhr.status})`));
      }
    };
    xhr.onerror = () => reject(new ApiError(0, "network error"));
    xhr.send(formData);
  });
}

/** Extract message from an unknown catch value.
 *
 * Never returns an empty / whitespace-only string — a blank banner
 * ("Error: ") is worse than a generic one ("An error occurred")
 * because it leaves the user wondering whether anything rendered.
 * ApiError additionally surfaces its HTTP status when its own
 * message is empty so an admin scanning logs has something to grep
 * for ("request failed (HTTP 502)").
 */
export function errMsg(e: unknown, fallback = "An error occurred"): string {
  // PAI-322: dead-session 401s are surfaced exclusively through the
  // SessionExpiredModal — returning empty here lets components that do
  // `if (error) {...}` skip rendering a duplicate per-action toast.
  if (isSessionExpiredError(e)) return "";
  let raw = "";
  if (e instanceof ApiError) {
    raw = e.message;
    if (raw.trim() === "") {
      raw = `request failed (HTTP ${e.status || "network"})`;
    }
  } else if (e instanceof Error) {
    raw = e.message;
  } else if (typeof e === "string") {
    raw = e;
  }
  return raw.trim() !== "" ? raw : fallback;
}

export const api = {
  get: <T>(path: string, opts?: RequestOptions) =>
    request<T>("GET", path, undefined, opts),
  getWithMeta: <T>(path: string, opts?: RequestOptions) =>
    requestWithMeta<T>("GET", path, undefined, opts),
  post: <T>(path: string, body: unknown, opts?: RequestOptions) =>
    request<T>("POST", path, body, opts),
  put: <T>(path: string, body: unknown, opts?: RequestOptions) =>
    request<T>("PUT", path, body, opts),
  patch: <T>(path: string, body: unknown, opts?: RequestOptions) =>
    request<T>("PATCH", path, body, opts),
  delete: <T>(path: string, body?: unknown, opts?: RequestOptions) =>
    request<T>("DELETE", path, body, opts),
  upload: <T>(
    path: string,
    formData: FormData,
    onProgress?: (pct: number) => void,
  ) => upload<T>(path, formData, onProgress),
};
