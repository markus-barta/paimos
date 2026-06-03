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
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/markus-barta/paimos/backend/db"
)

// PAI-579: booked-hours report for a project over an explicit from/to window,
// optionally narrowed to one user. Attribution is by the calendar date of
// started_at (the de-facto work date, user-settable via PAI-478). Hours use the
// canonical booked formula — kept character-identical to loadBookedHoursBatch
// and the scope=time_booked report so the three reconcile (guarded by tests).
//
// This report is hours/material only — it never exposes rates or money, so
// project-view access is sufficient (no rate leakage across users).
//
// bookedHoursCaseSQL expects the time_entries table aliased as `te`.
const bookedHoursCaseSQL = `SUM(CASE
	WHEN te.override IS NOT NULL THEN te.override
	WHEN te.stopped_at IS NOT NULL THEN (julianday(te.stopped_at) - julianday(te.started_at)) * 24
	ELSE 0 END)`

const materialSumSQL = `SUM(COALESCE(te.material_lp, 0))`

type timeReportUserRow struct {
	UserID     int64   `json:"user_id"`
	Username   string  `json:"username"`
	Hours      float64 `json:"hours"`
	MaterialLp float64 `json:"material_lp"`
	Entries    int     `json:"entries"`
}

type timeReportDayRow struct {
	Date       string  `json:"date"`
	Hours      float64 `json:"hours"`
	MaterialLp float64 `json:"material_lp"`
}

type timeReportIssueRow struct {
	IssueID    int64   `json:"issue_id"`
	IssueKey   string  `json:"issue_key"`
	Title      string  `json:"title"`
	Hours      float64 `json:"hours"`
	MaterialLp float64 `json:"material_lp"`
	Entries    int     `json:"entries"`
}

type timeReportResponse struct {
	ProjectID       int64                `json:"project_id"`
	From            string               `json:"from"`
	To              string               `json:"to"`
	UserID          *int64               `json:"user_id,omitempty"`
	TotalHours      float64              `json:"total_hours"`
	TotalMaterialLp float64              `json:"total_material_lp"`
	TotalEntries    int                  `json:"total_entries"`
	ByUser          []timeReportUserRow  `json:"by_user"`
	ByDay           []timeReportDayRow   `json:"by_day"`
	ByIssue         []timeReportIssueRow `json:"by_issue"`
}

// GetProjectTimeReport handles GET /api/projects/{id}/time-report?from=&to=&user=
func GetProjectTimeReport(w http.ResponseWriter, r *http.Request) {
	projectID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid project id", http.StatusBadRequest)
		return
	}
	from := strings.TrimSpace(r.URL.Query().Get("from"))
	to := strings.TrimSpace(r.URL.Query().Get("to"))
	if from == "" || to == "" {
		jsonError(w, "from and to are required (YYYY-MM-DD)", http.StatusBadRequest)
		return
	}

	// Shared window filter. Bound as: projectID, from, to [, userID].
	where := "i.project_id = ? AND i.deleted_at IS NULL" +
		" AND date(te.started_at) >= date(?) AND date(te.started_at) <= date(?)"
	args := []any{projectID, from, to}

	resp := timeReportResponse{ProjectID: projectID, From: from, To: to}
	if raw := strings.TrimSpace(r.URL.Query().Get("user")); raw != "" {
		uid, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || uid <= 0 {
			jsonError(w, "invalid user", http.StatusBadRequest)
			return
		}
		where += " AND te.user_id = ?"
		args = append(args, uid)
		resp.UserID = &uid
	}

	fromSQL := `
		FROM time_entries te
		JOIN issues i ON i.id = te.issue_id`
	whereSQL := " WHERE " + where

	// By user.
	userRows, err := db.DB.Query(`
		SELECT te.user_id, COALESCE(NULLIF(u.nickname,''), u.username, ''),
		       `+bookedHoursCaseSQL+`, `+materialSumSQL+`, COUNT(*)`+fromSQL+`
		LEFT JOIN users u ON u.id = te.user_id`+whereSQL+`
		GROUP BY te.user_id
		ORDER BY 3 DESC`, args...)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer userRows.Close()
	resp.ByUser = []timeReportUserRow{}
	for userRows.Next() {
		var row timeReportUserRow
		if err := userRows.Scan(&row.UserID, &row.Username, &row.Hours, &row.MaterialLp, &row.Entries); err != nil {
			continue
		}
		resp.ByUser = append(resp.ByUser, row)
		resp.TotalHours += row.Hours
		resp.TotalMaterialLp += row.MaterialLp
		resp.TotalEntries += row.Entries
	}
	resp.TotalHours = roundTo(resp.TotalHours, 2)
	resp.TotalMaterialLp = roundTo(resp.TotalMaterialLp, 2)

	// By day (calendar date of started_at).
	dayRows, err := db.DB.Query(`
		SELECT date(te.started_at), `+bookedHoursCaseSQL+`, `+materialSumSQL+fromSQL+whereSQL+`
		GROUP BY date(te.started_at)
		ORDER BY 1`, args...)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer dayRows.Close()
	resp.ByDay = []timeReportDayRow{}
	for dayRows.Next() {
		var row timeReportDayRow
		if err := dayRows.Scan(&row.Date, &row.Hours, &row.MaterialLp); err != nil {
			continue
		}
		row.Hours = roundTo(row.Hours, 2)
		row.MaterialLp = roundTo(row.MaterialLp, 2)
		resp.ByDay = append(resp.ByDay, row)
	}

	// By issue.
	issueRows, err := db.DB.Query(`
		SELECT te.issue_id, p.key || '-' || i.issue_number, i.title,
		       `+bookedHoursCaseSQL+`, `+materialSumSQL+`, COUNT(*)`+fromSQL+`
		JOIN projects p ON p.id = i.project_id`+whereSQL+`
		GROUP BY te.issue_id
		ORDER BY 4 DESC`, args...)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer issueRows.Close()
	resp.ByIssue = []timeReportIssueRow{}
	for issueRows.Next() {
		var row timeReportIssueRow
		if err := issueRows.Scan(&row.IssueID, &row.IssueKey, &row.Title, &row.Hours, &row.MaterialLp, &row.Entries); err != nil {
			continue
		}
		row.Hours = roundTo(row.Hours, 2)
		row.MaterialLp = roundTo(row.MaterialLp, 2)
		resp.ByIssue = append(resp.ByIssue, row)
	}

	jsonOK(w, resp)
}
