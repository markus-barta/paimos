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
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/markus-barta/paimos/backend/db"
)

// CSV column order (stable — never reorder, only append).
// depends_on and impacts were removed from the issues table in migration 32
// (data moved to issue_relations); columns kept in header for import backward-compat
// but are now written as empty strings in exports.
var csvHeaders = []string{
	"issue_key", "type", "parent_key",
	"title", "description", "acceptance_criteria", "notes",
	"status", "priority", "cost_unit", "release",
	"depends_on", "impacts", "assignee", "tags",
	"logged", "rollup", "override", "total",
}

// ── Export ────────────────────────────────────────────────────────────────────

// GET /api/projects/:id/export/csv
func ExportCSV(w http.ResponseWriter, r *http.Request) {
	projectID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid project id", http.StatusBadRequest)
		return
	}

	var projKey string
	if err := db.DB.QueryRow("SELECT key FROM projects WHERE id=?", projectID).Scan(&projKey); err != nil {
		jsonError(w, "project not found", http.StatusNotFound)
		return
	}

	idsParam := r.URL.Query().Get("ids")
	log.Printf("export/csv: project=%s ids=%q", projKey, idsParam)

	// Optional ?ids=1,2,3 filter — only export selected issues
	query := `
		SELECT
			i.id, i.issue_number, i.type, i.parent_id, p.issue_number,
			i.title, i.description, i.acceptance_criteria, i.notes,
			i.status, i.priority, i.cost_unit, i.release,
			COALESCE(u.username, ''),
			proj.key,
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
			i.time_override
		FROM issues i
		LEFT JOIN issues p   ON p.id = i.parent_id
		LEFT JOIN users u    ON u.id = i.assignee_id
		LEFT JOIN projects proj ON proj.id = i.project_id
		WHERE i.project_id = ? AND i.deleted_at IS NULL`
	args := []any{projectID}

	if idsParam := r.URL.Query().Get("ids"); idsParam != "" {
		var placeholders []string
		for _, part := range strings.Split(idsParam, ",") {
			part = strings.TrimSpace(part)
			if id, err := strconv.ParseInt(part, 10, 64); err == nil {
				placeholders = append(placeholders, "?")
				args = append(args, id)
			}
		}
		if len(placeholders) > 0 {
			query += " AND i.id IN (" + strings.Join(placeholders, ",") + ")"
		}
	}

	query += `
		ORDER BY
			CASE i.type WHEN 'epic' THEN 1 WHEN 'ticket' THEN 2 ELSE 3 END,
			i.issue_number`

	// Scan all issue rows into memory first — with SetMaxOpenConns(1) we cannot
	// hold an open cursor (rows) and open additional queries concurrently; doing
	// so causes a deadlock that hangs the request indefinitely.
	type issueRow struct {
		id           int64
		issueNum     *int
		parentID     *int64
		parentNum    *int
		typ          string
		title        string
		desc         string
		ac           string
		notes        string
		status       string
		priority     string
		costUnit     string
		release      string
		assignee     string
		projKeyVal   string
		logged       float64
		timeOverride *float64
	}

	rows, err := db.DB.Query(query, args...)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	var issueRows []issueRow
	for rows.Next() {
		var r issueRow
		if err := rows.Scan(
			&r.id, &r.issueNum, &r.typ, &r.parentID, &r.parentNum,
			&r.title, &r.desc, &r.ac, &r.notes,
			&r.status, &r.priority, &r.costUnit, &r.release,
			&r.assignee, &r.projKeyVal,
			&r.logged, &r.timeOverride,
		); err != nil {
			continue
		}
		issueRows = append(issueRows, r)
	}
	rows.Close() // release the connection before running follow-up queries

	// Compute rollup + total for each issue
	idxByID := map[int64]int{}
	childrenOf := map[int64][]int64{}
	for idx, r := range issueRows {
		idxByID[r.id] = idx
		if r.parentID != nil {
			childrenOf[*r.parentID] = append(childrenOf[*r.parentID], r.id)
		}
	}
	rollups := map[int64]float64{}
	totals := map[int64]float64{}
	var getTotal func(id int64) float64
	getTotal = func(id int64) float64 {
		if t, ok := totals[id]; ok {
			return t
		}
		idx, ok := idxByID[id]
		if !ok {
			return 0
		}
		r := issueRows[idx]
		var ru float64
		for _, cid := range childrenOf[id] {
			ru += getTotal(cid)
		}
		rollups[id] = ru
		if r.timeOverride != nil {
			totals[id] = *r.timeOverride
		} else {
			totals[id] = r.logged + ru
		}
		return totals[id]
	}
	for _, r := range issueRows {
		getTotal(r.id)
	}

	// tag map: issue_id → []name
	tagMap := map[int64][]string{}
	tagRows, _ := db.DB.Query(`
		SELECT it.issue_id, t.name
		FROM issue_tags it JOIN tags t ON t.id = it.tag_id
		WHERE it.issue_id IN (SELECT id FROM issues WHERE project_id=?)
		ORDER BY it.issue_id, t.name
	`, projectID)
	if tagRows != nil {
		for tagRows.Next() {
			var id int64
			var name string
			if tagRows.Scan(&id, &name) == nil {
				tagMap[id] = append(tagMap[id], name)
			}
		}
		tagRows.Close()
	}

	numToID := map[int]int64{}
	idRows, _ := db.DB.Query("SELECT id, issue_number FROM issues WHERE project_id=?", projectID)
	if idRows != nil {
		for idRows.Next() {
			var id int64
			var num int
			idRows.Scan(&id, &num)
			numToID[num] = id
		}
		idRows.Close()
	}

	date := time.Now().Format("2006-01-02")
	filename := fmt.Sprintf("%s-%s.csv", projKey, date)
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))

	cw := csv.NewWriter(w)
	cw.Write(csvHeaders)

	for _, r := range issueRows {
		issueKey := ""
		if r.issueNum != nil {
			issueKey = fmt.Sprintf("%s-%d", r.projKeyVal, *r.issueNum)
		}
		parentKey := ""
		if r.parentNum != nil {
			parentKey = fmt.Sprintf("%s-%d", r.projKeyVal, *r.parentNum)
		}
		tags := ""
		if r.issueNum != nil {
			if id, ok := numToID[*r.issueNum]; ok {
				tags = strings.Join(tagMap[id], ",")
			}
		}

		overrideStr := ""
		if r.timeOverride != nil {
			overrideStr = fmt.Sprintf("%.2f", *r.timeOverride)
		}
		cw.Write([]string{
			issueKey, r.typ, parentKey,
			r.title, r.desc, r.ac, r.notes,
			r.status, r.priority, r.costUnit, r.release,
			"", "", r.assignee, tags, // depends_on, impacts: removed in M32
			fmt.Sprintf("%.2f", r.logged),
			fmt.Sprintf("%.2f", rollups[r.id]),
			overrideStr,
			fmt.Sprintf("%.2f", totals[r.id]),
		})
	}
	cw.Flush()
}

// ── CSV parsing helper ────────────────────────────────────────────────────────

func parseCSV(r *http.Request) ([]ImportRow, error) {
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		return nil, fmt.Errorf("parse form failed")
	}
	f, _, err := r.FormFile("file")
	if err != nil {
		return nil, fmt.Errorf("file field required")
	}
	defer f.Close()

	cr := csv.NewReader(f)
	cr.TrimLeadingSpace = true
	records, err := cr.ReadAll()
	if err != nil || len(records) < 2 {
		return nil, fmt.Errorf("invalid CSV — must have header row and at least one data row")
	}

	header := records[0]
	col := map[string]int{}
	for i, h := range header {
		col[strings.TrimSpace(strings.ToLower(h))] = i
	}
	for _, req := range []string{"type", "title"} {
		if _, ok := col[req]; !ok {
			return nil, fmt.Errorf("CSV missing required column: %s", req)
		}
	}

	get := func(row []string, name string) string {
		i, ok := col[name]
		if !ok || i >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[i])
	}

	var rows []ImportRow
	for _, rec := range records[1:] {
		if len(rec) == 0 {
			continue
		}
		tagsRaw := get(rec, "tags")
		var tags []string
		for _, t := range strings.Split(tagsRaw, ",") {
			if t = strings.TrimSpace(t); t != "" {
				tags = append(tags, t)
			}
		}
		rows = append(rows, ImportRow{
			OrigKey:            get(rec, "issue_key"),
			Type:               strings.ToLower(get(rec, "type")),
			ParentKey:          get(rec, "parent_key"),
			Title:              get(rec, "title"),
			Description:        get(rec, "description"),
			AcceptanceCriteria: get(rec, "acceptance_criteria"),
			Notes:              get(rec, "notes"),
			Status:             get(rec, "status"),
			Priority:           get(rec, "priority"),
			CostUnit:           get(rec, "cost_unit"),
			Release:            get(rec, "release"),
			// depends_on / impacts columns ignored (removed from schema in M32)
			Assignee: get(rec, "assignee"),
			Tags:     tags,
		})
	}
	return rows, nil
}

// ── Preflight — project-scoped ────────────────────────────────────────────────

// POST /api/projects/:id/import/csv/preflight
func ImportCSVPreflight(w http.ResponseWriter, r *http.Request) {
	projectID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid project id", http.StatusBadRequest)
		return
	}
	var projKey string
	if db.DB.QueryRow("SELECT key FROM projects WHERE id=?", projectID).Scan(&projKey) != nil {
		jsonError(w, "project not found", http.StatusNotFound)
		return
	}
	rows, err := parseCSV(r)
	if err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}
	result := RunPreflight(rows, projKey)
	jsonOK(w, result)
}

// POST /api/projects/:id/import/csv
// Form fields: file (CSV), strategy ("skip"|"overwrite"|"insert")
func ImportCSV(w http.ResponseWriter, r *http.Request) {
	projectID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid project id", http.StatusBadRequest)
		return
	}
	rows, err := parseCSV(r)
	if err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}
	strategy := CollisionStrategy(r.FormValue("strategy"))
	if strategy == "" {
		strategy = StrategySkip
	}
	result := RunImport(rows, projectID, strategy)
	jsonOK(w, result)
}

// ── Global import (creates project if needed) ─────────────────────────────────

// POST /api/import/csv/preflight
func ImportCSVGlobalPreflight(w http.ResponseWriter, r *http.Request) {
	rows, err := parseCSV(r)
	if err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}
	if len(rows) == 0 {
		jsonError(w, "no rows in CSV", http.StatusBadRequest)
		return
	}
	// Infer project key from first issue_key
	projKey := projectKeyFromKey(rows[0].OrigKey)
	if projKey == "" {
		// fallback: scan all rows
		for _, row := range rows {
			if k := projectKeyFromKey(row.OrigKey); k != "" {
				projKey = k
				break
			}
		}
	}
	if projKey == "" {
		jsonError(w, "cannot determine project key — ensure issue_key column is populated", http.StatusBadRequest)
		return
	}
	result := RunPreflight(rows, projKey)
	jsonOK(w, result)
}

// POST /api/import/csv
// Form fields: file (CSV), strategy, project_name (optional, used when creating new project)
func ImportCSVGlobal(w http.ResponseWriter, r *http.Request) {
	rows, err := parseCSV(r)
	if err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}
	if len(rows) == 0 {
		jsonError(w, "no rows in CSV", http.StatusBadRequest)
		return
	}

	projKey := projectKeyFromKey(rows[0].OrigKey)
	for _, row := range rows {
		if k := projectKeyFromKey(row.OrigKey); k != "" {
			projKey = k
			break
		}
	}
	if projKey == "" {
		jsonError(w, "cannot determine project key from issue_key column", http.StatusBadRequest)
		return
	}

	projectName := r.FormValue("project_name")
	if projectName == "" {
		projectName = projKey
	}

	strategy := CollisionStrategy(r.FormValue("strategy"))
	if strategy == "" {
		strategy = StrategySkip
	}

	projectID, err := EnsureProjectExists(projKey, projectName)
	if err != nil {
		jsonError(w, "failed to create project: "+err.Error(), http.StatusInternalServerError)
		return
	}

	result := RunImport(rows, projectID, strategy)

	// Include project_id in response so frontend can navigate to it
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"project_id": projectID,
		"project_key": projKey,
		"imported":   result.Imported,
		"updated":    result.Updated,
		"skipped":    result.Skipped,
		"errors":     result.Errors,
	})
}
