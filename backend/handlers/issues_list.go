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

type issueListEnvelope struct {
	Issues   []models.Issue `json:"issues"`
	Total    int            `json:"total"`
	Offset   int            `json:"offset"`
	Limit    int            `json:"limit"`
	Sort     string         `json:"sort,omitempty"`
	Order    string         `json:"order,omitempty"`
	Query    string         `json:"query,omitempty"`
	Revision string         `json:"revision,omitempty"`
}

func issueListRevision(w http.ResponseWriter) string {
	return strings.Trim(w.Header().Get("ETag"), `"`)
}

func parseIssueListWindow(r *http.Request, defaultLimit int) (limit int, offset int) {
	limit = defaultLimit
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
	return limit, offset
}

func issueListSortOrder(r *http.Request, defaultOrder string, searchTerm string) (clause string, args []any, sortKey string, order string, err error) {
	q := r.URL.Query()
	sortKey = strings.ToLower(strings.TrimSpace(q.Get("sort")))
	order = strings.ToLower(strings.TrimSpace(q.Get("order")))
	if order == "" {
		order = "asc"
	}
	if order != "asc" && order != "desc" {
		return "", nil, "", "", fmt.Errorf("order must be asc or desc")
	}
	dir := strings.ToUpper(order)

	if sortKey == "" {
		if len(strings.TrimSpace(searchTerm)) >= 2 {
			orderSQL, orderArgs := issueSearchRankOrder(searchTerm)
			return orderSQL, orderArgs, "", "", nil
		}
		return defaultOrder, nil, "", "", nil
	}

	tieBreaker := ", i.id " + dir
	switch sortKey {
	case "key":
		return " ORDER BY UPPER(COALESCE(p.key,'')) " + dir + ", i.issue_number " + dir + tieBreaker, nil, sortKey, order, nil
	case "type":
		return " ORDER BY CASE i.type WHEN 'epic' THEN 0 WHEN 'cost_unit' THEN 1 WHEN 'release' THEN 2 WHEN 'sprint' THEN 3 WHEN 'ticket' THEN 4 WHEN 'task' THEN 5 ELSE 6 END " + dir + tieBreaker, nil, sortKey, order, nil
	case "title":
		return " ORDER BY LOWER(COALESCE(i.title,'')) " + dir + tieBreaker, nil, sortKey, order, nil
	case "status":
		return " ORDER BY CASE i.status WHEN 'new' THEN 0 WHEN 'backlog' THEN 1 WHEN 'in-progress' THEN 2 WHEN 'qa' THEN 3 WHEN 'done' THEN 4 WHEN 'delivered' THEN 5 WHEN 'accepted' THEN 6 WHEN 'invoiced' THEN 7 WHEN 'cancelled' THEN 8 ELSE 9 END " + dir + tieBreaker, nil, sortKey, order, nil
	case "priority":
		return " ORDER BY CASE i.priority WHEN 'high' THEN 0 WHEN 'medium' THEN 1 WHEN 'low' THEN 2 ELSE 3 END " + dir + tieBreaker, nil, sortKey, order, nil
	case "cost_unit":
		return " ORDER BY LOWER(COALESCE(i.cost_unit,'')) " + dir + tieBreaker, nil, sortKey, order, nil
	case "release":
		return " ORDER BY LOWER(COALESCE(i.release,'')) " + dir + tieBreaker, nil, sortKey, order, nil
	case "assignee":
		return " ORDER BY LOWER(COALESCE(u.username,'')) " + dir + tieBreaker, nil, sortKey, order, nil
	case "billing_type":
		return " ORDER BY LOWER(COALESCE(i.billing_type,'')) " + dir + tieBreaker, nil, sortKey, order, nil
	case "total_budget":
		return " ORDER BY COALESCE(i.total_budget, 0) " + dir + tieBreaker, nil, sortKey, order, nil
	case "rate_hourly":
		return " ORDER BY COALESCE(i.rate_hourly, 0) " + dir + tieBreaker, nil, sortKey, order, nil
	case "rate_lp":
		return " ORDER BY COALESCE(i.rate_lp, 0) " + dir + tieBreaker, nil, sortKey, order, nil
	case "estimate_hours":
		return " ORDER BY COALESCE(i.estimate_hours, 0) " + dir + tieBreaker, nil, sortKey, order, nil
	case "estimate_lp":
		return " ORDER BY COALESCE(i.estimate_lp, 0) " + dir + tieBreaker, nil, sortKey, order, nil
	case "ar_hours":
		return " ORDER BY COALESCE(i.ar_hours, 0) " + dir + tieBreaker, nil, sortKey, order, nil
	case "ar_lp":
		return " ORDER BY COALESCE(i.ar_lp, 0) " + dir + tieBreaker, nil, sortKey, order, nil
	case "start_date":
		return " ORDER BY COALESCE(i.start_date, '') " + dir + tieBreaker, nil, sortKey, order, nil
	case "end_date":
		return " ORDER BY COALESCE(i.end_date, '') " + dir + tieBreaker, nil, sortKey, order, nil
	case "group_state":
		return " ORDER BY LOWER(COALESCE(i.group_state,'')) " + dir + tieBreaker, nil, sortKey, order, nil
	case "sprint_state":
		return " ORDER BY LOWER(COALESCE(i.sprint_state,'')) " + dir + tieBreaker, nil, sortKey, order, nil
	case "jira_id":
		return " ORDER BY LOWER(COALESCE(i.jira_id,'')) " + dir + tieBreaker, nil, sortKey, order, nil
	case "jira_version":
		return " ORDER BY LOWER(COALESCE(i.jira_version,'')) " + dir + tieBreaker, nil, sortKey, order, nil
	case "report_summary":
		return " ORDER BY LOWER(COALESCE(i.report_summary,'')) " + dir + tieBreaker, nil, sortKey, order, nil
	case "created_at":
		return " ORDER BY i.created_at " + dir + tieBreaker, nil, sortKey, order, nil
	case "updated_at":
		return " ORDER BY i.updated_at " + dir + tieBreaker, nil, sortKey, order, nil
	case "accepted_at":
		return " ORDER BY COALESCE(i.accepted_at, '') " + dir + tieBreaker, nil, sortKey, order, nil
	case "invoiced_at":
		return " ORDER BY COALESCE(i.invoiced_at, '') " + dir + tieBreaker, nil, sortKey, order, nil
	default:
		return "", nil, "", "", fmt.Errorf("sort column not allowed: %s", sortKey)
	}
}

func countMatchingIssues(whereSQL string, args []any) (int, error) {
	var total int
	query := `SELECT COUNT(*) FROM issues i LEFT JOIN users u ON u.id = i.assignee_id LEFT JOIN projects p ON p.id = i.project_id WHERE ` + whereSQL
	// #nosec G701 -- whereSQL is composed from fixed fragments; user values are placeholders.
	err := db.DB.QueryRow(query, args...).Scan(&total)
	return total, err
}

func stripIssueListFields(issues []models.Issue) {
	for idx := range issues {
		issues[idx].Description = ""
		issues[idx].AcceptanceCriteria = ""
		issues[idx].Notes = ""
		issues[idx].JiraText = nil
	}
}

func writeIssueIDsOnly(w http.ResponseWriter, whereSQL string, args []any) {
	const idsOnlyCap = MaxBatchSize * 50
	total, err := countMatchingIssues(whereSQL, args)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	idsQuery := `SELECT i.id FROM issues i LEFT JOIN users u ON u.id = i.assignee_id LEFT JOIN projects p ON p.id = i.project_id WHERE ` + whereSQL + ` ORDER BY i.id LIMIT ?`
	idsArgs := append(append([]any{}, args...), idsOnlyCap+1)
	// #nosec G701 -- idsQuery is composed from fixed fragments; user values are placeholders.
	idRows, idErr := db.DB.Query(idsQuery, idsArgs...)
	if idErr != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer idRows.Close()
	ids := make([]int64, 0, 64)
	for idRows.Next() {
		var id int64
		if err := idRows.Scan(&id); err != nil {
			jsonError(w, "scan failed", http.StatusInternalServerError)
			return
		}
		ids = append(ids, id)
	}
	truncated := false
	if len(ids) > idsOnlyCap {
		ids = ids[:idsOnlyCap]
		truncated = true
	}
	jsonOK(w, map[string]any{
		"ids":       ids,
		"total":     total,
		"truncated": truncated,
		"cap":       idsOnlyCap,
	})
}

func applyProjectIDsFilter(whereSQL string, args []any, raw string) (string, []any) {
	if raw == "" {
		return whereSQL, args
	}
	var posIDs, negIDs []string
	posNull := false
	negNull := false
	for _, rawPart := range splitCSV(raw) {
		negated := strings.HasPrefix(rawPart, "!")
		p := strings.TrimPrefix(rawPart, "!")
		if p == "none" {
			if negated {
				negNull = true
			} else {
				posNull = true
			}
			continue
		}
		if _, err := strconv.ParseInt(p, 10, 64); err != nil {
			continue
		}
		if negated {
			negIDs = append(negIDs, p)
		} else {
			posIDs = append(posIDs, p)
		}
	}
	if len(posIDs) > 0 && posNull {
		ph := buildPlaceholders(len(posIDs))
		whereSQL += " AND (i.project_id IN (" + ph + ") OR i.project_id IS NULL)"
		for _, id := range posIDs {
			args = append(args, id)
		}
	} else if len(posIDs) > 0 {
		ph := buildPlaceholders(len(posIDs))
		whereSQL += " AND i.project_id IN (" + ph + ")"
		for _, id := range posIDs {
			args = append(args, id)
		}
	} else if posNull {
		whereSQL += " AND i.project_id IS NULL"
	}
	if len(negIDs) > 0 {
		ph := buildPlaceholders(len(negIDs))
		whereSQL += " AND (i.project_id IS NULL OR i.project_id NOT IN (" + ph + "))"
		for _, id := range negIDs {
			args = append(args, id)
		}
	}
	if negNull {
		whereSQL += " AND i.project_id IS NOT NULL"
	}
	return whereSQL, args
}

func ListIssues(w http.ResponseWriter, r *http.Request) {
	projectID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid project id", http.StatusBadRequest)
		return
	}

	whereSQL := `i.project_id = ? AND ` + liveIssuesWhere
	args := []any{projectID}

	whereSQL, args = applyIssueFilters(whereSQL, args, r.URL.Query())

	searchTerm := strings.TrimSpace(r.URL.Query().Get("q"))
	whereSQL, args = appendGlobalIssueSearchFilter(whereSQL, args, searchTerm)

	if handled, err := applyIssueListConditionalGET(w, r, whereSQL, args); err != nil {
		jsonError(w, "etag computation failed", http.StatusInternalServerError)
		return
	} else if handled {
		return
	}

	if r.URL.Query().Get("ids_only") == "1" {
		writeIssueIDsOnly(w, whereSQL, args)
		return
	}

	query := issueSelectCore + ` WHERE ` + whereSQL

	orderBy, orderArgs, sortKey, order, err := issueListSortOrder(r, " ORDER BY i.type DESC, i.issue_number ASC", searchTerm)
	if err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}
	query += orderBy
	args = append(args, orderArgs...)

	limit, offset := parseIssueListWindow(r, 0)
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
		stripIssueListFields(issues)
	}

	if r.URL.Query().Get("envelope") == "1" {
		total, err := countMatchingIssues(whereSQL, args[:len(args)-len(orderArgs)])
		if err != nil {
			jsonError(w, "internal error", http.StatusInternalServerError)
			return
		}
		jsonOK(w, issueListEnvelope{
			Issues:   issues,
			Total:    total,
			Offset:   offset,
			Limit:    limit,
			Sort:     sortKey,
			Order:    order,
			Query:    searchTerm,
			Revision: issueListRevision(w),
		})
		return
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
	limit, offset := parseIssueListWindow(r, 100)

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

	whereSQL, args = applyProjectIDsFilter(whereSQL, args, q.Get("project_ids"))

	searchTerm := strings.TrimSpace(q.Get("q"))
	whereSQL, args = appendGlobalIssueSearchFilter(whereSQL, args, searchTerm)

	if handled, err := applyIssueListConditionalGET(w, r, whereSQL, args); err != nil {
		jsonError(w, "etag computation failed", http.StatusInternalServerError)
		return
	} else if handled {
		return
	}

	if q.Get("ids_only") == "1" {
		writeIssueIDsOnly(w, whereSQL, args)
		return
	}

	query := issueSelectCore + ` WHERE ` + whereSQL
	orderBy, orderArgs, sortKey, order, err := issueListSortOrder(r, " ORDER BY i.updated_at DESC, i.id DESC", searchTerm)
	if err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}
	query += orderBy
	countArgs := append([]any{}, args...)
	args = append(args, orderArgs...)
	query += " LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	// #nosec G701 -- query is assembled from fixed SQL fragments; user values are placeholders.
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
		stripIssueListFields(issues)
	}

	total, err := countMatchingIssues(whereSQL, countArgs)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	jsonOK(w, issueListEnvelope{
		Issues:   issues,
		Total:    total,
		Offset:   offset,
		Limit:    limit,
		Sort:     sortKey,
		Order:    order,
		Query:    searchTerm,
		Revision: issueListRevision(w),
	})
}

// appendGlobalIssueSearchFilter documents and applies the match set for
// GET /api/issues?q=. The endpoint searches issue FTS rows, comment FTS rows,
// LIKE fallbacks for issue body fields, and computed issue keys. The companion
// issueSearchRankOrder below defines the visible order for the same result set.
func appendGlobalIssueSearchFilter(whereSQL string, args []any, raw string) (string, []any) {
	fts := strings.TrimSpace(raw)
	if len(fts) < 2 {
		return whereSQL, args
	}

	likePattern := "%" + fts + "%"
	keyOrBodyMatchSQL := `
				SELECT ii.id
				FROM issues ii
				LEFT JOIN projects pp ON pp.id = ii.project_id
				WHERE ii.deleted_at IS NULL AND (
					ii.title LIKE ? OR ii.description LIKE ? OR ii.acceptance_criteria LIKE ? OR ii.notes LIKE ?
					OR ii.report_summary LIKE ?
					OR (COALESCE(pp.key,'') || '-' || CAST(ii.issue_number AS TEXT)) LIKE ?
				)`

	// PAI-283 phase 2: sanitize FTS5 input — see sanitizeFTS5Token for the
	// rationale. When input has no tokenizable content, drop the FTS5 branch
	// and rely on the LIKE fallback alone.
	if ftsToken, useFTS := sanitizeFTS5Token(fts); useFTS {
		whereSQL += ` AND i.id IN (
				SELECT CAST(entity_id AS INTEGER) FROM search_index
				WHERE entity_type IN ('issue','comment') AND search_index MATCH ?
				UNION` + keyOrBodyMatchSQL + `
			)`
		args = append(args, ftsToken, likePattern, likePattern, likePattern, likePattern, likePattern, likePattern)
		return whereSQL, args
	}

	whereSQL += ` AND i.id IN (` + keyOrBodyMatchSQL + `
			)`
	args = append(args, likePattern, likePattern, likePattern, likePattern, likePattern, likePattern)
	return whereSQL, args
}

// issueSearchRankOrder is the explicit "best matches first" contract for
// GET /api/issues?q=. Match quality is primary; recency is only the tie-breaker
// inside comparable buckets:
//  1. exact issue key
//  2. issue key prefix
//  3. exact title
//  4. title prefix
//  5. title substring
//  6. issue body fields: description, acceptance criteria, notes
//  7. comment body
//  8. other FTS-only issue fields
func issueSearchRankOrder(raw string) (string, []any) {
	q := strings.TrimSpace(raw)
	contains := "%" + q + "%"
	key := strings.ToUpper(q)
	keyPrefix := key + "%"
	titlePrefix := q + "%"

	return ` ORDER BY
		CASE
			WHEN UPPER(COALESCE(p.key,'') || '-' || CAST(i.issue_number AS TEXT)) = ? THEN 0
			WHEN UPPER(COALESCE(p.key,'') || '-' || CAST(i.issue_number AS TEXT)) LIKE ? THEN 10
			WHEN LOWER(COALESCE(i.title,'')) = LOWER(?) THEN 20
			WHEN LOWER(COALESCE(i.title,'')) LIKE LOWER(?) THEN 30
			WHEN LOWER(COALESCE(i.title,'')) LIKE LOWER(?) THEN 40
			WHEN COALESCE(i.description,'') LIKE ?
			  OR COALESCE(i.acceptance_criteria,'') LIKE ?
			  OR COALESCE(i.notes,'') LIKE ?
			  OR COALESCE(i.report_summary,'') LIKE ? THEN 50
			WHEN EXISTS (
				SELECT 1 FROM comments c
				WHERE c.issue_id = i.id AND c.body LIKE ?
			) THEN 60
			ELSE 70
		END,
		i.updated_at DESC,
		i.id DESC`, []any{
			key,
			keyPrefix,
			q,
			titlePrefix,
			contains, // title substring
			contains, // description
			contains, // acceptance_criteria
			contains, // notes
			contains, // report_summary
			contains, // comment body
		}
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
	// #nosec G701 -- query is assembled from fixed SQL fragments; user values are placeholders.
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
		`SELECT DISTINCT label
		   FROM (
		         SELECT title AS label
		           FROM issues
		          WHERE project_id=? AND type='cost_unit' AND title != '' AND deleted_at IS NULL
		         UNION
		         SELECT cost_unit AS label
		           FROM issues
		          WHERE project_id=? AND cost_unit != '' AND deleted_at IS NULL
		        )
		  ORDER BY label COLLATE NOCASE`, projectID, projectID,
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
		`SELECT DISTINCT label
		   FROM (
		         SELECT title AS label
		           FROM issues
		          WHERE project_id=? AND type='release' AND title != '' AND deleted_at IS NULL
		         UNION
		         SELECT release AS label
		           FROM issues
		          WHERE project_id=? AND release != '' AND deleted_at IS NULL
		        )
		  ORDER BY label COLLATE NOCASE`, projectID, projectID,
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
	query := `WITH allowed_issues AS (
		SELECT i.title, i.type, i.cost_unit
		  FROM issues i
		 WHERE i.deleted_at IS NULL`
	args := []any{}
	if f, a := projectIDFilter(r, "i.project_id", true); f != "" {
		query += f // #nosec G202 -- projectIDFilter returns a fixed SQL fragment plus placeholder args.
		args = append(args, a...)
	}
	query += `
	)
	SELECT DISTINCT label
	  FROM (
	        SELECT title AS label FROM allowed_issues WHERE type='cost_unit' AND title != ''
	        UNION
	        SELECT cost_unit AS label FROM allowed_issues WHERE cost_unit != ''
	       )
	 ORDER BY label COLLATE NOCASE`
	// #nosec G701 -- query uses fixed fragments only; access values are placeholders.
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
	query := `WITH allowed_issues AS (
		SELECT i.title, i.type, i.release
		  FROM issues i
		 WHERE i.deleted_at IS NULL`
	args := []any{}
	if f, a := projectIDFilter(r, "i.project_id", true); f != "" {
		query += f // #nosec G202 -- projectIDFilter returns a fixed SQL fragment plus placeholder args.
		args = append(args, a...)
	}
	query += `
	)
	SELECT DISTINCT label
	  FROM (
	        SELECT title AS label FROM allowed_issues WHERE type='release' AND title != ''
	        UNION
	        SELECT release AS label FROM allowed_issues WHERE release != ''
	       )
	 ORDER BY label COLLATE NOCASE`
	// #nosec G701 -- query uses fixed fragments only; access values are placeholders.
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
