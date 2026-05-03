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
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/markus-barta/paimos/backend/auth"
	"github.com/markus-barta/paimos/backend/db"
	"github.com/markus-barta/paimos/backend/models"
)

// ── Issue Relations endpoints ─────────────────────────────────────────────────

// ListIssueRelations returns all relations where the issue is source or target.
func ListIssueRelations(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	relType := r.URL.Query().Get("type") // optional filter

	query := `
		SELECT ir.source_id, ir.target_id, ir.type,
		       CASE WHEN p.key IS NOT NULL THEN p.key || '-' || i2.issue_number
		            ELSE 'SPRINT-' || i2.id END,
		       i2.title,
		       i2.project_id
		FROM issue_relations ir
		JOIN issues i2 ON i2.id = CASE WHEN ir.source_id = ? THEN ir.target_id ELSE ir.source_id END
		LEFT JOIN projects p ON p.id = i2.project_id
		WHERE (ir.source_id = ? OR ir.target_id = ?)
	`
	args := []any{id, id, id}
	if relType != "" {
		query += " AND ir.type = ?"
		args = append(args, relType)
	}
	query += " ORDER BY ir.type, i2.issue_number"

	rows, err := db.DB.Query(query, args...)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	relations := []models.IssueRelation{}
	for rows.Next() {
		var rel models.IssueRelation
		var targetProjectID sql.NullInt64
		if err := rows.Scan(&rel.SourceID, &rel.TargetID, &rel.Type, &rel.TargetKey, &rel.TargetTitle, &targetProjectID); err != nil {
			continue
		}
		// Direction lets the UI render inverse labels for directional
		// relation types (follows_from, blocks) without storing a
		// second row. "outgoing" = this endpoint's {id} is the source;
		// "incoming" = it's the target.
		if rel.SourceID == id {
			rel.Direction = "outgoing"
		} else {
			rel.Direction = "incoming"
		}
		// Restrict: if the relation's target issue lives in a project
		// the caller can't view, keep the relation visible (so tooling
		// that counts them still works) but redact the title and key.
		if targetProjectID.Valid && !auth.CanViewProject(r, targetProjectID.Int64) {
			rel.TargetKey = "RESTRICTED"
			rel.TargetTitle = "Restricted"
		}
		relations = append(relations, rel)
	}
	jsonOK(w, relations)
}

// CreateIssueRelation adds a relation between two issues.
func CreateIssueRelation(w http.ResponseWriter, r *http.Request) {
	sourceID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	var body struct {
		TargetID int64  `json:"target_id"`
		Type     string `json:"type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.TargetID == 0 || body.Type == "" {
		jsonError(w, "target_id and type required", http.StatusBadRequest)
		return
	}
	validTypes := map[string]bool{
		"groups": true, "sprint": true, "depends_on": true, "impacts": true,
		// PAI-89: directional types for spin-offs, blockers, and loose "see also".
		"follows_from": true, "blocks": true, "related": true,
	}
	if !validTypes[body.Type] {
		jsonError(w, "type must be one of: groups, sprint, depends_on, impacts, follows_from, blocks, related", http.StatusBadRequest)
		return
	}
	if sourceID == body.TargetID {
		jsonError(w, "source and target must be different issues", http.StatusBadRequest)
		return
	}
	// Convention (after migration 32): source = container/owner, target = member.
	// For sprint relations the URL param (:id) is the member issue and body.target_id
	// is the sprint — so we store source=sprint, target=issue (swap for sprint type).
	dbSource, dbTarget := sourceID, body.TargetID
	if body.Type == "sprint" {
		dbSource, dbTarget = body.TargetID, sourceID
	}
	// For sprint relations, assign rank = max+1 so new items appear at the bottom
	rank := 0
	tx, err := db.DB.BeginTx(r.Context(), nil)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()
	beforeSnap, err := fetchRelationMutationSnapshotTx(tx, dbSource, dbTarget, body.Type)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	if body.Type == "sprint" {
		tx.QueryRow("SELECT COALESCE(MAX(rank),0)+1 FROM issue_relations WHERE source_id=? AND type='sprint'", dbSource).Scan(&rank)
	}
	_, err = tx.Exec(
		`INSERT OR IGNORE INTO issue_relations(source_id, target_id, type, rank) VALUES(?,?,?,?)`,
		dbSource, dbTarget, body.Type, rank,
	)
	if handleDBError(w, err, "issue relation") {
		return
	}
	afterSnap, err := fetchRelationMutationSnapshotTx(tx, dbSource, dbTarget, body.Type)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	var userID *int64
	if user := auth.GetUser(r); user != nil {
		userID = &user.ID
	}
	if _, err := recordMutation(r.Context(), tx, mutationRecordArgs{
		RequestID:    requestIDFromRequest(r),
		UserID:       userID,
		SessionID:    sessionIDFromRequest(r),
		MutationType: mutationTypeForRequest(r, "issue.relation.create"),
		SubjectType:  "issue_relation",
		SubjectID:    sourceID,
		InverseOp: InverseOp{
			Method: http.MethodDelete,
			Path:   fmt.Sprintf("/issues/%d/relations", sourceID),
			Body: map[string]any{
				"target_id": body.TargetID,
				"type":      body.Type,
			},
		},
		BeforeState: beforeSnap,
		AfterState:  afterSnap,
		Undoable:    true,
	}); err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(); err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	upsertIssueEntityRelation(dbSource, dbTarget, body.Type)
	w.WriteHeader(http.StatusCreated)
	jsonOK(w, models.IssueRelation{SourceID: dbSource, TargetID: dbTarget, Type: body.Type})
}

// DeleteIssueRelation removes a specific relation.
func DeleteIssueRelation(w http.ResponseWriter, r *http.Request) {
	sourceID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	var body struct {
		TargetID int64  `json:"target_id"`
		Type     string `json:"type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.TargetID == 0 || body.Type == "" {
		jsonError(w, "target_id and type required", http.StatusBadRequest)
		return
	}
	// Match the direction convention: source=container, target=member.
	// For sprint: URL :id = member issue, body.target_id = sprint.
	dbSource, dbTarget := sourceID, body.TargetID
	if body.Type == "sprint" {
		dbSource, dbTarget = body.TargetID, sourceID
	}
	tx, err := db.DB.BeginTx(r.Context(), nil)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()
	beforeSnap, err := fetchRelationMutationSnapshotTx(tx, dbSource, dbTarget, body.Type)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	res, err := tx.Exec(
		`DELETE FROM issue_relations WHERE source_id=? AND target_id=? AND type=?`,
		dbSource, dbTarget, body.Type,
	)
	if err != nil {
		jsonError(w, "delete failed", http.StatusInternalServerError)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	afterSnap, err := fetchRelationMutationSnapshotTx(tx, dbSource, dbTarget, body.Type)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	var userID *int64
	if user := auth.GetUser(r); user != nil {
		userID = &user.ID
	}
	if _, err := recordMutation(r.Context(), tx, mutationRecordArgs{
		RequestID:    requestIDFromRequest(r),
		UserID:       userID,
		SessionID:    sessionIDFromRequest(r),
		MutationType: mutationTypeForRequest(r, "issue.relation.delete"),
		SubjectType:  "issue_relation",
		SubjectID:    sourceID,
		InverseOp: InverseOp{
			Method: http.MethodPost,
			Path:   fmt.Sprintf("/issues/%d/relations", sourceID),
			Body: map[string]any{
				"target_id": body.TargetID,
				"type":      body.Type,
				"rank":      beforeSnap.Rank,
			},
		},
		BeforeState: beforeSnap,
		AfterState:  afterSnap,
		Undoable:    true,
	}); err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(); err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	deleteIssueEntityRelation(dbSource, dbTarget, body.Type)
	w.WriteHeader(http.StatusNoContent)
}

// ListIssuesByRelation returns issues linked to a container via issue_relations.
// GET /api/issues/:id/members?type=groups|sprint|depends_on|impacts
//
// Direction convention (unified after migration 32):
//
//	source = container/owner (epic, sprint, etc.)
//	target = member/child (ticket, task, etc.)
//
// All types use: source_id = :id (the container), member issues are target_id.
func ListIssuesByRelation(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	relType := r.URL.Query().Get("type")
	if relType == "" {
		relType = "groups"
	}

	rows, err := db.DB.Query(
		issueSelectCore+` JOIN issue_relations ir ON ir.target_id = i.id WHERE ir.source_id = ? AND ir.type = ? AND `+liveIssuesWhere+` ORDER BY ir.rank ASC, i.issue_number ASC`,
		id, relType,
	)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	issues := []models.Issue{}
	for rows.Next() {
		issue, err := scanIssue(rows)
		if err != nil {
			jsonError(w, "scan failed", http.StatusInternalServerError)
			return
		}
		issues = append(issues, *issue)
	}
	issues = enrichIssues(issues)

	// Orphan sprints span projects, so their members can live in any
	// project the caller may not have access to. Post-filter by the
	// caller's accessible project set so cross-project members never
	// leak through the sprint endpoint.
	if accessibleIDs := auth.AccessibleProjectIDs(r); accessibleIDs != nil {
		allowed := make(map[int64]bool, len(accessibleIDs))
		for _, pid := range accessibleIDs {
			allowed[pid] = true
		}
		filtered := issues[:0]
		for _, iss := range issues {
			if iss.ProjectID == nil || allowed[*iss.ProjectID] {
				filtered = append(filtered, iss)
			}
		}
		issues = filtered
	}

	// Apply rate cascade for sprint members so reporting gets resolved rates.
	if relType == "sprint" {
		for idx := range issues {
			ResolveRateCascade(&issues[idx])
		}
	}

	jsonOK(w, issues)
}
