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
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/markus-barta/paimos/backend/db"
)

// ── Aggregation endpoint ──────────────────────────────────────────────────────

// IssueAggregation is the response for GET /api/issues/:id/aggregation.
type IssueAggregation struct {
	MemberCount        int      `json:"member_count"`
	EstimateHours      *float64 `json:"estimate_hours"`
	EstimateLp         *float64 `json:"estimate_lp"`
	EstimateEur        *float64 `json:"estimate_eur"`
	ArHours            *float64 `json:"ar_hours"`
	ArLp               *float64 `json:"ar_lp"`
	ArEur              *float64 `json:"ar_eur"`
	ActualHours        *float64 `json:"actual_hours"`
	ActualInternalCost *float64 `json:"actual_internal_cost"`
	MarginEur          *float64 `json:"margin_eur"`
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

	// Sum estimate/AR across group members (issue_relations type='groups', source=container).
	// Members of a cross-project container (e.g. an orphan sprint) can live
	// in any project. Filter the members by the caller's accessible project
	// set so totals never leak hours/amounts from projects the caller
	// cannot view.
	aggAccessFilter, aggAccessArgs := projectIDFilter(r, "i.project_id", true)
	aggArgs := append([]any{id}, aggAccessArgs...)
	var agg IssueAggregation
	err = db.DB.QueryRow(`
		SELECT COUNT(*),
		       SUM(i.estimate_hours), SUM(i.estimate_lp),
		       SUM(i.ar_hours), SUM(i.ar_lp)
		FROM issues i
		JOIN issue_relations ir ON ir.target_id = i.id
		WHERE ir.source_id = ? AND ir.type = 'groups' AND i.deleted_at IS NULL`+aggAccessFilter,
		aggArgs...).Scan(&agg.MemberCount, &agg.EstimateHours, &agg.EstimateLp, &agg.ArHours, &agg.ArLp)
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

	// Sum actuals from time_entries on member issues + the container itself.
	// The same cross-project leak applies here as in the estimate aggregation,
	// so filter the members subquery by the caller's accessible projects.
	actArgs := append([]any{id}, aggAccessArgs...)
	actArgs = append(actArgs, id)
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
			SELECT i.id FROM issue_relations ir
			JOIN issues i ON i.id = ir.target_id
			WHERE ir.source_id = ? AND ir.type = 'groups'`+aggAccessFilter+`
			UNION ALL SELECT ?
		)
	`, actArgs...).Scan(&actualHours, &actualInternalCost)
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
