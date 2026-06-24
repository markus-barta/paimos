package knowledge

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

type dependentEntry struct {
	Slug  string `json:"slug"`
	Title string `json:"title"`
}

// MemoryDependentsHandler is GET
// /api/projects/{id}/knowledge/memory/{slug}/dependents (PAI-351).
//
// Returns the project's memory entries that declare a `depends_on` reference
// to {slug} — the reverse of the stored `depends_on` array, computed on read
// (never stored). The use case: before editing a parent rule, see who is
// downstream so you know what to re-review. Dependents are matched in Go
// rather than via SQLite JSON functions so a row with NULL / non-array
// category_metadata is simply skipped.
func MemoryDependentsHandler(w http.ResponseWriter, r *http.Request) {
	projectID, ok := projectIDFromRequest(r)
	if !ok {
		writeError(w, r, "invalid project id", http.StatusBadRequest)
		return
	}
	slug := strings.TrimSpace(chi.URLParam(r, "slug"))
	if slug == "" {
		writeError(w, r, "missing slug", http.StatusBadRequest)
		return
	}
	entries, err := loadByType(projectID, memoryModuleInstance)
	if err != nil {
		writeError(w, r, "query failed", http.StatusInternalServerError)
		return
	}
	deps := []dependentEntry{}
	for _, e := range entries {
		raw, ok := e.Metadata["depends_on"].([]any)
		if !ok {
			continue
		}
		for _, item := range raw {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if name, _ := m["name"].(string); name == slug {
				deps = append(deps, dependentEntry{Slug: e.Slug, Title: e.Title})
				break
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"slug": slug, "dependents": deps})
}
