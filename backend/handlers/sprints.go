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

// SprintSummary is a lightweight representation used by the filter picker and settings UI.
type SprintSummary struct {
	ID          int64    `json:"id"`
	Title       string   `json:"title"`
	StartDate   string   `json:"start_date"`
	EndDate     string   `json:"end_date"`
	Archived    bool     `json:"archived"`
	SprintState string   `json:"sprint_state"`
	TargetAR    *float64 `json:"target_ar"`
}

// GET /api/sprints[?include_archived=true]
// Returns all sprint issues (type=sprint) ordered by start_date ASC, then title.
// Archived sprints excluded unless include_archived=true.
func ListSprints(w http.ResponseWriter, r *http.Request) {
	includeArchived := r.URL.Query().Get("include_archived") == "true"

	query := `
		SELECT id, title, start_date, end_date, archived, sprint_state, target_ar
		FROM issues
		WHERE type = 'sprint'`
	if !includeArchived {
		query += ` AND archived = 0`
	}
	query += ` ORDER BY start_date ASC, title ASC`

	rows, err := db.DB.Query(query)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	sprints := []SprintSummary{}
	for rows.Next() {
		var s SprintSummary
		var archivedInt int
		if err := rows.Scan(&s.ID, &s.Title, &s.StartDate, &s.EndDate, &archivedInt, &s.SprintState, &s.TargetAR); err != nil {
			continue
		}
		s.Archived = archivedInt == 1
		sprints = append(sprints, s)
	}
	jsonOK(w, sprints)
}

// PATCH /api/issues/:id/archive  — admin only
// Toggles archived flag. Body: { "archived": true|false }
func ArchiveIssue(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	var body struct {
		Archived bool `json:"archived"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "invalid body", http.StatusBadRequest)
		return
	}
	val := 0
	if body.Archived {
		val = 1
	}
	res, err := db.DB.Exec(`UPDATE issues SET archived = ?, updated_at = datetime('now') WHERE id = ?`, val, id)
	if err != nil {
		jsonError(w, "update failed", http.StatusInternalServerError)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		jsonError(w, "issue not found", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// PUT /api/sprints/:id — admin only. Edit sprint title, dates, state.
func UpdateSprint(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}

	// Verify it's actually a sprint
	var issueType string
	if err := db.DB.QueryRow("SELECT type FROM issues WHERE id=?", id).Scan(&issueType); err != nil {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	if issueType != "sprint" {
		jsonError(w, "not a sprint", http.StatusBadRequest)
		return
	}

	var body struct {
		Title       *string  `json:"title"`
		StartDate   *string  `json:"start_date"`
		EndDate     *string  `json:"end_date"`
		SprintState *string  `json:"sprint_state"`
		TargetAR    *float64 `json:"target_ar"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "invalid body", http.StatusBadRequest)
		return
	}

	_, err = db.DB.Exec(`
		UPDATE issues SET
			title        = COALESCE(?, title),
			start_date   = COALESCE(?, start_date),
			end_date     = COALESCE(?, end_date),
			sprint_state = COALESCE(?, sprint_state),
			target_ar    = COALESCE(?, target_ar),
			updated_at   = datetime('now')
		WHERE id = ?
	`, body.Title, body.StartDate, body.EndDate, body.SprintState, body.TargetAR, id)
	if err != nil {
		jsonError(w, "update failed", http.StatusInternalServerError)
		return
	}

	// Return updated sprint summary
	var s SprintSummary
	if err := db.DB.QueryRow(`
		SELECT id, title, COALESCE(start_date,''), COALESCE(end_date,''), archived, COALESCE(sprint_state,''), target_ar
		FROM issues WHERE id = ?
	`, id).Scan(&s.ID, &s.Title, &s.StartDate, &s.EndDate, &s.Archived, &s.SprintState, &s.TargetAR); err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	jsonOK(w, s)
}

// POST /api/sprints/:id/move-incomplete — move incomplete items to the next sprint.
func MoveIncompleteToNextSprint(w http.ResponseWriter, r *http.Request) {
	sprintID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}

	// Get current sprint's end date to find the next sprint
	var endDate string
	if err := db.DB.QueryRow(`SELECT COALESCE(end_date,'') FROM issues WHERE id=? AND type='sprint'`, sprintID).Scan(&endDate); err != nil {
		jsonError(w, "sprint not found", http.StatusNotFound)
		return
	}

	// Find next sprint by start_date > current sprint's start_date
	var nextSprintID int64
	err = db.DB.QueryRow(`
		SELECT id FROM issues
		WHERE type='sprint' AND id != ? AND COALESCE(start_date,'') > COALESCE((SELECT start_date FROM issues WHERE id=?), '')
		ORDER BY start_date ASC LIMIT 1
	`, sprintID, sprintID).Scan(&nextSprintID)
	if err != nil {
		jsonError(w, "no next sprint found", http.StatusUnprocessableEntity)
		return
	}

	// Find incomplete issues in current sprint
	rows, err := db.DB.Query(`
		SELECT i.id, COALESCE(p.key || '-' || CAST(i.issue_number AS TEXT), ''), i.title, i.status
		FROM issue_relations ir
		JOIN issues i ON i.id = ir.target_id
		LEFT JOIN projects p ON p.id = i.project_id
		WHERE ir.source_id = ? AND ir.type = 'sprint'
		  AND i.status NOT IN ('done','delivered','accepted','invoiced','cancelled')
	`, sprintID)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type MovedIssue struct {
		ID     int64  `json:"id"`
		Key    string `json:"issue_key"`
		Title  string `json:"title"`
		Status string `json:"status"`
	}
	var moved []MovedIssue
	for rows.Next() {
		var m MovedIssue
		if err := rows.Scan(&m.ID, &m.Key, &m.Title, &m.Status); err == nil {
			moved = append(moved, m)
		}
	}

	if len(moved) == 0 {
		jsonOK(w, map[string]any{"moved": []MovedIssue{}, "count": 0})
		return
	}

	// Move each issue: remove from current sprint, add to next (idempotent).
	// Wrapped in a transaction so a partial failure rolls back cleanly.
	tx, err := db.DB.Begin()
	if err != nil {
		log.Printf("MoveIncomplete: begin tx sprint=%d: %v", sprintID, err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	for _, m := range moved {
		if _, err := tx.Exec(`DELETE FROM issue_relations WHERE source_id=? AND target_id=? AND type='sprint'`, sprintID, m.ID); err != nil {
			log.Printf("MoveIncomplete: delete relation sprint=%d issue=%d: %v", sprintID, m.ID, err)
			tx.Rollback()
			jsonError(w, "internal error", http.StatusInternalServerError)
			return
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO issue_relations(source_id, target_id, type) VALUES(?,?,?)`, nextSprintID, m.ID, "sprint"); err != nil {
			log.Printf("MoveIncomplete: insert relation sprint=%d issue=%d: %v", nextSprintID, m.ID, err)
			tx.Rollback()
			jsonError(w, "internal error", http.StatusInternalServerError)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		log.Printf("MoveIncomplete: commit tx sprint=%d: %v", sprintID, err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	jsonOK(w, map[string]any{"moved": moved, "count": len(moved), "next_sprint_id": nextSprintID})
}

// formatSprintTitle applies a title format template.
//
// Quoted sections (double-quotes) are treated as literals — their content is
// preserved exactly, quotes stripped. Unquoted tokens are substituted:
//   YYYY → 4-digit year   YY → 2-digit year   NN → zero-padded sprint number
//
// Examples (year=2026, num=1):
//   YY"S"NN              → 26S01
//   "Sprint "NN          → Sprint 01
//   YYYY"-W"NN           → 2026-W01
//   NN                   → 01  (no literals)
func formatSprintTitle(format string, year, num int) string {
	yy   := fmt.Sprintf("%02d", year%100)
	yyyy := fmt.Sprintf("%d", year)
	nn   := fmt.Sprintf("%02d", num)

	var result strings.Builder
	i := 0
	for i < len(format) {
		if format[i] == '"' {
			// Consume until closing quote (no escape support needed)
			i++ // skip opening quote
			for i < len(format) && format[i] != '"' {
				result.WriteByte(format[i])
				i++
			}
			if i < len(format) {
				i++ // skip closing quote
			}
		} else {
			// Try to match tokens (longest first)
			switch {
			case strings.HasPrefix(format[i:], "YYYY"):
				result.WriteString(yyyy)
				i += 4
			case strings.HasPrefix(format[i:], "YY"):
				result.WriteString(yy)
				i += 2
			case strings.HasPrefix(format[i:], "NN"):
				result.WriteString(nn)
				i += 2
			default:
				result.WriteByte(format[i])
				i++
			}
		}
	}
	return result.String()
}

// POST /api/sprints/batch  — admin only
// Body: { "first_day": "2026-01-05", "duration_days": 14, "year": 2026, "title_format": "YYsNN" }
// Computes full sprint schedule for the year, inserts sprints that don't exist
// (matched by title). Returns { created: N, skipped: ["26s01", ...] }.
func CreateSprintsBatch(w http.ResponseWriter, r *http.Request) {
	var body struct {
		FirstDay     string `json:"first_day"`     // e.g. "2026-01-05"
		DurationDays int    `json:"duration_days"` // e.g. 14
		Year         int    `json:"year"`          // e.g. 2026
		TitleFormat  string `json:"title_format"`  // e.g. "YYsNN" (default)
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.FirstDay == "" {
		jsonError(w, "first_day required", http.StatusBadRequest)
		return
	}
	if body.DurationDays <= 0 {
		body.DurationDays = 14
	}
	if body.Year <= 0 {
		body.Year = time.Now().Year()
	}
	if body.TitleFormat == "" {
		body.TitleFormat = `YY"S"NN`
	}

	firstDay, err := time.Parse("2006-01-02", body.FirstDay)
	if err != nil {
		jsonError(w, "first_day must be YYYY-MM-DD", http.StatusBadRequest)
		return
	}

	// Year boundaries
	yearStart := time.Date(body.Year, 1, 1, 0, 0, 0, 0, time.UTC)
	yearEnd   := time.Date(body.Year, 12, 31, 23, 59, 59, 0, time.UTC)

	type sprint struct {
		title string
		start time.Time
		end   time.Time
	}
	var sprints []sprint

	dur := time.Duration(body.DurationDays) * 24 * time.Hour

	// Sprint 0: leftover days from Jan 1 to day before firstDay (if any)
	if firstDay.After(yearStart) {
		sprint0End := firstDay.AddDate(0, 0, -1)
		if sprint0End.After(yearEnd) {
			sprint0End = yearEnd
		}
		sprints = append(sprints, sprint{
			title: formatSprintTitle(body.TitleFormat, body.Year, 0),
			start: yearStart,
			end:   sprint0End,
		})
	}

	// Numbered sprints
	sprintNum := 1
	cur := firstDay
	for !cur.After(yearEnd) {
		end := cur.Add(dur).AddDate(0, 0, -1)
		if end.After(yearEnd) {
			end = yearEnd
		}
		sprints = append(sprints, sprint{
			title: formatSprintTitle(body.TitleFormat, body.Year, sprintNum),
			start: cur,
			end:   end,
		})
		sprintNum++
		cur = cur.Add(dur)
	}

	// Fetch existing sprint titles to detect duplicates
	rows, err := db.DB.Query(`SELECT title FROM issues WHERE type='sprint'`)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	existing := map[string]bool{}
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			log.Printf("scan error: %v", err)
			continue
		}
		existing[t] = true
	}
	rows.Close()

	created := 0
	var skipped []string

	for _, s := range sprints {
		if existing[s.title] {
			skipped = append(skipped, s.title)
			continue
		}
		_, err := db.DB.Exec(`
			INSERT INTO issues(project_id, issue_number, type, title, status, priority,
			                   start_date, end_date, archived)
			VALUES(NULL, 0, 'sprint', ?, 'backlog', 'medium', ?, ?, 0)
		`, s.title,
			s.start.Format("2006-01-02"),
			s.end.Format("2006-01-02"),
		)
		if err != nil {
			log.Printf("CreateSprintsBatch: insert %q: %v", s.title, err)
			continue
		}
		created++
	}

	if skipped == nil {
		skipped = []string{}
	}

	jsonOK(w, map[string]any{
		"created": created,
		"skipped": skipped,
	})
}

// GET /api/sprints/:year — returns all sprints for a given year (all, including archived)
func ListSprintsByYear(w http.ResponseWriter, r *http.Request) {
	year := chi.URLParam(r, "year")
	rows, err := db.DB.Query(`
		SELECT id, title, start_date, end_date, archived, sprint_state, target_ar
		FROM issues
		WHERE type = 'sprint'
		  AND (start_date LIKE ? OR title LIKE ?)
		ORDER BY start_date ASC, title ASC
	`, year+"%", "%"+year)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	sprints := []SprintSummary{}
	for rows.Next() {
		var s SprintSummary
		var archivedInt int
		if err := rows.Scan(&s.ID, &s.Title, &s.StartDate, &s.EndDate, &archivedInt, &s.SprintState, &s.TargetAR); err != nil {
			continue
		}
		s.Archived = archivedInt == 1
		sprints = append(sprints, s)
	}
	jsonOK(w, sprints)
}

// GET /api/sprints/years — distinct years that have sprints
func ListSprintYears(w http.ResponseWriter, r *http.Request) {
	_ = auth.GetUser(r) // ensure auth middleware ran
	rows, err := db.DB.Query(`
		SELECT DISTINCT CAST(strftime('%Y', start_date) AS INTEGER) AS yr
		FROM issues
		WHERE type = 'sprint' AND start_date != ''
		ORDER BY yr DESC
	`)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	years := []int{}
	for rows.Next() {
		var y int
		if err := rows.Scan(&y); err != nil {
			log.Printf("scan error: %v", err)
			continue
		}
		years = append(years, y)
	}
	jsonOK(w, years)
}

// ReorderSprintMembers updates the rank of issues within a sprint.
// PUT /api/sprints/{id}/reorder
// Body: {"issue_ids": [4, 7, 2, 9]} — new order, sequential ranks assigned
func ReorderSprintMembers(w http.ResponseWriter, r *http.Request) {
	sprintID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	var body struct {
		IssueIDs []int64 `json:"issue_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.IssueIDs) == 0 {
		jsonError(w, "issue_ids required", http.StatusBadRequest)
		return
	}
	tx, err := db.DB.Begin()
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	for i, id := range body.IssueIDs {
		if _, err := tx.Exec(
			"UPDATE issue_relations SET rank=? WHERE source_id=? AND target_id=? AND type='sprint'",
			i, sprintID, id,
		); err != nil {
			log.Printf("ReorderSprintMembers: update rank sprint=%d issue=%d: %v", sprintID, id, err)
			tx.Rollback()
			jsonError(w, "internal error", http.StatusInternalServerError)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		log.Printf("ReorderSprintMembers: commit: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
