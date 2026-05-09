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

// PAI-347 — decay-based archive proposals. The endpoint surfaces
// memory entries that look stale (no reference in the last N days
// AND confidence ≤ medium AND no originating ticket currently in
// 'in-progress' or 'qa'). The user always gets the final say —
// the response is *proposals*, never auto-archives. The Knowledge
// tab consumes this from a "Stale memory" view; one click archives
// (existing flow), one click resets the clock (a write to
// last_referenced_at).
//
// Backwards-compat (per the ticket): a memory entry without an
// explicit last_referenced_at is treated as freshly referenced via
// COALESCE(last_referenced_at, created_at). Without that, the day
// M100 lands every existing memory immediately turns into a stale
// proposal — which is not the intent (the proposal mechanism only
// kicks in once we *observe* lack of references after the tracking
// is in place).

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/markus-barta/paimos/backend/db"
)

// StaleMemoryProposal is the on-the-wire shape returned by the
// `/memory/stale` endpoint. Mirrors the knowledge convenience
// endpoints' shape (id + slug + title + body + metadata) plus the
// fields specific to the stale-decision: reference_count,
// last_referenced_at, and the explicit `reason` string the UI can
// surface so the user understands why each row is on the list.
type StaleMemoryProposal struct {
	ID               int64          `json:"id"`
	ProjectID        int64          `json:"project_id"`
	Slug             string         `json:"slug"`
	Title            string         `json:"title"`
	Body             string         `json:"body"`
	Status           string         `json:"status"`
	Metadata         map[string]any `json:"metadata"`
	Confidence       string         `json:"confidence"`
	ReferenceCount   int64          `json:"reference_count"`
	LastReferencedAt string         `json:"last_referenced_at,omitempty"`
	CreatedAt        string         `json:"created_at"`
	UpdatedAt        string         `json:"updated_at"`
	DaysSinceRef     int            `json:"days_since_reference"`
}

// defaultStaleDays is the threshold the ticket calls out — 90 days
// without a reference is the "default" stale window. Callers can
// override via ?days=N. We clamp at 1 day (anything lower is almost
// certainly a typo) to avoid nonsense responses.
const defaultStaleDays = 90

// activeIssueStatuses is the set of statuses that mean an issue is
// "in flight" — a memory linked to such an issue is still relevant
// even if its own counter hasn't moved recently.
var activeIssueStatuses = []string{"in-progress", "qa"}

// ListStaleMemory powers GET /api/projects/:id/memory/stale.
//
// Query: ?days=N (default 90, min 1).
//
// Returns a JSON array of StaleMemoryProposal. Empty array (never
// null) when no rows match — the typical case on a fresh install.
func ListStaleMemory(w http.ResponseWriter, r *http.Request) {
	projectID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || projectID <= 0 {
		jsonError(w, "invalid project id", http.StatusBadRequest)
		return
	}
	days := defaultStaleDays
	if raw := strings.TrimSpace(r.URL.Query().Get("days")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			days = n
		}
	}

	out, err := loadStaleMemoryProposals(projectID, days)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	jsonOK(w, out)
}

// loadStaleMemoryProposals is the SQL-side workhorse for the
// endpoint. Pulls the project's live memory entries, filters them
// in Go (per-row category_metadata JSON inspection is cheaper than
// SQLite JSON1 path predicates for a 30–60 row corpus), and returns
// the proposal slice.
//
// The candidate rows are buffered into a slice before the per-row
// follow-up lookups (originating-ticket status) so we don't hold
// the outer rows-iterator open while issuing dependent queries —
// SQLite's WAL mode allows concurrent reads but reusing the same
// connection from a hot iterator can SQLITE_BUSY the dependent
// query under contention.
func loadStaleMemoryProposals(projectID int64, days int) ([]StaleMemoryProposal, error) {
	rows, err := db.DB.Query(`
		SELECT i.id, i.project_id, COALESCE(i.slug, ''),
		       i.title, i.description, i.status,
		       COALESCE(i.category_metadata, ''),
		       i.reference_count,
		       COALESCE(i.last_referenced_at, ''),
		       i.created_at, i.updated_at,
		       CAST(julianday('now') -
		            julianday(COALESCE(i.last_referenced_at, i.created_at))
		            AS INTEGER) AS days_since_ref
		  FROM issues i
		 WHERE i.project_id = ?
		   AND i.type       = 'memory'
		   AND i.deleted_at IS NULL
		   AND i.slug       IS NOT NULL
	`, projectID)
	if err != nil {
		return nil, err
	}
	type row struct {
		p          StaleMemoryProposal
		metaRaw    string
		lastRefStr string
	}
	buffered := []row{}
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.p.ID, &r.p.ProjectID, &r.p.Slug, &r.p.Title, &r.p.Body,
			&r.p.Status, &r.metaRaw, &r.p.ReferenceCount, &r.lastRefStr,
			&r.p.CreatedAt, &r.p.UpdatedAt, &r.p.DaysSinceRef); err != nil {
			rows.Close()
			return nil, err
		}
		buffered = append(buffered, r)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	out := []StaleMemoryProposal{}
	for _, r := range buffered {
		p := r.p
		// Parse metadata leniently — corrupt JSON is treated as
		// "no metadata" rather than failing the whole query, mirroring
		// the knowledge module's UnmarshalMetaDefault behaviour.
		p.Metadata = map[string]any{}
		if r.metaRaw != "" {
			_ = json.Unmarshal([]byte(r.metaRaw), &p.Metadata)
		}
		p.Confidence = confidenceFromMeta(p.Metadata)
		p.LastReferencedAt = r.lastRefStr

		// Condition 1 — must be older than `days`.
		if p.DaysSinceRef < days {
			continue
		}
		// Condition 2 — confidence must be low or medium (or unscored,
		// which we treat as medium per the backwards-compat rule).
		if p.Confidence == "high" {
			continue
		}
		// Condition 3 — no originating ticket currently in-progress / qa.
		hasActive, err := hasActiveOriginatingTicket(p.ID, p.Metadata, projectID)
		if err != nil {
			return nil, err
		}
		if hasActive {
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

// confidenceFromMeta extracts the confidence value from a parsed
// category_metadata map. Per the PAI-347 spec, missing / unknown
// values default to "medium" so existing memory entries pass through
// the "≤ medium" gate unchanged.
func confidenceFromMeta(meta map[string]any) string {
	if meta == nil {
		return "medium"
	}
	if v, ok := meta["confidence"]; ok {
		if s, ok := v.(string); ok {
			s = strings.TrimSpace(strings.ToLower(s))
			switch s {
			case "high", "medium", "low":
				return s
			}
		}
	}
	return "medium"
}

// hasActiveOriginatingTicket returns true when the memory has at
// least one `originating_tickets[]` entry — *or* an applies_to_memory
// graph link from a ticket — pointing at an issue in
// 'in-progress' or 'qa' status. Both surfaces are checked so the
// stale check matches the documented contract regardless of which
// linking style the project uses (PAI-338's free-text array vs.
// PAI-342's relation graph).
func hasActiveOriginatingTicket(memoryID int64, meta map[string]any, projectID int64) (bool, error) {
	// Path 1 — PAI-342 relation graph. issue → memory rows of type
	// applies_to_memory; we look up the ticket side and check status.
	rows, err := db.DB.Query(`
		SELECT t.status
		  FROM issue_relations ir
		  JOIN issues t ON t.id = ir.source_id
		 WHERE ir.target_id = ?
		   AND ir.type      = 'applies_to_memory'
		   AND t.deleted_at IS NULL
	`, memoryID)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		if err := rows.Scan(&status); err != nil {
			return false, err
		}
		if isActiveIssueStatus(status) {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, err
	}

	// Path 2 — PAI-338 free-text originating_tickets[]. Each entry
	// is an issue key (e.g. "PAI-339"); resolve to the local issue
	// row when the project key matches and check status. Cross-
	// instance keys can't be checked here — they're treated as
	// "not active" so they don't suppress proposals indefinitely.
	keys := stringArrayFromMeta(meta, "originating_tickets")
	for _, key := range keys {
		status, ok, err := resolveIssueKeyStatus(key, projectID)
		if err != nil {
			return false, err
		}
		if !ok {
			continue
		}
		if isActiveIssueStatus(status) {
			return true, nil
		}
	}
	return false, nil
}

// stringArrayFromMeta pulls a string-array field out of a generic
// metadata map. Returns an empty slice on missing / wrong-type fields
// so the caller can iterate without nil checks.
func stringArrayFromMeta(meta map[string]any, key string) []string {
	if meta == nil {
		return nil
	}
	raw, ok := meta[key]
	if !ok {
		return nil
	}
	arr, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, v := range arr {
		if s, ok := v.(string); ok {
			s = strings.TrimSpace(s)
			if s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

// isActiveIssueStatus reports whether `status` is one of the
// configured "in-flight" statuses that suppress stale proposals.
func isActiveIssueStatus(status string) bool {
	status = strings.TrimSpace(strings.ToLower(status))
	for _, s := range activeIssueStatuses {
		if s == status {
			return true
		}
	}
	return false
}

// resolveIssueKeyStatus parses an issue key like "PAI-339" into a
// project-key + issue-number pair, looks up the issue's status, and
// returns it. ok=false when the key doesn't resolve in this project
// or in another project on this instance — cross-instance refs fall
// here and are treated as "unresolvable, can't suppress".
func resolveIssueKeyStatus(key string, projectID int64) (string, bool, error) {
	parts := strings.SplitN(key, "-", 2)
	if len(parts) != 2 {
		return "", false, nil
	}
	projectKey := strings.TrimSpace(parts[0])
	num, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil || num <= 0 {
		return "", false, nil
	}
	// Look up the issue by (project_key, number). The same-project
	// fast path is the common case but we don't constrain to the
	// memory's project — a cross-project originating ticket is still
	// in this instance and its status should suppress the proposal.
	_ = projectID
	var status string
	err = db.DB.QueryRow(`
		SELECT i.status
		  FROM issues i
		  JOIN projects p ON p.id = i.project_id
		 WHERE p.key = ?
		   AND i.issue_number = ?
		   AND i.deleted_at IS NULL
	`, projectKey, num).Scan(&status)
	if err != nil {
		// sql.ErrNoRows → unresolvable, return ok=false.
		return "", false, nil
	}
	return status, true, nil
}
