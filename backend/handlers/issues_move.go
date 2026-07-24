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
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/inspr-at/paimos/backend/auth"
	"github.com/inspr-at/paimos/backend/db"
)

// PAI-690 — move an issue to another project. The issue row (and thus its id)
// is preserved, so every issue_id-keyed child row (comments, time entries,
// history, attachments, tags, relations, ai_calls, agent_runs) follows the
// move automatically. What changes: project_id + a fresh target-project issue
// number, so the issue is re-keyed (e.g. PAI-690 -> OPS-12). The former key is
// recorded in issue_key_aliases so old references still resolve (see
// auth.ResolveIssueRef). Project-scoped structural edges (parent, sprint,
// cost_unit, release, groups) that would become cross-project are detached and
// reported; cross-project-capable relations (depends_on/blocks/relates) are
// left intact.

// errMoveSameProject is returned when the target project equals the current
// one; the handler maps it to 400 rather than a server error.
var errMoveSameProject = errors.New("issue is already in the target project")

// errMoveProjectless is returned when the issue has no project (an orphan
// memory/sprint row); there is nothing to re-home. Mapped to 422.
var errMoveProjectless = errors.New("cannot move a project-less issue")

// The project-scoped relation types a cross-project move must detach in either
// direction are parent, sprint, cost_unit, release, and groups (see the SQL in
// detachStructuralEdges). depends_on/blocks/relates are omitted on purpose:
// dependencies across projects are meaningful and are preserved.

type moveResult struct {
	IssueID   int64    `json:"issue_id"`
	OldKey    string   `json:"old_key"`
	NewKey    string   `json:"new_key"`
	ProjectID int64    `json:"project_id"`
	Detached  []string `json:"detached"`        // structural edges dropped by the move
	Notes     []string `json:"notes,omitempty"` // non-blocking follow-ups (e.g. anchor re-scope)
}

// moveIssueTx performs the whole move inside tx: re-key, alias the old key,
// re-home denormalized project copies, and detach now-cross-project structural
// edges. It does NOT check authorization — callers must verify the actor can
// edit both the source and target project first. Returns errMoveSameProject if
// target == source, sql.ErrNoRows if the issue or target project is missing.
func moveIssueTx(ctx context.Context, tx *sql.Tx, issueID, targetProjectID int64) (*moveResult, error) {
	var srcProjectID sql.NullInt64
	var srcNumber int
	var srcKey string
	err := tx.QueryRowContext(ctx, `
		SELECT i.project_id, i.issue_number, COALESCE(p.key, '')
		FROM issues i
		LEFT JOIN projects p ON p.id = i.project_id
		WHERE i.id = ? AND i.deleted_at IS NULL
	`, issueID).Scan(&srcProjectID, &srcNumber, &srcKey)
	if err != nil {
		return nil, err
	}
	if !srcProjectID.Valid {
		return nil, errMoveProjectless
	}
	if srcProjectID.Int64 == targetProjectID {
		return nil, errMoveSameProject
	}

	var targetKey string
	err = tx.QueryRowContext(ctx, `SELECT key FROM projects WHERE id = ?`, targetProjectID).Scan(&targetKey)
	if err != nil {
		return nil, err
	}

	// Reserve the next number in the target project before re-keying so the
	// unique (project_id, issue_number) index is never briefly violated.
	newNum, err := db.NextIssueNumber(ctx, tx, targetProjectID)
	if err != nil {
		return nil, err
	}

	// Record the former key so old references keep resolving. ON CONFLICT keeps
	// this idempotent if the same former key was somehow aliased before.
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO issue_key_aliases(project_key, issue_number, issue_id)
		VALUES(?, ?, ?)
		ON CONFLICT(project_key, issue_number) DO UPDATE SET issue_id = excluded.issue_id
	`, srcKey, srcNumber, issueID); err != nil {
		return nil, err
	}

	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	if _, err := tx.ExecContext(ctx, `
		UPDATE issues SET project_id = ?, issue_number = ?, updated_at = ? WHERE id = ?
	`, targetProjectID, newNum, now, issueID); err != nil {
		return nil, err
	}

	// Keep denormalized project copies consistent with the issue's new home.
	anchorNote, err := rehomeDenormProjectID(ctx, tx, issueID, targetProjectID)
	if err != nil {
		return nil, err
	}

	detached, err := detachStructuralEdges(ctx, tx, issueID)
	if err != nil {
		return nil, err
	}

	res := &moveResult{
		IssueID:   issueID,
		OldKey:    fmt.Sprintf("%s-%d", srcKey, srcNumber),
		NewKey:    fmt.Sprintf("%s-%d", targetKey, newNum),
		ProjectID: targetProjectID,
		Detached:  detached,
	}
	if anchorNote != "" {
		res.Notes = append(res.Notes, anchorNote)
	}
	return res, nil
}

// rehomeDenormProjectID updates the denormalized project_id copies on
// issue_anchors and agent_runs so they track the issue's new project. Anchors
// point at code in the source project's repos, so a note is returned when any
// exist, flagging that they should be re-verified against the target's repos.
func rehomeDenormProjectID(ctx context.Context, tx *sql.Tx, issueID, targetProjectID int64) (string, error) {
	anchorRes, err := tx.ExecContext(ctx,
		`UPDATE issue_anchors SET project_id = ? WHERE issue_id = ?`, targetProjectID, issueID)
	if err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE agent_runs SET project_id = ? WHERE issue_id = ?`, targetProjectID, issueID); err != nil {
		return "", err
	}
	if n, _ := anchorRes.RowsAffected(); n > 0 {
		return fmt.Sprintf("%d code anchor(s) re-scoped to the target project — re-verify against its repos", n), nil
	}
	return "", nil
}

// detachStructuralEdges removes every project-scoped structural relation
// touching issueID (in either direction) and returns human-readable labels for
// what was dropped, so the caller can report exactly what the move detached.
func detachStructuralEdges(ctx context.Context, tx *sql.Tx, issueID int64) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT r.type,
		       CASE WHEN r.target_id = ? THEN 'in' ELSE 'out' END AS dir,
		       COALESCE(op.key || '-' || oi.issue_number, '') AS other_key,
		       COALESCE(oi.title, '') AS other_title
		FROM issue_relations r
		JOIN issues oi ON oi.id = CASE WHEN r.target_id = ? THEN r.source_id ELSE r.target_id END
		LEFT JOIN projects op ON op.id = oi.project_id
		WHERE (r.source_id = ? OR r.target_id = ?)
		  AND r.type IN ('parent', 'sprint', 'cost_unit', 'release', 'groups')
	`, issueID, issueID, issueID, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var detached []string
	for rows.Next() {
		var edgeType, dir, otherKey, otherTitle string
		if err := rows.Scan(&edgeType, &dir, &otherKey, &otherTitle); err != nil {
			return nil, err
		}
		detached = append(detached, structuralEdgeLabel(edgeType, dir, otherKey, otherTitle))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if _, err := tx.ExecContext(ctx, `
		DELETE FROM issue_relations
		WHERE (source_id = ? OR target_id = ?)
		  AND type IN ('parent', 'sprint', 'cost_unit', 'release', 'groups')
	`, issueID, issueID); err != nil {
		return nil, err
	}
	return detached, nil
}

// structuralEdgeLabel renders one detached edge for the move report. "in"
// means the moved issue was the child/member; "out" means it was the
// parent/container of the other side.
func structuralEdgeLabel(edgeType, dir, otherKey, otherTitle string) string {
	ident := otherTitle
	if edgeType == "parent" && otherKey != "" {
		ident = otherKey
	}
	switch edgeType {
	case "parent":
		if dir == "in" {
			return fmt.Sprintf("parent %s", ident)
		}
		return fmt.Sprintf("child %s", ident)
	case "sprint":
		return fmt.Sprintf("sprint %s", ident)
	case "cost_unit":
		return fmt.Sprintf("cost unit %s", ident)
	case "release":
		return fmt.Sprintf("release %s", ident)
	case "groups":
		return fmt.Sprintf("group %s", ident)
	default:
		return fmt.Sprintf("%s %s", edgeType, ident)
	}
}

// resolveMoveTarget parses a move-target project reference from the request
// body ("project_id": numeric id). Returns the id and whether it was present.
func decodeMoveTarget(body []byte) (int64, bool, error) {
	var payload struct {
		ProjectID *int64 `json:"project_id"`
	}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &payload); err != nil {
			return 0, false, err
		}
	}
	if payload.ProjectID == nil {
		return 0, false, nil
	}
	return *payload.ProjectID, true, nil
}

// authorizeMove verifies the actor can edit the target project. The source
// project is already gated by RequireIssueEdit on the single-issue route; the
// bulk route calls this after its own per-issue source check.
func authorizeMoveTarget(r *http.Request, targetProjectID int64) bool {
	return auth.CanEditProject(r, targetProjectID)
}

// MoveIssue handles POST /api/issues/{id}/move with body {"project_id": <id>}.
// RequireIssueEdit has already confirmed edit rights on the source project.
func MoveIssue(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		jsonError(w, "invalid body", http.StatusBadRequest)
		return
	}
	targetProjectID, present, err := decodeMoveTarget(raw)
	if err != nil {
		jsonError(w, "invalid body", http.StatusBadRequest)
		return
	}
	if !present || targetProjectID <= 0 {
		jsonError(w, "project_id required", http.StatusBadRequest)
		return
	}
	if !authorizeMoveTarget(r, targetProjectID) {
		jsonError(w, "no edit access to the target project", http.StatusForbidden)
		return
	}

	result, status, err := runMove(r, id, targetProjectID)
	if err != nil {
		jsonError(w, err.Error(), status)
		return
	}
	jsonOK(w, result)
}

// MoveIssuesBulk handles POST /api/issues/move with body
// {"issue_ids": [...], "project_id": <id>}. It moves each issue in its own
// transaction and returns a per-issue result so a partial failure (e.g. one
// issue the caller can't edit) never blocks the rest of a reorg campaign.
// Because the target-project write check is uniform, it is verified once; each
// issue's source-project edit right is checked per item.
func MoveIssuesBulk(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IssueIDs  []int64 `json:"issue_ids"`
		ProjectID int64   `json:"project_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "invalid body", http.StatusBadRequest)
		return
	}
	if body.ProjectID <= 0 {
		jsonError(w, "project_id required", http.StatusBadRequest)
		return
	}
	if len(body.IssueIDs) == 0 {
		jsonError(w, "issue_ids required", http.StatusBadRequest)
		return
	}
	if !authorizeMoveTarget(r, body.ProjectID) {
		jsonError(w, "no edit access to the target project", http.StatusForbidden)
		return
	}

	type bulkItem struct {
		IssueID int64       `json:"issue_id"`
		OK      bool        `json:"ok"`
		Error   string      `json:"error,omitempty"`
		Result  *moveResult `json:"result,omitempty"`
	}
	out := struct {
		Moved   int        `json:"moved"`
		Failed  int        `json:"failed"`
		Results []bulkItem `json:"results"`
	}{Results: make([]bulkItem, 0, len(body.IssueIDs))}

	for _, id := range body.IssueIDs {
		item := bulkItem{IssueID: id}
		// Per-issue source-project edit check: the moved issue's current owner.
		if pid, found, _ := auth.ProjectIDForIssue(id); !found || !auth.CanEditProject(r, pid) {
			item.Error = "no edit access to the source project"
			out.Failed++
			out.Results = append(out.Results, item)
			continue
		}
		result, _, err := runMove(r, id, body.ProjectID)
		if err != nil {
			item.Error = err.Error()
			out.Failed++
			out.Results = append(out.Results, item)
			continue
		}
		item.OK = true
		item.Result = result
		out.Moved++
		out.Results = append(out.Results, item)
	}
	jsonOK(w, out)
}

// runMove wraps moveIssueTx in a transaction and records the post-move history
// snapshot. Returns the result, an HTTP status to use on error, and the error.
func runMove(r *http.Request, issueID, targetProjectID int64) (*moveResult, int, error) {
	existing := getIssueByID(issueID)
	if existing == nil || existing.DeletedAt != nil {
		return nil, http.StatusNotFound, fmt.Errorf("issue not found")
	}

	tx, err := db.DB.BeginTx(r.Context(), nil)
	if err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("internal error")
	}
	defer tx.Rollback()

	result, err := moveIssueTx(r.Context(), tx, issueID, targetProjectID)
	if err != nil {
		switch {
		case errors.Is(err, errMoveSameProject):
			return nil, http.StatusBadRequest, err
		case errors.Is(err, errMoveProjectless):
			return nil, http.StatusUnprocessableEntity, err
		case errors.Is(err, sql.ErrNoRows):
			return nil, http.StatusNotFound, fmt.Errorf("issue or target project not found")
		default:
			return nil, http.StatusInternalServerError, fmt.Errorf("internal error")
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("internal error")
	}

	// Post-commit: record the new state in history (attribution) and refresh
	// derived surfaces. Best-effort — the move itself already committed.
	if moved := getIssueByID(issueID); moved != nil {
		saveSnapshot(moved, auth.GetUser(r), r)
		EvaluateSystemTags(issueID)
	}
	return result, http.StatusOK, nil
}
