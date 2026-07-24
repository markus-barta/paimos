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

// PAI-353 — knowledge convenience-endpoint write hooks. The
// `handlers/knowledge` sub-package owns the per-type Module registry
// and the HTTP shape, but its CRUD originally hit `db.Exec(...)`
// directly. That meant knowledge entries skipped:
//
//   1. issue_history snapshots (PAI-324 attribution columns).
//   2. mutation_log rows (PAI-209 undo / redo).
//   3. system tag re-evaluation, search index updates.
//
// Routing the writes back through the public UpdateIssue handler
// would mean synthesizing an HTTP request inside Go (Style A from
// PAI-353). Style B — extract the side-effect orchestration into
// shared functions — is cleaner. Putting those functions here, in
// the parent handlers package, keeps the import direction
// one-way: knowledge declares hook variables; this file populates
// them at init() time. No circular import, no dependency-injection
// container.
//
// The implementations are deliberately the *minimal* port of the
// existing direct-SQL paths plus a wrapper that:
//   - opens a transaction
//   - takes a before-snapshot via fetchIssueMutationSnapshotTx
//   - runs the knowledge-specific UPDATE / INSERT
//   - takes an after-snapshot
//   - calls recordMutation (mutation_log + undo stack accounting)
//   - commits
//   - calls saveSnapshot (issue_history + agent attribution)
//   - calls EvaluateSystemTags so budget thresholds get a chance to
//     re-fire when status changes
//
// The CREATE path's mutation_log entry uses the canonical
// "issue.create" / "issue.delete" mutation types so the existing
// undo machinery routes them through the same DELETE / restore
// inverse-op handlers as a regular issue CreateIssue would.

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/inspr-at/paimos/backend/auth"
	"github.com/inspr-at/paimos/backend/db"
	"github.com/inspr-at/paimos/backend/handlers/knowledge"
)

func init() {
	// Register the canonical-issue write paths with the knowledge
	// sub-package. Production binaries always import this package, so
	// the hooks are always populated; sub-package unit tests still
	// fall back to the direct-SQL implementations the knowledge file
	// keeps for parity.
	knowledge.CreateEntryHook = createKnowledgeEntry
	knowledge.UpdateEntryHook = updateKnowledgeEntry
	knowledge.DeleteEntryHook = deleteKnowledgeEntry
}

// createKnowledgeEntry inserts a fresh knowledge entry, then writes
// the issue_history snapshot + mutation_log row that
// /api/projects/:id/issues/POST would. The mutation type is
// "issue.create" so PAI-209's existing inverse-op handlers (which
// soft-delete the issue) work without a knowledge-specific dispatch.
func createKnowledgeEntry(r *http.Request, projectID int64, mod knowledge.Module, in knowledge.Input) (knowledge.Output, error) {
	metaJSON, err := mod.MarshalMeta(in.Metadata)
	if err != nil {
		return knowledge.Output{}, err
	}
	status := strings.TrimSpace(in.Status)
	if status == "" {
		status = mod.DefaultStatus()
	}
	var createdBy *int64
	if u := auth.GetUser(r); u != nil {
		createdBy = &u.ID
	}

	tx, err := db.DB.BeginTx(r.Context(), nil)
	if err != nil {
		return knowledge.Output{}, err
	}
	defer tx.Rollback()

	// Same per-project numbering scheme CreateIssue uses, so issue_key
	// resolution stays consistent (PAI-339: a memory entry's PAI-NNN
	// alias works the same as a ticket's).
	nextNum, err := db.NextIssueNumber(r.Context(), tx, projectID)
	if err != nil {
		return knowledge.Output{}, err
	}

	res, err := tx.ExecContext(r.Context(), `
		INSERT INTO issues(project_id, issue_number, type, title, description, status, priority,
		                   created_by, slug, category_metadata)
		VALUES(?,?,?,?,?,?,?,?,?,?)
	`, projectID, nextNum, mod.Type(), strings.TrimSpace(in.Title), in.Body, status, "medium",
		createdBy, in.Slug, metaJSON)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return knowledge.Output{}, knowledge.ErrSlugTaken
		}
		return knowledge.Output{}, err
	}
	id, _ := res.LastInsertId()

	// Snapshot the new row inside the same tx so before/after deltas
	// are diff-coherent — recordMutation hashes both sides for the
	// undo-stack equality check.
	afterSnap, err := fetchIssueMutationSnapshotTx(tx, id)
	if err != nil {
		return knowledge.Output{}, err
	}
	var userID *int64
	if u := auth.GetUser(r); u != nil {
		userID = &u.ID
	}
	if _, err := recordMutation(r.Context(), tx, mutationRecordArgs{
		RequestID:    requestIDFromRequest(r),
		UserID:       userID,
		SessionID:    sessionIDFromRequest(r),
		AgentName:    agentNameFromRequest(r),
		MutationType: mutationTypeForRequest(r, "issue.create"),
		SubjectType:  "issue",
		SubjectID:    id,
		// Inverse of create is delete — PAI-209's executeInverseOpTx
		// already understands the issue DELETE shape; reuse it so a
		// later undo on a freshly-created knowledge entry trashes it
		// rather than running a knowledge-specific path.
		InverseOp: InverseOp{
			Method: http.MethodDelete,
			Path:   fmt.Sprintf("/issues/%d", id),
		},
		BeforeState: nil,
		AfterState:  afterSnap,
		Undoable:    true,
	}); err != nil {
		return knowledge.Output{}, err
	}
	if err := tx.Commit(); err != nil {
		return knowledge.Output{}, err
	}

	// PAI-324 attribution: snapshot post-commit so an aborted tx
	// doesn't leave a phantom history row. saveSnapshot itself reads
	// the X-Paimos-Agent-Name + session-id headers off r.
	if issue := getIssueByID(id); issue != nil {
		saveSnapshot(issue, auth.GetUser(r), r)
	}
	// Knowledge entries can have status, so let the budget-threshold
	// rules look at them too — a no-op for the common case but cheap.
	EvaluateSystemTags(id)

	out, err := knowledge.LoadOneByID(id, mod)
	if err != nil {
		return out, err
	}
	// PAI-341 — fan out an SSE event so any active `paimos sync watch`
	// subscriber can re-pull the new entry. Best-effort: the broker
	// never blocks the request path.
	publishKnowledgeChange(projectID, mod.Type(), out.Slug, knowledgeRevForOutput(out))
	return out, nil
}

// normalizeBody canonicalises a knowledge body for change detection:
// CRLF→LF then trim surrounding whitespace, so a pure line-ending or
// trailing-whitespace edit doesn't trip the content-revised signal.
func normalizeBody(s string) string {
	return strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n"))
}

// updateKnowledgeEntry mutates an existing knowledge entry, then
// records the mutation_log + history snapshot identical to UpdateIssue.
// The SQL is intentionally narrow — it touches the columns the
// convenience endpoint owns (title, description/body, slug,
// category_metadata, status) and nothing else. Cross-cuts (relations,
// tags, comments) live on the regular /issues path.
func updateKnowledgeEntry(r *http.Request, projectID int64, mod knowledge.Module, currentSlug string, in knowledge.Input) (knowledge.Output, error) {
	now := time.Now().UTC().Format("2006-01-02 15:04:05")

	tx, err := db.DB.BeginTx(r.Context(), nil)
	if err != nil {
		return knowledge.Output{}, err
	}
	defer tx.Rollback()

	var existingID int64
	var oldBody string
	var metaJSON string
	if err := tx.QueryRowContext(r.Context(),
		`SELECT id, COALESCE(description,''), COALESCE(category_metadata,'') FROM issues WHERE project_id=? AND type=? AND slug=? AND deleted_at IS NULL`,
		projectID, mod.Type(), currentSlug).Scan(&existingID, &oldBody, &metaJSON); err != nil {
		return knowledge.Output{}, err
	}
	if in.MetadataSet {
		var err error
		metaJSON, err = mod.MarshalMeta(in.Metadata)
		if err != nil {
			return knowledge.Output{}, err
		}
	}

	beforeSnap, err := fetchIssueMutationSnapshotTx(tx, existingID)
	if err != nil {
		return knowledge.Output{}, err
	}

	// PAI-351 slice 2 — when a MEMORY entry's body meaningfully changes,
	// stamp content_revised_at so its dependents recompute "needs re-review".
	// Body-only on purpose: title / slug / status / metadata edits (incl. the
	// parent editing its own depends_on) deliberately do not trip the signal.
	bodyChanged := mod.Type() == "memory" && normalizeBody(oldBody) != normalizeBody(in.Body)

	statusUpdate := strings.TrimSpace(in.Status)
	updateSQL := `UPDATE issues
		   SET title             = ?,
		       description       = ?,
		       slug              = ?,
		       category_metadata = ?,
		       updated_at        = ?`
	args := []any{strings.TrimSpace(in.Title), in.Body, in.Slug, metaJSON, now}
	if statusUpdate != "" {
		updateSQL += `, status = ?`
		args = append(args, statusUpdate)
	}
	if bodyChanged {
		updateSQL += `, content_revised_at = ?`
		args = append(args, now)
	}
	updateSQL += ` WHERE id = ?`
	args = append(args, existingID)

	if _, err := tx.ExecContext(r.Context(), updateSQL, args...); err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return knowledge.Output{}, knowledge.ErrSlugTaken
		}
		return knowledge.Output{}, err
	}

	afterSnap, err := fetchIssueMutationSnapshotTx(tx, existingID)
	if err != nil {
		return knowledge.Output{}, err
	}
	var userID *int64
	if u := auth.GetUser(r); u != nil {
		userID = &u.ID
	}
	if _, err := recordMutation(r.Context(), tx, mutationRecordArgs{
		RequestID:    requestIDFromRequest(r),
		UserID:       userID,
		SessionID:    sessionIDFromRequest(r),
		AgentName:    agentNameFromRequest(r),
		MutationType: mutationTypeForRequest(r, "issue.update"),
		SubjectType:  "issue",
		SubjectID:    existingID,
		InverseOp: InverseOp{
			Method: http.MethodPut,
			Path:   fmt.Sprintf("/issues/%d", existingID),
			Body:   beforeSnap,
		},
		BeforeState: beforeSnap,
		AfterState:  afterSnap,
		Undoable:    true,
	}); err != nil {
		return knowledge.Output{}, err
	}
	if err := tx.Commit(); err != nil {
		return knowledge.Output{}, err
	}

	if issue := getIssueByID(existingID); issue != nil {
		saveSnapshot(issue, auth.GetUser(r), r)
	}
	EvaluateSystemTags(existingID)

	out, err := knowledge.LoadOneByID(existingID, mod)
	if err != nil {
		return out, err
	}
	// PAI-341 — see createKnowledgeEntry. UPDATE invalidates the cached
	// rev so subscribers re-pull. Use the new slug (post-rename) so the
	// event addresses the row's current identifier.
	publishKnowledgeChange(projectID, mod.Type(), out.Slug, knowledgeRevForOutput(out))
	return out, nil
}

// deleteKnowledgeEntry soft-deletes a knowledge entry the same way
// /api/issues/:id DELETE does, but scoped to a (project_id, type,
// slug) tuple so the convenience-endpoint URL space stays contained.
// Returns 0 row-count when the slug doesn't match — the dispatcher
// maps that to 404.
func deleteKnowledgeEntry(r *http.Request, projectID int64, mod knowledge.Module, slug string) (int64, error) {
	var deletedBy *int64
	user := auth.GetUser(r)
	if user != nil {
		deletedBy = &user.ID
	}

	tx, err := db.DB.BeginTx(r.Context(), nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	// Lookup ID first so the snapshot + mutation_log entry can address
	// the row by primary key. Soft-deleting by slug directly is
	// possible, but then mutation_log.subject_id would be a guess.
	var existingID int64
	err = tx.QueryRowContext(r.Context(),
		`SELECT id FROM issues WHERE project_id=? AND type=? AND slug=? AND deleted_at IS NULL`,
		projectID, mod.Type(), slug).Scan(&existingID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}

	beforeSnap, err := fetchIssueMutationSnapshotTx(tx, existingID)
	if err != nil {
		return 0, err
	}

	res, err := tx.ExecContext(r.Context(), `
		UPDATE issues
		   SET deleted_at = datetime('now'),
		       deleted_by = ?
		 WHERE id = ?
		   AND deleted_at IS NULL
	`, deletedBy, existingID)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		// Race: someone else trashed it between the SELECT and UPDATE.
		// Treat as 404 to mirror the original direct-SQL behavior.
		return 0, nil
	}

	afterSnap, err := fetchIssueMutationSnapshotTx(tx, existingID)
	if err != nil {
		return 0, err
	}
	var userID *int64
	if user != nil {
		userID = &user.ID
	}
	if _, err := recordMutation(r.Context(), tx, mutationRecordArgs{
		RequestID:    requestIDFromRequest(r),
		UserID:       userID,
		SessionID:    sessionIDFromRequest(r),
		AgentName:    agentNameFromRequest(r),
		MutationType: mutationTypeForRequest(r, "issue.delete"),
		SubjectType:  "issue",
		SubjectID:    existingID,
		// Inverse-op for soft-delete is "restore by clearing
		// deleted_at" — the existing applyIssueSnapshotTx writes the
		// before-snapshot back over the row, which restores deleted_at
		// to NULL because the snapshot captured the live value.
		InverseOp: InverseOp{
			Method: http.MethodPut,
			Path:   fmt.Sprintf("/issues/%d", existingID),
			Body:   beforeSnap,
		},
		BeforeState: beforeSnap,
		AfterState:  afterSnap,
		Undoable:    true,
	}); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}

	if snap := getIssueByID(existingID); snap != nil {
		saveSnapshot(snap, user, r)
	}
	// PAI-341 — fan out a delete-shaped change. Subscribers can decide
	// to re-pull (the slug now 404s) or drop the local cache file. The
	// rev is left empty because no live row remains to hash.
	publishKnowledgeChange(projectID, mod.Type(), slug, "")
	return n, nil
}

// Compile-time assertion the registered hooks have the exact
// signatures the knowledge package expects. If knowledge ever evolves
// the contract, these lines break the build before the tests do.
var (
	_ func(r *http.Request, projectID int64, mod knowledge.Module, in knowledge.Input) (knowledge.Output, error)                     = createKnowledgeEntry
	_ func(r *http.Request, projectID int64, mod knowledge.Module, currentSlug string, in knowledge.Input) (knowledge.Output, error) = updateKnowledgeEntry
	_ func(r *http.Request, projectID int64, mod knowledge.Module, slug string) (int64, error)                                       = deleteKnowledgeEntry
)
