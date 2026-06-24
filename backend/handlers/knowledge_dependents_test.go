package handlers_test

import (
	"fmt"
	"net/http"
	"testing"
)

// PAI-351: memory `depends_on` declaration + reverse `dependents` (computed on
// read) at GET /api/projects/{id}/knowledge/memory/{slug}/dependents.
func TestMemoryDependents(t *testing.T) {
	ts := newTestServer(t)
	projectID := createTestProject(t, ts, "Deps", "DEPS")

	mkMem := func(slug, title string, meta map[string]any) *http.Response {
		body := map[string]any{"type": "memory", "slug": slug, "title": title, "body": "x"}
		if meta != nil {
			body["metadata"] = meta
		}
		return ts.post(t, knowledgeBaseURL(projectID), ts.adminCookie, body)
	}

	// Parent rule + two children that depend on it + one unrelated entry.
	assertStatus(t, mkMem("parent_rule", "Parent rule", nil), http.StatusCreated)
	assertStatus(t, mkMem("child_a", "Child A", map[string]any{
		"depends_on": []any{map[string]any{"name": "parent_rule"}},
	}), http.StatusCreated)
	assertStatus(t, mkMem("child_b", "Child B", map[string]any{
		"depends_on": []any{map[string]any{"name": "parent_rule", "project_key": "DEPS"}},
	}), http.StatusCreated)
	assertStatus(t, mkMem("unrelated", "Unrelated", nil), http.StatusCreated)

	depsURL := func(slug string) string {
		return fmt.Sprintf("/api/projects/%d/knowledge/memory/%s/dependents", projectID, slug)
	}
	var out struct {
		Slug       string `json:"slug"`
		Dependents []struct {
			Slug  string `json:"slug"`
			Title string `json:"title"`
		} `json:"dependents"`
	}

	// parent_rule has two dependents.
	resp := ts.get(t, depsURL("parent_rule"), ts.adminCookie)
	assertStatus(t, resp, http.StatusOK)
	decode(t, resp, &out)
	got := map[string]bool{}
	for _, d := range out.Dependents {
		got[d.Slug] = true
	}
	if len(out.Dependents) != 2 || !got["child_a"] || !got["child_b"] {
		t.Fatalf("parent_rule dependents = %#v, want child_a + child_b", out.Dependents)
	}

	// A leaf with no dependents → empty array (not null).
	resp = ts.get(t, depsURL("child_a"), ts.adminCookie)
	assertStatus(t, resp, http.StatusOK)
	out.Dependents = nil
	decode(t, resp, &out)
	if out.Dependents == nil || len(out.Dependents) != 0 {
		t.Fatalf("child_a dependents = %#v, want empty array", out.Dependents)
	}
}

// A malformed depends_on (not an array of {name} objects) is rejected at write.
func TestMemoryDependsOnValidation(t *testing.T) {
	ts := newTestServer(t)
	projectID := createTestProject(t, ts, "DepsVal", "DPV")

	mk := func(meta any) *http.Response {
		return ts.post(t, knowledgeBaseURL(projectID), ts.adminCookie, map[string]any{
			"type": "memory", "slug": "x", "title": "X", "body": "b",
			"metadata": map[string]any{"depends_on": meta},
		})
	}
	assertStatus(t, mk("not-an-array"), http.StatusBadRequest)
	assertStatus(t, mk([]any{"bare-string"}), http.StatusBadRequest)
	assertStatus(t, mk([]any{map[string]any{"name": ""}}), http.StatusBadRequest)
	// well-formed → accepted
	assertStatus(t, mk([]any{map[string]any{"name": "parent_rule"}}), http.StatusCreated)
}
