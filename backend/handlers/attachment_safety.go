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

// PAI-110 — attachment upload + serve hardening.
//
// Two policies, deliberately split:
//
//   - rejectActiveContent decides whether a freshly received upload may
//     enter storage at all. It runs against the actual first 512 bytes
//     (magic-byte sniff via http.DetectContentType) plus the
//     client-supplied Content-Type. We deny anything a browser could
//     execute when later served same-origin: HTML, XHTML, JS, SVG
//     (which can carry <script>), and a small set of executable
//     binaries. Trusting the client header alone would be a mistake;
//     trusting the sniff alone misses cases where the client lies and
//     the magic bytes are ambiguous (e.g. text/plain that's actually
//     a script). Rejecting if EITHER signal lights up is intentional.
//
//   - safeServePolicy decides what headers to attach when bytes leave
//     the server. Even with rejectActiveContent in place, legacy data
//     and ambiguous types exist; the serve path adopts a strict
//     allowlist for inline rendering — only static images and PDF.
//     Everything else is forced to download with octet-stream and a
//     restrictive CSP that prevents any embedded resource from
//     activating if a browser does try to render it.

package handlers

import (
	"net/http"
	"strings"
)

// activeContentTypes are MIME types a browser would execute or render
// with script capability when served same-origin. Lowercased, no
// parameters; callers must normalize before comparing.
var activeContentTypes = map[string]struct{}{
	"text/html":                   {},
	"application/xhtml+xml":       {},
	"application/javascript":      {},
	"text/javascript":             {},
	"application/x-javascript":    {},
	"image/svg+xml":               {},
	"application/x-msdownload":    {},
	"application/x-msdos-program": {},
	"application/x-executable":    {},
	"application/x-sh":            {},
	"application/x-shellscript":   {},
}

// inlineSafeTypes may be served with their real Content-Type and
// Content-Disposition: inline. Anything outside this set is forced to
// download. Kept narrow on purpose — adding a type here is a security
// decision, not a convenience one.
var inlineSafeTypes = map[string]struct{}{
	"image/png":       {},
	"image/jpeg":      {},
	"image/gif":       {},
	"image/webp":      {},
	"application/pdf": {},
}

// attachmentServeCSP is applied on every served attachment. Same shape
// as the SVG branding policy in branding.go but stricter (no inline
// styles for non-SVG bytes, no script execution at all). The `sandbox`
// directive disables popups, form submission, and same-origin
// privileges for the response — so even if a browser does render the
// payload, it cannot reach the user's session.
const attachmentServeCSP = "default-src 'none'; img-src 'self' data:; style-src 'unsafe-inline'; sandbox"

// normalizeContentType strips parameters ("text/html; charset=utf-8" ->
// "text/html") and lowercases. Empty input returns empty.
func normalizeContentType(ct string) string {
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = ct[:i]
	}
	return strings.ToLower(strings.TrimSpace(ct))
}

// looksLikeSVGBytes catches SVG payloads that http.DetectContentType
// returns as text/xml or text/plain. Any payload containing "<svg"
// in its first 512 bytes is treated as SVG regardless of the declared
// type — clients can and do lie about Content-Type to bypass naive
// allowlists.
func looksLikeSVGBytes(head []byte) bool {
	return strings.Contains(strings.ToLower(string(head)), "<svg")
}

// looksLikeHTMLBytes catches HTML payloads whose magic bytes don't
// trigger http.DetectContentType's HTML rule (e.g. payload starts with
// whitespace, or with a `<script>` tag without a `<html>` wrapper).
func looksLikeHTMLBytes(head []byte) bool {
	s := strings.ToLower(strings.TrimSpace(string(head)))
	return strings.HasPrefix(s, "<!doctype html") ||
		strings.HasPrefix(s, "<html") ||
		strings.Contains(s, "<script")
}

// rejectActiveContent returns a non-empty reason string if the upload
// looks like browser-executable content and must be rejected. The
// reason is the MIME type (or shape label) that triggered the rule —
// suitable for an error message and an audit log.
//
// Both the declared type (multipart header) and the sniffed type
// (http.DetectContentType on the first 512 bytes) are checked, plus a
// payload-shape pass for SVG/HTML in case both type signals were
// benign-looking but the bytes are not.
func rejectActiveContent(declaredCT string, head []byte) string {
	declared := normalizeContentType(declaredCT)
	detected := normalizeContentType(http.DetectContentType(head))

	if _, bad := activeContentTypes[declared]; bad {
		return declared
	}
	if _, bad := activeContentTypes[detected]; bad {
		return detected
	}
	if looksLikeSVGBytes(head) {
		return "image/svg+xml"
	}
	if looksLikeHTMLBytes(head) {
		return "text/html"
	}
	return ""
}

// safeServePolicy returns the headers used to serve a stored
// attachment. Only known-safe types render inline; everything else is
// forced to download as application/octet-stream. The restrictive CSP
// is applied uniformly so a legacy SVG (uploaded before PAI-110) still
// cannot run script when a user fetches it directly.
func safeServePolicy(detectedCT string) (servedCT, disposition, csp string) {
	ct := normalizeContentType(detectedCT)
	if _, ok := inlineSafeTypes[ct]; ok {
		return ct, "inline", attachmentServeCSP
	}
	return "application/octet-stream", "attachment", attachmentServeCSP
}
