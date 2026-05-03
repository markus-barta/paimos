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
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/markus-barta/paimos/backend/db"
	"github.com/markus-barta/paimos/backend/models"
)

func ListIssues(w http.ResponseWriter, r *http.Request) {
	projectID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid project id", http.StatusBadRequest)
		return
	}

	whereSQL := `i.project_id = ? AND ` + liveIssuesWhere
	args := []any{projectID}

	whereSQL, args = applyIssueFilters(whereSQL, args, r.URL.Query())

	if fts := strings.TrimSpace(r.URL.Query().Get("q")); len(fts) >= 2 {
		likePattern := "%" + fts + "%"
		// PAI-283 phase 2: sanitize FTS5 input to avoid parser crashes
		// from special characters (e.g. `doc/` → `fts5: syntax error
		// near "/"`). When the input has no tokenizable content, drop
		// the FTS5 branch and rely on the LIKE fallback alone.
		if ftsToken, useFTS := sanitizeFTS5Token(fts); useFTS {
			whereSQL += ` AND i.id IN (
				SELECT CAST(entity_id AS INTEGER) FROM search_index
				WHERE entity_type IN ('issue','comment') AND search_index MATCH ?
				UNION
				SELECT id FROM issues WHERE project_id = ? AND deleted_at IS NULL AND (
					title LIKE ? OR description LIKE ? OR acceptance_criteria LIKE ? OR notes LIKE ?
					OR (SELECT key FROM projects WHERE id = issues.project_id) || '-' || issue_number LIKE ?
				)
			)`
			args = append(args, ftsToken, projectID, likePattern, likePattern, likePattern, likePattern, likePattern)
		} else {
			whereSQL += ` AND i.id IN (
				SELECT id FROM issues WHERE project_id = ? AND deleted_at IS NULL AND (
					title LIKE ? OR description LIKE ? OR acceptance_criteria LIKE ? OR notes LIKE ?
					OR (SELECT key FROM projects WHERE id = issues.project_id) || '-' || issue_number LIKE ?
				)
			)`
			args = append(args, projectID, likePattern, likePattern, likePattern, likePattern, likePattern)
		}
	}

	if handled, err := applyIssueListConditionalGET(w, r, whereSQL, args); err != nil {
		jsonError(w, "etag computation failed", http.StatusInternalServerError)
		return
	} else if handled {
		return
	}

	query := issueSelectCore + ` WHERE ` + whereSQL

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

	whereSQL := `i.project_id = ? AND ` + liveIssuesWhere
	args := []any{projectID}
	if handled, err := applyIssueListConditionalGET(w, r, whereSQL, args); err != nil {
		jsonError(w, "etag computation failed", http.StatusInternalServerError)
		return
	} else if handled {
		return
	}

	rows, err := db.DB.Query(issueSelectCore+` WHERE `+whereSQL+` ORDER BY i.issue_number ASC`, args...)
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

func GetIssueChildren(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	rows, err := db.DB.Query(issueSelectCore+` WHERE i.parent_id=? AND `+liveIssuesWhere+` ORDER BY i.issue_number ASC`, id)
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

// ListTrashIssues returns every soft-deleted issue the caller has view access
// to, ordered by deleted_at DESC. Admin-gated at the route level.
func ListTrashIssues(w http.ResponseWriter, r *http.Request) {
	query := issueSelectCore + ` WHERE i.deleted_at IS NOT NULL`
	args := []any{}
	if accessFilter, accessArgs := projectIDFilter(r, "i.project_id", true); accessFilter != "" {
		query += accessFilter
		args = append(args, accessArgs...)
	}
	query += ` ORDER BY i.deleted_at DESC, i.id DESC`
	rows, err := db.DB.Query(query, args...)
	if err != nil {
		jsonError(w, "list failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	issues := []models.Issue{}
	for rows.Next() {
		iss, err := scanIssue(rows)
		if err != nil {
			jsonError(w, "scan failed", http.StatusInternalServerError)
			return
		}
		issues = append(issues, *iss)
	}
	jsonOK(w, issues)
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

	whereSQL := liveIssuesWhere
	args := []any{}

	// Scope by per-project access (admins pass through with empty filter).
	// Orphan issues (NULL project_id — sprints) are cross-project and
	// remain visible to every authenticated internal user.
	if accessFilter, accessArgs := projectIDFilter(r, "i.project_id", true); accessFilter != "" {
		whereSQL += accessFilter
		args = append(args, accessArgs...)
	}

	// Apply shared filters (status, priority, type, assignee, cost_unit, release, tags, sprints)
	whereSQL, args = applyIssueFilters(whereSQL, args, q)

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
			whereSQL += " AND (i.project_id IN (" + placeholders + ") OR i.project_id IS NULL)"
		} else if placeholders != "" {
			whereSQL += " AND i.project_id IN (" + placeholders + ")"
		} else if wantNull {
			whereSQL += " AND i.project_id IS NULL"
		}
	}

	if fts := strings.TrimSpace(q.Get("q")); len(fts) >= 2 {
		likePattern := "%" + fts + "%"
		// PAI-283 phase 2: sanitize FTS5 input — see sanitizeFTS5Token
		// for the rationale. When input has no tokenizable content,
		// drop the FTS5 branch and rely on the LIKE fallback alone.
		if ftsToken, useFTS := sanitizeFTS5Token(fts); useFTS {
			whereSQL += ` AND i.id IN (
				SELECT CAST(entity_id AS INTEGER) FROM search_index
				WHERE entity_type IN ('issue','comment') AND search_index MATCH ?
				UNION
				SELECT id FROM issues WHERE deleted_at IS NULL AND (
					title LIKE ? OR description LIKE ? OR acceptance_criteria LIKE ? OR notes LIKE ?
				)
			)`
			args = append(args, ftsToken, likePattern, likePattern, likePattern, likePattern)
		} else {
			whereSQL += ` AND i.id IN (
				SELECT id FROM issues WHERE deleted_at IS NULL AND (
					title LIKE ? OR description LIKE ? OR acceptance_criteria LIKE ? OR notes LIKE ?
				)
			)`
			args = append(args, likePattern, likePattern, likePattern, likePattern)
		}
	}

	if handled, err := applyIssueListConditionalGET(w, r, whereSQL, args); err != nil {
		jsonError(w, "etag computation failed", http.StatusInternalServerError)
		return
	} else if handled {
		return
	}

	query := issueSelectCore + ` WHERE ` + whereSQL
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
	countQuery := `SELECT COUNT(*) FROM issues i WHERE ` + liveIssuesWhere
	countArgs := []any{}
	if accessFilter, accessArgs := projectIDFilter(r, "i.project_id", true); accessFilter != "" {
		countQuery += accessFilter
		countArgs = append(countArgs, accessArgs...)
	}
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
		// PAI-283 phase 2: mirror the data-query's FTS+LIKE union here
		// so the count agrees with the rendered list. The pre-existing
		// version applied only the FTS branch, which under-counted
		// LIKE-only matches — masked previously, surfaced now that
		// FTS-incompatible queries fall through to the LIKE branch.
		likePattern := "%" + fts + "%"
		if ftsToken, useFTS := sanitizeFTS5Token(fts); useFTS {
			countQuery += ` AND i.id IN (
				SELECT CAST(entity_id AS INTEGER) FROM search_index
				WHERE entity_type IN ('issue','comment') AND search_index MATCH ?
				UNION
				SELECT id FROM issues WHERE deleted_at IS NULL AND (
					title LIKE ? OR description LIKE ? OR acceptance_criteria LIKE ? OR notes LIKE ?
				)
			)`
			countArgs = append(countArgs, ftsToken, likePattern, likePattern, likePattern, likePattern)
		} else {
			countQuery += ` AND i.id IN (
				SELECT id FROM issues WHERE deleted_at IS NULL AND (
					title LIKE ? OR description LIKE ? OR acceptance_criteria LIKE ? OR notes LIKE ?
				)
			)`
			countArgs = append(countArgs, likePattern, likePattern, likePattern, likePattern)
		}
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

func RecentIssues(w http.ResponseWriter, r *http.Request) {
	whereSQL := liveIssuesWhere
	args := []any{}
	if accessFilter, accessArgs := projectIDFilter(r, "i.project_id", true); accessFilter != "" {
		whereSQL += accessFilter
		args = append(args, accessArgs...)
	}
	if handled, err := applyIssueListConditionalGET(w, r, whereSQL, args); err != nil {
		jsonError(w, "etag computation failed", http.StatusInternalServerError)
		return
	} else if handled {
		return
	}
	query := issueSelectCore + ` WHERE ` + whereSQL
	query += ` ORDER BY i.updated_at DESC LIMIT 20`
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
		`SELECT DISTINCT cost_unit FROM issues WHERE project_id=? AND cost_unit != '' AND deleted_at IS NULL ORDER BY cost_unit`, projectID,
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
		`SELECT DISTINCT release FROM issues WHERE project_id=? AND release != '' AND deleted_at IS NULL ORDER BY release`, projectID,
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
	query := `SELECT DISTINCT cost_unit FROM issues i WHERE cost_unit != '' AND i.deleted_at IS NULL`
	args := []any{}
	if f, a := projectIDFilter(r, "i.project_id", true); f != "" {
		query += f
		args = append(args, a...)
	}
	query += ` ORDER BY cost_unit`
	rows, err := db.DB.Query(query, args...)
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
	query := `SELECT DISTINCT release FROM issues i WHERE release != '' AND i.deleted_at IS NULL`
	args := []any{}
	if f, a := projectIDFilter(r, "i.project_id", true); f != "" {
		query += f
		args = append(args, a...)
	}
	query += ` ORDER BY release`
	rows, err := db.DB.Query(query, args...)
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
