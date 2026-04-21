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

package auth

import (
	"database/sql"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/markus-barta/paimos/backend/db"
)

// issueKeyPattern matches strings shaped like "PAI-83" or "PMO26-639":
// one uppercase letter, up to 15 more uppercase alphanumerics, dash, digits.
// Soft-bound so obviously-not-keys ("a", "x-1") fail fast without a DB hit.
var issueKeyPattern = regexp.MustCompile(`^[A-Z][A-Z0-9]{0,15}-\d+$`)

// IsIssueKey reports whether s has the shape of an issue key. Shape only —
// does not check that the key exists.
func IsIssueKey(s string) bool { return issueKeyPattern.MatchString(s) }

// ResolveIssueRef accepts either a numeric issue id ("462") or an issue key
// ("PAI-83") and returns the corresponding internal issue ID.
//
// Soft-deleted issues resolve successfully — visibility is the handler's
// job (e.g. RestoreIssue and PurgeIssue must be able to target trashed
// rows). Returns (0, false) when s is neither a valid integer nor a key
// that matches an existing (project.key, issue.issue_number) pair.
func ResolveIssueRef(s string) (int64, bool) {
	if n, err := strconv.ParseInt(s, 10, 64); err == nil && n > 0 {
		return n, true
	}
	if !IsIssueKey(s) {
		return 0, false
	}
	dash := strings.LastIndex(s, "-")
	projectKey := s[:dash]
	issueNum, err := strconv.Atoi(s[dash+1:])
	if err != nil {
		return 0, false
	}
	var id int64
	err = db.DB.QueryRow(`
		SELECT i.id FROM issues i
		JOIN projects p ON p.id = i.project_id
		WHERE p.key = ? AND i.issue_number = ?
	`, projectKey, issueNum).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false
	}
	if err != nil {
		return 0, false
	}
	return id, true
}

// rewriteIssueRefToID rewrites the chi "id" URL param from an issue key to
// the numeric issue ID so downstream middleware and handlers see the same
// shape they always did. Three outcomes:
//
//   - ok=true,  malformed=false — param was numeric, or key resolved
//   - ok=false, malformed=true  — not a number or an issue-key shape
//   - ok=false, malformed=false — key shape, but no matching issue
//
// Callers surface these as 200/400/404 respectively.
func rewriteIssueRefToID(r *http.Request) (ok bool, malformed bool) {
	raw := chi.URLParam(r, "id")
	if raw == "" {
		return false, true
	}
	if _, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return true, false
	}
	if !IsIssueKey(raw) {
		return false, true
	}
	id, found := ResolveIssueRef(raw)
	if !found {
		return false, false
	}
	rctx := chi.RouteContext(r.Context())
	if rctx == nil {
		return false, true
	}
	// URLParams.Add appends; chi's URLParam getter iterates in reverse,
	// so the appended numeric value shadows the key-shaped one without
	// us having to mutate the existing slice in place.
	rctx.URLParams.Add("id", strconv.FormatInt(id, 10))
	return true, false
}
