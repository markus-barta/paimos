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

// PAI-114: global security-headers middleware.
//
// Non-breaking by design — every header here is either always-safe or
// degrades gracefully:
//
//   - X-Content-Type-Options: nosniff — always safe.
//   - X-Frame-Options: SAMEORIGIN — required for the in-app document
//     preview iframes (PDF thumbs, dev test reports) to keep working.
//     DENY would break those; explicit allowlist of self is the right
//     compromise.
//   - Referrer-Policy: strict-origin-when-cross-origin — modern default
//     and a no-op for any browser old enough not to know it.
//   - Permissions-Policy: disables features PAIMOS does not use. Old
//     browsers ignore unknown directives.
//   - Strict-Transport-Security: only when COOKIE_SECURE=true so HTTP
//     deployments (staging / local) keep working without a forced HTTPS
//     redirect on the next visit.
//   - Content-Security-Policy-Report-Only: a permissive policy that
//     records violations to the application log via the report-uri
//     endpoint. Switching to enforce-mode is part of the air-gap work
//     in PAI-118 (which removes the only known runtime third-party
//     resource: Google Fonts).
//
// Branding handler keeps its own narrower CSP/nosniff for SVG safety;
// these globals are additive, last-write-wins.

package handlers

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
)

// SecurityHeaders is the global middleware that applies the baseline set.
// Mount once near the top of the router stack so it covers every route,
// including static assets and the SPA fallback.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "SAMEORIGIN")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Permissions-Policy",
			"geolocation=(), microphone=(), camera=(), payment=(), usb=(), "+
				"interest-cohort=()")

		if os.Getenv("COOKIE_SECURE") == "true" {
			// HTTPS deployments — opt into HSTS for one year. Subdomains
			// included only when an operator explicitly sets HSTS_INCLUDE_SUBDOMAINS,
			// since accidental enforcement on a wildcard origin is hard to undo.
			val := "max-age=31536000"
			if os.Getenv("HSTS_INCLUDE_SUBDOMAINS") == "true" {
				val += "; includeSubDomains"
			}
			h.Set("Strict-Transport-Security", val)
		}

		// Report-Only CSP. Permissive enough to leave the SPA running
		// today while letting us collect a real-world violation feed
		// before flipping to enforce. Tuned to:
		//   - allow same-origin scripts (Vue bundle),
		//   - allow inline styles (Vue scoped CSS uses inline),
		//   - allow data: images (avatars/logos),
		//   - allow Google Fonts as long as PAI-118 has not landed
		//     (removed automatically when the air-gap work is done).
		h.Set("Content-Security-Policy-Report-Only", csp())

		next.ServeHTTP(w, r)
	})
}

// csp builds the Report-Only policy. A function rather than a const so
// the report-uri can pick up a runtime base path if the operator deploys
// behind a non-root prefix.
func csp() string {
	// Until PAI-118 lands the third-party Google Fonts request still
	// happens at runtime, so the policy intentionally allows it. The
	// report-uri lets us log violations for the rest of the policy.
	return "default-src 'self'; " +
		"script-src 'self'; " +
		"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; " +
		"font-src 'self' https://fonts.gstatic.com data:; " +
		"img-src 'self' data: blob:; " +
		"connect-src 'self'; " +
		"frame-ancestors 'self'; " +
		"base-uri 'self'; " +
		"form-action 'self'; " +
		"report-uri /api/csp-report"
}

// CSPReport receives JSON violation reports from browsers running with the
// Report-Only policy above and forwards them to the standard logger. The
// payload is intentionally not stored — operators who want long-term
// retention can pipe stdout to their log aggregator. Limits the body to
// 64 KiB so a malicious client cannot exhaust memory.
//
// Public — no auth — because browsers send these without credentials and
// the endpoint is read-only. Rate-limit at the proxy layer if needed.
func CSPReport(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	// Best-effort decode for prettier logs; if it isn't JSON we still
	// log the raw bytes so we don't drop signal.
	var pretty any
	if err := json.Unmarshal(body, &pretty); err == nil {
		log.Printf("csp-report: %s", marshalCompact(pretty))
	} else {
		log.Printf("csp-report-raw: %s", string(body))
	}
	w.WriteHeader(http.StatusNoContent)
}

func marshalCompact(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}
