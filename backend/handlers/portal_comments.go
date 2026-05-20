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

package handlers

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/markus-barta/paimos/backend/db"
)

// PortalListIssueComments returns the customer-visible comments on an
// issue (PAI-475 visibility='external'). Project access is checked via
// the same gate as the rest of the portal; the issue must also carry
// the CUSTOMERPORTAL tag when enforcement is on, otherwise the request
// 404s — never disclose that an internal-only issue exists at this id.
//
// GET /api/portal/issues/{id}/comments
func PortalListIssueComments(w http.ResponseWriter, r *http.Request) {
	issueID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}

	// Resolve the project for access control + apply visibility gate in
	// a single round-trip. If the visibility condition is enforced and
	// the issue is missing CUSTOMERPORTAL, ErrNoRows fires and we 404.
	visFrag, visArg, visOn := portalVisibilityCondition("i")
	args := []any{issueID}
	if visOn {
		args = append(args, visArg)
	}
	var projectID int64
	err = db.DB.QueryRow(`
		SELECT i.project_id
		FROM issues i
		WHERE i.id = ? AND i.deleted_at IS NULL AND `+visFrag+`
	`, args...).Scan(&projectID)
	if err != nil {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	if !checkPortalAccess(r, projectID) {
		// 404 (not 403) is intentional — see PortalGetIssue for the
		// rationale around not disclosing internal-id existence.
		jsonError(w, "not found", http.StatusNotFound)
		return
	}

	rows, err := db.DB.Query(`
		SELECT c.id, c.issue_id, c.author_id,
		       COALESCE(NULLIF(u.nickname,''), u.username),
		       u.avatar_path, c.body, c.visibility, c.created_at
		FROM comments c
		LEFT JOIN users u ON u.id = c.author_id
		WHERE c.issue_id = ? AND c.visibility = 'external'
		ORDER BY c.created_at ASC
	`, issueID)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	comments := []Comment{}
	for rows.Next() {
		var c Comment
		if err := rows.Scan(&c.ID, &c.IssueID, &c.AuthorID, &c.Author, &c.AvatarPath, &c.Body, &c.Visibility, &c.CreatedAt); err == nil {
			comments = append(comments, c)
		}
	}
	jsonOK(w, comments)
}
