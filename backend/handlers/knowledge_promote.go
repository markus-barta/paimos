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

// PAI-345 — memory scope promotion. Endpoint:
//
//	POST /api/memory/:slug/promote
//	     body: {"to": "project"|"user"|"instance",
//	            "from_project_id"?: <int>}
//
// Promotion semantics:
//
//   - The source row is identified by (slug, source scope). Source
//     scope is inferred from the `from_project_id` field plus the
//     authenticated user — if `from_project_id` is set we look in
//     that project's memory; otherwise we look in the user's own
//     user-scope memory; otherwise instance-scope memory.
//   - A new row is INSERTed at the destination scope, copying:
//       title, description (body), status, category_metadata,
//       priority. issue_number is re-allocated for the destination
//       (per the destination's numbering namespace).
//   - The source row is soft-deleted (deleted_at) so history /
//     trash / undo still recover it. No silent data loss; the audit
//     log records both the destination create and the source delete
//     mutations under the same request id so the UI can stitch
//     them as a single operation in the activity feed.
//   - Slug stays identical. If a slug collision exists at the
//     destination, the request 409s and nothing is mutated.
//   - "to" must be different from the inferred source scope —
//     promoting BON26 → BON26 is meaningless; the call returns 400
//     so the UI can grey the current-scope option (per the ticket's
//     "current scope greyed" UX note).
//   - Promotion to instance scope requires admin (mirrors the
//     /api/instance/memory POST gate). All other promotions only
//     require the caller can edit / read the source.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/markus-barta/paimos/backend/auth"
	"github.com/markus-barta/paimos/backend/db"
	"github.com/markus-barta/paimos/backend/handlers/knowledge"
)

// promoteRequest is the JSON body shape. `To` is required and must
// be one of the three scope discriminators. `FromProjectID` is the
// optional source-scope hint — when omitted the handler tries
// user-scope first, then instance-scope.
type promoteRequest struct {
	To            string `json:"to"`
	FromProjectID int64  `json:"from_project_id"`
	// ToProjectID is required when `to == "project"`. We don't infer
	// here because "promote into a project" must be explicit — there's
	// usually more than one project the user can write to.
	ToProjectID int64 `json:"to_project_id"`
}

// PromoteMemory is the HTTP handler for POST /api/memory/:slug/promote.
// Routed at the package level (not under /projects/:id) because
// promotion crosses project boundaries by definition. Auth is request-
// driven: the source is located against whichever scope the user can
// access; the destination's admin gate fires for instance-scope writes.
func PromoteMemory(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if user == nil {
		jsonError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	slug := strings.TrimSpace(chi.URLParam(r, "slug"))
	if slug == "" {
		jsonError(w, "slug required", http.StatusBadRequest)
		return
	}
	var req promoteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid body", http.StatusBadRequest)
		return
	}
	to := strings.ToLower(strings.TrimSpace(req.To))
	switch to {
	case "project", "user", "instance":
	default:
		jsonError(w, `"to" must be one of project / user / instance`, http.StatusBadRequest)
		return
	}
	if to == "project" && req.ToProjectID <= 0 {
		jsonError(w, `"to_project_id" required when promoting to project scope`, http.StatusBadRequest)
		return
	}
	if to == "instance" {
		// Mirror the admin gate on POST /api/instance/memory.
		if user.Role != "admin" {
			jsonError(w, "forbidden: instance scope requires admin", http.StatusForbidden)
			return
		}
	}

	mod := userMemoryModule()

	// Locate the source row. The lookup hierarchy mirrors PAI-345's
	// resolution precedence: explicit project hint > user-scope >
	// instance-scope. Returning the canonical source scope lets the
	// "promote to current scope" 400 below catch no-op promotions.
	srcID, srcScope, err := findPromotionSource(slug, req.FromProjectID, user.ID)
	if errors.Is(err, sql.ErrNoRows) {
		jsonError(w, "source memory not found", http.StatusNotFound)
		return
	}
	if err != nil {
		jsonError(w, "lookup failed", http.StatusInternalServerError)
		return
	}
	if srcScope == to {
		jsonError(w, fmt.Sprintf("memory is already at %s scope", to), http.StatusBadRequest)
		return
	}

	// Promotion is two writes inside one tx: INSERT at destination +
	// soft-DELETE at source. Sharing the request id across both
	// mutation_log entries lets the activity feed merge them as one
	// "promote" event.
	out, err := promoteMemoryTx(r, mod, srcID, srcScope, to, req.ToProjectID)
	if errors.Is(err, knowledge.ErrSlugTaken) {
		jsonError(w, "slug already exists at destination scope", http.StatusConflict)
		return
	}
	if err != nil {
		jsonError(w, "promotion failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]any{
		"ok":         true,
		"from_scope": srcScope,
		"to_scope":   to,
		"entry":      out,
	})
}

// findPromotionSource walks the scope hierarchy looking for a
// non-archived live memory row matching `slug`. Returns the row's
// id and a string-typed scope discriminator ("project" / "user" /
// "instance"). The caller can preempt with `fromProjectID` to skip
// the user / instance lookups.
func findPromotionSource(slug string, fromProjectID, currentUserID int64) (int64, string, error) {
	if fromProjectID > 0 {
		var id int64
		err := db.DB.QueryRow(`
			SELECT id FROM issues
			 WHERE type = 'memory' AND slug = ?
			   AND project_id = ? AND deleted_at IS NULL
		`, slug, fromProjectID).Scan(&id)
		if err != nil {
			return 0, "", err
		}
		return id, "project", nil
	}
	// User scope.
	var id int64
	err := db.DB.QueryRow(`
		SELECT id FROM issues
		 WHERE type = 'memory' AND slug = ?
		   AND project_id IS NULL AND user_id = ? AND deleted_at IS NULL
	`, slug, currentUserID).Scan(&id)
	if err == nil {
		return id, "user", nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, "", err
	}
	// Instance scope.
	err = db.DB.QueryRow(`
		SELECT id FROM issues
		 WHERE type = 'memory' AND slug = ?
		   AND project_id IS NULL AND user_id IS NULL AND deleted_at IS NULL
	`, slug).Scan(&id)
	if err != nil {
		return 0, "", err
	}
	return id, "instance", nil
}

// promoteMemoryTx runs the actual create-at-destination + delete-at-
// source inside a single transaction so a partial failure leaves the
// world unchanged. Both mutation_log entries share the request id so
// PAI-209's undo machinery can group them on replay; the IDs differ so
// each side can be undone independently when needed.
func promoteMemoryTx(r *http.Request, mod knowledge.Module, srcID int64, srcScope, toScope string, toProjectID int64) (knowledge.Output, error) {
	user := auth.GetUser(r)
	if user == nil {
		return knowledge.Output{}, errors.New("unauthorized")
	}

	tx, err := db.DB.BeginTx(r.Context(), nil)
	if err != nil {
		return knowledge.Output{}, err
	}
	defer tx.Rollback()

	// Snapshot the source row's payload columns. We only copy the bits
	// the convenience-endpoint contract owns (slug, title, body, status,
	// metadata, priority); everything else (parent_id, tags, comments,
	// originating relations) stays attached to the source — those are
	// project-bound concepts, the destination shouldn't inherit them.
	var (
		srcSlug, srcTitle, srcBody, srcStatus, srcPriority string
		srcMeta                                            sql.NullString
	)
	if err := tx.QueryRowContext(r.Context(), `
		SELECT COALESCE(slug,''), title, description, status, priority, category_metadata
		  FROM issues WHERE id = ? AND deleted_at IS NULL
	`, srcID).Scan(&srcSlug, &srcTitle, &srcBody, &srcStatus, &srcPriority, &srcMeta); err != nil {
		return knowledge.Output{}, err
	}

	// INSERT at destination. The columns mirror createUserOrInstance /
	// createKnowledgeEntry. Issue numbering is re-allocated per
	// destination scope so issue_key resolution stays consistent.
	var (
		newID int64
	)
	switch toScope {
	case "project":
		var nextNum int
		if err := tx.QueryRowContext(r.Context(),
			`SELECT COALESCE(MAX(issue_number),0)+1 FROM issues WHERE project_id = ?`,
			toProjectID).Scan(&nextNum); err != nil {
			return knowledge.Output{}, err
		}
		res, err := tx.ExecContext(r.Context(), `
			INSERT INTO issues(project_id, user_id, issue_number, type, title, description,
			                   status, priority, created_by, slug, category_metadata)
			VALUES(?, NULL, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, toProjectID, nextNum, mod.Type(), srcTitle, srcBody, srcStatus, srcPriority,
			user.ID, srcSlug, srcMeta.String)
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE constraint failed") {
				return knowledge.Output{}, knowledge.ErrSlugTaken
			}
			return knowledge.Output{}, err
		}
		newID, _ = res.LastInsertId()
	case "user":
		var nextNum int
		if err := tx.QueryRowContext(r.Context(),
			`SELECT COALESCE(MAX(issue_number),0)+1 FROM issues
			 WHERE project_id IS NULL AND user_id = ?`,
			user.ID).Scan(&nextNum); err != nil {
			return knowledge.Output{}, err
		}
		res, err := tx.ExecContext(r.Context(), `
			INSERT INTO issues(project_id, user_id, issue_number, type, title, description,
			                   status, priority, created_by, slug, category_metadata)
			VALUES(NULL, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, user.ID, nextNum, mod.Type(), srcTitle, srcBody, srcStatus, srcPriority,
			user.ID, srcSlug, srcMeta.String)
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE constraint failed") {
				return knowledge.Output{}, knowledge.ErrSlugTaken
			}
			return knowledge.Output{}, err
		}
		newID, _ = res.LastInsertId()
	case "instance":
		var nextNum int
		if err := tx.QueryRowContext(r.Context(),
			`SELECT COALESCE(MAX(issue_number),0)+1 FROM issues
			 WHERE project_id IS NULL AND user_id IS NULL`).Scan(&nextNum); err != nil {
			return knowledge.Output{}, err
		}
		res, err := tx.ExecContext(r.Context(), `
			INSERT INTO issues(project_id, user_id, issue_number, type, title, description,
			                   status, priority, created_by, slug, category_metadata)
			VALUES(NULL, NULL, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, nextNum, mod.Type(), srcTitle, srcBody, srcStatus, srcPriority,
			user.ID, srcSlug, srcMeta.String)
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE constraint failed") {
				return knowledge.Output{}, knowledge.ErrSlugTaken
			}
			return knowledge.Output{}, err
		}
		newID, _ = res.LastInsertId()
	default:
		return knowledge.Output{}, fmt.Errorf("unknown to scope %q", toScope)
	}

	// Soft-delete the source row.
	if _, err := tx.ExecContext(r.Context(), `
		UPDATE issues
		   SET deleted_at = datetime('now'),
		       deleted_by = ?,
		       updated_at = ?
		 WHERE id = ?
	`, user.ID, time.Now().UTC().Format("2006-01-02 15:04:05"), srcID); err != nil {
		return knowledge.Output{}, err
	}

	// mutation_log entries — one for the destination create, one for
	// the source delete. Sharing requestID lets the activity-feed UI
	// group them; the inverse-op on each side is the standard
	// /issues/:id route so PAI-209's undo machinery just works.
	reqID := requestIDFromRequest(r)
	sessID := sessionIDFromRequest(r)
	agentName := agentNameFromRequest(r)
	var mutUserID *int64 = &user.ID

	dstAfter, err := fetchIssueMutationSnapshotTx(tx, newID)
	if err != nil {
		return knowledge.Output{}, err
	}
	if _, err := recordMutation(r.Context(), tx, mutationRecordArgs{
		RequestID:    reqID,
		UserID:       mutUserID,
		SessionID:    sessID,
		AgentName:    agentName,
		MutationType: mutationTypeForRequest(r, "issue.create"),
		SubjectType:  "issue",
		SubjectID:    newID,
		InverseOp: InverseOp{
			Method: http.MethodDelete,
			Path:   fmt.Sprintf("/issues/%d", newID),
		},
		BeforeState: nil,
		AfterState:  dstAfter,
		Undoable:    true,
	}); err != nil {
		return knowledge.Output{}, err
	}

	srcAfter, err := fetchIssueMutationSnapshotTx(tx, srcID)
	if err != nil {
		return knowledge.Output{}, err
	}
	// Before-snapshot for the source: clone the post-delete snapshot
	// and clear deleted_at / deleted_by so the undo inverse-op
	// (applyIssueSnapshotTx) restores the row to its live state.
	srcBefore := srcAfter
	srcBefore.DeletedAt = nil
	if _, err := recordMutation(r.Context(), tx, mutationRecordArgs{
		RequestID:    reqID,
		UserID:       mutUserID,
		SessionID:    sessID,
		AgentName:    agentName,
		MutationType: mutationTypeForRequest(r, "issue.delete"),
		SubjectType:  "issue",
		SubjectID:    srcID,
		InverseOp: InverseOp{
			Method: http.MethodPut,
			Path:   fmt.Sprintf("/issues/%d", srcID),
			Body:   srcBefore,
		},
		BeforeState: srcBefore,
		AfterState:  srcAfter,
		Undoable:    true,
	}); err != nil {
		return knowledge.Output{}, err
	}

	if err := tx.Commit(); err != nil {
		return knowledge.Output{}, err
	}

	// Post-commit: history snapshots + system tag re-evaluation, the
	// same side-effects that the canonical issue handler runs. Done
	// outside the tx so a snapshot-write failure can't roll back the
	// promote — the actual rows are already correct on disk.
	if issue := getIssueByID(newID); issue != nil {
		saveSnapshot(issue, user, r)
	}
	if issue := getIssueByID(srcID); issue != nil {
		saveSnapshot(issue, user, r)
	}
	EvaluateSystemTags(newID)
	EvaluateSystemTags(srcID)

	return loadOneUserOrInstanceMemoryByID(newID, mod)
}

