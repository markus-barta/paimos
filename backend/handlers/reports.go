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

	"github.com/markus-barta/paimos/backend/db"
	"github.com/go-chi/chi/v5"
)

// ── Lieferbericht types ──────────────────────────────────────────────────────

type lbIssue struct {
	IssueKey      string   `json:"issue_key"`
	Type          string   `json:"type"`
	Title         string   `json:"title"`
	Description   string   `json:"description"`
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

type lbGroup struct {
	EpicKey   string     `json:"epic_key"`
	EpicTitle string     `json:"epic_title"`
	Issues    []lbIssue  `json:"issues"`
	Subtotal  lbSubtotal `json:"subtotal"`
}

type lbReport struct {
	ProjectKey  string     `json:"project_key"`
	ProjectName string     `json:"project_name"`
	GeneratedAt string     `json:"generated_at"`
	Groups      []lbGroup  `json:"groups"`
	GrandTotal  lbSubtotal `json:"grand_total"`
}

// ── Query helper ─────────────────────────────────────────────────────────────

func buildLieferbericht(projectID int64, scope, sprintIDs, fromDate, toDate string) (*lbReport, error) {
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

	query := `
		SELECT
			p.key || '-' || i.issue_number AS issue_key,
			i.type, i.title, i.description, i.status,
			i.estimate_lp, i.estimate_hours,
			i.ar_lp, i.ar_hours,
			COALESCE(i.rate_hourly, epic.rate_hourly) AS rate_hourly,
			COALESCE(i.rate_lp, epic.rate_lp) AS rate_lp,
			COALESCE(epic.id, 0) AS epic_id,
			COALESCE(p.key || '-' || epic.issue_number, '') AS epic_key,
			COALESCE(epic.title, '') AS epic_title
		FROM issues i
		JOIN projects p ON p.id = i.project_id
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
			issueKey, iType, title, desc, status string
			estLp, estH, arLp, arH               *float64
			rateLp, rateH                         *float64
			epicID                                int64
			epicKey, epicTitle                     string
		)
		if err := rows.Scan(
			&issueKey, &iType, &title, &desc, &status,
			&estLp, &estH, &arLp, &arH, &rateH, &rateLp,
			&epicID, &epicKey, &epicTitle,
		); err != nil {
			continue
		}

		arEur := optMul(arLp, rateLp) + optMul(arH, rateH)

		issue := lbIssue{
			IssueKey: issueKey, Type: iType, Title: title, Description: desc, Status: status,
			EstimateLp: estLp, EstimateHours: estH, ArLp: arLp, ArHours: arH,
			RateLp: rateLp, RateHourly: rateH, ArEur: arEur,
		}

		gKey := epicKey
		gTitle := epicTitle
		if epicID == 0 {
			gKey = "Ungrouped"
			gTitle = "Ungrouped"
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
		ProjectKey:  projectKey,
		ProjectName: projectName,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
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

	report, err := buildLieferbericht(projectID, scope, sprintIDs, fromDate, toDate)
	if err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}

	jsonOK(w, report)
}
