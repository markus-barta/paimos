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

// PAI-116: incident logging — minimum viable surface for the NIS2
// readiness claim. Admins record security/availability incidents,
// transition them through open → investigating → resolved → closed,
// and export the log for SIEM ingestion. Intentionally small in v1:
// no notification fan-out, no per-team queues, no SLA timers. Those
// are valid follow-ons but outside the 8.5/10 readiness target.

package handlers

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/markus-barta/paimos/backend/auth"
	"github.com/markus-barta/paimos/backend/db"
)

var allowedIncidentSeverities = map[string]bool{
	"low": true, "medium": true, "high": true, "critical": true,
}
var allowedIncidentStatuses = map[string]bool{
	"open": true, "investigating": true, "resolved": true, "closed": true,
}

// Incident is the JSON shape returned by the API.
type Incident struct {
	ID          int64   `json:"id"`
	Severity    string  `json:"severity"`
	Kind        string  `json:"kind"`
	Title       string  `json:"title"`
	Summary     string  `json:"summary"`
	Details     string  `json:"details"`
	ReportedBy  *int64  `json:"reported_by"`
	Status      string  `json:"status"`
	DetectedAt  string  `json:"detected_at"`
	ResolvedAt  *string `json:"resolved_at"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

// ListIncidents — GET /api/incidents
//
// Filters: status, severity, since (RFC3339), limit (default 100, max 1000).
// Newest first (detected_at DESC) so the operator dashboard shows the
// freshest events without paging.
func ListIncidents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	clauses := []string{"1=1"}
	args := []any{}
	if s := q.Get("status"); s != "" && allowedIncidentStatuses[s] {
		clauses = append(clauses, "status = ?")
		args = append(args, s)
	}
	if s := q.Get("severity"); s != "" && allowedIncidentSeverities[s] {
		clauses = append(clauses, "severity = ?")
		args = append(args, s)
	}
	if s := q.Get("since"); s != "" {
		if _, err := time.Parse(time.RFC3339, s); err == nil {
			clauses = append(clauses, "detected_at >= ?")
			args = append(args, s)
		}
	}
	limit := 100
	if l := q.Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 1000 {
			limit = n
		}
	}
	args = append(args, limit)

	rows, err := db.DB.Query(`
		SELECT id, severity, kind, title, summary, details, reported_by,
		       status, detected_at, resolved_at, created_at, updated_at
		FROM incident_log
		WHERE `+strings.Join(clauses, " AND ")+`
		ORDER BY detected_at DESC, id DESC
		LIMIT ?
	`, args...)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	out := []Incident{}
	for rows.Next() {
		var inc Incident
		if err := rows.Scan(&inc.ID, &inc.Severity, &inc.Kind, &inc.Title,
			&inc.Summary, &inc.Details, &inc.ReportedBy, &inc.Status,
			&inc.DetectedAt, &inc.ResolvedAt, &inc.CreatedAt, &inc.UpdatedAt); err != nil {
			continue
		}
		out = append(out, inc)
	}
	jsonOK(w, out)
}

// GetIncident — GET /api/incidents/{id}
func GetIncident(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	inc := getIncidentByID(id)
	if inc == nil {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	jsonOK(w, inc)
}

// CreateIncident — POST /api/incidents
func CreateIncident(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Severity   string  `json:"severity"`
		Kind       string  `json:"kind"`
		Title      string  `json:"title"`
		Summary    string  `json:"summary"`
		Details    string  `json:"details"`
		DetectedAt *string `json:"detected_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "invalid body", http.StatusBadRequest)
		return
	}
	body.Severity = strings.ToLower(strings.TrimSpace(body.Severity))
	if !allowedIncidentSeverities[body.Severity] {
		jsonError(w, "severity must be low|medium|high|critical", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(body.Title) == "" {
		jsonError(w, "title required", http.StatusBadRequest)
		return
	}
	if body.Kind == "" {
		body.Kind = "other"
	}
	user := auth.GetUser(r)
	var reporter *int64
	if user != nil {
		reporter = &user.ID
	}
	detected := time.Now().UTC().Format("2006-01-02 15:04:05")
	if body.DetectedAt != nil && *body.DetectedAt != "" {
		if _, err := time.Parse(time.RFC3339, *body.DetectedAt); err == nil {
			detected = *body.DetectedAt
		}
	}
	res, err := db.DB.Exec(`
		INSERT INTO incident_log(severity, kind, title, summary, details, reported_by, detected_at)
		VALUES(?, ?, ?, ?, ?, ?, ?)
	`, body.Severity, body.Kind, body.Title, body.Summary, body.Details, reporter, detected)
	if err != nil {
		jsonError(w, "insert failed", http.StatusInternalServerError)
		return
	}
	id, _ := res.LastInsertId()
	w.WriteHeader(http.StatusCreated)
	jsonOK(w, getIncidentByID(id))
}

// UpdateIncident — PATCH /api/incidents/{id}
func UpdateIncident(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	var body struct {
		Severity *string `json:"severity"`
		Kind     *string `json:"kind"`
		Title    *string `json:"title"`
		Summary  *string `json:"summary"`
		Details  *string `json:"details"`
		Status   *string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "invalid body", http.StatusBadRequest)
		return
	}
	if body.Severity != nil && !allowedIncidentSeverities[*body.Severity] {
		jsonError(w, "invalid severity", http.StatusBadRequest)
		return
	}
	if body.Status != nil && !allowedIncidentStatuses[*body.Status] {
		jsonError(w, "invalid status", http.StatusBadRequest)
		return
	}
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	// resolved_at flips automatically when the status transitions into a
	// terminal state and clears when it moves back to open/investigating.
	resolvedAt := "resolved_at"
	if body.Status != nil {
		switch *body.Status {
		case "resolved", "closed":
			resolvedAt = "COALESCE(resolved_at, ?)"
		case "open", "investigating":
			resolvedAt = "NULL"
		}
	}
	args := []any{
		body.Severity, body.Kind, body.Title, body.Summary, body.Details,
		body.Status,
	}
	if strings.Contains(resolvedAt, "?") {
		args = append(args, now)
	}
	args = append(args, now, id)
	_, err = db.DB.Exec(fmt.Sprintf(`
		UPDATE incident_log SET
			severity   = COALESCE(?, severity),
			kind       = COALESCE(?, kind),
			title      = COALESCE(?, title),
			summary    = COALESCE(?, summary),
			details    = COALESCE(?, details),
			status     = COALESCE(?, status),
			resolved_at= %s,
			updated_at = ?
		WHERE id = ?
	`, resolvedAt), args...)
	if err != nil {
		jsonError(w, "update failed", http.StatusInternalServerError)
		return
	}
	inc := getIncidentByID(id)
	if inc == nil {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	jsonOK(w, inc)
}

// DeleteIncident — DELETE /api/incidents/{id}
//
// Hard delete is intentional. Admins are the only callers, retention is
// addressed in PAI-117, and incident records are append-mostly anyway.
func DeleteIncident(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	if _, err := db.DB.Exec("DELETE FROM incident_log WHERE id=?", id); err != nil {
		jsonError(w, "delete failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ExportIncidents — GET /api/incidents/export?format=csv|json
//
// Streams the full table for offline review or SIEM ingestion. Default
// format is JSON; CSV is offered for spreadsheet-driven workflows.
func ExportIncidents(w http.ResponseWriter, r *http.Request) {
	format := strings.ToLower(r.URL.Query().Get("format"))
	if format == "" {
		format = "json"
	}
	rows, err := db.DB.Query(`
		SELECT id, severity, kind, title, summary, details, reported_by,
		       status, detected_at, resolved_at, created_at, updated_at
		FROM incident_log
		ORDER BY detected_at DESC, id DESC
	`)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	switch format {
	case "csv":
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="incidents.csv"`)
		cw := csv.NewWriter(w)
		_ = cw.Write([]string{
			"id", "severity", "kind", "title", "summary", "details",
			"reported_by", "status", "detected_at", "resolved_at",
			"created_at", "updated_at",
		})
		for rows.Next() {
			var inc Incident
			if err := rows.Scan(&inc.ID, &inc.Severity, &inc.Kind, &inc.Title,
				&inc.Summary, &inc.Details, &inc.ReportedBy, &inc.Status,
				&inc.DetectedAt, &inc.ResolvedAt, &inc.CreatedAt, &inc.UpdatedAt); err != nil {
				continue
			}
			rep := ""
			if inc.ReportedBy != nil {
				rep = strconv.FormatInt(*inc.ReportedBy, 10)
			}
			resolved := ""
			if inc.ResolvedAt != nil {
				resolved = *inc.ResolvedAt
			}
			_ = cw.Write([]string{
				strconv.FormatInt(inc.ID, 10), inc.Severity, inc.Kind, inc.Title,
				inc.Summary, inc.Details, rep, inc.Status,
				inc.DetectedAt, resolved, inc.CreatedAt, inc.UpdatedAt,
			})
		}
		cw.Flush()
	default:
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", `attachment; filename="incidents.json"`)
		out := []Incident{}
		for rows.Next() {
			var inc Incident
			if err := rows.Scan(&inc.ID, &inc.Severity, &inc.Kind, &inc.Title,
				&inc.Summary, &inc.Details, &inc.ReportedBy, &inc.Status,
				&inc.DetectedAt, &inc.ResolvedAt, &inc.CreatedAt, &inc.UpdatedAt); err != nil {
				continue
			}
			out = append(out, inc)
		}
		_ = json.NewEncoder(w).Encode(out)
	}
}

func getIncidentByID(id int64) *Incident {
	var inc Incident
	err := db.DB.QueryRow(`
		SELECT id, severity, kind, title, summary, details, reported_by,
		       status, detected_at, resolved_at, created_at, updated_at
		FROM incident_log WHERE id = ?
	`, id).Scan(&inc.ID, &inc.Severity, &inc.Kind, &inc.Title, &inc.Summary,
		&inc.Details, &inc.ReportedBy, &inc.Status, &inc.DetectedAt,
		&inc.ResolvedAt, &inc.CreatedAt, &inc.UpdatedAt)
	if err != nil {
		return nil
	}
	return &inc
}
