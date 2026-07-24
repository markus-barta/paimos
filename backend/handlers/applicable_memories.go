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
	"database/sql"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/inspr-at/paimos/backend/db"
)

// ── PAI-342: Applicable memories on issues ────────────────────────────────
//
// `applicable_memories[]` is the set of memory entries that should
// surface when working on a ticket. Stored as rows in issue_relations
// with type='applies_to_memory' (M97). The reverse direction
// (memory → originating tickets) is the same table read from the
// other side — no second row, no JSON-column duplication.
//
// GET /api/issues/:id/applicable-memories
//   - default: returns the manually-linked set
//   - ?suggest=1: returns up to 3 candidates not yet linked, scored
//     against the ticket's tags / parent epic / release/sprint metadata.
//
// Mutations reuse POST/DELETE /api/issues/:id/relations with type=
// 'applies_to_memory'. Round-trip is symmetric: the same row that
// surfaces under the issue's applicable_memories also surfaces under
// the memory's originating-tickets list.

// ApplicableMemory is the API shape returned by the new endpoint.
// Keeps the surface narrow — the UI only needs slug + title +
// preview to render a card and a route target. Score / matched are
// populated only for the suggest=1 path so the manual-list path
// stays cheap (and the JSON shape stays compact).
type ApplicableMemory struct {
	ID         int64    `json:"id"`
	ProjectID  int64    `json:"project_id"`
	ProjectKey string   `json:"project_key,omitempty"`
	Slug       string   `json:"slug"`
	Title      string   `json:"title"`
	Preview    string   `json:"preview,omitempty"` // first non-empty body line
	IssueKey   string   `json:"issue_key,omitempty"`
	Score      int      `json:"score,omitempty"`   // suggest=1 only
	Matched    []string `json:"matched,omitempty"` // suggest=1 only
}

// ListApplicableMemories powers GET /api/issues/:id/applicable-memories.
// Without `?suggest=1` it returns the manually-curated set, ordered
// by slug for stable rendering. With `?suggest=1` it returns top-3
// candidates the user hasn't linked yet, scored against the ticket's
// tags / parent epic / release-sprint metadata (see scoreCandidate).
//
// The endpoint is read-only — the writes go through the existing
// POST/DELETE /api/issues/:id/relations path with type=
// 'applies_to_memory'. That keeps the relation history / mutation
// log / undo flow consistent with every other relation type.
func ListApplicableMemories(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	suggest := r.URL.Query().Get("suggest") == "1"
	if suggest {
		out, err := buildSuggestions(id)
		if err != nil {
			jsonError(w, "query failed", http.StatusInternalServerError)
			return
		}
		jsonOK(w, out)
		return
	}
	out, err := loadLinkedMemories(id)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	jsonOK(w, out)
}

// loadLinkedMemories returns the memory entries already linked to
// the issue via issue_relations(type='applies_to_memory'). The
// source side of the row is always the ticket so the JOIN here is
// one-directional. Cross-project links work out of the box because
// issue_relations doesn't constrain the target's project_id.
func loadLinkedMemories(issueID int64) ([]ApplicableMemory, error) {
	rows, err := db.DB.Query(`
		SELECT m.id, m.project_id, COALESCE(p.key, ''),
		       COALESCE(m.slug, ''), m.title, m.description,
		       m.issue_number
		  FROM issue_relations ir
		  JOIN issues   m ON m.id = ir.target_id
		  LEFT JOIN projects p ON p.id = m.project_id
		 WHERE ir.source_id = ?
		   AND ir.type      = 'applies_to_memory'
		   AND m.type       = 'memory'
		   AND m.deleted_at IS NULL
		 ORDER BY m.slug ASC
	`, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ApplicableMemory{}
	for rows.Next() {
		var (
			am     ApplicableMemory
			body   string
			issNum int
		)
		if err := rows.Scan(&am.ID, &am.ProjectID, &am.ProjectKey,
			&am.Slug, &am.Title, &body, &issNum); err != nil {
			return nil, err
		}
		am.Preview = previewLine(body)
		if am.ProjectKey != "" && issNum > 0 {
			am.IssueKey = am.ProjectKey + "-" + strconv.Itoa(issNum)
		}
		out = append(out, am)
	}
	return out, rows.Err()
}

// buildSuggestions returns up to 3 candidate memories the issue is
// not yet linked to, scored by the v1 algorithm in PAI-342:
//
//  1. Memory tags overlap with ticket tags          → +3 each
//  2. Memory body contains the parent epic name      → +2
//  3. Memory applies_to_environments overlaps with
//     ticket's release / sprint name                  → +2 each
//
// No embeddings, no FTS — substring + tag overlap only. Deterministic
// for a given issue + memory set, which keeps the test surface flat.
func buildSuggestions(issueID int64) ([]ApplicableMemory, error) {
	ctx, err := loadSuggestContext(issueID)
	if err != nil {
		return nil, err
	}
	candidates, err := loadCandidateMemories(ctx)
	if err != nil {
		return nil, err
	}
	scored := scoreCandidates(candidates, ctx)
	if len(scored) > 3 {
		scored = scored[:3]
	}
	// PAI-347 — every memory we surface as a candidate counts as a
	// fresh reference for decay purposes. The bump is best-effort: a
	// failure here must not break the suggest response (the user-
	// visible payload is far more important than the counter).
	if len(scored) > 0 {
		ids := make([]int64, 0, len(scored))
		for _, s := range scored {
			ids = append(ids, s.ID)
		}
		_, _ = bumpMemoryReferenceCounts(db.DB, ctx.projectID, ids)
	}
	return scored, nil
}

// suggestContext bundles the per-issue inputs the scorer needs.
// Loaded once per request so the scoring loop is straight maths.
type suggestContext struct {
	issueID    int64
	projectID  int64
	tags       []string
	parentName string
	envs       []string // release + sprint name(s)
}

// loadSuggestContext gathers the ticket's project + tags + parent
// title + release/sprint names. Missing data degrades gracefully —
// e.g. a ticket without a parent epic just doesn't earn the body-
// substring boost.
func loadSuggestContext(issueID int64) (suggestContext, error) {
	ctx := suggestContext{issueID: issueID}
	var (
		projectID sql.NullInt64
		parentID  sql.NullInt64
		release   sql.NullString
	)
	// PAI-584 P6: parent via the parent edge, not i.parent_id.
	err := db.DB.QueryRow(`
		SELECT i.project_id,
		       (SELECT source_id FROM issue_relations WHERE target_id = i.id AND type='parent'),
		       `+releaseLabelExpr+`
		  FROM issues i
		 WHERE i.id = ?
		   AND i.deleted_at IS NULL
	`, issueID).Scan(&projectID, &parentID, &release)
	if err != nil {
		return ctx, err
	}
	if projectID.Valid {
		ctx.projectID = projectID.Int64
	}
	if release.Valid && strings.TrimSpace(release.String) != "" {
		ctx.envs = append(ctx.envs, strings.ToLower(strings.TrimSpace(release.String)))
	}

	// Tag names on the ticket (lowercased — match memory tags
	// case-insensitively).
	tagRows, err := db.DB.Query(`
		SELECT t.name
		  FROM issue_tags it
		  JOIN tags t ON t.id = it.tag_id
		 WHERE it.issue_id = ?
	`, issueID)
	if err == nil {
		defer tagRows.Close()
		for tagRows.Next() {
			var name string
			if scanErr := tagRows.Scan(&name); scanErr == nil {
				ctx.tags = append(ctx.tags, strings.ToLower(strings.TrimSpace(name)))
			}
		}
	}

	// Parent epic name — only loaded when parent_id is set. Used
	// for the body-substring boost (rule #2).
	if parentID.Valid {
		var parentTitle string
		if err := db.DB.QueryRow(`SELECT title FROM issues WHERE id=?`, parentID.Int64).Scan(&parentTitle); err == nil {
			ctx.parentName = strings.TrimSpace(parentTitle)
		}
	}

	// Sprint memberships add another env-bucket the rule #3 boost
	// can match against. Stored as the sprint issue's title (we
	// already lowercase the name when we compare).
	sprintRows, err := db.DB.Query(`
		SELECT s.title
		  FROM issue_relations ir
		  JOIN issues s ON s.id = ir.source_id
		 WHERE ir.target_id = ?
		   AND ir.type      = 'sprint'
		   AND s.type       = 'sprint'
	`, issueID)
	if err == nil {
		defer sprintRows.Close()
		for sprintRows.Next() {
			var title string
			if scanErr := sprintRows.Scan(&title); scanErr == nil {
				ctx.envs = append(ctx.envs, strings.ToLower(strings.TrimSpace(title)))
			}
		}
	}
	return ctx, nil
}

// loadCandidateMemories returns the project's live memory entries
// the ticket isn't already linked to. Only the project's memories
// are considered — cross-project suggestions are out of scope for
// the v1 algorithm; users can still curate them manually via the
// POST /relations endpoint.
func loadCandidateMemories(ctx suggestContext) ([]candidate, error) {
	if ctx.projectID == 0 {
		return nil, nil
	}
	rows, err := db.DB.Query(`
		SELECT m.id, m.project_id, COALESCE(p.key, ''),
		       COALESCE(m.slug, ''), m.title, m.description,
		       COALESCE(m.category_metadata, ''), m.issue_number
		  FROM issues m
		  LEFT JOIN projects p ON p.id = m.project_id
		 WHERE m.project_id = ?
		   AND m.type       = 'memory'
		   AND m.deleted_at IS NULL
		   AND m.slug       IS NOT NULL
		   AND m.id NOT IN (
		       SELECT target_id FROM issue_relations
		        WHERE source_id = ? AND type = 'applies_to_memory'
		   )
	`, ctx.projectID, ctx.issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cands := []candidate{}
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.ID, &c.ProjectID, &c.ProjectKey,
			&c.Slug, &c.Title, &c.Body, &c.MetaJSON, &c.IssueNumber); err != nil {
			return nil, err
		}
		cands = append(cands, c)
	}
	return cands, rows.Err()
}

// candidate is the in-memory shape used while we score memories
// for the suggest path. Kept private — the public API surface is
// ApplicableMemory.
type candidate struct {
	ID          int64
	ProjectID   int64
	ProjectKey  string
	Slug        string
	Title       string
	Body        string
	MetaJSON    string
	IssueNumber int
}

// scoreCandidates applies the PAI-342 v1 scoring rules to each
// candidate, drops zero-scored entries, and returns the result
// sorted by score (descending) with id as a deterministic
// tiebreaker. Caller truncates to top-3.
func scoreCandidates(cands []candidate, ctx suggestContext) []ApplicableMemory {
	memTags := loadMemoryTagsBatch(cands)
	parentLow := strings.ToLower(ctx.parentName)
	out := []ApplicableMemory{}
	for _, c := range cands {
		score := 0
		matched := []string{}

		// Rule 1: tag overlap — +3 per shared tag.
		if len(ctx.tags) > 0 {
			seen := map[string]bool{}
			for _, mt := range memTags[c.ID] {
				if seen[mt] {
					continue
				}
				for _, it := range ctx.tags {
					if mt == it {
						score += 3
						matched = append(matched, "tag:"+mt)
						seen[mt] = true
						break
					}
				}
			}
		}

		// Rule 2: parent epic name appears in memory body.
		if parentLow != "" {
			if strings.Contains(strings.ToLower(c.Body), parentLow) {
				score += 2
				matched = append(matched, "parent:"+ctx.parentName)
			}
		}

		// Rule 3: applies_to_environments overlap.
		envs := envsFromMeta(c.MetaJSON)
		if len(envs) > 0 && len(ctx.envs) > 0 {
			seen := map[string]bool{}
			for _, e := range envs {
				if seen[e] {
					continue
				}
				for _, ie := range ctx.envs {
					if e == ie {
						score += 2
						matched = append(matched, "env:"+e)
						seen[e] = true
						break
					}
				}
			}
		}

		if score == 0 {
			continue
		}
		am := ApplicableMemory{
			ID:         c.ID,
			ProjectID:  c.ProjectID,
			ProjectKey: c.ProjectKey,
			Slug:       c.Slug,
			Title:      c.Title,
			Preview:    previewLine(c.Body),
			Score:      score,
			Matched:    matched,
		}
		if c.ProjectKey != "" && c.IssueNumber > 0 {
			am.IssueKey = c.ProjectKey + "-" + strconv.Itoa(c.IssueNumber)
		}
		out = append(out, am)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		// Stable tiebreaker: smaller id wins. Deterministic for tests.
		return out[i].ID < out[j].ID
	})
	return out
}

// loadMemoryTagsBatch fetches tags for the candidate set in a single
// query. Avoids N+1 — typical projects have 30–60 memories so the
// IN-list stays small enough to inline.
func loadMemoryTagsBatch(cands []candidate) map[int64][]string {
	out := map[int64][]string{}
	if len(cands) == 0 {
		return out
	}
	placeholders := make([]string, 0, len(cands))
	args := make([]any, 0, len(cands))
	for _, c := range cands {
		placeholders = append(placeholders, "?")
		args = append(args, c.ID)
	}
	// #nosec G202 -- IN-list is ?-only placeholder assembly; ids are bound as args.
	q := `SELECT it.issue_id, t.name
	        FROM issue_tags it
	        JOIN tags t ON t.id = it.tag_id
	       WHERE it.issue_id IN (` + strings.Join(placeholders, ",") + `)`
	rows, err := db.DB.Query(q, args...)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id   int64
			name string
		)
		if err := rows.Scan(&id, &name); err == nil {
			out[id] = append(out[id], strings.ToLower(strings.TrimSpace(name)))
		}
	}
	return out
}

// envsFromMeta pulls the applies_to_environments[] field out of the
// `category_metadata` JSON column. Returns lowercased values to make
// comparison case-insensitive without forcing the caller to do it.
// Bad JSON / missing key → empty slice (the candidate just doesn't
// earn the rule-3 boost).
func envsFromMeta(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var meta map[string]any
	if err := json.Unmarshal([]byte(raw), &meta); err != nil {
		return nil
	}
	arr, ok := meta["applies_to_environments"].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, v := range arr {
		if s, ok := v.(string); ok {
			s = strings.ToLower(strings.TrimSpace(s))
			if s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

// previewLine returns the first non-empty trimmed line of the body
// for use as a card-preview string. Caps at 160 chars so the JSON
// payload stays small even when a memory body opens with a long
// paragraph. Markdown control characters (#, *, etc.) are kept —
// the UI renders them inline when desired.
func previewLine(body string) string {
	for _, ln := range strings.Split(body, "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		if len(ln) > 160 {
			return ln[:157] + "..."
		}
		return ln
	}
	return ""
}
