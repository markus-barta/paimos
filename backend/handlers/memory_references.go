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

// PAI-347 — memory reference-count tracking. The schema choice (a)
// from the ticket: cheap counter columns on `issues`
// (reference_count + last_referenced_at, M100) updated whenever a
// memory entry is consumed in a context where its decay clock should
// reset. Two surfaces drive the bumps in v1:
//
//   1. `paimos session start --bundle full` (PAI-340) — every memory
//      included in the resolved bundle gets bumped via the
//      POST /api/projects/:id/memory/references endpoint that the CLI
//      calls after filterMemory has narrowed the set.
//   2. PAI-342's auto-suggest endpoint — every memory surfaced as a
//      candidate (score > 0) gets bumped server-side from
//      buildSuggestions before the response is written.
//
// The endpoint accepts a JSON body of memory ids (numeric, in the
// project) and increments the counter / updates the timestamp in a
// single UPDATE per call. Cross-project ids are rejected to avoid
// turning the endpoint into a backdoor for arbitrary memory probes.

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/markus-barta/paimos/backend/db"
)

// memoryReferenceBumpRequest is the on-the-wire shape for the
// reference-count update endpoint. `Source` is informational — we
// don't yet persist per-event audit (the schema choice is the cheap
// counter, not the append-only log) — but the field is accepted so
// future telemetry has a canonical place to live.
type memoryReferenceBumpRequest struct {
	IDs    []int64 `json:"ids"`
	Source string  `json:"source,omitempty"`
}

// memoryReferenceBumpResponse echoes the count of rows actually
// updated so the caller can surface "stale ids" without a second
// round-trip.
type memoryReferenceBumpResponse struct {
	Updated int64 `json:"updated"`
}

// BumpMemoryReferences powers POST /api/projects/:id/memory/references.
// Increments the counter and stamps last_referenced_at for every
// memory id in the body that lives in the URL project. Soft-deleted /
// non-memory rows are silently skipped; we never error on a partial
// match because the typical caller is a CLI that already narrowed
// the set, not a UI that needs strict validation.
func BumpMemoryReferences(w http.ResponseWriter, r *http.Request) {
	projectID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || projectID <= 0 {
		jsonError(w, "invalid project id", http.StatusBadRequest)
		return
	}
	var req memoryReferenceBumpRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid body", http.StatusBadRequest)
		return
	}
	if len(req.IDs) == 0 {
		jsonOK(w, memoryReferenceBumpResponse{Updated: 0})
		return
	}
	updated, err := bumpMemoryReferenceCounts(db.DB, projectID, req.IDs)
	if err != nil {
		jsonError(w, "update failed", http.StatusInternalServerError)
		return
	}
	jsonOK(w, memoryReferenceBumpResponse{Updated: updated})
}

// bumpMemoryReferenceCounts is the shared implementation used by both
// the HTTP endpoint and the in-process auto-suggest path. Open-coded
// IN-list (rather than per-id UPDATE) keeps the hot path one DB
// round-trip; the typical bundle has 30–60 memories and the suggest
// path tops out at the configured candidate count.
//
// `db` is parameterized so the test harness can pass a *sql.DB
// without depending on the package-level db.DB. Pass db.DB in
// production callers.
func bumpMemoryReferenceCounts(d *sql.DB, projectID int64, ids []int64) (int64, error) {
	if d == nil || len(ids) == 0 {
		return 0, nil
	}
	// De-dup so the same id in the input doesn't double-bump.
	seen := map[int64]bool{}
	uniq := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		uniq = append(uniq, id)
	}
	if len(uniq) == 0 {
		return 0, nil
	}
	// Build the placeholder list. The `?` placeholders bind positionally
	// after projectID so the args slice puts the project first.
	args := make([]any, 0, len(uniq)+1)
	args = append(args, projectID)
	placeholders := make([]byte, 0, len(uniq)*2)
	for i, id := range uniq {
		if i > 0 {
			placeholders = append(placeholders, ',')
		}
		placeholders = append(placeholders, '?')
		args = append(args, id)
	}
	// #nosec G202 -- IN-list is ?-only placeholder assembly; project id and memory ids are bound args.
	query := `UPDATE issues
		   SET reference_count    = reference_count + 1,
		       last_referenced_at = datetime('now')
		 WHERE project_id = ?
		   AND type       = 'memory'
		   AND deleted_at IS NULL
		   AND id IN (` + string(placeholders) + `)`
	res, err := d.Exec(query, args...)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}
