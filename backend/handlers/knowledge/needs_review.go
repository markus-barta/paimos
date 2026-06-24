package knowledge

import "net/http"

// computeNeedsReview derives the PAI-351 slice-2 "needs re-review" flag for a
// slice of knowledge entries, in place. A memory entry D is flagged when one
// of its depends_on parents P was revised (P.ContentRevisedAt) more recently
// than D was last reviewed — where "last reviewed" is D.DepsReviewedAt, or
// D.CreatedAt if D has never been acknowledged. All three timestamps share the
// `YYYY-MM-DD HH:MM:SS` UTC format (Go's now + SQLite datetime('now')), so the
// lexicographic comparison is chronological. Derived, never stored, so it can
// never drift — drop/rename/delete a parent and it simply recomputes.
//
// project_key on a depends_on item is ignored here, exactly as the dependents
// endpoint does: names resolve against this project's memory slugs, so a
// cross-project reference (whose name isn't a local slug) naturally never
// flags. Self-references are skipped.
func computeNeedsReview(entries []Output) {
	revised := make(map[string]string, len(entries))
	for _, e := range entries {
		if e.Type == "memory" && e.ContentRevisedAt != "" {
			revised[e.Slug] = e.ContentRevisedAt
		}
	}
	for i := range entries {
		d := &entries[i]
		if d.Type != "memory" {
			continue
		}
		deps, ok := d.Metadata["depends_on"].([]any)
		if !ok {
			continue
		}
		baseline := d.DepsReviewedAt
		if baseline == "" {
			baseline = d.CreatedAt
		}
		for _, item := range deps {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			name, _ := m["name"].(string)
			if name == "" || name == d.Slug {
				continue
			}
			if pts := revised[name]; pts != "" && pts > baseline {
				d.NeedsReview = true
				d.ReviewReason = "parent '" + name + "' changed"
				break
			}
		}
	}
}

type needsReviewEntry struct {
	Slug         string `json:"slug"`
	Title        string `json:"title"`
	ReviewReason string `json:"review_reason"`
}

// NeedsReviewHandler is GET
// /api/projects/{id}/knowledge/memory/needs-review (PAI-351 slice 2).
//
// The triage queue: the project's memory entries whose depends_on parent was
// revised after their last review. Computed on read (mirrors ListStaleMemory);
// the array is always present (never null).
func NeedsReviewHandler(w http.ResponseWriter, r *http.Request) {
	projectID, ok := projectIDFromRequest(r)
	if !ok {
		writeError(w, r, "invalid project id", http.StatusBadRequest)
		return
	}
	entries, err := loadByType(projectID, memoryModuleInstance)
	if err != nil {
		writeError(w, r, "query failed", http.StatusInternalServerError)
		return
	}
	computeNeedsReview(entries)
	flagged := []needsReviewEntry{}
	for _, e := range entries {
		if e.NeedsReview {
			flagged = append(flagged, needsReviewEntry{Slug: e.Slug, Title: e.Title, ReviewReason: e.ReviewReason})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"needs_review": flagged, "count": len(flagged)})
}
