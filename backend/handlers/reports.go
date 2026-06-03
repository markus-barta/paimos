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
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/markus-barta/paimos/backend/db"
)

// ── Lieferbericht types ──────────────────────────────────────────────────────

type lbIssue struct {
	ID            int64    `json:"id"`
	IssueKey      string   `json:"issue_key"`
	Type          string   `json:"type"`
	Title         string   `json:"title"`
	Description   string   `json:"description"`
	ReportSummary string   `json:"report_summary"`
	Status        string   `json:"status"`
	EstimateLp    *float64 `json:"estimate_lp"`
	EstimateHours *float64 `json:"estimate_hours"`
	ArLp          *float64 `json:"ar_lp"`
	ArHours       *float64 `json:"ar_hours"`
	RateLp        *float64 `json:"rate_lp"`
	RateHourly    *float64 `json:"rate_hourly"`
	ArEur         float64  `json:"ar_eur"`
}

type lbSubtotal struct {
	EstimateLp    float64 `json:"estimate_lp"`
	EstimateHours float64 `json:"estimate_hours"`
	ArLp          float64 `json:"ar_lp"`
	ArHours       float64 `json:"ar_hours"`
	ArEur         float64 `json:"ar_eur"`
}

type lbReportCols struct {
	SP    bool `json:"sp"`
	H     bool `json:"h"`
	ARSP  bool `json:"ar_sp"`
	ARH   bool `json:"ar_h"`
	AREUR bool `json:"ar_eur"`
}

type lbGroup struct {
	EpicKey   string     `json:"epic_key"`
	EpicTitle string     `json:"epic_title"`
	Issues    []lbIssue  `json:"issues"`
	Subtotal  lbSubtotal `json:"subtotal"`
}

type lbReport struct {
	ProjectID   int64        `json:"project_id"`
	ProjectKey  string       `json:"project_key"`
	ProjectName string       `json:"project_name"`
	GeneratedAt string       `json:"generated_at"`
	Cols        lbReportCols `json:"cols"`
	Groups      []lbGroup    `json:"groups"`
	GrandTotal  lbSubtotal   `json:"grand_total"`
}

// ── Query helper ─────────────────────────────────────────────────────────────

// lbFilters bundles the optional narrowing filters layered on top of the
// scope preset (PAI-404). Each is stacked with AND; empty slices are skipped.
type lbFilters struct {
	TagIDs          []int64
	ExcludeTagIDs   []int64
	Statuses        []string
	ExcludeStatuses []string
	DateField       string
	DateFrom        string
	DateTo          string
	Type            string
	Priority        string
	CostUnit        string
	Release         string
	AssigneeID      string
	IssueIDs        []int64
}

func buildLieferbericht(projectID int64, scope, sprintIDs, fromDate, toDate string, filters lbFilters) (*lbReport, error) {
	// Load project
	var projectKey, projectName string
	if err := db.DB.QueryRow("SELECT key, name FROM projects WHERE id=?", projectID).Scan(&projectKey, &projectName); err != nil {
		return nil, fmt.Errorf("project not found: %w", err)
	}

	// Build query
	args := []any{projectID}
	where := []string{"i.project_id = ?", "i.type IN ('ticket','task')", "i.deleted_at IS NULL"}

	switch scope {
	case "sprint":
		if sprintIDs == "" {
			return nil, fmt.Errorf("sprint_ids required for sprint scope")
		}
		parts := strings.Split(sprintIDs, ",")
		ph := make([]string, 0, len(parts))
		for _, s := range parts {
			id, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
			if err != nil {
				continue
			}
			ph = append(ph, "?")
			args = append(args, id)
		}
		if len(ph) == 0 {
			return nil, fmt.Errorf("no valid sprint_ids")
		}
		where = append(where, "i.id IN (SELECT target_id FROM issue_relations WHERE type='sprint' AND source_id IN ("+strings.Join(ph, ",")+"))")

	case "date_range":
		if fromDate != "" {
			where = append(where, "i.updated_at >= ?")
			args = append(args, fromDate)
		}
		if toDate != "" {
			where = append(where, "i.updated_at <= ?")
			args = append(args, toDate+" 23:59:59")
		}

	default: // all_open
		where = append(where, "i.status NOT IN ('done','delivered','accepted','invoiced','cancelled')")
	}

	// PAI-404: tag filter — issue must carry at least one of the selected tags.
	if len(filters.TagIDs) > 0 {
		ph := make([]string, 0, len(filters.TagIDs))
		for _, id := range filters.TagIDs {
			ph = append(ph, "?")
			args = append(args, id)
		}
		where = append(where, "i.id IN (SELECT issue_id FROM issue_tags WHERE tag_id IN ("+strings.Join(ph, ",")+"))")
	}
	if len(filters.ExcludeTagIDs) > 0 {
		ph := make([]string, 0, len(filters.ExcludeTagIDs))
		for _, id := range filters.ExcludeTagIDs {
			ph = append(ph, "?")
			args = append(args, id)
		}
		where = append(where, "i.id NOT IN (SELECT issue_id FROM issue_tags WHERE tag_id IN ("+strings.Join(ph, ",")+"))")
	}

	// PAI-404: explicit status filter, AND-ed on top of scope's default. Picking
	// a status excluded by scope=all_open's default-OUT list yields no rows —
	// expected; users wanting "delivered only" should switch to scope=date_range.
	if len(filters.Statuses) > 0 {
		ph := make([]string, 0, len(filters.Statuses))
		for _, s := range filters.Statuses {
			ph = append(ph, "?")
			args = append(args, s)
		}
		where = append(where, "i.status IN ("+strings.Join(ph, ",")+")")
	}
	if len(filters.ExcludeStatuses) > 0 {
		ph := make([]string, 0, len(filters.ExcludeStatuses))
		for _, s := range filters.ExcludeStatuses {
			ph = append(ph, "?")
			args = append(args, s)
		}
		where = append(where, "i.status NOT IN ("+strings.Join(ph, ",")+")")
	}
	where, args = appendLBMultiFilter(where, args, "i.priority", filters.Priority)
	where, args = appendLBMultiFilter(where, args, "i.type", filters.Type)
	where, args = appendLBMultiFilter(where, args, "i.cost_unit", filters.CostUnit)
	where, args = appendLBMultiFilter(where, args, "i.release", filters.Release)
	where, args = appendLBAssigneeFilter(where, args, filters.AssigneeID)
	if len(filters.IssueIDs) > 0 {
		ph := make([]string, 0, len(filters.IssueIDs))
		for _, id := range filters.IssueIDs {
			if id <= 0 {
				continue
			}
			ph = append(ph, "?")
			args = append(args, id)
		}
		if len(ph) == 0 {
			return nil, fmt.Errorf("no valid issue ids")
		}
		where = append(where, "i.id IN ("+strings.Join(ph, ",")+")")
	}

	if filters.DateField != "" && (filters.DateFrom != "" || filters.DateTo != "") {
		dateWhere, dateArgs := issueDateWhereSQL(filters.DateField, filters.DateFrom, filters.DateTo)
		if dateWhere != "" {
			where = append(where, dateWhere)
			args = append(args, dateArgs...)
		}
	}

	query := `
		SELECT
			i.id,
			p.key || '-' || i.issue_number AS issue_key,
			i.type, i.title, i.description, i.report_summary, i.status,
			i.estimate_lp, i.estimate_hours,
			i.ar_lp, i.ar_hours,
			-- Effective rate hierarchy: issue → epic → project → customer
			-- (mirrors applyEffectiveRates in projects.go; PAI-54). Without the
			-- project/customer fallback the AR EUR column is blank whenever the
			-- rate is only configured at the project or customer level.
			COALESCE(i.rate_hourly, epic.rate_hourly, p.rate_hourly, c.rate_hourly) AS rate_hourly,
			COALESCE(i.rate_lp, epic.rate_lp, p.rate_lp, c.rate_lp) AS rate_lp,
			COALESCE(epic.id, 0) AS epic_id,
			COALESCE(p.key || '-' || epic.issue_number, '') AS epic_key,
			COALESCE(epic.title, '') AS epic_title
		FROM issues i
		JOIN projects p ON p.id = i.project_id
		LEFT JOIN customers c ON c.id = p.customer_id
		LEFT JOIN issues epic ON epic.id = i.parent_id AND epic.type = 'epic'
		WHERE ` + strings.Join(where, " AND ") + `
		ORDER BY epic_key, i.issue_number
	`

	rows, err := db.DB.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	// Group by epic
	groupMap := map[string]*lbGroup{}
	var groupOrder []string

	for rows.Next() {
		var (
			issueKey, iType, title, desc, reportSummary, status string
			estLp, estH, arLp, arH                              *float64
			rateLp, rateH                                       *float64
			epicID                                              int64
			issueID                                             int64
			epicKey, epicTitle                                  string
		)
		if err := rows.Scan(
			&issueID,
			&issueKey, &iType, &title, &desc, &reportSummary, &status,
			&estLp, &estH, &arLp, &arH, &rateH, &rateLp,
			&epicID, &epicKey, &epicTitle,
		); err != nil {
			continue
		}

		arEur := optMul(arLp, rateLp) + optMul(arH, rateH)

		issue := lbIssue{
			ID: issueID, IssueKey: issueKey, Type: iType, Title: title, Description: desc, ReportSummary: reportSummary, Status: status,
			EstimateLp: estLp, EstimateHours: estH, ArLp: arLp, ArHours: arH,
			RateLp: rateLp, RateHourly: rateH, ArEur: arEur,
		}

		gKey := epicKey
		gTitle := epicTitle
		if epicID == 0 {
			gKey = projectKey
			gTitle = projectKey
		}

		if _, ok := groupMap[gKey]; !ok {
			groupMap[gKey] = &lbGroup{EpicKey: gKey, EpicTitle: gTitle, Issues: []lbIssue{}}
			groupOrder = append(groupOrder, gKey)
		}
		g := groupMap[gKey]
		g.Issues = append(g.Issues, issue)
		g.Subtotal.EstimateLp += optVal(estLp)
		g.Subtotal.EstimateHours += optVal(estH)
		g.Subtotal.ArLp += optVal(arLp)
		g.Subtotal.ArHours += optVal(arH)
		g.Subtotal.ArEur += arEur
	}

	// Build result
	report := &lbReport{
		ProjectID:   projectID,
		ProjectKey:  projectKey,
		ProjectName: projectName,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Cols:        lbReportColsFromSet(defaultLBColSet()),
		Groups:      make([]lbGroup, 0, len(groupOrder)),
	}

	for _, key := range groupOrder {
		g := groupMap[key]
		// Round subtotals
		g.Subtotal.ArEur = roundTo(g.Subtotal.ArEur, 2)
		report.Groups = append(report.Groups, *g)
		report.GrandTotal.EstimateLp += g.Subtotal.EstimateLp
		report.GrandTotal.EstimateHours += g.Subtotal.EstimateHours
		report.GrandTotal.ArLp += g.Subtotal.ArLp
		report.GrandTotal.ArHours += g.Subtotal.ArHours
		report.GrandTotal.ArEur += g.Subtotal.ArEur
	}
	report.GrandTotal.ArEur = roundTo(report.GrandTotal.ArEur, 2)

	return report, nil
}

func lbReportColsFromSet(set lbColSet) lbReportCols {
	return lbReportCols{
		SP:    set.SP,
		H:     set.H,
		ARSP:  set.ARSP,
		ARH:   set.ARH,
		AREUR: set.AREUR,
	}
}

func requestLBColSet(r *http.Request) lbColSet {
	// PAI-400: distinguish "param absent" (back-compat → all visible) from
	// "param present but empty" (PAI-401 → zero numeric columns). url.Values.Get
	// returns "" for both, so check the underlying slice directly.
	if _, present := r.URL.Query()["cols"]; present {
		return parseLBColSet(r.URL.Query().Get("cols"))
	}
	return defaultLBColSet()
}

func optVal(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

func optMul(a, b *float64) float64 {
	if a == nil || b == nil {
		return 0
	}
	return *a * *b
}

func roundTo(v float64, places int) float64 {
	p := math.Pow(10, float64(places))
	return math.Round(v*p) / p
}

// ── JSON handler ─────────────────────────────────────────────────────────────

// parseLBFilters reads the PAI-404 narrowing filters from the request query.
// Empty values yield empty slices (skipped in WHERE).
func parseLBFilters(r *http.Request) lbFilters {
	var f lbFilters
	if raw := r.URL.Query().Get("tag_ids"); raw != "" {
		for _, s := range strings.Split(raw, ",") {
			token := strings.TrimSpace(s)
			neg := strings.HasPrefix(token, "!")
			if neg {
				token = strings.TrimSpace(strings.TrimPrefix(token, "!"))
			}
			if id, err := strconv.ParseInt(token, 10, 64); err == nil && id > 0 {
				if neg {
					f.ExcludeTagIDs = append(f.ExcludeTagIDs, id)
				} else {
					f.TagIDs = append(f.TagIDs, id)
				}
			}
		}
	}
	if raw := r.URL.Query().Get("statuses"); raw != "" {
		for _, s := range strings.Split(raw, ",") {
			token := strings.TrimSpace(s)
			neg := strings.HasPrefix(token, "!")
			if neg {
				token = strings.TrimSpace(strings.TrimPrefix(token, "!"))
			}
			if token != "" {
				if neg {
					f.ExcludeStatuses = append(f.ExcludeStatuses, token)
				} else {
					f.Statuses = append(f.Statuses, token)
				}
			}
		}
	}
	f.DateField = strings.TrimSpace(r.URL.Query().Get("date_field"))
	f.DateFrom = strings.TrimSpace(r.URL.Query().Get("date_from"))
	f.DateTo = strings.TrimSpace(r.URL.Query().Get("date_to"))
	f.Type = strings.TrimSpace(r.URL.Query().Get("type"))
	f.Priority = strings.TrimSpace(r.URL.Query().Get("priority"))
	f.CostUnit = strings.TrimSpace(r.URL.Query().Get("cost_unit"))
	f.Release = strings.TrimSpace(r.URL.Query().Get("release"))
	f.AssigneeID = strings.TrimSpace(r.URL.Query().Get("assignee_id"))
	return f
}

func appendLBMultiFilter(where []string, args []any, col, raw string) ([]string, []any) {
	if raw == "" {
		return where, args
	}
	query, nextArgs := applyMultiFilter("", args, col, raw)
	clause := strings.TrimPrefix(query, " AND ")
	if clause != "" {
		where = append(where, clause)
	}
	return where, nextArgs
}

func appendLBAssigneeFilter(where []string, args []any, raw string) ([]string, []any) {
	if raw == "" {
		return where, args
	}
	vals := splitCSV(raw)
	hasUnassigned := false
	ids := []string{}
	for _, v := range vals {
		if strings.HasPrefix(v, "!") {
			continue
		}
		if v == "unassigned" {
			hasUnassigned = true
		} else if _, err := strconv.ParseInt(v, 10, 64); err == nil {
			ids = append(ids, v)
		}
	}
	if len(ids) == 0 && !hasUnassigned {
		return where, args
	}
	ph := make([]string, 0, len(ids))
	for _, id := range ids {
		ph = append(ph, "?")
		args = append(args, id)
	}
	if len(ids) > 0 && hasUnassigned {
		where = append(where, "(i.assignee_id IN ("+strings.Join(ph, ",")+") OR i.assignee_id IS NULL)")
	} else if len(ids) > 0 {
		where = append(where, "i.assignee_id IN ("+strings.Join(ph, ",")+")")
	} else {
		where = append(where, "i.assignee_id IS NULL")
	}
	return where, args
}

func issueDateWhereSQL(field, from, to string) (string, []any) {
	args := []any{}
	if field == "completed" {
		where := `i.status IN ('done','delivered','accepted','invoiced') AND i.id IN (
			SELECT issue_id FROM (
				SELECT
					h.issue_id,
					h.changed_at,
					json_extract(h.snapshot, '$.status') AS new_status,
					LAG(json_extract(h.snapshot, '$.status'))
						OVER (PARTITION BY h.issue_id ORDER BY h.changed_at, h.id) AS prev_status
				FROM issue_history h
			)
			WHERE new_status IN ('done','delivered','accepted','invoiced')
			  AND (prev_status IS NULL OR prev_status != new_status)`
		if from != "" {
			where += " AND changed_at >= ?"
			args = append(args, from)
		}
		if to != "" {
			where += " AND changed_at < ?"
			args = append(args, issueDateToExclusive(to))
		}
		where += ")"
		return where, args
	}
	col := issueDateColumn(field)
	if col == "" {
		return "", nil
	}
	where := []string{}
	if from != "" {
		where = append(where, col+" >= ?")
		args = append(args, from)
	}
	if to != "" {
		where = append(where, col+" < ?")
		args = append(args, issueDateToExclusive(to))
	}
	return strings.Join(where, " AND "), args
}

// GET /api/projects/{id}/reports/lieferbericht
func GetLieferbericht(w http.ResponseWriter, r *http.Request) {
	projectID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid project id", http.StatusBadRequest)
		return
	}

	scope := r.URL.Query().Get("scope")
	if scope == "" {
		scope = "all_open"
	}
	sprintIDs := r.URL.Query().Get("sprint_ids")
	fromDate := r.URL.Query().Get("from")
	toDate := r.URL.Query().Get("to")

	report, err := buildLieferbericht(projectID, scope, sprintIDs, fromDate, toDate, parseLBFilters(r))
	if err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}
	report.Cols = lbReportColsFromSet(requestLBColSet(r))

	jsonOK(w, report)
}
