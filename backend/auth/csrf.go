// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public
// License along with this program. If not, see <https://www.gnu.org/licenses/>.

// PAI-113: CSRF defenses for cookie-authenticated browser flows.
//
// Threat model: a session cookie is sent ambiently by the browser on any
// cross-origin request, so an attacker page can trigger an authenticated
// state change just by getting the user to load it. SameSite=Lax mitigates
// this for top-level navigations but does not cover form submissions or
// CORS POSTs in older browsers, and it does not give us defence in depth
// against subdomain takeovers or malicious browser extensions.
//
// Design:
//
//   - Each session row carries a per-session csrf_token (M72).
//   - On login / TOTP verify the token is also written into a non-HttpOnly
//     cookie so the SPA can read it from document.cookie and echo it back
//     in an X-CSRF-Token header.
//   - On every authenticated request, the middleware below:
//       * skips the check entirely for safe methods (GET / HEAD / OPTIONS),
//       * skips the check for API-key callers (no ambient browser auth),
//       * for session-authenticated mutations, requires both a same-origin
//         Origin/Referer AND a header that matches the session's stored
//         token.
//   - Tokens rotate naturally on logout (session deleted) and on password
//     reset (which deletes all sessions). No additional rotation surface
//     exists in v1 by design — keep the moving parts small.
//
// The check is opt-in by route group: the middleware is only mounted in
// auth-required groups so the public auth endpoints (login, totp/verify,
// forgot, reset) keep working without a token in hand.

package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/markus-barta/paimos/backend/db"
)

const (
	// CSRFCookieName is the cookie the SPA reads to find the current
	// session's CSRF token. Non-HttpOnly by design — the entire point is
	// that JS in the SPA reads it and echoes it back in a header.
	CSRFCookieName = "csrf_token"

	// CSRFHeaderName is the header the SPA sets on every mutating request.
	CSRFHeaderName = "X-CSRF-Token"
)

// sessionAuthKey is the context flag set by Middleware when a request was
// authenticated by a session cookie (as opposed to an API key). The CSRF
// middleware uses it to skip API-key callers, who don't carry ambient
// browser credentials and therefore aren't a CSRF target.
type sessionAuthKeyType struct{}

var sessionAuthKey = sessionAuthKeyType{}

// csrfTokenKey carries the active session's CSRF token through the request
// pipeline so the CSRF middleware can compare without re-reading the DB.
type csrfTokenKeyType struct{}

var csrfTokenKey = csrfTokenKeyType{}

// withSessionAuth marks the request as session-cookie authenticated and
// attaches the active CSRF token. Called by Middleware.
func withSessionAuth(ctx context.Context, csrf string) context.Context {
	ctx = context.WithValue(ctx, sessionAuthKey, true)
	ctx = context.WithValue(ctx, csrfTokenKey, csrf)
	return ctx
}

func sessionAuthFromCtx(ctx context.Context) bool {
	v, _ := ctx.Value(sessionAuthKey).(bool)
	return v
}

func csrfTokenFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(csrfTokenKey).(string)
	return v
}

// NewCSRFToken returns a random hex token suitable for session-binding.
// 32 hex chars (16 bytes) is comfortably above the brute-force threshold
// for the per-session lifetime.
func NewCSRFToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// SetCSRFCookie writes the non-HttpOnly CSRF cookie that the SPA reads.
// Lifetime tracks the session cookie. Callers MUST also set the matching
// session row column.
func SetCSRFCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     CSRFCookieName,
		Value:    token,
		Path:     "/",
		// Intentionally NOT HttpOnly — the SPA must read this from JS.
		HttpOnly: false,
		Secure:   cookieSecure,
		// Strict is fine: the cookie is only ever needed when the SPA is
		// running, which by definition is a same-site context.
		SameSite: http.SameSiteStrictMode,
	})
}

// ClearCSRFCookie removes the cookie at logout time.
func ClearCSRFCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:   CSRFCookieName,
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})
}

// IssueCSRFForSession generates a new token, persists it on the session
// row, and sets the matching cookie on the response. Used by login, TOTP
// verify, and the lazy-upgrade path in Middleware for sessions that
// pre-date M72.
func IssueCSRFForSession(w http.ResponseWriter, sessionID string) (string, error) {
	tok, err := NewCSRFToken()
	if err != nil {
		return "", err
	}
	if _, err := db.DB.Exec(
		"UPDATE sessions SET csrf_token=? WHERE id=?", tok, sessionID,
	); err != nil {
		return "", err
	}
	SetCSRFCookie(w, tok)
	return tok, nil
}

// CSRFMiddleware enforces same-origin + token match for session-cookie
// authenticated mutations. Mount this AFTER Middleware (so the session
// auth flag is set) and AFTER any per-request setup that might need the
// session-auth signal too.
func CSRFMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isCSRFSafeMethod(r.Method) {
			next.ServeHTTP(w, r)
			return
		}
		// Only apply the check to session-cookie auth. API key callers
		// (machine-to-machine) don't carry ambient browser credentials.
		if !sessionAuthFromCtx(r.Context()) {
			next.ServeHTTP(w, r)
			return
		}
		if !sameOrigin(r) {
			log.Printf("audit: csrf_origin_blocked path=%s origin=%q referer=%q host=%q",
				r.URL.Path, r.Header.Get("Origin"), r.Header.Get("Referer"), r.Host)
			http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
			return
		}
		got := r.Header.Get(CSRFHeaderName)
		want := csrfTokenFromCtx(r.Context())
		if got == "" || want == "" || subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
			log.Printf("audit: csrf_token_mismatch path=%s ip=%s", r.URL.Path, ClientIP(r))
			http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isCSRFSafeMethod(m string) bool {
	switch m {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	return false
}

// sameOrigin returns true if the request's Origin or Referer host matches
// the request's own Host header. We do not require a specific public URL
// — operators run PAIMOS behind many different host names — but we do
// require that a same-host header is present and matches.
func sameOrigin(r *http.Request) bool {
	host := r.Host
	if origin := r.Header.Get("Origin"); origin != "" {
		if hostOf(origin) == host {
			return true
		}
		// Origin is present but mismatched — fail; do NOT fall back to
		// Referer in this case, because a present-but-wrong Origin is a
		// stronger signal than a missing one.
		return false
	}
	if ref := r.Header.Get("Referer"); ref != "" {
		return hostOf(ref) == host
	}
	// Neither header — almost certainly not a browser-driven request,
	// but for cookie-auth that means we can't verify same-origin. Fail
	// closed: the SPA always sets at least one of the two.
	return false
}

func hostOf(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		// Fallback parser for the rare browser that sends just an
		// authority without a scheme.
		s := strings.TrimPrefix(strings.TrimPrefix(rawURL, "https://"), "http://")
		if i := strings.IndexAny(s, "/?#"); i >= 0 {
			s = s[:i]
		}
		return s
	}
	return u.Host
}
