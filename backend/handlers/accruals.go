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
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/markus-barta/paimos/backend/db"
)

// ACME-1 — Project accruals (Vorräte) report.
//
// Returns AR-hour totals per project, broken down by current issue status,
// where each issue counts only if its most recent transition INTO its
// current status falls within [from, to] (inclusive both ends, end is
// extended to end-of-day server-side).
//
// Date semantics use issue_history snapshots: a transition is detected via
// LAG over (issue_id ORDER BY changed_at, id) where the prior snapshot's
// status differs from the current one (or is the very first snapshot).
//
// Admin-gated at the route level.

// AccrualsStatuses is the canonical column order for the report.
// Includes all 9 PAIMOS statuses; cancelled is shown but excluded from totals.
var AccrualsStatuses = []string{
	"new", "backlog", "in-progress", "qa",
	"done", "delivered", "accepted", "invoiced", "cancelled",
}

type accrualsRow struct {
	ProjectID   int64              `json:"project_id"`
	ProjectKey  string             `json:"project_key"`
	ProjectName string             `json:"project_name"`
	Totals      map[string]float64 `json:"totals"`
}

type accrualsResponse struct {
	From     string         `json:"from"`
	To       string         `json:"to"`
	Statuses []string       `json:"statuses"`
	Rows     []accrualsRow  `json:"rows"`
}

// parseAccrualsRange parses ?from= and ?to= query params (YYYY-MM-DD).
// Defaults: from = current year start, to = end of last completed month.
// The returned `toExclusive` is `to + 1 day` so a half-open interval can be
// used in SQL ([from, toExclusive)).
func parseAccrualsRange(r *http.Request) (from, to string, toExclusive string, err error) {
	now := time.Now()
	defaultFrom := time.Date(now.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
	defaultTo := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, -1)

	from = r.URL.Query().Get("from")
	to = r.URL.Query().Get("to")
	if from == "" {
		from = defaultFrom.Format("2006-01-02")
	}
	if to == "" {
		to = defaultTo.Format("2006-01-02")
	}

	fromT, e1 := time.Parse("2006-01-02", from)
	toT, e2 := time.Parse("2006-01-02", to)
	if e1 != nil || e2 != nil || toT.Before(fromT) {
		return "", "", "", fmt.Errorf("invalid date range")
	}
	toExclusive = toT.AddDate(0, 0, 1).Format("2006-01-02")
	return from, to, toExclusive, nil
}

// queryAccruals runs the aggregation query and returns one row per project,
// totals filled in for every status (zero if no contribution).
func queryAccruals(from, toExclusive string) ([]accrualsRow, error) {
	// One CTE walks issue_history with LAG to detect "transition into status".
	// Outer query joins per-issue aggregations against the issue's CURRENT status,
	// then aggregates AR hours by project + status.
	//
	// Filter: most-recent entry into the current status falls in [from, toExclusive).
	const q = `
		WITH transitions AS (
			SELECT
				h.issue_id,
				h.changed_at,
				json_extract(h.snapshot, '$.status') AS new_status,
				LAG(json_extract(h.snapshot, '$.status'))
					OVER (PARTITION BY h.issue_id ORDER BY h.changed_at, h.id) AS prev_status
			FROM issue_history h
		),
		entries AS (
			SELECT issue_id, new_status, MAX(changed_at) AS entered_at
			FROM transitions
			WHERE prev_status IS NULL OR prev_status != new_status
			GROUP BY issue_id, new_status
		)
		SELECT i.project_id, i.status, COALESCE(SUM(i.ar_hours), 0) AS hours
		FROM issues i
		JOIN entries e ON e.issue_id = i.id AND e.new_status = i.status
		WHERE i.archived = 0
		  AND e.entered_at >= ?
		  AND e.entered_at <  ?
		GROUP BY i.project_id, i.status
	`
	rows, err := db.DB.Query(q, from, toExclusive)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Aggregate into a project_id → status → hours map
	byProject := map[int64]map[string]float64{}
	for rows.Next() {
		var pid int64
		var status string
		var hours float64
		if err := rows.Scan(&pid, &status, &hours); err != nil {
			return nil, err
		}
		if _, ok := byProject[pid]; !ok {
			byProject[pid] = map[string]float64{}
		}
		byProject[pid][status] = hours
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Fetch all non-archived projects so empty ones still appear
	pRows, err := db.DB.Query(`SELECT id, key, name FROM projects WHERE status='active' OR status IS NULL ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer pRows.Close()

	out := []accrualsRow{}
	for pRows.Next() {
		var row accrualsRow
		if err := pRows.Scan(&row.ProjectID, &row.ProjectKey, &row.ProjectName); err != nil {
			return nil, err
		}
		row.Totals = map[string]float64{}
		for _, s := range AccrualsStatuses {
			row.Totals[s] = 0
		}
		if got, ok := byProject[row.ProjectID]; ok {
			for k, v := range got {
				row.Totals[k] = v
			}
		}
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].ProjectName) < strings.ToLower(out[j].ProjectName)
	})
	return out, nil
}

// GetAccruals — GET /api/reports/accruals?from=YYYY-MM-DD&to=YYYY-MM-DD
// Admin-only. Returns JSON for the project list card stats display.
func GetAccruals(w http.ResponseWriter, r *http.Request) {
	from, to, toEx, err := parseAccrualsRange(r)
	if err != nil {
		jsonError(w, "invalid date range", http.StatusBadRequest)
		return
	}
	rows, err := queryAccruals(from, toEx)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	jsonOK(w, accrualsResponse{
		From:     from,
		To:       to,
		Statuses: AccrualsStatuses,
		Rows:     rows,
	})
}

