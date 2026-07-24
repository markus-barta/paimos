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

// PAI-349 — stale proposed-memory surface. Endpoint is read-only:
// admins one-click bulk-archive (or per-row) by issuing the existing
// PUT /api/projects/:id/memory/:slug with status='cancelled' and
// `category_metadata.archived_reason='stale'`.
//
// v1 deliberately does NOT auto-archive on the server. The ticket
// calls out "don't auto-archive without admin confirmation"; the
// endpoint surfaces the candidate list, the human (or the admin UI)
// drives the actual archive transition.
//
// The threshold is `?days=N` (default 30, configurable instance-wide
// via PAIMOS_PROPOSE_STALE_DAYS). Runs entirely on `updated_at` since
// proposed entries don't carry a separate "last-touched" timestamp;
// the existing column already moves on every PUT (including the
// metadata edits human reviewers do during the review flow).

import (
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/inspr-at/paimos/backend/db"
)

// StaleProposedProposal mirrors the StaleMemoryProposal shape (PAI-347)
// for the proposed-memory case. The two stay separate even though they
// look similar because the criteria differ — stale proposals are about
// "no human reviewed this draft in N days", stale active memory is
// about "no agent referenced this rule in N days".
type StaleProposedProposal struct {
	ID                 int64          `json:"id"`
	ProjectID          int64          `json:"project_id"`
	Slug               string         `json:"slug"`
	Title              string         `json:"title"`
	Body               string         `json:"body"`
	Status             string         `json:"status"`
	Metadata           map[string]any `json:"metadata"`
	CreatedAt          string         `json:"created_at"`
	UpdatedAt          string         `json:"updated_at"`
	DaysSinceUpdate    int            `json:"days_since_update"`
}

// staleProposedDaysFromEnv returns the configured stale threshold for
// proposed entries. Honors PAIMOS_PROPOSE_STALE_DAYS, falls back to
// the default (30). The endpoint accepts a per-request `?days=N`
// override that wins over the env value.
func staleProposedDaysFromEnv() int {
	raw := strings.TrimSpace(os.Getenv("PAIMOS_PROPOSE_STALE_DAYS"))
	if raw == "" {
		return defaultProposeStaleDays
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return defaultProposeStaleDays
	}
	return n
}

// ListStaleProposedMemory powers GET /api/projects/:id/memory/proposed/stale.
// Returns the proposed memory entries the admin should consider
// archiving (untouched for ≥ N days). Empty array (never null) when
// nothing matches — the typical case on a fresh / well-tended project.
func ListStaleProposedMemory(w http.ResponseWriter, r *http.Request) {
	projectID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || projectID <= 0 {
		jsonError(w, "invalid project id", http.StatusBadRequest)
		return
	}
	days := staleProposedDaysFromEnv()
	if raw := strings.TrimSpace(r.URL.Query().Get("days")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			days = n
		}
	}
	out, err := loadStaleProposedProposals(projectID, days)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	jsonOK(w, out)
}

// loadStaleProposedProposals is the SQL workhorse — single SELECT,
// returns the proposals as a value slice. The `days_since_update`
// column is computed in-DB so we don't need a second pass in Go.
func loadStaleProposedProposals(projectID int64, days int) ([]StaleProposedProposal, error) {
	rows, err := db.DB.Query(`
		SELECT i.id, i.project_id, COALESCE(i.slug, ''),
		       i.title, i.description, i.status,
		       COALESCE(i.category_metadata, ''),
		       i.created_at, i.updated_at,
		       CAST(julianday('now') - julianday(i.updated_at) AS INTEGER) AS days_since_update
		  FROM issues i
		 WHERE i.project_id = ?
		   AND i.type       = 'memory'
		   AND i.status     = 'proposed'
		   AND i.deleted_at IS NULL
		   AND i.slug       IS NOT NULL
		   AND CAST(julianday('now') - julianday(i.updated_at) AS INTEGER) >= ?
		 ORDER BY i.updated_at ASC
	`, projectID, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []StaleProposedProposal{}
	for rows.Next() {
		var (
			p       StaleProposedProposal
			metaRaw string
		)
		if err := rows.Scan(&p.ID, &p.ProjectID, &p.Slug, &p.Title, &p.Body,
			&p.Status, &metaRaw, &p.CreatedAt, &p.UpdatedAt, &p.DaysSinceUpdate); err != nil {
			return nil, err
		}
		p.Metadata = map[string]any{}
		if metaRaw != "" {
			_ = json.Unmarshal([]byte(metaRaw), &p.Metadata)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
