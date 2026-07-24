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
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/inspr-at/paimos/backend/auth"
	"github.com/inspr-at/paimos/backend/db"
	"github.com/inspr-at/paimos/backend/models"
)

// parentEdgeExecer is satisfied by both *sql.Tx and *sql.DB so setParentEdge
// can run inside a transaction (the common case) or against the pool.
type parentEdgeExecer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// setParentEdge writes the issue hierarchy directly to the `parent` edge
// (source=parent, target=child) — the single source of truth after P6 dropped
// the issues.parent_id column. Idempotent delete-then-insert: clears any
// existing parent for childID, then inserts the new one when parentID is set.
// A nil/zero/self parentID leaves the child parentless. Callers still accept a
// legacy `parent_id` input and pass it straight through here.
func setParentEdge(ctx context.Context, ex parentEdgeExecer, childID int64, parentID *int64) error {
	if childID <= 0 {
		return nil
	}
	if _, err := ex.ExecContext(ctx,
		`DELETE FROM issue_relations WHERE target_id=? AND type='parent'`, childID); err != nil {
		return err
	}
	if parentID == nil || *parentID <= 0 || *parentID == childID {
		return nil
	}
	_, err := ex.ExecContext(ctx,
		`INSERT OR IGNORE INTO issue_relations(source_id, target_id, type) VALUES(?,?,'parent')`,
		*parentID, childID)
	return err
}

// setLabelEdge sets (or clears) a ticket's cost_unit/release container edge
// from a string label (PAI-599) — the single source of truth after the
// string columns were dropped. Empty label clears the edge; a label with no
// existing container creates one (reserved issue number, counter-synced).
// Idempotent delete-then-insert, one edge of this type per ticket. dimension
// is "cost_unit" or "release"; both double as the container issue type and the
// edge type. Requires a *sql.Tx because container creation reserves a number.
func setLabelEdge(ctx context.Context, tx *sql.Tx, dimension string, ticketID, projectID int64, label string) error {
	if dimension != "cost_unit" && dimension != "release" {
		return fmt.Errorf("setLabelEdge: invalid dimension %q", dimension)
	}
	if ticketID <= 0 {
		return nil
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM issue_relations WHERE target_id=? AND type=?`, ticketID, dimension); err != nil {
		return err
	}
	label = strings.TrimSpace(label)
	if label == "" || projectID <= 0 {
		return nil
	}
	containerID, err := resolveOrCreateLabelContainer(ctx, tx, dimension, projectID, label)
	if err != nil {
		return err
	}
	if containerID <= 0 || containerID == ticketID {
		return nil
	}
	_, err = tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO issue_relations(source_id, target_id, type) VALUES(?,?,?)`,
		containerID, ticketID, dimension)
	return err
}

// resolveOrCreateLabelContainer returns the id of the cost_unit/release
// container issue with the given label in the project, creating it (with a
// reserved, counter-synced issue number) if none exists. Used by the runtime
// write path; the one-time migration backfill is done in SQL (M122).
func resolveOrCreateLabelContainer(ctx context.Context, tx *sql.Tx, dimension string, projectID int64, label string) (int64, error) {
	var id int64
	// ORDER BY id keeps resolution deterministic if duplicate same-title
	// containers ever exist (the issues table has no uniqueness on
	// project_id+type+title): always reuse the lowest-id one, matching the
	// M122 dedup rule, so tickets never split across duplicate containers.
	err := tx.QueryRowContext(ctx,
		`SELECT id FROM issues WHERE project_id=? AND type=? AND title=? AND deleted_at IS NULL
		 ORDER BY id LIMIT 1`,
		projectID, dimension, label).Scan(&id)
	switch {
	case err == nil:
		return id, nil
	case !errors.Is(err, sql.ErrNoRows):
		return 0, err
	}
	num, err := db.NextIssueNumber(ctx, tx, projectID)
	if err != nil {
		return 0, err
	}
	res, err := tx.ExecContext(ctx,
		`INSERT INTO issues(project_id, issue_number, type, title, status, priority)
		 VALUES(?,?,?,?,'backlog','medium')`, projectID, num, dimension, label)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// labelOf returns the label of an edge-sourced cost_unit/release ref, or "".
func labelOf(ref *models.LabelRef) string {
	if ref == nil {
		return ""
	}
	return ref.Label
}

// wouldCycleParent reports whether making newParentID the parent of childID
// would introduce a cycle — i.e. childID is already an ancestor of newParentID
// along the `parent` edge (PAI-584 P5). Walks up from newParentID; the 64-hop
// cap is a belt-and-suspenders bound against pre-existing corrupt data.
func wouldCycleParent(childID, newParentID int64) bool {
	cur := newParentID
	for hops := 0; hops < 64 && cur > 0; hops++ {
		if cur == childID {
			return true
		}
		var next sql.NullInt64
		if err := db.DB.QueryRow(
			`SELECT source_id FROM issue_relations WHERE target_id=? AND type='parent'`, cur,
		).Scan(&next); err != nil || !next.Valid {
			break
		}
		cur = next.Int64
	}
	return false
}

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
	if ev := validateEnumField("relation.type", body.Type); ev != nil {
		writeEnumViolation(w, r, ev)
		return
	}
	if sourceID == body.TargetID {
		jsonError(w, "source and target must be different issues", http.StatusBadRequest)
		return
	}
	// PAI-584 P4: epic→ticket membership is the `parent` edge now. Legacy
	// callers using type=groups against an EPIC source are auto-translated to
	// `parent` so the link is fully visible everywhere (children/filter/
	// reports read the parent edge, not groups). cost_unit/release containers
	// keep type=groups — those are orthogonal grouping axes (P7–P9).
	if body.Type == "groups" {
		var srcType string
		if db.DB.QueryRow("SELECT type FROM issues WHERE id=?", sourceID).Scan(&srcType) == nil && srcType == "epic" {
			body.Type = "parent"
		}
	}
	// Convention (after migration 32): source = container/owner, target = member.
	// For sprint relations the URL param (:id) is the member issue and body.target_id
	// is the sprint — so we store source=sprint, target=issue (swap for sprint type).
	dbSource, dbTarget := sourceID, body.TargetID
	if body.Type == "sprint" {
		dbSource, dbTarget = body.TargetID, sourceID
	}
	// PAI-584 P5: validate the parent edge (source=parent, target=child) —
	// hierarchy type rules (parity with the parent_id column path) and no
	// cycles. The one-parent invariant is enforced below + by the DB index.
	if body.Type == "parent" {
		child := getIssueByID(dbTarget)
		if child == nil {
			jsonError(w, "target issue not found", http.StatusNotFound)
			return
		}
		if err := validateParent(child.Type, &dbSource, child.ProjectID); err != nil {
			jsonError(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}
		if wouldCycleParent(dbTarget, dbSource) {
			jsonError(w, "relation would create a parent cycle", http.StatusUnprocessableEntity)
			return
		}
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
	// PAI-584 P4: enforce one parent per child. Adding a parent edge for a
	// child that already has a different parent is rejected (remove it first /
	// reparent via PUT parent_id) — matches the P5 unique index and avoids a
	// silent reparent the mutation log wouldn't capture. Re-adding the same
	// parent is idempotent.
	if body.Type == "parent" {
		var existingParent int64
		switch err := tx.QueryRow(
			`SELECT source_id FROM issue_relations WHERE target_id=? AND type='parent'`, dbTarget,
		).Scan(&existingParent); {
		case err == sql.ErrNoRows:
			// no parent yet — proceed
		case err != nil:
			jsonError(w, "internal error", http.StatusInternalServerError)
			return
		case existingParent != dbSource:
			jsonError(w, "issue already has a parent; remove it first or reparent via parent_id", http.StatusConflict)
			return
		}
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
		AgentName:    agentNameFromRequest(r),
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
		AgentName:    agentNameFromRequest(r),
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

	var rows *sql.Rows
	if relType == "groups" {
		// PAI-584 P2: container membership spans the `parent` edge
		// (epic→ticket, the new SSOT) AND legacy `groups` (cost_unit /
		// release containers, until P7–P9). Union via a DISTINCT IN
		// subquery so a member linked by both isn't listed twice. rank
		// ordering doesn't apply to groups membership — order by number.
		rows, err = db.DB.Query(
			issueSelectCore+` WHERE i.id IN (SELECT target_id FROM issue_relations WHERE source_id = ? AND type IN ('parent','groups')) AND `+liveIssuesWhere+` ORDER BY i.issue_number ASC`,
			id,
		)
	} else {
		rows, err = db.DB.Query(
			issueSelectCore+` JOIN issue_relations ir ON ir.target_id = i.id WHERE ir.source_id = ? AND ir.type = ? AND `+liveIssuesWhere+` ORDER BY ir.rank ASC, i.issue_number ASC`,
			id, relType,
		)
	}
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
