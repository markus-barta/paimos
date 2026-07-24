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

// PAI-351 slice 2 — acknowledge a memory entry's "needs re-review" flag.
// The flag is derived (a depends_on parent was revised after this entry was
// last reviewed); acknowledging stamps deps_reviewed_at = now, which clears
// the computed flag until a parent is revised again. A dedicated endpoint
// (not clear-via-PUT) keeps the acknowledge an intentional act — editing the
// entry's own body never silently clears its flag.

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/inspr-at/paimos/backend/db"
	"github.com/inspr-at/paimos/backend/handlers/knowledge"
)

// MarkMemoryReviewed powers
// POST /api/projects/:id/knowledge/memory/:slug/reviewed (admin).
func MarkMemoryReviewed(w http.ResponseWriter, r *http.Request) {
	projectID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || projectID <= 0 {
		jsonError(w, "invalid project id", http.StatusBadRequest)
		return
	}
	slug := strings.TrimSpace(chi.URLParam(r, "slug"))
	if slug == "" {
		jsonError(w, "slug required", http.StatusBadRequest)
		return
	}
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	res, err := db.DB.Exec(
		`UPDATE issues SET deps_reviewed_at = ?
		  WHERE project_id = ? AND type = 'memory' AND slug = ? AND deleted_at IS NULL`,
		now, projectID, slug)
	if err != nil {
		jsonError(w, "update failed", http.StatusInternalServerError)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	mod, err := knowledge.RouteByType("memory")
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	var id int64
	if err := db.DB.QueryRow(
		`SELECT id FROM issues WHERE project_id=? AND type='memory' AND slug=? AND deleted_at IS NULL`,
		projectID, slug).Scan(&id); err != nil {
		jsonError(w, "reload failed", http.StatusInternalServerError)
		return
	}
	out, err := knowledge.LoadOneByID(id, mod)
	if err != nil {
		jsonError(w, "reload failed", http.StatusInternalServerError)
		return
	}
	// Fire the memory-changed event so subscribers refetch and pick up the
	// cleared flag. The rev itself is unchanged — acknowledge touches only
	// deps_reviewed_at (not content), so this is an event nudge, not a rev bump.
	publishKnowledgeChange(projectID, "memory", out.Slug, knowledgeRevForOutput(out))
	jsonOK(w, out)
}
