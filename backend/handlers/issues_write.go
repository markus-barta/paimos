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
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/markus-barta/paimos/backend/auth"
	"github.com/markus-barta/paimos/backend/db"
	"github.com/markus-barta/paimos/backend/models"
)

func CreateIssue(w http.ResponseWriter, r *http.Request) {
	projectID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid project id", http.StatusBadRequest)
		return
	}

	var body struct {
		Title              string   `json:"title"`
		Description        string   `json:"description"`
		AcceptanceCriteria string   `json:"acceptance_criteria"`
		Notes              string   `json:"notes"`
		Type               string   `json:"type"`
		ParentID           *int64   `json:"parent_id"`
		Status             string   `json:"status"`
		Priority           string   `json:"priority"`
		CostUnit           string   `json:"cost_unit"`
		Release            string   `json:"release"`
		BillingType        string   `json:"billing_type"`
		TotalBudget        *float64 `json:"total_budget"`
		RateHourly         *float64 `json:"rate_hourly"`
		RateLp             *float64 `json:"rate_lp"`
		StartDate          string   `json:"start_date"`
		EndDate            string   `json:"end_date"`
		GroupState         string   `json:"group_state"`
		SprintState        string   `json:"sprint_state"`
		JiraID             string   `json:"jira_id"`
		JiraVersion        string   `json:"jira_version"`
		JiraText           string   `json:"jira_text"`
		EstimateHours      *float64 `json:"estimate_hours"`
		EstimateLp         *float64 `json:"estimate_lp"`
		ArHours            *float64 `json:"ar_hours"`
		ArLp               *float64 `json:"ar_lp"`
		TimeOverride       *float64 `json:"time_override"`
		Color              *string  `json:"color"`
		AssigneeID         *int64   `json:"assignee_id"`
		SprintIDs          []int64  `json:"sprint_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Title == "" {
		jsonError(w, "title required", http.StatusBadRequest)
		return
	}
	if body.Status == "" {
		body.Status = "new"
	}
	if body.Priority == "" {
		body.Priority = "medium"
	}
	if body.Type == "" {
		body.Type = inferType(body.ParentID)
	}

	// Validate hierarchy
	if err := validateParent(body.Type, body.ParentID, &projectID); err != nil {
		jsonError(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}

	// Assign issue_number atomically
	var nextNum int
	if err := db.DB.QueryRow(
		"SELECT COALESCE(MAX(issue_number),0)+1 FROM issues WHERE project_id=?", projectID,
	).Scan(&nextNum); err != nil {
		jsonError(w, "numbering failed", http.StatusInternalServerError)
		return
	}

	var createdByID *int64
	if user := auth.GetUser(r); user != nil {
		createdByID = &user.ID
	}

	res, err := db.DB.Exec(`
		INSERT INTO issues(project_id,issue_number,type,parent_id,title,description,
		                   acceptance_criteria,notes,status,priority,cost_unit,release,
		                   billing_type,total_budget,rate_hourly,rate_lp,
		                   start_date,end_date,group_state,sprint_state,jira_id,jira_version,jira_text,
		                   estimate_hours,estimate_lp,ar_hours,ar_lp,time_override,
		                   color,assignee_id,created_by)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
	`, projectID, nextNum, body.Type, body.ParentID, body.Title, body.Description,
		body.AcceptanceCriteria, body.Notes, body.Status, body.Priority,
		body.CostUnit, body.Release,
		body.BillingType, body.TotalBudget, body.RateHourly, body.RateLp,
		body.StartDate, body.EndDate, body.GroupState, body.SprintState,
		body.JiraID, body.JiraVersion, body.JiraText,
		body.EstimateHours, body.EstimateLp, body.ArHours, body.ArLp, body.TimeOverride,
		body.Color, body.AssigneeID, createdByID)
	if handleDBError(w, err, "issue") {
		return
	}
	id, _ := res.LastInsertId()

	// Sprint membership: source=sprint, target=member issue.
	for _, sid := range body.SprintIDs {
		if sid <= 0 {
			continue
		}
		if _, err := db.DB.Exec(
			`INSERT OR IGNORE INTO issue_relations(source_id, target_id, type) VALUES(?, ?, 'sprint')`,
			sid, id,
		); err != nil {
			log.Printf("CreateIssue: sprint relation insert failed (sprint=%d, issue=%d): %v", sid, id, err)
		}
		upsertIssueEntityRelation(sid, id, "sprint")
	}

	issue := getIssueByID(id)
	if issue == nil {
		jsonError(w, "not found after insert", http.StatusInternalServerError)
		return
	}
	saveSnapshot(issue, auth.GetUser(r))
	w.WriteHeader(http.StatusCreated)
	jsonOK(w, issue)
}

// CloneIssue duplicates an issue with copied fields, tags, and a relation to the original.
// POST /api/issues/:id/clone
func CloneIssue(w http.ResponseWriter, r *http.Request) {
	sourceID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}

	src := getIssueByID(sourceID)
	if src == nil {
		jsonError(w, "source issue not found", http.StatusNotFound)
		return
	}

	// Assign next issue_number
	var nextNum int
	if err := db.DB.QueryRow(
		"SELECT COALESCE(MAX(issue_number),0)+1 FROM issues WHERE project_id=?", src.ProjectID,
	).Scan(&nextNum); err != nil {
		jsonError(w, "numbering failed", http.StatusInternalServerError)
		return
	}

	var createdByID *int64
	if user := auth.GetUser(r); user != nil {
		createdByID = &user.ID
	}

	res, err := db.DB.Exec(`
		INSERT INTO issues(project_id,issue_number,type,parent_id,title,description,
		                   acceptance_criteria,notes,status,priority,cost_unit,release,
		                   billing_type,total_budget,rate_hourly,rate_lp,
		                   start_date,end_date,group_state,sprint_state,jira_id,jira_version,jira_text,
		                   estimate_hours,estimate_lp,ar_hours,ar_lp,time_override,
		                   color,assignee_id,created_by)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
	`, src.ProjectID, nextNum, src.Type, src.ParentID,
		"Copy of "+src.Title, src.Description,
		src.AcceptanceCriteria, src.Notes, "backlog", src.Priority,
		src.CostUnit, src.Release,
		ptrOrEmpty(src.BillingType), src.TotalBudget, src.RateHourly, src.RateLp,
		ptrOrEmpty(src.StartDate), ptrOrEmpty(src.EndDate),
		ptrOrEmpty(src.GroupState), ptrOrEmpty(src.SprintState),
		ptrOrEmpty(src.JiraID), ptrOrEmpty(src.JiraVersion), ptrOrEmpty(src.JiraText),
		src.EstimateHours, src.EstimateLp, src.ArHours, src.ArLp, nil,
		src.Color, src.AssigneeID, createdByID)
	if handleDBError(w, err, "issue") {
		return
	}
	newID, _ := res.LastInsertId()

	// Copy tags
	tagRows, err := db.DB.Query("SELECT tag_id FROM issue_tags WHERE issue_id=?", sourceID)
	if err == nil {
		defer tagRows.Close()
		for tagRows.Next() {
			var tagID int64
			if tagRows.Scan(&tagID) == nil {
				if _, err := db.DB.Exec("INSERT INTO issue_tags(issue_id,tag_id) VALUES(?,?)", newID, tagID); err != nil {
					log.Printf("CloneIssue: copy tag issue=%d tag=%d: %v", newID, tagID, err)
					continue
				}
			}
		}
	}

	// Add relation: clone → original (type "related")
	if _, err := db.DB.Exec(`INSERT INTO issue_relations(source_id, target_id, type) VALUES(?,?,?)`,
		newID, sourceID, "related"); err != nil {
		log.Printf("CloneIssue: insert relation clone=%d source=%d: %v", newID, sourceID, err)
	}
	upsertIssueEntityRelation(newID, sourceID, "related")

	clone := getIssueByID(newID)
	if clone == nil {
		jsonError(w, "not found after clone", http.StatusInternalServerError)
		return
	}
	saveSnapshot(clone, auth.GetUser(r))
	w.WriteHeader(http.StatusCreated)
	jsonOK(w, clone)
}

func GetIssue(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	issue := getIssueByID(id)
	if issue == nil || issue.DeletedAt != nil {
		// Soft-deleted issues look like "not found" to every non-trash
		// endpoint. To view/restore, go through /issues/trash.
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	jsonOK(w, issue)
}
func UpdateIssue(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}

	existing := getIssueByID(id)
	if existing == nil || existing.DeletedAt != nil {
		// Trashed issues are read-only from the update path; restore first.
		jsonError(w, "not found", http.StatusNotFound)
		return
	}

	var body struct {
		Title              *string  `json:"title"`
		Description        *string  `json:"description"`
		AcceptanceCriteria *string  `json:"acceptance_criteria"`
		Notes              *string  `json:"notes"`
		Type               *string  `json:"type"`
		ParentID           *int64   `json:"parent_id"`
		Status             *string  `json:"status"`
		Priority           *string  `json:"priority"`
		CostUnit           *string  `json:"cost_unit"`
		Release            *string  `json:"release"`
		BillingType        *string  `json:"billing_type"`
		TotalBudget        *float64 `json:"total_budget"`
		RateHourly         *float64 `json:"rate_hourly"`
		RateLp             *float64 `json:"rate_lp"`
		StartDate          *string  `json:"start_date"`
		EndDate            *string  `json:"end_date"`
		GroupState         *string  `json:"group_state"`
		SprintState        *string  `json:"sprint_state"`
		JiraID             *string  `json:"jira_id"`
		JiraVersion        *string  `json:"jira_version"`
		JiraText           *string  `json:"jira_text"`
		EstimateHours      *float64 `json:"estimate_hours"`
		EstimateLp         *float64 `json:"estimate_lp"`
		ArHours            *float64 `json:"ar_hours"`
		ArLp               *float64 `json:"ar_lp"`
		TimeOverride       *float64 `json:"time_override"`
		Color              *string  `json:"color"`
		AssigneeID         *int64   `json:"assignee_id"`
		CascadeChildren    *bool    `json:"cascade_children"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "invalid body", http.StatusBadRequest)
		return
	}

	// Validate hierarchy if type or parent changing
	newType := existing.Type
	if body.Type != nil {
		newType = *body.Type
	}
	newParent := existing.ParentID
	if body.ParentID != nil {
		newParent = body.ParentID
	}
	if body.Type != nil || body.ParentID != nil {
		if err := validateParent(newType, newParent, existing.ProjectID); err != nil {
			jsonError(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}
	}

	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	tx, err := db.DB.BeginTx(r.Context(), nil)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()
	beforeSnap, err := fetchIssueMutationSnapshotTx(tx, id)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	_, err = tx.Exec(`
		UPDATE issues SET
			title               = COALESCE(?, title),
			description         = COALESCE(?, description),
			acceptance_criteria = COALESCE(?, acceptance_criteria),
			notes               = COALESCE(?, notes),
			type                = COALESCE(?, type),
			parent_id           = CASE WHEN ? IS NOT NULL THEN ? ELSE parent_id END,
			status              = COALESCE(?, status),
			priority            = COALESCE(?, priority),
			cost_unit           = COALESCE(?, cost_unit),
			release             = COALESCE(?, release),
			billing_type        = COALESCE(?, billing_type),
			total_budget        = COALESCE(?, total_budget),
			rate_hourly         = COALESCE(?, rate_hourly),
			rate_lp             = COALESCE(?, rate_lp),
			start_date          = COALESCE(?, start_date),
			end_date            = COALESCE(?, end_date),
			group_state         = COALESCE(?, group_state),
			sprint_state        = COALESCE(?, sprint_state),
			jira_id             = COALESCE(?, jira_id),
			jira_version        = COALESCE(?, jira_version),
			jira_text           = COALESCE(?, jira_text),
			estimate_hours      = COALESCE(?, estimate_hours),
			estimate_lp         = COALESCE(?, estimate_lp),
			ar_hours            = COALESCE(?, ar_hours),
			ar_lp               = COALESCE(?, ar_lp),
			time_override       = COALESCE(?, time_override),
			color               = COALESCE(?, color),
			assignee_id         = CASE WHEN ? IS NOT NULL THEN ? ELSE assignee_id END,
			updated_at          = ?
		WHERE id=?
	`, body.Title, body.Description, body.AcceptanceCriteria, body.Notes,
		body.Type,
		body.ParentID, body.ParentID,
		body.Status, body.Priority, body.CostUnit, body.Release,
		body.BillingType, body.TotalBudget, body.RateHourly, body.RateLp,
		body.StartDate, body.EndDate, body.GroupState, body.SprintState,
		body.JiraID, body.JiraVersion, body.JiraText,
		body.EstimateHours, body.EstimateLp, body.ArHours, body.ArLp,
		body.TimeOverride,
		body.Color,
		body.AssigneeID, body.AssigneeID,
		now, id)
	if handleDBError(w, err, "issue") {
		return
	}
	afterSnap, err := fetchIssueMutationSnapshotTx(tx, id)
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
		MutationType: mutationTypeForRequest(r, "issue.update"),
		SubjectType:  "issue",
		SubjectID:    id,
		InverseOp: InverseOp{
			Method: http.MethodPut,
			Path:   fmt.Sprintf("/issues/%d", id),
			Body:   beforeSnap,
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

	issue := getIssueByID(id)
	if issue == nil {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	saveSnapshot(issue, auth.GetUser(r))

	// Auto-promote parent epic: if this ticket just moved to in-progress,
	// and its parent is an epic still in backlog, bump the epic to in-progress.
	if body.Status != nil && *body.Status == "in-progress" &&
		existing.Status != "in-progress" &&
		issue.ParentID != nil {
		parent := getIssueByID(*issue.ParentID)
		if parent != nil && parent.Type == "epic" && (parent.Status == "backlog" || parent.Status == "new") {
			now2 := time.Now().UTC().Format("2006-01-02 15:04:05")
			if _, err := db.DB.Exec(`UPDATE issues SET status='in-progress', updated_at=? WHERE id=?`, now2, parent.ID); err != nil {
				log.Printf("UpdateIssue: auto-promote parent=%d: %v", parent.ID, err)
			}
			if promoted := getIssueByID(parent.ID); promoted != nil {
				saveSnapshot(promoted, auth.GetUser(r))
			}
		}
	}

	// Auto-set billing lifecycle timestamps on status transitions
	if body.Status != nil {
		actor := auth.GetUser(r)
		nowTS := time.Now().UTC().Format("2006-01-02 15:04:05")
		if *body.Status == "accepted" && existing.Status != "accepted" {
			if _, err := db.DB.Exec(`UPDATE issues SET accepted_at=?, accepted_by=? WHERE id=? AND accepted_at IS NULL`,
				nowTS, actor.ID, id); err != nil {
				log.Printf("UpdateIssue: set accepted_at id=%d: %v", id, err)
			}
		}
		if *body.Status == "invoiced" && existing.Status != "invoiced" {
			if _, err := db.DB.Exec(`UPDATE issues SET invoiced_at=? WHERE id=? AND invoiced_at IS NULL`, nowTS, id); err != nil {
				log.Printf("UpdateIssue: set invoiced_at id=%d: %v", id, err)
			}
		}

		// Auto-cascade children for issues moving to done/accepted/invoiced
		// - Tickets: always cascade to tasks (existing behavior)
		// - Epics: cascade only when cascade_children is explicitly true
		terminalStatuses := map[string]bool{"done": true, "delivered": true, "accepted": true, "invoiced": true}
		if terminalStatuses[*body.Status] {
			shouldCascade := false
			if issue.Type == "ticket" {
				// Tickets always cascade to tasks (unless explicitly disabled)
				shouldCascade = body.CascadeChildren == nil || *body.CascadeChildren
			} else if issue.Type == "epic" {
				// Epics only cascade when explicitly requested
				shouldCascade = body.CascadeChildren != nil && *body.CascadeChildren
			}
			if shouldCascade {
				if _, err := db.DB.Exec(`UPDATE issues SET status=?, updated_at=? WHERE parent_id=? AND deleted_at IS NULL AND status NOT IN ('done','delivered','accepted','invoiced','cancelled')`,
					*body.Status, nowTS, id); err != nil {
					log.Printf("UpdateIssue: cascade children id=%d: %v", id, err)
				}
				// For epics, also cascade to grandchildren (tasks under tickets)
				if issue.Type == "epic" {
					if _, err := db.DB.Exec(`UPDATE issues SET status=?, updated_at=? WHERE parent_id IN (SELECT id FROM issues WHERE parent_id=? AND deleted_at IS NULL) AND deleted_at IS NULL AND status NOT IN ('done','delivered','accepted','invoiced','cancelled')`,
						*body.Status, nowTS, id); err != nil {
						log.Printf("UpdateIssue: cascade grandchildren id=%d: %v", id, err)
					}
				}
			}
		}
	}

	// Evaluate system tags (budget threshold, etc.)
	EvaluateSystemTags(id)

	// Re-fetch to include auto-set fields
	issue = getIssueByID(id)
	jsonOK(w, issue)
}

// CompleteEpic marks an epic as done, optionally bulk-closing open children first.
// POST /issues/{id}/complete-epic?force=true
// Without ?force=true: returns 422 {"open_count": N} if non-terminal children exist.
// With ?force=true: sets all non-terminal children to done, then sets the epic to done.
func CompleteEpic(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	epic := getIssueByID(id)
	if epic == nil {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	if epic.Type != "epic" {
		jsonError(w, "issue is not an epic", http.StatusUnprocessableEntity)
		return
	}

	rows, err := db.DB.Query(issueSelectCore+` WHERE i.parent_id=? AND `+liveIssuesWhere+` ORDER BY i.issue_number ASC`, id)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	var children []models.Issue
	for rows.Next() {
		ch, err := scanIssue(rows)
		if err != nil {
			continue
		}
		children = append(children, *ch)
	}
	children = enrichIssues(children)

	openCount := 0
	for _, ch := range children {
		if ch.Status != "done" && ch.Status != "delivered" && ch.Status != "accepted" && ch.Status != "invoiced" && ch.Status != "cancelled" {
			openCount++
		}
	}

	force := r.URL.Query().Get("force") == "true"
	if openCount > 0 && !force {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		json.NewEncoder(w).Encode(map[string]any{"open_count": openCount})
		return
	}

	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	user := auth.GetUser(r)

	if force {
		for _, ch := range children {
			if ch.Status != "done" && ch.Status != "delivered" && ch.Status != "accepted" && ch.Status != "invoiced" && ch.Status != "cancelled" {
				if _, err := db.DB.Exec(`UPDATE issues SET status='done', updated_at=? WHERE id=?`, now, ch.ID); err != nil {
					log.Printf("CompleteEpic: close child id=%d: %v", ch.ID, err)
					continue
				}
				if updated := getIssueByID(ch.ID); updated != nil {
					saveSnapshot(updated, user)
				}
			}
		}
	}

	if _, err := db.DB.Exec(`UPDATE issues SET status='done', updated_at=? WHERE id=?`, now, id); err != nil {
		log.Printf("CompleteEpic: close epic id=%d: %v", id, err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	updated := getIssueByID(id)
	if updated == nil {
		jsonError(w, "not found after update", http.StatusInternalServerError)
		return
	}
	saveSnapshot(updated, user)
	jsonOK(w, updated)
}

// DeleteIssue moves an issue to the Trash (soft-delete). The issue and every
// descendant reachable via parent_id (tasks under a ticket, and any deeper
// orphan chains) get deleted_at stamped atomically. Related rows — comments,
// history, tags, time_entries, attachments, issue_relations — are preserved
// so a later Restore re-attaches them automatically.
//
// Caller can hard-delete via DELETE /issues/{id}/purge once the issue is in
// the Trash. Deleting an already-trashed issue is a no-op 404 (the UI would
// only offer Restore or Purge on those rows).
func DeleteIssue(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	user := auth.GetUser(r)
	var deletedBy *int64
	if user != nil {
		deletedBy = &user.ID
	}
	res, err := db.DB.Exec(`
		WITH RECURSIVE descendants(id) AS (
			SELECT id FROM issues WHERE id = ? AND deleted_at IS NULL
			UNION ALL
			SELECT i.id FROM issues i
			JOIN descendants d ON i.parent_id = d.id
			WHERE i.deleted_at IS NULL
		)
		UPDATE issues
		   SET deleted_at = datetime('now'),
		       deleted_by = ?
		 WHERE id IN (SELECT id FROM descendants)
	`, id, deletedBy)
	if err != nil {
		jsonError(w, "delete failed", http.StatusInternalServerError)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		// Either no such issue or it was already in the trash.
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	// History snapshot on the targeted issue only — cascaded tasks are
	// reconstructible from the ticket snapshot + parent_id chain.
	if snap := getIssueByID(id); snap != nil {
		saveSnapshot(snap, user)
	}
	w.WriteHeader(http.StatusNoContent)
}

// RestoreIssue clears deleted_at on a trashed issue. Descendants that were
// cascaded on delete are NOT auto-restored — restore is deliberately explicit
// so an operator can pick what to bring back. Surviving issue_relations
// (group / sprint / depends_on / impacts) re-attach automatically because
// they were never touched at delete time.
func RestoreIssue(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	res, err := db.DB.Exec(
		`UPDATE issues SET deleted_at = NULL, deleted_by = NULL
		  WHERE id = ? AND deleted_at IS NOT NULL`, id)
	if err != nil {
		jsonError(w, "restore failed", http.StatusInternalServerError)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	restored := getIssueByID(id)
	if restored == nil {
		jsonError(w, "not found after restore", http.StatusInternalServerError)
		return
	}
	saveSnapshot(restored, auth.GetUser(r))
	jsonOK(w, restored)
}

// PurgeIssue permanently deletes a trashed issue and everything ON DELETE
// CASCADE-bound to it (comments, history, tags, time_entries, attachments,
// issue_relations rows where this issue is source or target). Only works on
// issues already in the Trash — to purge a live issue you must soft-delete
// it first. This is intentionally a two-step flow to prevent accidents.
func PurgeIssue(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	rows, err := db.DB.Query(`
		WITH RECURSIVE descendants(id) AS (
			SELECT id FROM issues WHERE id = ? AND deleted_at IS NOT NULL
			UNION ALL
			SELECT i.id FROM issues i
			JOIN descendants d ON i.parent_id = d.id
		)
		SELECT id FROM descendants
	`, id)
	if err != nil {
		jsonError(w, "purge failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	descendantIDs := make([]int64, 0, 8)
	for rows.Next() {
		var issueID int64
		if rows.Scan(&issueID) == nil {
			descendantIDs = append(descendantIDs, issueID)
		}
	}
	if len(descendantIDs) == 0 {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	deleteAnchorEntityRelationsByIssueIDs(descendantIDs)
	ph := makePlaceholders(len(descendantIDs))
	args := make([]any, 0, len(descendantIDs)*2)
	for _, issueID := range descendantIDs {
		args = append(args, issueID)
	}
	for _, issueID := range descendantIDs {
		args = append(args, issueID)
	}
	if _, err := db.DB.Exec(`DELETE FROM entity_relations WHERE (source_type='issue' AND source_id IN (`+ph+`)) OR (target_type='issue' AND target_id IN (`+ph+`))`, args...); err != nil {
		jsonError(w, "purge failed", http.StatusInternalServerError)
		return
	}
	res, err := db.DB.Exec(
		`DELETE FROM issues WHERE id = ? AND deleted_at IS NOT NULL`, id)
	if err != nil {
		jsonError(w, "purge failed", http.StatusInternalServerError)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// CreateOrphanIssue creates an issue without a project — only allowed for type=sprint.
// POST /api/issues  { "title": "...", "type": "sprint", ... }
func CreateOrphanIssue(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		Type        string `json:"type"`
		Status      string `json:"status"`
		Priority    string `json:"priority"`
		StartDate   string `json:"start_date"`
		EndDate     string `json:"end_date"`
		SprintState string `json:"sprint_state"`
		AssigneeID  *int64 `json:"assignee_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Title == "" {
		jsonError(w, "title required", http.StatusBadRequest)
		return
	}
	if body.Type == "" {
		body.Type = "sprint"
	}
	if body.Type != "sprint" {
		jsonError(w, "only sprint issues may be created without a project", http.StatusUnprocessableEntity)
		return
	}
	if body.Status == "" {
		body.Status = "backlog" // sprints default to backlog, not new
	}
	if body.Priority == "" {
		body.Priority = "medium"
	}

	var orphanCreatedBy *int64
	if u := auth.GetUser(r); u != nil {
		orphanCreatedBy = &u.ID
	}

	res, err := db.DB.Exec(`
		INSERT INTO issues(project_id, issue_number, type, title, description,
		                   status, priority, start_date, end_date, sprint_state, assignee_id, created_by)
		VALUES(NULL, 0, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, body.Type, body.Title, body.Description,
		body.Status, body.Priority, body.StartDate, body.EndDate, body.SprintState, body.AssigneeID, orphanCreatedBy)
	if err != nil {
		jsonError(w, "insert failed", http.StatusInternalServerError)
		return
	}
	id, _ := res.LastInsertId()

	issue := getIssueByID(id)
	if issue == nil {
		jsonError(w, "fetch after insert failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(issue)
}
