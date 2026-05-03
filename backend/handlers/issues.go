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
	"fmt"
	"log"
	"net/url"
	"strconv"
	"strings"

	"github.com/markus-barta/paimos/backend/db"
	"github.com/markus-barta/paimos/backend/models"
)

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
	       i.accepted_at, i.accepted_by, i.invoiced_at, i.invoice_number,
	       i.deleted_at, i.deleted_by, COALESCE(du.username, '')
	FROM issues i
	LEFT JOIN users u ON u.id = i.assignee_id
	LEFT JOIN projects p ON p.id = i.project_id
	LEFT JOIN users cb ON cb.id = i.created_by
	LEFT JOIN users du ON du.id = i.deleted_by
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
	       i.accepted_at, i.accepted_by, i.invoiced_at, i.invoice_number,
	       i.deleted_at, i.deleted_by, COALESCE(du.username, '')
	FROM issues i
	LEFT JOIN users u ON u.id = i.assignee_id
	LEFT JOIN projects p ON p.id = i.project_id
	LEFT JOIN users cb ON cb.id = i.created_by
	LEFT JOIN users du ON du.id = i.deleted_by
`

// liveIssuesWhere is the WHERE predicate every issue-listing query must apply
// to hide soft-deleted rows. Trash-listing endpoints bypass this (using
// `i.deleted_at IS NOT NULL` explicitly). Prefix with " AND " when appending
// after an existing WHERE clause.
const liveIssuesWhere = `i.deleted_at IS NULL`

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
		&i.DeletedAt, &i.DeletedBy, &i.DeletedByName,
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
