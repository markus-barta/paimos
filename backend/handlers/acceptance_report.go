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
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/markus-barta/paimos/backend/auth"
	"github.com/markus-barta/paimos/backend/db"
)

// GET /api/projects/{id}/acceptance-report?date=2026-03-25
// GET /api/portal/projects/{id}/acceptance-report?date=2026-03-25
// Returns standalone HTML summarizing acceptance activity for a given date.
func AcceptanceReport(w http.ResponseWriter, r *http.Request) {
	projectID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}

	// The portal route reaches this handler without a project-view
	// middleware, so re-check view access here. The internal route
	// already gates via RequireProjectView and this call is a cheap
	// cache hit, so there's no reason to skip it there.
	if !auth.CanViewProject(r, projectID) {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}

	date := r.URL.Query().Get("date")
	if date == "" {
		date = time.Now().UTC().Format("2006-01-02")
	}

	var projName, projKey string
	if err := db.DB.QueryRow("SELECT name, key FROM projects WHERE id=?", projectID).Scan(&projName, &projKey); err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	type reportItem struct {
		Key   string
		Title string
		// rejection details
		TaskKey     string
		TaskTitle   string
		IsRejected  bool
	}

	var accepted []reportItem
	var rejected []reportItem

	// Accepted on this date
	aRows, _ := db.DB.Query(`
		SELECT COALESCE(p.key || '-' || i.issue_number, ''), i.title
		FROM issues i
		LEFT JOIN projects p ON p.id = i.project_id
		WHERE i.project_id = ? AND i.accepted_at LIKE ?
		ORDER BY i.accepted_at
	`, projectID, date+"%")
	if aRows != nil {
		for aRows.Next() {
			var it reportItem
			if err := aRows.Scan(&it.Key, &it.Title); err != nil {
				log.Printf("scan error: %v", err)
				continue
			}
			accepted = append(accepted, it)
		}
		aRows.Close()
	}

	// Rejected on this date
	rRows, _ := db.DB.Query(`
		SELECT COALESCE(pp.key || '-' || parent.issue_number, ''),
		       parent.title,
		       COALESCE(pp.key || '-' || i.issue_number, ''),
		       i.title
		FROM issues i
		JOIN issues parent ON parent.id = i.parent_id
		LEFT JOIN projects pp ON pp.id = i.project_id
		WHERE i.project_id = ? AND i.notes = '[portal rejection]' AND i.created_at LIKE ?
		ORDER BY i.created_at
	`, projectID, date+"%")
	if rRows != nil {
		for rRows.Next() {
			var it reportItem
			if err := rRows.Scan(&it.Key, &it.Title, &it.TaskKey, &it.TaskTitle); err != nil {
				log.Printf("scan error: %v", err)
				continue
			}
			it.IsRejected = true
			rejected = append(rejected, it)
		}
		rRows.Close()
	}

	// Build HTML
	var sb strings.Builder
	sb.WriteString(`<!DOCTYPE html><html><head><meta charset="utf-8">`)
	sb.WriteString(fmt.Sprintf(`<title>Acceptance Report — %s — %s</title>`, projKey, date))
	sb.WriteString(`<style>
		body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif; max-width: 700px; margin: 2rem auto; padding: 0 1rem; color: #1a2636; }
		h1 { font-size: 20px; margin-bottom: .25rem; }
		.subtitle { color: #637383; font-size: 13px; margin-bottom: 1.5rem; }
		h2 { font-size: 15px; border-bottom: 2px solid #d1dce8; padding-bottom: .35rem; margin: 1.5rem 0 .75rem; }
		.accepted h2 { border-color: #16a34a; color: #16a34a; }
		.rejected h2 { border-color: #c0392b; color: #c0392b; }
		table { width: 100%; border-collapse: collapse; font-size: 13px; }
		th { text-align: left; font-size: 11px; font-weight: 600; text-transform: uppercase; letter-spacing: .04em; color: #637383; padding: .4rem .6rem; border-bottom: 1px solid #d1dce8; }
		td { padding: .5rem .6rem; border-bottom: 1px solid #eee; }
		.key { font-weight: 600; color: #2e6da4; white-space: nowrap; }
		.summary { margin-top: 2rem; padding: 1rem; background: #f2f5f8; border-radius: 6px; font-size: 13px; }
		.empty { color: #637383; font-style: italic; }
		@media print { body { max-width: 100%; } }
	</style></head><body>`)
	sb.WriteString(fmt.Sprintf(`<h1>Acceptance Report</h1>`))
	sb.WriteString(fmt.Sprintf(`<p class="subtitle">%s — %s</p>`, projName, date))

	// Accepted
	sb.WriteString(`<div class="accepted"><h2>Accepted</h2>`)
	if len(accepted) == 0 {
		sb.WriteString(`<p class="empty">No issues accepted on this date.</p>`)
	} else {
		sb.WriteString(`<table><thead><tr><th>Key</th><th>Title</th></tr></thead><tbody>`)
		for _, a := range accepted {
			sb.WriteString(fmt.Sprintf(`<tr><td class="key">%s</td><td>%s</td></tr>`, a.Key, a.Title))
		}
		sb.WriteString(`</tbody></table>`)
	}
	sb.WriteString(`</div>`)

	// Rejected
	sb.WriteString(`<div class="rejected"><h2>Rejected</h2>`)
	if len(rejected) == 0 {
		sb.WriteString(`<p class="empty">No issues rejected on this date.</p>`)
	} else {
		sb.WriteString(`<table><thead><tr><th>Key</th><th>Title</th><th>Problem</th></tr></thead><tbody>`)
		for _, r := range rejected {
			sb.WriteString(fmt.Sprintf(`<tr><td class="key">%s</td><td>%s</td><td>%s</td></tr>`, r.Key, r.Title, r.TaskTitle))
		}
		sb.WriteString(`</tbody></table>`)
	}
	sb.WriteString(`</div>`)

	// Summary
	sb.WriteString(fmt.Sprintf(`<div class="summary"><strong>Summary:</strong> %d accepted, %d rejected</div>`, len(accepted), len(rejected)))
	sb.WriteString(`</body></html>`)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(sb.String()))
}
