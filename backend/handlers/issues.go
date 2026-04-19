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
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/markus-barta/paimos/backend/auth"
	"github.com/markus-barta/paimos/backend/db"
	"github.com/markus-barta/paimos/backend/models"
)

// saveSnapshot inserts a full JSON snapshot of the issue into issue_history.
func saveSnapshot(issue *models.Issue, changedBy *models.User) {
	blob, err := json.Marshal(issue)
	if err != nil {
		return
	}
	var uid *int64
	if changedBy != nil {
		uid = &changedBy.ID
	}
	if _, err := db.DB.Exec(
		`INSERT INTO issue_history(issue_id, changed_by, snapshot, changed_at) VALUES(?,?,?,?)`,
		issue.ID, uid, string(blob), time.Now().UTC().Format("2006-01-02 15:04:05"),
	); err != nil {
		log.Printf("saveSnapshot: issue_id=%d: %v", issue.ID, err)
	}
}

// issueSelect is the standard SELECT + JOIN used for single-row queries (getIssueByID).
const issueSelect = `
	SELECT i.id, i.project_id, i.issue_number, i.type, i.parent_id,
	       i.title, i.description, i.acceptance_criteria, i.notes,
	       i.status, i.priority, i.cost_unit, i.release,
	       i.billing_type, i.total_budget, i.rate_hourly, i.rate_lp,
	       i.start_date, i.end_date, i.group_state, i.sprint_state,
	       i.jira_id, i.jira_version, i.jira_text,
	       i.estimate_hours, i.estimate_lp, i.ar_hours, i.ar_lp,
	       i.time_override,
	       i.color,
	       i.assignee_id, i.created_at, i.updated_at,
	       u.id, u.username, u.role, u.created_at,
	       p.key,
	       COALESCE((
	           SELECT GROUP_CONCAT(ir.source_id)
	           FROM issue_relations ir
	           WHERE ir.target_id = i.id AND ir.type = 'sprint'
	       ), '') AS sprint_ids,
	       i.archived,
	       i.created_by, COALESCE(cb.username, ''),
	       COALESCE((
	           SELECT eu.username FROM issue_history ih
	           JOIN users eu ON eu.id = ih.changed_by
	           WHERE ih.issue_id = i.id
	           ORDER BY ih.changed_at DESC, ih.id DESC LIMIT 1
	       ), ''),
	       COALESCE((
	           SELECT SUM(
	               CASE
	                   WHEN te.override IS NOT NULL THEN te.override
	                   WHEN te.stopped_at IS NOT NULL THEN
	                       (julianday(te.stopped_at) - julianday(te.started_at)) * 24
	                   ELSE 0
	               END
	           ) FROM time_entries te WHERE te.issue_id = i.id
	       ), 0),
	       i.accepted_at, i.accepted_by, i.invoiced_at, i.invoice_number
	FROM issues i
	LEFT JOIN users u ON u.id = i.assignee_id
	LEFT JOIN projects p ON p.id = i.project_id
	LEFT JOIN users cb ON cb.id = i.created_by
`

// issueSelectCore is like issueSelect but without the 3 correlated subqueries
// (sprint_ids, last_changed_by, booked_hours). Use enrichIssues() to batch-load them.
const issueSelectCore = `
	SELECT i.id, i.project_id, i.issue_number, i.type, i.parent_id,
	       i.title, i.description, i.acceptance_criteria, i.notes,
	       i.status, i.priority, i.cost_unit, i.release,
	       i.billing_type, i.total_budget, i.rate_hourly, i.rate_lp,
	       i.start_date, i.end_date, i.group_state, i.sprint_state,
	       i.jira_id, i.jira_version, i.jira_text,
	       i.estimate_hours, i.estimate_lp, i.ar_hours, i.ar_lp,
	       i.time_override,
	       i.color,
	       i.assignee_id, i.created_at, i.updated_at,
	       u.id, u.username, u.role, u.created_at,
	       p.key,
	       '' AS sprint_ids,
	       i.archived,
	       i.created_by, COALESCE(cb.username, ''),
	       '' AS last_changed_by,
	       0 AS booked_hours,
	       i.accepted_at, i.accepted_by, i.invoiced_at, i.invoice_number
	FROM issues i
	LEFT JOIN users u ON u.id = i.assignee_id
	LEFT JOIN projects p ON p.id = i.project_id
	LEFT JOIN users cb ON cb.id = i.created_by
`

func scanIssue(rows interface {
	Scan(...any) error
}) (*models.Issue, error) {
	var i models.Issue
	var uidInt *int64
	var uname, urole, ucreated *string
	var projKey *string
	// v2 nullable fields — stored as empty string NOT NULL DEFAULT '' in DB;
	// treat empty string as nil for clean JSON output.
	var billingType, startDate, endDate, groupState, sprintState string
	var jiraID, jiraVersion, jiraText string
	var sprintIDsCSV string
	var archivedInt int
	if err := rows.Scan(
		&i.ID, &i.ProjectID, &i.IssueNumber, &i.Type, &i.ParentID,
		&i.Title, &i.Description, &i.AcceptanceCriteria, &i.Notes,
		&i.Status, &i.Priority, &i.CostUnit, &i.Release,
		&billingType, &i.TotalBudget, &i.RateHourly, &i.RateLp,
		&startDate, &endDate, &groupState, &sprintState,
		&jiraID, &jiraVersion, &jiraText,
		&i.EstimateHours, &i.EstimateLp, &i.ArHours, &i.ArLp,
		&i.TimeOverride,
		&i.Color,
		&i.AssigneeID, &i.CreatedAt, &i.UpdatedAt,
		&uidInt, &uname, &urole, &ucreated,
		&projKey,
		&sprintIDsCSV, &archivedInt,
		&i.CreatedBy, &i.CreatedByName, &i.LastChangedByName,
		&i.BookedHours,
		&i.AcceptedAt, &i.AcceptedBy, &i.InvoicedAt, &i.InvoiceNumber,
	); err != nil {
		return nil, err
	}
	// Parse sprint_ids CSV → []int64
	i.SprintIDs = []int64{}
	for _, s := range strings.Split(sprintIDsCSV, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if id, err := strconv.ParseInt(s, 10, 64); err == nil {
			i.SprintIDs = append(i.SprintIDs, id)
		}
	}
	i.Archived = archivedInt == 1
	if projKey != nil && *projKey != "" && i.IssueNumber > 0 {
		i.IssueKey = fmt.Sprintf("%s-%d", *projKey, i.IssueNumber)
	} else if i.ProjectID == nil {
		// Project-less sprint: use SPRINT-{id} as the key
		i.IssueKey = fmt.Sprintf("SPRINT-%d", i.ID)
	}
	if uidInt != nil && uname != nil {
		i.Assignee = &models.User{
			ID: *uidInt, Username: *uname, Role: ptrStr(urole), CreatedAt: ptrStr(ucreated),
		}
	}
	// map empty strings → nil pointers for clean JSON
	if billingType != "" {
		i.BillingType = &billingType
	}
	if startDate != "" {
		i.StartDate = &startDate
	}
	if endDate != "" {
		i.EndDate = &endDate
	}
	if groupState != "" {
		i.GroupState = &groupState
	}
	if sprintState != "" {
		i.SprintState = &sprintState
	}
	if jiraID != "" {
		i.JiraID = &jiraID
	}
	if jiraVersion != "" {
		i.JiraVersion = &jiraVersion
	}
	if jiraText != "" {
		i.JiraText = &jiraText
	}
	return &i, nil
}

// computeTimeFields sets the 4-field time model on each issue:
// logged = direct time entries (already in BookedHours), rollup = sum of children's total, total = override ?? logged + rollup
func computeTimeFields(issues []models.Issue) []models.Issue {
	byID := make(map[int64]int, len(issues))
	for idx := range issues {
		issues[idx].TimeLogged = issues[idx].BookedHours
		byID[issues[idx].ID] = idx
	}
	// Build children map
	children := make(map[int64][]int64)
	for idx := range issues {
		if issues[idx].ParentID != nil {
			children[*issues[idx].ParentID] = append(children[*issues[idx].ParentID], issues[idx].ID)
		}
	}
	// Recursive total computation (memoized)
	totals := make(map[int64]float64)
	var getTotal func(id int64) float64
	getTotal = func(id int64) float64 {
		if t, ok := totals[id]; ok {
			return t
		}
		idx, exists := byID[id]
		if !exists {
			return 0
		}
		i := &issues[idx]
		logged := i.TimeLogged
		var rollup float64
		for _, childID := range children[id] {
			rollup += getTotal(childID)
		}
		i.TimeRollup = rollup
		if i.TimeOverride != nil {
			i.TimeTotal = *i.TimeOverride
		} else {
			i.TimeTotal = logged + rollup
		}
		totals[id] = i.TimeTotal
		return i.TimeTotal
	}
	for idx := range issues {
		getTotal(issues[idx].ID)
	}
	return issues
}

// ── Batch loaders ────────────────────────────────────────────────────────────

// loadSprintIDsBatch loads sprint_ids for all given issues in a single query.
func loadSprintIDsBatch(issues []models.Issue) {
	if len(issues) == 0 {
		return
	}
	ids := make([]any, len(issues))
	byID := make(map[int64]int, len(issues))
	for i, iss := range issues {
		ids[i] = iss.ID
		byID[iss.ID] = i
	}
	placeholders := buildPlaceholders(len(ids))
	rows, err := db.DB.Query(`
		SELECT ir.target_id, GROUP_CONCAT(ir.source_id)
		FROM issue_relations ir
		WHERE ir.target_id IN (`+placeholders+`) AND ir.type = 'sprint'
		GROUP BY ir.target_id
	`, ids...)
	if err != nil {
		log.Printf("loadSprintIDsBatch: %v", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var targetID int64
		var csv string
		if err := rows.Scan(&targetID, &csv); err != nil {
			log.Printf("loadSprintIDsBatch scan: %v", err)
			continue
		}
		if idx, ok := byID[targetID]; ok {
			issues[idx].SprintIDs = []int64{}
			for _, s := range strings.Split(csv, ",") {
				s = strings.TrimSpace(s)
				if s == "" {
					continue
				}
				if id, err := strconv.ParseInt(s, 10, 64); err == nil {
					issues[idx].SprintIDs = append(issues[idx].SprintIDs, id)
				}
			}
		}
	}
}

// loadLastChangedByBatch loads the last_changed_by username for all given issues.
func loadLastChangedByBatch(issues []models.Issue) {
	if len(issues) == 0 {
		return
	}
	ids := make([]any, len(issues))
	byID := make(map[int64]int, len(issues))
	for i, iss := range issues {
		ids[i] = iss.ID
		byID[iss.ID] = i
	}
	placeholders := buildPlaceholders(len(ids))
	// Use MAX(id) per issue to find the latest history entry — avoids correlated subquery.
	rows, err := db.DB.Query(`
		SELECT ih.issue_id, eu.username
		FROM issue_history ih
		JOIN users eu ON eu.id = ih.changed_by
		WHERE ih.id IN (
			SELECT MAX(ih2.id) FROM issue_history ih2
			WHERE ih2.issue_id IN (`+placeholders+`)
			GROUP BY ih2.issue_id
		)
	`, ids...)
	if err != nil {
		log.Printf("loadLastChangedByBatch: %v", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var issueID int64
		var username string
		if err := rows.Scan(&issueID, &username); err != nil {
			log.Printf("loadLastChangedByBatch scan: %v", err)
			continue
		}
		if idx, ok := byID[issueID]; ok {
			issues[idx].LastChangedByName = username
		}
	}
}

// loadBookedHoursBatch loads booked hours for all given issues in a single query.
func loadBookedHoursBatch(issues []models.Issue) {
	if len(issues) == 0 {
		return
	}
	ids := make([]any, len(issues))
	byID := make(map[int64]int, len(issues))
	for i, iss := range issues {
		ids[i] = iss.ID
		byID[iss.ID] = i
	}
	placeholders := buildPlaceholders(len(ids))
	rows, err := db.DB.Query(`
		SELECT te.issue_id, SUM(
			CASE
				WHEN te.override IS NOT NULL THEN te.override
				WHEN te.stopped_at IS NOT NULL THEN
					(julianday(te.stopped_at) - julianday(te.started_at)) * 24
				ELSE 0
			END
		) FROM time_entries te
		WHERE te.issue_id IN (`+placeholders+`)
		GROUP BY te.issue_id
	`, ids...)
	if err != nil {
		log.Printf("loadBookedHoursBatch: %v", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var issueID int64
		var hours float64
		if err := rows.Scan(&issueID, &hours); err != nil {
			log.Printf("loadBookedHoursBatch scan: %v", err)
			continue
		}
		if idx, ok := byID[issueID]; ok {
			issues[idx].BookedHours = hours
		}
	}
}

// enrichIssues batch-loads sprint IDs, last changed by, booked hours, tags,
// and computes time fields for a slice of issues.
func enrichIssues(issues []models.Issue) []models.Issue {
	loadSprintIDsBatch(issues)
	loadLastChangedByBatch(issues)
	loadBookedHoursBatch(issues)
	issues = LoadTagsForIssues(issues)
	issues = computeTimeFields(issues)
	return issues
}

// ptrOrEmpty returns the string value of a *string, or "" if nil.
func ptrOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func getIssueByID(id int64) *models.Issue {
	row := db.DB.QueryRow(issueSelect+` WHERE i.id=?`, id)
	i, err := scanIssue(row)
	if err != nil {
		return nil
	}
	LoadTagsForIssue(i)
	return i
}

// applyIssueFilters adds WHERE clauses based on common filter query params.
// Supports multi-value (comma-separated) and negation (! prefix).
func applyIssueFilters(query string, args []any, q url.Values) (string, []any) {
	// status: multi-value, negation
	if s := q.Get("status"); s != "" {
		query, args = applyMultiFilter(query, args, "i.status", s)
	}
	// priority: multi-value, negation
	if p := q.Get("priority"); p != "" {
		query, args = applyMultiFilter(query, args, "i.priority", p)
	}
	// type: multi-value, negation
	if t := q.Get("type"); t != "" {
		query, args = applyMultiFilter(query, args, "i.type", t)
	}
	// cost_unit: multi-value
	if cu := q.Get("cost_unit"); cu != "" {
		query, args = applyMultiFilter(query, args, "i.cost_unit", cu)
	}
	// release: multi-value
	if rel := q.Get("release"); rel != "" {
		query, args = applyMultiFilter(query, args, "i.release", rel)
	}
	// assignee_id: multi-value, special "unassigned" sentinel
	if aid := q.Get("assignee_id"); aid != "" {
		vals := splitCSV(aid)
		hasUnassigned := false
		ids := []string{}
		for _, v := range vals {
			if v == "unassigned" {
				hasUnassigned = true
			} else {
				ids = append(ids, v)
			}
		}
		if len(ids) > 0 && hasUnassigned {
			ph := ""
			for _, id := range ids {
				if ph != "" {
					ph += ","
				}
				ph += "?"
				args = append(args, id)
			}
			query += " AND (i.assignee_id IN (" + ph + ") OR i.assignee_id IS NULL)"
		} else if len(ids) > 0 {
			ph := ""
			for _, id := range ids {
				if ph != "" {
					ph += ","
				}
				ph += "?"
				args = append(args, id)
			}
			query += " AND i.assignee_id IN (" + ph + ")"
		} else if hasUnassigned {
			query += " AND i.assignee_id IS NULL"
		}
	}
	// tags: comma-separated tag IDs (ANY match)
	if tags := q.Get("tags"); tags != "" {
		tagIDs := splitCSV(tags)
		if len(tagIDs) > 0 {
			ph := ""
			for _, tid := range tagIDs {
				if ph != "" {
					ph += ","
				}
				ph += "?"
				args = append(args, tid)
			}
			query += " AND i.id IN (SELECT issue_id FROM issue_tags WHERE tag_id IN (" + ph + "))"
		}
	}
	// sprints: comma-separated sprint IDs
	if sprints := q.Get("sprints"); sprints != "" {
		sids := splitCSV(sprints)
		if len(sids) > 0 {
			ph := ""
			for _, sid := range sids {
				if ph != "" {
					ph += ","
				}
				ph += "?"
				args = append(args, sid)
			}
			query += " AND i.id IN (SELECT target_id FROM issue_relations WHERE type='sprint' AND source_id IN (" + ph + "))"
		}
	}
	return query, args
}

// applyMultiFilter handles comma-separated values with optional ! negation prefix.
// e.g. "done,accepted" → IN ('done','accepted')
// e.g. "!done,!cancelled" → NOT IN ('done','cancelled')
// Mixed positive and negative are combined with AND.
func applyMultiFilter(query string, args []any, col string, raw string) (string, []any) {
	vals := splitCSV(raw)
	var pos, neg []string
	for _, v := range vals {
		if strings.HasPrefix(v, "!") {
			neg = append(neg, strings.TrimPrefix(v, "!"))
		} else {
			pos = append(pos, v)
		}
	}
	if len(pos) > 0 {
		ph := ""
		for _, v := range pos {
			if ph != "" {
				ph += ","
			}
			ph += "?"
			args = append(args, v)
		}
		query += " AND " + col + " IN (" + ph + ")"
	}
	if len(neg) > 0 {
		ph := ""
		for _, v := range neg {
			if ph != "" {
				ph += ","
			}
			ph += "?"
			args = append(args, v)
		}
		query += " AND " + col + " NOT IN (" + ph + ")"
	}
	return query, args
}

func ListIssues(w http.ResponseWriter, r *http.Request) {
	projectID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid project id", http.StatusBadRequest)
		return
	}

	query := issueSelectCore + ` WHERE i.project_id = ?`
	args := []any{projectID}

	query, args = applyIssueFilters(query, args, r.URL.Query())

	if fts := strings.TrimSpace(r.URL.Query().Get("q")); len(fts) >= 2 {
		likePattern := "%" + fts + "%"
		query += ` AND i.id IN (
			SELECT CAST(entity_id AS INTEGER) FROM search_index
			WHERE entity_type IN ('issue','comment') AND search_index MATCH ?
			UNION
			SELECT id FROM issues WHERE project_id = ? AND (
				title LIKE ? OR description LIKE ? OR acceptance_criteria LIKE ? OR notes LIKE ?
				OR (SELECT key FROM projects WHERE id = issues.project_id) || '-' || issue_number LIKE ?
			)
		)`
		args = append(args, fts+"*", projectID, likePattern, likePattern, likePattern, likePattern, likePattern)
	}

	// Pagination
	orderBy := " ORDER BY i.type DESC, i.issue_number ASC"
	query += orderBy

	limit := 0
	offset := 0
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 {
			limit = v
		}
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if v, err := strconv.Atoi(o); err == nil && v >= 0 {
			offset = v
		}
	}
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d OFFSET %d", limit, offset)
	}

	rows, err := db.DB.Query(query, args...)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	issues := []models.Issue{}
	for rows.Next() {
		i, err := scanIssue(rows)
		if err != nil {
			jsonError(w, "scan failed", http.StatusInternalServerError)
			return
		}
		issues = append(issues, *i)
	}
	issues = enrichIssues(issues)

	if r.URL.Query().Get("fields") == "list" {
		for idx := range issues {
			issues[idx].Description = ""
			issues[idx].AcceptanceCriteria = ""
			issues[idx].Notes = ""
			issues[idx].JiraText = nil
		}
	}

	jsonOK(w, issues)
}

func GetIssueTree(w http.ResponseWriter, r *http.Request) {
	projectID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid project id", http.StatusBadRequest)
		return
	}

	rows, err := db.DB.Query(issueSelectCore+` WHERE i.project_id=? ORDER BY i.issue_number ASC`, projectID)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	all := map[int64]*models.Issue{}
	order := []int64{}
	flat := []models.Issue{}
	for rows.Next() {
		i, err := scanIssue(rows)
		if err != nil {
			jsonError(w, "scan failed", http.StatusInternalServerError)
			return
		}
		flat = append(flat, *i)
		order = append(order, i.ID)
	}
	flat = enrichIssues(flat)
	for idx := range flat {
		all[flat[idx].ID] = &flat[idx]
	}

	// Build tree
	roots := []models.Issue{}
	for _, id := range order {
		i := all[id]
		if i.ParentID == nil {
			roots = append(roots, *i)
		} else if parent, ok := all[*i.ParentID]; ok {
			parent.Children = append(parent.Children, *i)
			all[*i.ParentID] = parent
		} else {
			roots = append(roots, *i) // orphan — parent deleted
		}
	}
	jsonOK(w, roots)
}

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
	if issue == nil {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	jsonOK(w, issue)
}

func GetIssueChildren(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	rows, err := db.DB.Query(issueSelectCore+` WHERE i.parent_id=? ORDER BY i.issue_number ASC`, id)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	issues := []models.Issue{}
	for rows.Next() {
		i, err := scanIssue(rows)
		if err != nil {
			jsonError(w, "scan failed", http.StatusInternalServerError)
			return
		}
		issues = append(issues, *i)
	}
	issues = enrichIssues(issues)
	jsonOK(w, issues)
}

func UpdateIssue(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}

	existing := getIssueByID(id)
	if existing == nil {
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
	_, err = db.DB.Exec(`
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
				if _, err := db.DB.Exec(`UPDATE issues SET status=?, updated_at=? WHERE parent_id=? AND status NOT IN ('done','delivered','accepted','invoiced','cancelled')`,
					*body.Status, nowTS, id); err != nil {
					log.Printf("UpdateIssue: cascade children id=%d: %v", id, err)
				}
				// For epics, also cascade to grandchildren (tasks under tickets)
				if issue.Type == "epic" {
					if _, err := db.DB.Exec(`UPDATE issues SET status=?, updated_at=? WHERE parent_id IN (SELECT id FROM issues WHERE parent_id=?) AND status NOT IN ('done','delivered','accepted','invoiced','cancelled')`,
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

	rows, err := db.DB.Query(issueSelectCore+` WHERE i.parent_id=? ORDER BY i.issue_number ASC`, id)
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

func DeleteIssue(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	res, err := db.DB.Exec("DELETE FROM issues WHERE id=?", id)
	if err != nil {
		jsonError(w, "delete failed", http.StatusInternalServerError)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListAllIssues returns issues across all projects, with optional project_ids filter and pagination.
// GET /api/issues?project_ids=1,2,3&limit=100&offset=0
func ListAllIssues(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	// Pagination
	limit := 100
	offset := 0
	if l := q.Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 {
			limit = v
		}
	}
	if o := q.Get("offset"); o != "" {
		if v, err := strconv.Atoi(o); err == nil && v >= 0 {
			offset = v
		}
	}

	query := issueSelectCore + ` WHERE 1=1`
	args := []any{}

	// Apply shared filters (status, priority, type, assignee, cost_unit, release, tags, sprints)
	query, args = applyIssueFilters(query, args, q)

	// Optional project_ids filter (comma-separated); "none" = project_id IS NULL
	if pids := q.Get("project_ids"); pids != "" {
		wantNull := false
		placeholders := ""
		for _, p := range splitCSV(pids) {
			if p == "none" {
				wantNull = true
				continue
			}
			if id, err := strconv.ParseInt(p, 10, 64); err == nil {
				if placeholders != "" {
					placeholders += ","
				}
				placeholders += "?"
				args = append(args, id)
			}
		}
		if placeholders != "" && wantNull {
			query += " AND (i.project_id IN (" + placeholders + ") OR i.project_id IS NULL)"
		} else if placeholders != "" {
			query += " AND i.project_id IN (" + placeholders + ")"
		} else if wantNull {
			query += " AND i.project_id IS NULL"
		}
	}

	if fts := strings.TrimSpace(q.Get("q")); len(fts) >= 2 {
		likePattern := "%" + fts + "%"
		ftsClause := ` AND i.id IN (
			SELECT CAST(entity_id AS INTEGER) FROM search_index
			WHERE entity_type IN ('issue','comment') AND search_index MATCH ?
			UNION
			SELECT id FROM issues WHERE (
				title LIKE ? OR description LIKE ? OR acceptance_criteria LIKE ? OR notes LIKE ?
			)
		)`
		query += ftsClause
		args = append(args, fts+"*", likePattern, likePattern, likePattern, likePattern)
	}
	query += " ORDER BY i.updated_at DESC, i.id DESC"
	query += fmt.Sprintf(" LIMIT %d OFFSET %d", limit, offset)

	rows, err := db.DB.Query(query, args...)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	issues := []models.Issue{}
	for rows.Next() {
		i, err := scanIssue(rows)
		if err != nil {
			jsonError(w, "scan failed", http.StatusInternalServerError)
			return
		}
		issues = append(issues, *i)
	}
	issues = enrichIssues(issues)

	if q.Get("fields") == "list" {
		for idx := range issues {
			issues[idx].Description = ""
			issues[idx].AcceptanceCriteria = ""
			issues[idx].Notes = ""
			issues[idx].JiraText = nil
		}
	}

	// Also return total count for the same filter (for "X remaining" UI)
	// Build count query with same filters
	countQuery := `SELECT COUNT(*) FROM issues i WHERE 1=1`
	countArgs := []any{}
	countQuery, countArgs = applyIssueFilters(countQuery, countArgs, q)
	if pids := q.Get("project_ids"); pids != "" {
		wantNull := false
		placeholders := ""
		for _, p := range splitCSV(pids) {
			if p == "none" {
				wantNull = true
				continue
			}
			if id, err := strconv.ParseInt(p, 10, 64); err == nil {
				if placeholders != "" {
					placeholders += ","
				}
				placeholders += "?"
				countArgs = append(countArgs, id)
			}
		}
		if placeholders != "" && wantNull {
			countQuery += " AND (i.project_id IN (" + placeholders + ") OR i.project_id IS NULL)"
		} else if placeholders != "" {
			countQuery += " AND i.project_id IN (" + placeholders + ")"
		} else if wantNull {
			countQuery += " AND i.project_id IS NULL"
		}
	}
	if fts := strings.TrimSpace(q.Get("q")); len(fts) >= 2 {
		countQuery += ` AND i.id IN (
			SELECT CAST(entity_id AS INTEGER) FROM search_index
			WHERE entity_type IN ('issue','comment') AND search_index MATCH ?
		)`
		countArgs = append(countArgs, fts+"*")
	}
	var total int
	if err := db.DB.QueryRow(countQuery, countArgs...).Scan(&total); err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	type response struct {
		Issues []models.Issue `json:"issues"`
		Total  int            `json:"total"`
		Offset int            `json:"offset"`
		Limit  int            `json:"limit"`
	}
	jsonOK(w, response{Issues: issues, Total: total, Offset: offset, Limit: limit})
}

// CreateOrphanIssue creates an issue without a project — only allowed for type=sprint.
// POST /api/issues  { "title": "...", "type": "sprint", ... }
func CreateOrphanIssue(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Title       string  `json:"title"`
		Description string  `json:"description"`
		Type        string  `json:"type"`
		Status      string  `json:"status"`
		Priority    string  `json:"priority"`
		StartDate   string  `json:"start_date"`
		EndDate     string  `json:"end_date"`
		SprintState string  `json:"sprint_state"`
		AssigneeID  *int64  `json:"assignee_id"`
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

// splitCSV splits a comma-separated string into trimmed non-empty parts.
func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func RecentIssues(w http.ResponseWriter, r *http.Request) {
	rows, err := db.DB.Query(issueSelectCore + ` ORDER BY i.updated_at DESC LIMIT 20`)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	issues := []models.Issue{}
	for rows.Next() {
		i, err := scanIssue(rows)
		if err != nil {
			continue
		}
		issues = append(issues, *i)
	}
	issues = enrichIssues(issues)
	jsonOK(w, issues)
}

func ListCostUnits(w http.ResponseWriter, r *http.Request) {
	projectID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid project id", http.StatusBadRequest)
		return
	}
	rows, err := db.DB.Query(
		`SELECT DISTINCT cost_unit FROM issues WHERE project_id=? AND cost_unit != '' ORDER BY cost_unit`, projectID,
	)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	vals := []string{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			log.Printf("scan error: %v", err)
			continue
		}
		vals = append(vals, v)
	}
	jsonOK(w, vals)
}

func ListReleases(w http.ResponseWriter, r *http.Request) {
	projectID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid project id", http.StatusBadRequest)
		return
	}
	rows, err := db.DB.Query(
		`SELECT DISTINCT release FROM issues WHERE project_id=? AND release != '' ORDER BY release`, projectID,
	)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	vals := []string{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			log.Printf("scan error: %v", err)
			continue
		}
		vals = append(vals, v)
	}
	jsonOK(w, vals)
}

// ListAllCostUnits returns distinct cost_unit values across all projects.
func ListAllCostUnits(w http.ResponseWriter, r *http.Request) {
	rows, err := db.DB.Query(
		`SELECT DISTINCT cost_unit FROM issues WHERE cost_unit != '' ORDER BY cost_unit`,
	)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	vals := []string{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			log.Printf("scan error: %v", err)
			continue
		}
		vals = append(vals, v)
	}
	jsonOK(w, vals)
}

// ListAllReleases returns distinct release values across all projects.
func ListAllReleases(w http.ResponseWriter, r *http.Request) {
	rows, err := db.DB.Query(
		`SELECT DISTINCT release FROM issues WHERE release != '' ORDER BY release`,
	)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	vals := []string{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			log.Printf("scan error: %v", err)
			continue
		}
		vals = append(vals, v)
	}
	jsonOK(w, vals)
}

// inferType auto-selects issue type based on parent.
func inferType(parentID *int64) string {
	if parentID == nil {
		return "ticket" // v2: default to ticket, not epic; caller supplies type explicitly for groups
	}
	parent := getIssueByID(*parentID)
	if parent == nil {
		return "ticket"
	}
	switch parent.Type {
	case "ticket":
		return "task"
	default:
		return "ticket"
	}
}

// validateParent enforces v2 hierarchy rules:
//   - Groups (epic, cost_unit, release) and sprints: no parent
//   - Ticket: no parent required; if parent set, must be same project
//   - Task: parent must be a ticket in same project
func validateParent(issueType string, parentID *int64, projectID *int64) error {
	switch issueType {
	case "epic", "cost_unit", "release", "sprint":
		if parentID != nil {
			return fmt.Errorf("%s cannot have a parent", issueType)
		}
		return nil
	}
	if parentID == nil {
		return nil // tickets and tasks can be top-level
	}
	parent := getIssueByID(*parentID)
	if parent == nil {
		return fmt.Errorf("parent issue not found")
	}
	// Cross-project check: only enforce when both sides have a project
	if projectID != nil && parent.ProjectID != nil && *parent.ProjectID != *projectID {
		return fmt.Errorf("parent must be in the same project")
	}
	if issueType == "task" && parent.Type != "ticket" {
		return fmt.Errorf("task parent must be a ticket")
	}
	return nil
}

func ptrStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// Ensure sql.Row satisfies the scanIssue interface.
var _ interface{ Scan(...any) error } = (*sql.Row)(nil)

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
		       i2.title
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
		if err := rows.Scan(&rel.SourceID, &rel.TargetID, &rel.Type, &rel.TargetKey, &rel.TargetTitle); err != nil {
			continue
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
	validTypes := map[string]bool{"groups": true, "sprint": true, "depends_on": true, "impacts": true}
	if !validTypes[body.Type] {
		jsonError(w, "type must be one of: groups, sprint, depends_on, impacts", http.StatusBadRequest)
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
	if body.Type == "sprint" {
		db.DB.QueryRow("SELECT COALESCE(MAX(rank),0)+1 FROM issue_relations WHERE source_id=? AND type='sprint'", dbSource).Scan(&rank)
	}
	_, err = db.DB.Exec(
		`INSERT OR IGNORE INTO issue_relations(source_id, target_id, type, rank) VALUES(?,?,?,?)`,
		dbSource, dbTarget, body.Type, rank,
	)
	if handleDBError(w, err, "issue relation") {
		return
	}
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
	res, err := db.DB.Exec(
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
	w.WriteHeader(http.StatusNoContent)
}

// ListIssuesByRelation returns issues linked to a container via issue_relations.
// GET /api/issues/:id/members?type=groups|sprint|depends_on|impacts
//
// Direction convention (unified after migration 32):
//   source = container/owner (epic, sprint, etc.)
//   target = member/child (ticket, task, etc.)
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
		issueSelectCore+` JOIN issue_relations ir ON ir.target_id = i.id WHERE ir.source_id = ? AND ir.type = ? ORDER BY ir.rank ASC, i.issue_number ASC`,
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

	// Apply rate cascade for sprint members so reporting gets resolved rates.
	if relType == "sprint" {
		for idx := range issues {
			ResolveRateCascade(&issues[idx])
		}
	}

	jsonOK(w, issues)
}

// ── Aggregation endpoint ──────────────────────────────────────────────────────

// IssueAggregation is the response for GET /api/issues/:id/aggregation.
type IssueAggregation struct {
	MemberCount      int      `json:"member_count"`
	EstimateHours    *float64 `json:"estimate_hours"`
	EstimateLp       *float64 `json:"estimate_lp"`
	EstimateEur      *float64 `json:"estimate_eur"`
	ArHours          *float64 `json:"ar_hours"`
	ArLp             *float64 `json:"ar_lp"`
	ArEur            *float64 `json:"ar_eur"`
	ActualHours      *float64 `json:"actual_hours"`
	ActualInternalCost *float64 `json:"actual_internal_cost"`
	MarginEur        *float64 `json:"margin_eur"`
}

// GetIssueAggregation computes aggregated estimate/AR/actual values for a
// container issue (cost_unit, epic) by summing across its group members.
// GET /api/issues/:id/aggregation
func GetIssueAggregation(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}

	// Fetch the container issue to get its rates
	var rateHourly, rateLp *float64
	err = db.DB.QueryRow("SELECT rate_hourly, rate_lp FROM issues WHERE id = ?", id).Scan(&rateHourly, &rateLp)
	if err != nil {
		jsonError(w, "issue not found", http.StatusNotFound)
		return
	}

	// Sum estimate/AR across group members (issue_relations type='groups', source=container)
	var agg IssueAggregation
	err = db.DB.QueryRow(`
		SELECT COUNT(*),
		       SUM(i.estimate_hours), SUM(i.estimate_lp),
		       SUM(i.ar_hours), SUM(i.ar_lp)
		FROM issues i
		JOIN issue_relations ir ON ir.target_id = i.id
		WHERE ir.source_id = ? AND ir.type = 'groups'
	`, id).Scan(&agg.MemberCount, &agg.EstimateHours, &agg.EstimateLp, &agg.ArHours, &agg.ArLp)
	if err != nil {
		jsonError(w, "aggregation query failed", http.StatusInternalServerError)
		return
	}

	// Also include the container's own values (cost units often have direct estimates)
	var selfEstH, selfEstLp, selfArH, selfArLp *float64
	if err := db.DB.QueryRow("SELECT estimate_hours, estimate_lp, ar_hours, ar_lp FROM issues WHERE id = ?", id).Scan(&selfEstH, &selfEstLp, &selfArH, &selfArLp); err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	addF := func(a *float64, b *float64) *float64 {
		if a == nil && b == nil {
			return nil
		}
		v := ptrF(a) + ptrF(b)
		return &v
	}

	agg.EstimateHours = addF(agg.EstimateHours, selfEstH)
	agg.EstimateLp = addF(agg.EstimateLp, selfEstLp)
	agg.ArHours = addF(agg.ArHours, selfArH)
	agg.ArLp = addF(agg.ArLp, selfArLp)

	// Compute EUR values using the container's rates
	rh := ptrF(rateHourly)
	rl := ptrF(rateLp)

	if agg.EstimateHours != nil || agg.EstimateLp != nil {
		v := ptrF(agg.EstimateHours)*rh + ptrF(agg.EstimateLp)*rl
		agg.EstimateEur = &v
	}
	if agg.ArHours != nil || agg.ArLp != nil {
		v := ptrF(agg.ArHours)*rh + ptrF(agg.ArLp)*rl
		agg.ArEur = &v
	}

	// Sum actuals from time_entries on member issues + the container itself
	var actualHours, actualInternalCost *float64
	err = db.DB.QueryRow(`
		SELECT SUM(
			CASE
				WHEN te.override IS NOT NULL THEN te.override
				WHEN te.stopped_at IS NOT NULL THEN
					(julianday(te.stopped_at) - julianday(te.started_at)) * 24
				ELSE 0
			END
		),
		SUM(
			CASE
				WHEN te.override IS NOT NULL THEN te.override
				WHEN te.stopped_at IS NOT NULL THEN
					(julianday(te.stopped_at) - julianday(te.started_at)) * 24
				ELSE 0
			END * COALESCE(te.internal_rate_hourly, 0)
		)
		FROM time_entries te
		WHERE te.issue_id IN (
			SELECT ir.target_id FROM issue_relations ir
			WHERE ir.source_id = ? AND ir.type = 'groups'
			UNION ALL SELECT ?
		)
	`, id, id).Scan(&actualHours, &actualInternalCost)
	if err == nil {
		agg.ActualHours = actualHours
		agg.ActualInternalCost = actualInternalCost
	}

	// Margin = AR EUR - internal cost
	if agg.ArEur != nil && agg.ActualInternalCost != nil {
		v := *agg.ArEur - *agg.ActualInternalCost
		agg.MarginEur = &v
	}

	jsonOK(w, agg)
}

func ptrF(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

// ── History endpoints ─────────────────────────────────────────────────────────

type HistoryEntry struct {
	ID              int64  `json:"id"`
	IssueID         int64  `json:"issue_id"`
	ChangedBy       *int64 `json:"changed_by"`
	ChangedByName   string `json:"changed_by_name"`
	Snapshot        any    `json:"snapshot"`
	ChangedAt       string `json:"changed_at"`
}

// GetIssueHistory returns all history entries for an issue, oldest→newest.
func GetIssueHistory(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}

	rows, err := db.DB.Query(`
		SELECT h.id, h.issue_id, h.changed_by, COALESCE(u.username,''), h.snapshot, h.changed_at
		FROM issue_history h
		LEFT JOIN users u ON u.id = h.changed_by
		WHERE h.issue_id = ?
		ORDER BY h.changed_at ASC, h.id ASC
	`, id)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	entries := []HistoryEntry{}
	for rows.Next() {
		var e HistoryEntry
		var rawSnapshot string
		if err := rows.Scan(&e.ID, &e.IssueID, &e.ChangedBy, &e.ChangedByName, &rawSnapshot, &e.ChangedAt); err != nil {
			continue
		}
		// Unmarshal snapshot so it's returned as a proper JSON object (not a string)
		var snap any
		if err := json.Unmarshal([]byte(rawSnapshot), &snap); err == nil {
			e.Snapshot = snap
		}
		entries = append(entries, e)
	}
	jsonOK(w, entries)
}
