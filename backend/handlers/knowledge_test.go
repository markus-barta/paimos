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

package handlers_test

// PAI-338 acceptance tests. Pin the load-bearing invariants of
// the knowledge plane:
//   1. CRUD round-trips per type (memory, runbook,
//      external_system, related_project, guideline).
//   2. Slug uniqueness within (project_id, type) — duplicate POST
//      → 409.
//   3. Cross-type slug independence — the same slug is allowed
//      across (memory, runbook) within the same project.
//   4. Slug pattern validation — invalid slug → 400.
//   5. Default-hide on the project issue list — knowledge entries
//      do NOT show up in /api/projects/:id/issues unless an
//      explicit ?type= filter is passed.
//   6. memory_ref-target lookup — a memory created via the
//      convenience endpoint is queryable as
//      SELECT * FROM issues WHERE type='memory' AND slug=? AND project_id=?
//      (the resolution path PAI-329 / PAI-330 will use).

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/markus-barta/paimos/backend/db"
)

type knowledgeEntry struct {
	ID               int64                  `json:"id"`
	ProjectID        int64                  `json:"project_id"`
	Type             string                 `json:"type"`
	Slug             string                 `json:"slug"`
	Title            string                 `json:"title"`
	Body             string                 `json:"body"`
	Status           string                 `json:"status"`
	Metadata         map[string]interface{} `json:"metadata"`
	CreatedAt        string                 `json:"created_at"`
	UpdatedAt        string                 `json:"updated_at"`
	ReferenceCount   int64                  `json:"reference_count"`
	LastReferencedAt string                 `json:"last_referenced_at,omitempty"`
}

type issueListItem struct {
	ID    int64  `json:"id"`
	Type  string `json:"type"`
	Title string `json:"title"`
}

func knowledgeURL(projectID int64, alias string) string {
	return fmt.Sprintf("/api/projects/%d/%s", projectID, alias)
}

func knowledgeEntryURL(projectID int64, alias, slug string) string {
	return fmt.Sprintf("/api/projects/%d/%s/%s", projectID, alias, slug)
}

func TestKnowledge_CRUDRoundTripPerType(t *testing.T) {
	cases := []struct {
		alias string
		typ   string
		extra map[string]interface{}
	}{
		{"memory", "memory", map[string]interface{}{"category": "feedback"}},
		{"runbooks", "runbook", map[string]interface{}{"steps": 5}},
		{"external-systems", "external_system", map[string]interface{}{"url": "https://sentry.example.com/org"}},
		{"related-projects", "related_project", map[string]interface{}{"instance_url": "https://acme.example.com"}},
		{"guidelines", "guideline", map[string]interface{}{}},
	}
	for _, tc := range cases {
		t.Run(tc.alias, func(t *testing.T) {
			ts := newTestServer(t)
			projectID := createTestProject(t, ts, "K "+tc.alias, "K"+tc.alias[:2])

			// Create
			createResp := ts.post(t, knowledgeURL(projectID, tc.alias), ts.adminCookie, map[string]interface{}{
				"slug":     "first_entry",
				"title":    "First entry",
				"body":     "Some body text.",
				"metadata": tc.extra,
			})
			assertStatus(t, createResp, http.StatusCreated)
			var created knowledgeEntry
			decode(t, createResp, &created)
			if created.Type != tc.typ {
				t.Errorf("type round-trip: got %q, want %q", created.Type, tc.typ)
			}
			if created.Slug != "first_entry" {
				t.Errorf("slug round-trip: got %q", created.Slug)
			}
			if created.Title != "First entry" {
				t.Errorf("title round-trip: got %q", created.Title)
			}
			if created.Body != "Some body text." {
				t.Errorf("body round-trip: got %q", created.Body)
			}

			// Get-by-slug
			getResp := ts.get(t, knowledgeEntryURL(projectID, tc.alias, "first_entry"), ts.adminCookie)
			assertStatus(t, getResp, http.StatusOK)
			var got knowledgeEntry
			decode(t, getResp, &got)
			if got.ID != created.ID {
				t.Errorf("get-by-slug returned different row: %d vs %d", got.ID, created.ID)
			}

			// List
			listResp := ts.get(t, knowledgeURL(projectID, tc.alias), ts.adminCookie)
			assertStatus(t, listResp, http.StatusOK)
			var listed []knowledgeEntry
			decode(t, listResp, &listed)
			if len(listed) != 1 {
				t.Fatalf("expected 1 entry in list; got %d", len(listed))
			}

			// Update — change title + body, keep slug.
			updResp := ts.put(t, knowledgeEntryURL(projectID, tc.alias, "first_entry"), ts.adminCookie, map[string]interface{}{
				"slug":     "first_entry",
				"title":    "Updated entry",
				"body":     "New body.",
				"metadata": tc.extra,
			})
			assertStatus(t, updResp, http.StatusOK)
			var updated knowledgeEntry
			decode(t, updResp, &updated)
			if updated.Title != "Updated entry" {
				t.Errorf("title not updated: %q", updated.Title)
			}
			if updated.Body != "New body." {
				t.Errorf("body not updated: %q", updated.Body)
			}
			if updated.ID != created.ID {
				t.Errorf("update created a new row instead of mutating: %d vs %d", updated.ID, created.ID)
			}

			// Delete (soft)
			delResp := ts.del(t, knowledgeEntryURL(projectID, tc.alias, "first_entry"), ts.adminCookie)
			assertStatus(t, delResp, http.StatusNoContent)

			// And it's gone from the list
			afterResp := ts.get(t, knowledgeURL(projectID, tc.alias), ts.adminCookie)
			assertStatus(t, afterResp, http.StatusOK)
			var afterDelete []knowledgeEntry
			decode(t, afterResp, &afterDelete)
			if len(afterDelete) != 0 {
				t.Fatalf("expected 0 entries after delete; got %d", len(afterDelete))
			}

			// And get-by-slug returns 404
			missing := ts.get(t, knowledgeEntryURL(projectID, tc.alias, "first_entry"), ts.adminCookie)
			assertStatus(t, missing, http.StatusNotFound)
		})
	}
}

func TestKnowledge_DuplicateSlugReturns409(t *testing.T) {
	ts := newTestServer(t)
	projectID := createTestProject(t, ts, "Dup Slug", "DSP")

	first := ts.post(t, knowledgeURL(projectID, "memory"), ts.adminCookie, map[string]interface{}{
		"slug":  "thread_dump",
		"title": "Thread dump signature",
	})
	assertStatus(t, first, http.StatusCreated)

	second := ts.post(t, knowledgeURL(projectID, "memory"), ts.adminCookie, map[string]interface{}{
		"slug":  "thread_dump",
		"title": "Another title",
	})
	assertStatus(t, second, http.StatusConflict)
}

func TestKnowledge_CrossTypeSlugIndependence(t *testing.T) {
	ts := newTestServer(t)
	projectID := createTestProject(t, ts, "Cross Type", "XCT")

	// Same slug in memory AND runbook should both succeed — the
	// partial UNIQUE index is scoped on (type, slug, project_id).
	memResp := ts.post(t, knowledgeURL(projectID, "memory"), ts.adminCookie, map[string]interface{}{
		"slug":  "deploy",
		"title": "Memory: deploy facts",
	})
	assertStatus(t, memResp, http.StatusCreated)

	runResp := ts.post(t, knowledgeURL(projectID, "runbooks"), ts.adminCookie, map[string]interface{}{
		"slug":  "deploy",
		"title": "Runbook: how to deploy",
	})
	assertStatus(t, runResp, http.StatusCreated)
}

func TestKnowledge_SlugPatternValidation(t *testing.T) {
	ts := newTestServer(t)
	projectID := createTestProject(t, ts, "Bad Slug", "BSL")

	bad := []string{
		"",
		"1memory",
		"Memory",
		"my memory",
		"my/memory",
	}
	for _, s := range bad {
		t.Run(fmt.Sprintf("slug=%q", s), func(t *testing.T) {
			resp := ts.post(t, knowledgeURL(projectID, "memory"), ts.adminCookie, map[string]interface{}{
				"slug":  s,
				"title": "X",
			})
			assertStatus(t, resp, http.StatusBadRequest)
		})
	}
}

func TestKnowledge_DefaultHideOnIssueList(t *testing.T) {
	ts := newTestServer(t)
	projectID := createTestProject(t, ts, "Default Hide", "DHD")

	// Seed one regular ticket directly so the project's issue list
	// has at least one expected row to compare against.
	if _, err := db.DB.Exec(`
		INSERT INTO issues(project_id, issue_number, type, title, status, priority)
		VALUES(?, 1, 'ticket', 'A real ticket', 'new', 'medium')
	`, projectID); err != nil {
		t.Fatalf("seed ticket: %v", err)
	}

	// Create one knowledge entry of each type. Their issue_number
	// rolls forward over the same per-project sequence.
	for _, alias := range []string{"memory", "runbooks", "external-systems", "related-projects", "guidelines"} {
		body := map[string]interface{}{
			"slug":  "auto_" + alias,
			"title": "Auto " + alias,
		}
		if alias == "external-systems" {
			body["metadata"] = map[string]interface{}{"url": "https://example.com"}
		}
		resp := ts.post(t, knowledgeURL(projectID, alias), ts.adminCookie, body)
		assertStatus(t, resp, http.StatusCreated)
	}

	// Default issue list — should ONLY contain the regular ticket.
	listResp := ts.get(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.adminCookie)
	assertStatus(t, listResp, http.StatusOK)
	var list []issueListItem
	decode(t, listResp, &list)
	if len(list) != 1 {
		t.Fatalf("expected 1 default-visible issue, got %d (types: %v)", len(list), typesOf(list))
	}
	if list[0].Type != "ticket" {
		t.Fatalf("expected the surviving issue to be the ticket, got %q", list[0].Type)
	}

	// Explicit ?type=memory should expose the memory entry.
	memResp := ts.get(t, fmt.Sprintf("/api/projects/%d/issues?type=memory", projectID), ts.adminCookie)
	assertStatus(t, memResp, http.StatusOK)
	var memList []issueListItem
	decode(t, memResp, &memList)
	if len(memList) != 1 {
		t.Fatalf("expected 1 memory entry from explicit ?type=memory, got %d", len(memList))
	}
	if memList[0].Type != "memory" {
		t.Fatalf("expected type=memory, got %q", memList[0].Type)
	}

	// Explicit comma-separated knowledge filter: returns all 5.
	allKnowledge := ts.get(t, fmt.Sprintf("/api/projects/%d/issues?type=memory,runbook,external_system,related_project,guideline", projectID), ts.adminCookie)
	assertStatus(t, allKnowledge, http.StatusOK)
	var allList []issueListItem
	decode(t, allKnowledge, &allList)
	if len(allList) != 5 {
		t.Fatalf("expected 5 knowledge entries when explicitly filtered, got %d", len(allList))
	}
}

func TestKnowledge_MemoryRefTargetLookup(t *testing.T) {
	// PAI-329 / PAI-330 cross-cut: a memory entry created via the
	// convenience endpoint must be findable by the canonical
	// memory_ref resolution query.
	ts := newTestServer(t)
	projectID := createTestProject(t, ts, "MemRef", "MRF")

	resp := ts.post(t, knowledgeURL(projectID, "memory"), ts.adminCookie, map[string]interface{}{
		"slug":  "thread_dump_lock_signature_match",
		"title": "Thread dump signature match",
		"body":  "Match by stack trace prefix.",
	})
	assertStatus(t, resp, http.StatusCreated)

	row := db.DB.QueryRow(`
		SELECT id, type, slug, title FROM issues
		WHERE type = 'memory' AND slug = ? AND project_id = ? AND deleted_at IS NULL
	`, "thread_dump_lock_signature_match", projectID)
	var (
		id    int64
		typ   string
		slug  string
		title string
	)
	if err := row.Scan(&id, &typ, &slug, &title); err != nil {
		t.Fatalf("memory_ref-target lookup failed: %v", err)
	}
	if typ != "memory" {
		t.Errorf("type: got %q, want memory", typ)
	}
	if slug != "thread_dump_lock_signature_match" {
		t.Errorf("slug: got %q", slug)
	}
	if title != "Thread dump signature match" {
		t.Errorf("title: got %q", title)
	}
}

func TestKnowledge_DeleteMissingReturns404(t *testing.T) {
	ts := newTestServer(t)
	projectID := createTestProject(t, ts, "Del Miss", "DLM")

	resp := ts.del(t, knowledgeEntryURL(projectID, "memory", "ghost"), ts.adminCookie)
	assertStatus(t, resp, http.StatusNotFound)
}

func TestKnowledge_PutMissingReturns404(t *testing.T) {
	ts := newTestServer(t)
	projectID := createTestProject(t, ts, "Put Miss", "PTM")

	resp := ts.put(t, knowledgeEntryURL(projectID, "memory", "ghost"), ts.adminCookie, map[string]interface{}{
		"slug":  "ghost",
		"title": "Whatever",
	})
	assertStatus(t, resp, http.StatusNotFound)
}

func TestKnowledge_ExternalSystemBadURLReturns400(t *testing.T) {
	ts := newTestServer(t)
	projectID := createTestProject(t, ts, "Bad URL", "BUR")

	resp := ts.post(t, knowledgeURL(projectID, "external-systems"), ts.adminCookie, map[string]interface{}{
		"slug":     "broken",
		"title":    "Broken",
		"metadata": map[string]interface{}{"url": "not-a-url"},
	})
	assertStatus(t, resp, http.StatusBadRequest)
}

// typesOf is a tiny helper that flattens an issue list to its type
// values for diagnostic Errorf calls.
func typesOf(list []issueListItem) []string {
	out := make([]string, len(list))
	for i, item := range list {
		out[i] = item.Type
	}
	return out
}

// PAI-347 — confidence selector persists + reads back from
// category_metadata. Existing memory without an explicit confidence
// is treated as medium on read by the bundle filter and the stale
// proposal logic; the round-trip itself doesn't backfill the field
// (the editor handles that on next save), so the test asserts only
// what's actually persisted.
func TestKnowledge_ConfidencePersistsAndRoundTrips(t *testing.T) {
	ts := newTestServer(t)
	projectID := createTestProject(t, ts, "PAI-347 confidence", "C347")

	for _, conf := range []string{"high", "medium", "low"} {
		t.Run(conf, func(t *testing.T) {
			slug := "rule_" + conf
			createResp := ts.post(t, knowledgeURL(projectID, "memory"), ts.adminCookie, map[string]interface{}{
				"slug":     slug,
				"title":    "Rule " + conf,
				"body":     "Some rule.",
				"metadata": map[string]interface{}{"confidence": conf},
			})
			assertStatus(t, createResp, http.StatusCreated)
			var created knowledgeEntry
			decode(t, createResp, &created)
			if got := created.Metadata["confidence"]; got != conf {
				t.Errorf("create round-trip: confidence=%v, want %q", got, conf)
			}

			// Reads pull the same value back.
			getResp := ts.get(t, knowledgeEntryURL(projectID, "memory", slug), ts.adminCookie)
			assertStatus(t, getResp, http.StatusOK)
			var got knowledgeEntry
			decode(t, getResp, &got)
			if v := got.Metadata["confidence"]; v != conf {
				t.Errorf("get-by-slug confidence=%v, want %q", v, conf)
			}
			// New decay-tracking columns surface on the response shape
			// (zero by default — the bundle / suggest paths are the
			// only writers and neither has fired in this test).
			if got.ReferenceCount != 0 {
				t.Errorf("reference_count=%d, want 0 for fresh entry", got.ReferenceCount)
			}
		})
	}

	// Existing memory without confidence: nothing is persisted on
	// creation, but reads should still succeed and the metadata map
	// just lacks the field. The "missing → medium" rule is enforced
	// by downstream consumers (filter, stale, bundle), not by the
	// raw round-trip.
	createResp := ts.post(t, knowledgeURL(projectID, "memory"), ts.adminCookie, map[string]interface{}{
		"slug":     "no_confidence_field",
		"title":    "No confidence",
		"body":     "x",
		"metadata": map[string]interface{}{},
	})
	assertStatus(t, createResp, http.StatusCreated)
	var created knowledgeEntry
	decode(t, createResp, &created)
	if _, ok := created.Metadata["confidence"]; ok {
		t.Errorf("expected no confidence in metadata when not provided, got %v", created.Metadata)
	}
}
