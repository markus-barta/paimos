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

// PAI-119: serve an OpenAPI 3.1 contract from /api/openapi.json.
//
// The doc is intentionally focused on the canonical surface — auth,
// users, projects, issues, comments, time entries, attachments,
// documents, incidents and the GDPR ops endpoints. Internal one-off
// admin tooling (jira/mite imports, dev test reports, branding writes)
// is omitted by design: scriptable callers don't need them, and keeping
// the spec narrow means reviewers can audit it. Adding new sections is
// a one-function change.

package handlers

import (
	_ "embed"
	"net/http"
)

//go:embed openapi.json
var openAPIDoc []byte

// GetOpenAPI — GET /api/openapi.json
//
// Public. Returns the OpenAPI 3.1 contract for PAIMOS. The bytes are
// embedded at build time so the doc is always in sync with the binary
// it ships with.
func GetOpenAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=300")
	_, _ = w.Write(openAPIDoc)
}
