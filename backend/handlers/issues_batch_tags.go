// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/markus-barta/paimos/backend/auth"
	"github.com/markus-barta/paimos/backend/db"
)

// PAI-465: POST /api/issues/batch/tags
//
// Atomic bulk attach/detach of a single tag across N issues. Backs the
// IssueList "Make visible in portal" / "Hide from portal" bulk actions
// and is generic enough to host future bulk-tag operations.
//
// Body shape:
//
//	{
//	  "issue_ids": [12, 24, 36],
//	  "tag_id":    42,
//	  "op":        "add" | "remove"
//	}
//
// Behaviour:
//   - Admins can target any non-deleted issue; non-admins must have
//     editor access on every issue's project. A single permission gap
//     fails the whole batch with 403 — UI prevents this via the
//     mixed-permission disable, so backend treats it as a contract
//     violation rather than a partial result.
//   - System tags follow the same exemption as the singular tag API:
//     CUSTOMERPORTAL is allowed, every other system tag is rejected.
//   - One transaction, one batch_id, one mutation_log row per affected
//     issue. The mutation_type encodes the operation
//     (issue.tag.bulk_add / issue.tag.bulk_remove) so the PAI-467
//     audit feed can render bulk events distinctly.

const maxBatchTagSize = 500

type batchTagRequest struct {
	IssueIDs []int64 `json:"issue_ids"`
	TagID    int64   `json:"tag_id"`
	Op       string  `json:"op"`
}

type batchTagResponse struct {
	BatchID  string `json:"batch_id"`
	Affected int    `json:"affected"`
	Op       string `json:"op"`
}

func BatchTagIssues(w http.ResponseWriter, r *http.Request) {
	var body batchTagRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "invalid body", http.StatusBadRequest)
		return
	}
	if len(body.IssueIDs) == 0 {
		jsonError(w, "issue_ids required", http.StatusBadRequest)
		return
	}
	if len(body.IssueIDs) > maxBatchTagSize {
		jsonError(w, fmt.Sprintf("batch size %d exceeds limit %d", len(body.IssueIDs), maxBatchTagSize), http.StatusRequestEntityTooLarge)
		return
	}
	if body.TagID <= 0 {
		jsonError(w, "tag_id required", http.StatusBadRequest)
		return
	}
	if body.Op != "add" && body.Op != "remove" {
		jsonError(w, "op must be add or remove", http.StatusBadRequest)
		return
	}

	// System-tag exemption mirrors the singular API: CUSTOMERPORTAL is
	// the one system tag end users may toggle via this endpoint.
	if isSystemTag(body.TagID) && !isPortalVisibilityTag(body.TagID) {
		jsonError(w, "system tags cannot be modified manually", http.StatusForbidden)
		return
	}

	user := auth.GetUser(r)
	if user == nil {
		jsonError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	isAdmin := auth.IsAdmin(user)

	// Resolve each issue's project up front so we can both authorize
	// the request and surface a clean 404/403 before we touch any
	// state. Missing issues fail the batch — UI shouldn't be sending
	// stale ids.
	rows, err := db.DB.Query(`
		SELECT id, project_id FROM issues
		WHERE id IN (`+buildPlaceholders(len(body.IssueIDs))+`)
		  AND deleted_at IS NULL
	`, anySliceFromInt64(body.IssueIDs)...)
	if err != nil {
		log.Printf("BatchTagIssues: lookup: %v", err)
		jsonError(w, "lookup failed", http.StatusInternalServerError)
		return
	}
	projectByIssue := map[int64]int64{}
	for rows.Next() {
		var iid, pid int64
		if err := rows.Scan(&iid, &pid); err != nil {
			continue
		}
		projectByIssue[iid] = pid
	}
	rows.Close()

	if len(projectByIssue) != len(body.IssueIDs) {
		jsonError(w, "one or more issues not found", http.StatusNotFound)
		return
	}
	if !isAdmin {
		for _, iid := range body.IssueIDs {
			pid := projectByIssue[iid]
			if !auth.CanEditProject(r, pid) {
				jsonError(w, "editor access required on every selected issue's project", http.StatusForbidden)
				return
			}
		}
	}

	batchID, err := newBatchID()
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	tx, err := db.DB.BeginTx(r.Context(), nil)
	if err != nil {
		log.Printf("BatchTagIssues: begin tx: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	mutationType := "issue.tag.bulk_add"
	if body.Op == "remove" {
		mutationType = "issue.tag.bulk_remove"
	}

	userID := &user.ID

	affected := 0
	for _, issueID := range body.IssueIDs {
		before, err := fetchIssueTagMutationSnapshotTx(tx, issueID, body.TagID)
		if err != nil {
			log.Printf("BatchTagIssues: snapshot issue=%d: %v", issueID, err)
			jsonError(w, "snapshot failed", http.StatusInternalServerError)
			return
		}

		if body.Op == "add" {
			if _, err := tx.ExecContext(r.Context(), `INSERT OR IGNORE INTO issue_tags(issue_id, tag_id) VALUES(?, ?)`, issueID, body.TagID); err != nil {
				log.Printf("BatchTagIssues: attach issue=%d: %v", issueID, err)
				jsonError(w, "attach failed", http.StatusInternalServerError)
				return
			}
		} else {
			if _, err := tx.ExecContext(r.Context(), `DELETE FROM issue_tags WHERE issue_id=? AND tag_id=?`, issueID, body.TagID); err != nil {
				log.Printf("BatchTagIssues: detach issue=%d: %v", issueID, err)
				jsonError(w, "detach failed", http.StatusInternalServerError)
				return
			}
		}

		after, err := fetchIssueTagMutationSnapshotTx(tx, issueID, body.TagID)
		if err != nil {
			log.Printf("BatchTagIssues: post-snapshot issue=%d: %v", issueID, err)
			jsonError(w, "snapshot failed", http.StatusInternalServerError)
			return
		}

		// Audit every issue, even no-ops — the user explicitly requested
		// the operation across this set, so the trail should reflect
		// that. on_user_stack=0 keeps bulk ops off individual undo
		// stacks (the user undoes via the inverse bulk action, not via
		// the per-issue undo button).
		if _, err := recordMutation(r.Context(), tx, mutationRecordArgs{
			RequestID:    requestIDFromRequest(r),
			UserID:       userID,
			SessionID:    sessionIDFromRequest(r),
			AgentName:    agentNameFromRequest(r),
			MutationType: mutationType,
			SubjectType:  "issue_tag",
			SubjectID:    issueID,
			BatchID:      batchID,
			BeforeState:  before,
			AfterState:   after,
			Undoable:     false,
		}); err != nil {
			log.Printf("BatchTagIssues: audit issue=%d: %v", issueID, err)
			jsonError(w, "audit failed", http.StatusInternalServerError)
			return
		}

		if before.Exists != after.Exists {
			affected++
		}
	}

	if err := tx.Commit(); err != nil {
		log.Printf("BatchTagIssues: commit: %v", err)
		jsonError(w, "commit failed", http.StatusInternalServerError)
		return
	}

	jsonOK(w, batchTagResponse{
		BatchID:  batchID,
		Affected: affected,
		Op:       body.Op,
	})
}

func newBatchID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "batch-" + hex.EncodeToString(b), nil
}

// anySliceFromInt64 widens an int64 slice to []any for parameterized
// IN-clause use. Kept here (not in a shared utils file) because batch
// endpoints are the main place this conversion is needed.
func anySliceFromInt64(in []int64) []any {
	out := make([]any, len(in))
	for i, v := range in {
		out[i] = v
	}
	return out
}
