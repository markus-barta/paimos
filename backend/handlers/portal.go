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
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/markus-barta/paimos/backend/auth"
	"github.com/markus-barta/paimos/backend/db"
)

// ── Portal types (customer-facing, no internal fields) ──────────────────────

type portalProject struct {
	ID          int64  `json:"id"`
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Status      string `json:"status"`
	LogoPath    string `json:"logo_path"`
	IssueCount  int    `json:"issue_count"`
	DoneCount   int    `json:"done_count"`
}

type portalIssue struct {
	ID                 int64    `json:"id"`
	IssueKey           string   `json:"issue_key"`
	Title              string   `json:"title"`
	Description        string   `json:"description"`
	AcceptanceCriteria string   `json:"acceptance_criteria"`
	Status             string   `json:"status"`
	Priority           string   `json:"priority"`
	Type               string   `json:"type"`
	CostUnit           string   `json:"cost_unit"`
	Release            string   `json:"release"`
	EstimateHours      *float64 `json:"estimate_hours"`
	EstimateLp         *float64 `json:"estimate_lp"`
	ArHours            *float64 `json:"ar_hours"`
	ArLp               *float64 `json:"ar_lp"`
	EstimateEur        *float64 `json:"estimate_eur"`
	ArEur              *float64 `json:"ar_eur"`
	AcceptedAt         *string  `json:"accepted_at"`
	CreatedAt          string   `json:"created_at"`
	UpdatedAt          string   `json:"updated_at"`
}

type portalSummary struct {
	TotalIssues  int      `json:"total_issues"`
	ByStatus     map[string]int `json:"by_status"`
	TotalEstEur  *float64 `json:"total_estimate_eur"`
	TotalArEur   *float64 `json:"total_ar_eur"`
}

// ── Helpers ──────────────────────────────────────────────────────────────────

func portalProjectID(r *http.Request) (int64, error) {
	return strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
}

func checkPortalAccess(r *http.Request, projectID int64) bool {
	return auth.HasProjectAccess(r, projectID)
}

// checkPortalEdit gates portal mutations (accept / reject / undo). Viewers
// may browse a project through the portal but cannot mutate issue status —
// that capability requires editor access.
func checkPortalEdit(r *http.Request, projectID int64) bool {
	return auth.CanEditProject(r, projectID)
}

// computeEur calculates EUR from hours/lp and the project's cost-unit rates.
// For portal display, we use the rate_hourly and rate_lp from the issue's
// parent epic or cost_unit. As a simpler approach, we compute from the issue
// itself if it has rate fields, otherwise return nil.
func computeEur(hours, lp, rateH, rateLP *float64) *float64 {
	var total float64
	var has bool
	if hours != nil && rateH != nil {
		total += *hours * *rateH
		has = true
	}
	if lp != nil && rateLP != nil {
		total += *lp * *rateLP
		has = true
	}
	if !has {
		return nil
	}
	return &total
}

// ── GET /api/portal/projects ─────────────────────────────────────────────────

func PortalListProjects(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if user == nil {
		jsonError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var query string
	var args []any

	if user.Role == "admin" {
		query = `
			SELECT p.id, p.key, p.name, p.description, p.status,
			       COALESCE(p.logo_path, ''),
			       COUNT(i.id) as issue_count,
			       COUNT(CASE WHEN i.status = 'done' THEN 1 END) as done_count
			FROM projects p
			LEFT JOIN issues i ON i.project_id = p.id
			WHERE p.status = 'active'
			GROUP BY p.id
			ORDER BY p.name`
	} else {
		query = `
			SELECT p.id, p.key, p.name, p.description, p.status,
			       COALESCE(p.logo_path, ''),
			       COUNT(i.id) as issue_count,
			       COUNT(CASE WHEN i.status = 'done' THEN 1 END) as done_count
			FROM projects p
			JOIN project_members pm ON pm.project_id = p.id AND pm.user_id = ? AND pm.access_level IN ('viewer','editor')
			LEFT JOIN issues i ON i.project_id = p.id
			WHERE p.status = 'active'
			GROUP BY p.id
			ORDER BY p.name`
		args = append(args, user.ID)
	}

	rows, err := db.DB.Query(query, args...)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	projects := []portalProject{}
	for rows.Next() {
		var p portalProject
		if err := rows.Scan(&p.ID, &p.Key, &p.Name, &p.Description, &p.Status,
			&p.LogoPath, &p.IssueCount, &p.DoneCount); err != nil {
			log.Printf("scan error: %v", err)
			continue
		}
		projects = append(projects, p)
	}
	jsonOK(w, projects)
}

// ── GET /api/portal/projects/{id} ────────────────────────────────────────────

func PortalGetProject(w http.ResponseWriter, r *http.Request) {
	id, err := portalProjectID(r)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	if !checkPortalAccess(r, id) {
		jsonError(w, "forbidden", http.StatusForbidden)
		return
	}

	var p portalProject
	err = db.DB.QueryRow(`
		SELECT p.id, p.key, p.name, p.description, p.status,
		       COALESCE(p.logo_path, ''),
		       COUNT(i.id) as issue_count,
		       COUNT(CASE WHEN i.status = 'done' THEN 1 END) as done_count
		FROM projects p
		LEFT JOIN issues i ON i.project_id = p.id
		WHERE p.id = ?
		GROUP BY p.id
	`, id).Scan(&p.ID, &p.Key, &p.Name, &p.Description, &p.Status,
		&p.LogoPath, &p.IssueCount, &p.DoneCount)
	if err != nil {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	jsonOK(w, p)
}

// ── GET /api/portal/projects/{id}/issues ─────────────────────────────────────

func PortalListIssues(w http.ResponseWriter, r *http.Request) {
	projectID, err := portalProjectID(r)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	if !checkPortalAccess(r, projectID) {
		jsonError(w, "forbidden", http.StatusForbidden)
		return
	}

	q := r.URL.Query()
	where := "WHERE i.project_id = ? AND i.deleted_at IS NULL"
	args := []any{projectID}

	if v := q.Get("status"); v != "" {
		where += " AND i.status = ?"
		args = append(args, v)
	}
	if v := q.Get("type"); v != "" {
		where += " AND i.type = ?"
		args = append(args, v)
	}
	if v := q.Get("cost_unit"); v != "" {
		where += " AND i.cost_unit = ?"
		args = append(args, v)
	}
	if fts := strings.TrimSpace(q.Get("q")); len(fts) >= 2 {
		// PAI-283 phase 2: sanitize FTS5 input — skip the filter
		// entirely if no tokenizable content remains.
		if ftsToken, useFTS := sanitizeFTS5Token(fts); useFTS {
			where += ` AND i.id IN (
				SELECT CAST(entity_id AS INTEGER) FROM search_index
				WHERE entity_type IN ('issue','comment') AND search_index MATCH ?
			)`
			args = append(args, ftsToken)
		}
	}

	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT i.id, COALESCE(p.key || '-' || i.issue_number, ''),
		       i.title, i.description, i.acceptance_criteria,
		       i.status, i.priority, i.type,
		       i.cost_unit, i.release,
		       i.estimate_hours, i.estimate_lp, i.ar_hours, i.ar_lp,
		       i.rate_hourly, i.rate_lp,
		       i.accepted_at,
		       i.created_at, i.updated_at
		FROM issues i
		LEFT JOIN projects p ON p.id = i.project_id
		%s
		ORDER BY i.updated_at DESC
	`, where), args...)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	issues := []portalIssue{}
	for rows.Next() {
		var pi portalIssue
		var rateH, rateLP *float64
		if err := rows.Scan(&pi.ID, &pi.IssueKey,
			&pi.Title, &pi.Description, &pi.AcceptanceCriteria,
			&pi.Status, &pi.Priority, &pi.Type,
			&pi.CostUnit, &pi.Release,
			&pi.EstimateHours, &pi.EstimateLp, &pi.ArHours, &pi.ArLp,
			&rateH, &rateLP,
			&pi.AcceptedAt,
			&pi.CreatedAt, &pi.UpdatedAt); err != nil {
			log.Printf("scan error: %v", err)
			continue
		}
		pi.EstimateEur = computeEur(pi.EstimateHours, pi.EstimateLp, rateH, rateLP)
		pi.ArEur = computeEur(pi.ArHours, pi.ArLp, rateH, rateLP)
		issues = append(issues, pi)
	}
	jsonOK(w, issues)
}

// ── GET /api/portal/projects/{id}/issues/{issueId} ──────────────────────────

func PortalGetIssue(w http.ResponseWriter, r *http.Request) {
	projectID, err := portalProjectID(r)
	if err != nil {
		jsonError(w, "invalid project id", http.StatusBadRequest)
		return
	}
	if !checkPortalAccess(r, projectID) {
		jsonError(w, "forbidden", http.StatusForbidden)
		return
	}
	issueID, err := strconv.ParseInt(chi.URLParam(r, "issueId"), 10, 64)
	if err != nil {
		jsonError(w, "invalid issue id", http.StatusBadRequest)
		return
	}

	var pi portalIssue
	var rateH, rateLP *float64
	err = db.DB.QueryRow(`
		SELECT i.id, COALESCE(p.key || '-' || i.issue_number, ''),
		       i.title, i.description, i.acceptance_criteria,
		       i.status, i.priority, i.type,
		       i.cost_unit, i.release,
		       i.estimate_hours, i.estimate_lp, i.ar_hours, i.ar_lp,
		       i.rate_hourly, i.rate_lp,
		       i.accepted_at,
		       i.created_at, i.updated_at
		FROM issues i
		LEFT JOIN projects p ON p.id = i.project_id
		WHERE i.id = ? AND i.project_id = ? AND i.deleted_at IS NULL
	`, issueID, projectID).Scan(&pi.ID, &pi.IssueKey,
		&pi.Title, &pi.Description, &pi.AcceptanceCriteria,
		&pi.Status, &pi.Priority, &pi.Type,
		&pi.CostUnit, &pi.Release,
		&pi.EstimateHours, &pi.EstimateLp, &pi.ArHours, &pi.ArLp,
		&rateH, &rateLP,
		&pi.AcceptedAt,
		&pi.CreatedAt, &pi.UpdatedAt)
	if err != nil {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	pi.EstimateEur = computeEur(pi.EstimateHours, pi.EstimateLp, rateH, rateLP)
	pi.ArEur = computeEur(pi.ArHours, pi.ArLp, rateH, rateLP)
	jsonOK(w, pi)
}

// ── POST /api/portal/projects/{id}/requests ──────────────────────────────────

func PortalSubmitRequest(w http.ResponseWriter, r *http.Request) {
	projectID, err := portalProjectID(r)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	if !checkPortalAccess(r, projectID) {
		jsonError(w, "forbidden", http.StatusForbidden)
		return
	}

	var body struct {
		Title       string `json:"title"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Title == "" {
		jsonError(w, "title required", http.StatusBadRequest)
		return
	}

	// Get next issue_number for this project
	var maxNum int
	if err := db.DB.QueryRow("SELECT COALESCE(MAX(issue_number), 0) FROM issues WHERE project_id=?", projectID).Scan(&maxNum); err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	nextNum := maxNum + 1

	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	res, err := db.DB.Exec(`
		INSERT INTO issues (project_id, issue_number, type, title, description, status, priority, created_at, updated_at, notes)
		VALUES (?, ?, 'ticket', ?, ?, 'new', 'medium', ?, ?, '[customer request]')
	`, projectID, nextNum, body.Title, body.Description, now, now)
	if err != nil {
		jsonError(w, "create failed", http.StatusInternalServerError)
		return
	}

	id, _ := res.LastInsertId()
	// Return the key
	var key string
	if err := db.DB.QueryRow("SELECT COALESCE(p.key || '-' || ?, '') FROM projects p WHERE p.id=?", nextNum, projectID).Scan(&key); err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	jsonOK(w, map[string]any{"id": id, "issue_key": key, "issue_number": nextNum})
}

// ── POST /api/portal/issues/{id}/accept ──────────────────────────────────────

func PortalAcceptIssue(w http.ResponseWriter, r *http.Request) {
	issueID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}

	// Verify issue exists and is done, and user has project access
	var projectID int64
	var status string
	err = db.DB.QueryRow("SELECT project_id, status FROM issues WHERE id=? AND deleted_at IS NULL", issueID).Scan(&projectID, &status)
	if err != nil {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	if !checkPortalEdit(r, projectID) {
		jsonError(w, "forbidden", http.StatusForbidden)
		return
	}
	if status == "accepted" {
		// Already accepted — idempotent no-op
		jsonOK(w, map[string]any{"accepted": true, "status": "accepted"})
		return
	}
	if status != "done" && status != "delivered" {
		jsonError(w, "only done or delivered issues can be accepted", http.StatusUnprocessableEntity)
		return
	}

	user := auth.GetUser(r)
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	if _, err := db.DB.Exec("UPDATE issues SET status='accepted', accepted_at=?, accepted_by=?, updated_at=? WHERE id=?", now, user.ID, now, issueID); err != nil {
		log.Printf("PortalAcceptIssue: id=%d: %v", issueID, err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]any{"accepted": true, "status": "accepted"})
}

// ── POST /api/portal/issues/{id}/reject ───────────────────────────────────────

func PortalRejectIssue(w http.ResponseWriter, r *http.Request) {
	issueID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}

	var body struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		// Legacy field — maps to title if title is empty
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "invalid body", http.StatusBadRequest)
		return
	}
	// Support both new (title+description) and legacy (reason) formats
	title := strings.TrimSpace(body.Title)
	if title == "" {
		title = strings.TrimSpace(body.Reason)
	}
	if title == "" {
		jsonError(w, "title or reason required", http.StatusBadRequest)
		return
	}
	description := strings.TrimSpace(body.Description)
	if description == "" {
		description = title
	}

	// Verify issue exists and is done, and user has project access
	var projectID int64
	var status, priority string
	var assigneeID *int64
	err = db.DB.QueryRow("SELECT project_id, status, priority, assignee_id FROM issues WHERE id=? AND deleted_at IS NULL", issueID).Scan(&projectID, &status, &priority, &assigneeID)
	if err != nil {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	if !checkPortalEdit(r, projectID) {
		jsonError(w, "forbidden", http.StatusForbidden)
		return
	}
	if status != "done" && status != "delivered" {
		jsonError(w, "only done or delivered issues can be rejected", http.StatusUnprocessableEntity)
		return
	}

	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	user := auth.GetUser(r)

	// Create child task describing the rejection
	var nextNum int
	if err := db.DB.QueryRow("SELECT COALESCE(MAX(issue_number),0)+1 FROM issues WHERE project_id=?", projectID).Scan(&nextNum); err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	sqlRes, err := db.DB.Exec(`
		INSERT INTO issues (project_id, issue_number, type, parent_id, title, description,
			status, priority, assignee_id, created_by, created_at, updated_at, notes)
		VALUES (?, ?, 'task', ?, ?, ?, 'backlog', ?, ?, ?, ?, ?, '[portal rejection]')
	`, projectID, nextNum, issueID, title, description, priority, assigneeID, user.ID, now, now)
	if handleDBError(w, err, "issue") {
		return
	}
	childID, _ := sqlRes.LastInsertId()

	// Reopen parent to in-progress, clear accepted_at/accepted_by
	if _, err := db.DB.Exec("UPDATE issues SET status='in-progress', accepted_at=NULL, accepted_by=NULL, updated_at=? WHERE id=?", now, issueID); err != nil {
		log.Printf("PortalRejectIssue: reopen parent id=%d: %v", issueID, err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	jsonOK(w, map[string]any{"rejected": true, "status": "in-progress", "child_id": childID})
}

// ── POST /api/portal/issues/{id}/undo-accept ─────────────────────────────────

func PortalUndoAccept(w http.ResponseWriter, r *http.Request) {
	issueID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}

	var projectID int64
	var status string
	var acceptedAt *string
	err = db.DB.QueryRow("SELECT project_id, status, accepted_at FROM issues WHERE id=? AND deleted_at IS NULL", issueID).Scan(&projectID, &status, &acceptedAt)
	if err != nil {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	if !checkPortalEdit(r, projectID) {
		jsonError(w, "forbidden", http.StatusForbidden)
		return
	}
	if status != "accepted" || acceptedAt == nil {
		jsonError(w, "issue is not accepted", http.StatusUnprocessableEntity)
		return
	}
	// Same-day check
	today := time.Now().UTC().Format("2006-01-02")
	if !strings.HasPrefix(*acceptedAt, today) {
		jsonError(w, "can only undo today's acceptance", http.StatusUnprocessableEntity)
		return
	}

	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	if _, err := db.DB.Exec("UPDATE issues SET status='done', accepted_at=NULL, accepted_by=NULL, updated_at=? WHERE id=?", now, issueID); err != nil {
		log.Printf("PortalUndoAccept: id=%d: %v", issueID, err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]any{"undone": true, "status": "done"})
}

// ── POST /api/portal/issues/{id}/undo-reject ─────────────────────────────────

func PortalUndoReject(w http.ResponseWriter, r *http.Request) {
	issueID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}

	var projectID int64
	var status string
	err = db.DB.QueryRow("SELECT project_id, status FROM issues WHERE id=? AND deleted_at IS NULL", issueID).Scan(&projectID, &status)
	if err != nil {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	if !checkPortalEdit(r, projectID) {
		jsonError(w, "forbidden", http.StatusForbidden)
		return
	}

	// Find today's rejection child task
	today := time.Now().UTC().Format("2006-01-02")
	var childID int64
	err = db.DB.QueryRow(
		"SELECT id FROM issues WHERE parent_id=? AND notes='[portal rejection]' AND created_at LIKE ? LIMIT 1",
		issueID, today+"%",
	).Scan(&childID)
	if err != nil {
		jsonError(w, "no rejection from today found", http.StatusUnprocessableEntity)
		return
	}

	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	// Cancel the child task
	if _, err := db.DB.Exec("UPDATE issues SET status='cancelled', updated_at=? WHERE id=?", now, childID); err != nil {
		log.Printf("PortalUndoReject: cancel child id=%d: %v", childID, err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	// Restore parent to done
	if _, err := db.DB.Exec("UPDATE issues SET status='done', updated_at=? WHERE id=?", now, issueID); err != nil {
		log.Printf("PortalUndoReject: restore parent id=%d: %v", issueID, err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	jsonOK(w, map[string]any{"undone": true, "status": "done"})
}

// ── GET /api/projects/{id}/acceptance-log ─────────────────────────────────────

func AcceptanceLog(w http.ResponseWriter, r *http.Request) {
	projectID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}

	type action struct {
		IssueKey string `json:"issue_key"`
		Action   string `json:"action"`
		TaskKey  string `json:"task_key,omitempty"`
		Title    string `json:"title,omitempty"`
		At       string `json:"at"`
	}
	type group struct {
		Date    string   `json:"date"`
		User    string   `json:"user"`
		Actions []action `json:"actions"`
	}

	// Accepted issues
	acceptRows, err := db.DB.Query(`
		SELECT COALESCE(p.key || '-' || i.issue_number, ''),
		       COALESCE(u.username, ''), i.accepted_at
		FROM issues i
		LEFT JOIN projects p ON p.id = i.project_id
		LEFT JOIN users u ON u.id = i.accepted_by
		WHERE i.project_id = ? AND i.accepted_at IS NOT NULL AND i.deleted_at IS NULL
		ORDER BY i.accepted_at DESC
	`, projectID)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer acceptRows.Close()

	groupMap := map[string]*group{} // key: date|user
	var groupOrder []string

	for acceptRows.Next() {
		var issueKey, username, acceptedAt string
		if err := acceptRows.Scan(&issueKey, &username, &acceptedAt); err != nil {
			log.Printf("scan error: %v", err)
			continue
		}
		date := acceptedAt[:10]
		gk := date + "|" + username
		g, ok := groupMap[gk]
		if !ok {
			g = &group{Date: date, User: username}
			groupMap[gk] = g
			groupOrder = append(groupOrder, gk)
		}
		g.Actions = append(g.Actions, action{IssueKey: issueKey, Action: "accepted", At: acceptedAt})
	}

	// Rejected issues (child tasks with [portal rejection] notes)
	rejectRows, err := db.DB.Query(`
		SELECT COALESCE(pp.key || '-' || parent.issue_number, ''),
		       COALESCE(pp.key || '-' || i.issue_number, ''),
		       i.title, COALESCE(u.username, ''), i.created_at
		FROM issues i
		JOIN issues parent ON parent.id = i.parent_id
		LEFT JOIN projects pp ON pp.id = i.project_id
		LEFT JOIN users u ON u.id = i.created_by
		WHERE i.project_id = ? AND i.notes = '[portal rejection]' AND i.deleted_at IS NULL
		ORDER BY i.created_at DESC
	`, projectID)
	if err == nil {
		defer rejectRows.Close()
		for rejectRows.Next() {
			var parentKey, taskKey, title, username, createdAt string
			if err := rejectRows.Scan(&parentKey, &taskKey, &title, &username, &createdAt); err != nil {
				log.Printf("scan error: %v", err)
				continue
			}
			date := createdAt[:10]
			gk := date + "|" + username
			g, ok := groupMap[gk]
			if !ok {
				g = &group{Date: date, User: username}
				groupMap[gk] = g
				groupOrder = append(groupOrder, gk)
			}
			g.Actions = append(g.Actions, action{IssueKey: parentKey, Action: "rejected", TaskKey: taskKey, Title: title, At: createdAt})
		}
	}

	result := []group{}
	for _, gk := range groupOrder {
		result = append(result, *groupMap[gk])
	}
	if result == nil {
		result = []group{}
	}
	jsonOK(w, result)
}

// ── GET /api/portal/projects/{id}/summary ────────────────────────────────────

func PortalProjectSummary(w http.ResponseWriter, r *http.Request) {
	projectID, err := portalProjectID(r)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	if !checkPortalAccess(r, projectID) {
		jsonError(w, "forbidden", http.StatusForbidden)
		return
	}

	rows, err := db.DB.Query(`
		SELECT status, COUNT(*),
		       SUM(COALESCE(estimate_hours, 0) * COALESCE(rate_hourly, 0) + COALESCE(estimate_lp, 0) * COALESCE(rate_lp, 0)),
		       SUM(COALESCE(ar_hours, 0) * COALESCE(rate_hourly, 0) + COALESCE(ar_lp, 0) * COALESCE(rate_lp, 0))
		FROM issues
		WHERE project_id = ? AND deleted_at IS NULL
		GROUP BY status
	`, projectID)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	summary := portalSummary{ByStatus: map[string]int{}}
	var totalEst, totalAr float64
	for rows.Next() {
		var st string
		var cnt int
		var estSum, arSum float64
		if err := rows.Scan(&st, &cnt, &estSum, &arSum); err != nil {
			log.Printf("scan error: %v", err)
			continue
		}
		summary.ByStatus[st] = cnt
		summary.TotalIssues += cnt
		totalEst += estSum
		totalAr += arSum
	}
	if totalEst > 0 {
		summary.TotalEstEur = &totalEst
	}
	if totalAr > 0 {
		summary.TotalArEur = &totalAr
	}
	jsonOK(w, summary)
}
