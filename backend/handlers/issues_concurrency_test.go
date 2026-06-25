package handlers_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/markus-barta/paimos/backend/db"
)

// PAI-231 — optimistic concurrency: GET returns a strong per-row ETag; PUT
// with a stale If-Match is rejected with 412 + a structured conflict body; a
// matching If-Match succeeds; no If-Match stays backward-compatible.
func TestUpdateIssue_OptimisticConcurrency(t *testing.T) {
	ts := newTestServer(t)
	pid := createTestProject(t, ts, "Concurrency", "CONC")
	id := responseID(t, ts.post(t, fmt.Sprintf("/api/projects/%d/issues", pid), ts.adminCookie, map[string]any{
		"title": "Edit me", "type": "ticket",
	}))
	url := fmt.Sprintf("/api/issues/%d", id)

	g := ts.get(t, url, ts.adminCookie)
	assertStatus(t, g, http.StatusOK)
	etag1 := g.Header.Get("ETag")
	if etag1 == "" {
		t.Fatalf("GET %s must return an ETag", url)
	}

	// Simulate a concurrent edit so etag1 is now stale.
	if _, err := db.DB.Exec(`UPDATE issues SET updated_at='2030-01-01 00:00:00' WHERE id=?`, id); err != nil {
		t.Fatalf("exec: %v", err)
	}

	// Stale If-Match → 412 + structured conflict body.
	stale := putWithHeaders(t, ts, url, ts.adminCookie,
		map[string]any{"title": "My edit"}, map[string]string{"If-Match": etag1})
	assertStatus(t, stale, http.StatusPreconditionFailed)
	var conflict struct {
		Error          string   `json:"error"`
		DivergedFields []string `json:"diverged_fields"`
		CurrentState   struct {
			ID int64 `json:"id"`
		} `json:"current_state"`
	}
	decode(t, stale, &conflict)
	if conflict.Error != "conflict" || conflict.CurrentState.ID != id {
		t.Fatalf("412 body = %#v, want error=conflict + current_state.id=%d", conflict, id)
	}
	hasTitle := false
	for _, f := range conflict.DivergedFields {
		if f == "title" {
			hasTitle = true
		}
	}
	if !hasTitle {
		t.Errorf("diverged_fields should include 'title' (client changes it, server differs), got %v", conflict.DivergedFields)
	}

	// Re-load → fresh ETag → matching If-Match succeeds + returns a new ETag.
	g2 := ts.get(t, url, ts.adminCookie)
	assertStatus(t, g2, http.StatusOK)
	etag2 := g2.Header.Get("ETag")
	if etag2 == "" || etag2 == etag1 {
		t.Fatalf("etag must change after the concurrent edit (etag1=%q etag2=%q)", etag1, etag2)
	}
	ok := putWithHeaders(t, ts, url, ts.adminCookie,
		map[string]any{"title": "My edit"}, map[string]string{"If-Match": etag2})
	assertStatus(t, ok, http.StatusOK)
	if ok.Header.Get("ETag") == "" {
		t.Errorf("a successful update must return a new ETag")
	}

	// No If-Match → proceeds (backward compatible — CLI / API-key clients).
	noIM := ts.put(t, url, ts.adminCookie, map[string]any{"title": "Again"})
	assertStatus(t, noIM, http.StatusOK)
}
