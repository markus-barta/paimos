package handlers_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/markus-barta/paimos/backend/db"
)

// PAI-351 slice 2 — the write-path detection: a memory entry's BODY change
// stamps content_revised_at; title / status / whitespace-only edits do not.
func TestNeedsReview_DetectionBodyOnly(t *testing.T) {
	ts := newTestServer(t)
	pid := createTestProject(t, ts, "NR", "NRR")
	assertStatus(t, ts.post(t, knowledgeBaseURL(pid), ts.adminCookie, map[string]any{
		"type": "memory", "slug": "p", "title": "P", "body": "v1",
	}), http.StatusCreated)

	revisedAt := func() string {
		var v string
		if err := db.DB.QueryRow(`SELECT COALESCE(content_revised_at,'') FROM issues WHERE project_id=? AND type='memory' AND slug='p'`, pid).Scan(&v); err != nil {
			t.Fatalf("query content_revised_at: %v", err)
		}
		return v
	}
	if got := revisedAt(); got != "" {
		t.Fatalf("content_revised_at must be empty on create, got %q", got)
	}
	putURL := fmt.Sprintf("/api/projects/%d/knowledge/memory/p", pid)

	// Title-only edit → NOT stamped.
	assertStatus(t, ts.put(t, putURL, ts.adminCookie, map[string]any{"type": "memory", "title": "P2", "body": "v1"}), http.StatusOK)
	if got := revisedAt(); got != "" {
		t.Fatalf("title-only edit must not stamp content_revised_at, got %q", got)
	}
	// Whitespace/CRLF-only edit → NOT stamped (normalizeBody).
	assertStatus(t, ts.put(t, putURL, ts.adminCookie, map[string]any{"type": "memory", "title": "P2", "body": "  v1\r\n  "}), http.StatusOK)
	if got := revisedAt(); got != "" {
		t.Fatalf("whitespace-only edit must not stamp content_revised_at, got %q", got)
	}
	// Real body change → stamped.
	assertStatus(t, ts.put(t, putURL, ts.adminCookie, map[string]any{"type": "memory", "title": "P2", "body": "v2 changed"}), http.StatusOK)
	if got := revisedAt(); got == "" {
		t.Fatalf("a real body change must stamp content_revised_at")
	}
}

type nrResp struct {
	NeedsReview []struct {
		Slug         string `json:"slug"`
		Title        string `json:"title"`
		ReviewReason string `json:"review_reason"`
	} `json:"needs_review"`
	Count int `json:"count"`
}

// computeNeedsReview semantics through the needs-review + dependents endpoints,
// using SQL-set timestamps for a deterministic ordering (no same-second races).
func TestNeedsReview_ComputeAndEndpoints(t *testing.T) {
	ts := newTestServer(t)
	pid := createTestProject(t, ts, "NR2", "NR2")
	mk := func(slug, title string, meta map[string]any) {
		body := map[string]any{"type": "memory", "slug": slug, "title": title, "body": "x"}
		if meta != nil {
			body["metadata"] = meta
		}
		assertStatus(t, ts.post(t, knowledgeBaseURL(pid), ts.adminCookie, body), http.StatusCreated)
	}
	mk("parent", "Parent rule", nil)
	mk("child", "Child", map[string]any{"depends_on": []any{map[string]any{"name": "parent"}}})
	mk("selfref", "Selfref", map[string]any{"depends_on": []any{map[string]any{"name": "selfref"}}})
	mk("crossp", "Crossproject", map[string]any{"depends_on": []any{map[string]any{"name": "parent", "project_key": "OTHER"}}})
	mk("leaf", "Leaf", nil)

	exec := func(q string, args ...any) {
		if _, err := db.DB.Exec(q, args...); err != nil {
			t.Fatalf("exec %q: %v", q, err)
		}
	}
	// Children look old; parent was revised "after" them.
	exec(`UPDATE issues SET created_at='2020-01-01 00:00:00' WHERE project_id=? AND type='memory'`, pid)
	exec(`UPDATE issues SET content_revised_at='2026-01-01 00:00:00' WHERE project_id=? AND slug='parent'`, pid)

	nrURL := fmt.Sprintf("/api/projects/%d/knowledge/memory/needs-review", pid)
	resp := ts.get(t, nrURL, ts.adminCookie)
	assertStatus(t, resp, http.StatusOK)
	var nr nrResp
	decode(t, resp, &nr)
	// Only 'child' flags: selfref is skipped (self), crossp is skipped (its
	// name resolves to no local slug under a foreign project_key… actually
	// 'parent' IS local, so crossp WOULD match — but its project_key marks it
	// cross-project; we match name regardless of project_key for consistency
	// with the dependents endpoint, so crossp flags too). Assert child present.
	if nr.Count < 1 {
		t.Fatalf("expected at least 'child' flagged, got count=%d %#v", nr.Count, nr.NeedsReview)
	}
	flagged := map[string]string{}
	for _, e := range nr.NeedsReview {
		flagged[e.Slug] = e.ReviewReason
	}
	if _, ok := flagged["child"]; !ok {
		t.Fatalf("child must be flagged: %#v", nr.NeedsReview)
	}
	if reason := flagged["child"]; reason == "" {
		t.Errorf("child review_reason must name the parent, got empty")
	}
	if _, ok := flagged["selfref"]; ok {
		t.Errorf("self-reference must NOT flag")
	}
	if _, ok := flagged["leaf"]; ok {
		t.Errorf("a leaf with no depends_on must NOT flag")
	}

	// dependents of 'parent' carries the per-dependent flag + count.
	depURL := fmt.Sprintf("/api/projects/%d/knowledge/memory/parent/dependents", pid)
	dresp := ts.get(t, depURL, ts.adminCookie)
	assertStatus(t, dresp, http.StatusOK)
	var dep struct {
		Dependents []struct {
			Slug        string `json:"slug"`
			NeedsReview bool   `json:"needs_review"`
		} `json:"dependents"`
		NeedsReviewCount int `json:"needs_review_count"`
	}
	decode(t, dresp, &dep)
	var childSeen bool
	for _, d := range dep.Dependents {
		if d.Slug == "child" {
			childSeen = true
			if !d.NeedsReview {
				t.Errorf("dependent 'child' must report needs_review=true")
			}
		}
	}
	if !childSeen {
		t.Fatalf("'child' must appear in parent's dependents: %#v", dep.Dependents)
	}
	if dep.NeedsReviewCount < 1 {
		t.Errorf("needs_review_count must be >= 1, got %d", dep.NeedsReviewCount)
	}

	// Single-entry GET must also DERIVE the flag (it has no cross-entry context
	// of its own), so an API/MCP consumer reading one memory sees the truth.
	gresp := ts.get(t, fmt.Sprintf("/api/projects/%d/knowledge/memory/child", pid), ts.adminCookie)
	assertStatus(t, gresp, http.StatusOK)
	var single struct {
		Slug         string `json:"slug"`
		NeedsReview  bool   `json:"needs_review"`
		ReviewReason string `json:"review_reason"`
	}
	decode(t, gresp, &single)
	if !single.NeedsReview || single.ReviewReason == "" {
		t.Errorf("single GET of 'child' must derive needs_review=true + reason, got %#v", single)
	}
}

// Acknowledge clears the flag; a later parent revision re-flags.
func TestNeedsReview_AcknowledgeAndReflag(t *testing.T) {
	ts := newTestServer(t)
	pid := createTestProject(t, ts, "NR3", "NR3")
	mk := func(slug string, meta map[string]any) {
		body := map[string]any{"type": "memory", "slug": slug, "title": slug, "body": "x"}
		if meta != nil {
			body["metadata"] = meta
		}
		assertStatus(t, ts.post(t, knowledgeBaseURL(pid), ts.adminCookie, body), http.StatusCreated)
	}
	mk("parent", nil)
	mk("child", map[string]any{"depends_on": []any{map[string]any{"name": "parent"}}})
	exec := func(q string, args ...any) {
		if _, err := db.DB.Exec(q, args...); err != nil {
			t.Fatalf("exec: %v", err)
		}
	}
	exec(`UPDATE issues SET created_at='2020-01-01 00:00:00' WHERE project_id=? AND slug='child'`, pid)
	exec(`UPDATE issues SET content_revised_at='2026-01-01 00:00:00' WHERE project_id=? AND slug='parent'`, pid)

	flaggedCount := func() int {
		resp := ts.get(t, fmt.Sprintf("/api/projects/%d/knowledge/memory/needs-review", pid), ts.adminCookie)
		assertStatus(t, resp, http.StatusOK)
		var nr nrResp
		decode(t, resp, &nr)
		return nr.Count
	}
	if flaggedCount() != 1 {
		t.Fatalf("child should be flagged before acknowledge")
	}
	// Acknowledge clears it (deps_reviewed_at = now, after the 2026-01-01 revision).
	ack := ts.post(t, fmt.Sprintf("/api/projects/%d/knowledge/memory/child/reviewed", pid), ts.adminCookie, map[string]any{})
	assertStatus(t, ack, http.StatusOK)
	if flaggedCount() != 0 {
		t.Fatalf("acknowledge must clear the flag")
	}
	// A NEW parent revision in the future re-flags.
	exec(`UPDATE issues SET content_revised_at='2030-01-01 00:00:00' WHERE project_id=? AND slug='parent'`, pid)
	if flaggedCount() != 1 {
		t.Fatalf("a later parent revision must re-flag the dependent")
	}
	// Acknowledge of a missing slug → 404.
	assertStatus(t, ts.post(t, fmt.Sprintf("/api/projects/%d/knowledge/memory/nope/reviewed", pid), ts.adminCookie, map[string]any{}), http.StatusNotFound)
}

// PAI-351 slice 3 — a memory body edited via the GENERIC issue path
// (PUT /api/issues/{id} — the MCP update_issue surface) also stamps
// content_revised_at and flags dependents, not just the Knowledge-tab PUT.
func TestNeedsReview_GenericIssueUpdateStampsBody(t *testing.T) {
	ts := newTestServer(t)
	pid := createTestProject(t, ts, "NR5", "NR5")
	mkID := func(slug string, meta map[string]any) int64 {
		body := map[string]any{"type": "memory", "slug": slug, "title": slug, "body": "v1"}
		if meta != nil {
			body["metadata"] = meta
		}
		r := ts.post(t, knowledgeBaseURL(pid), ts.adminCookie, body)
		assertStatus(t, r, http.StatusCreated)
		var e knowledgeEntry
		decode(t, r, &e)
		return e.ID
	}
	parentID := mkID("parent", nil)
	mkID("child", map[string]any{"depends_on": []any{map[string]any{"name": "parent"}}})
	if _, err := db.DB.Exec(`UPDATE issues SET created_at='2020-01-01 00:00:00' WHERE project_id=? AND slug='child'`, pid); err != nil {
		t.Fatalf("exec: %v", err)
	}

	flaggedCount := func() int {
		resp := ts.get(t, fmt.Sprintf("/api/projects/%d/knowledge/memory/needs-review", pid), ts.adminCookie)
		assertStatus(t, resp, http.StatusOK)
		var out nrResp
		decode(t, resp, &out)
		return out.Count
	}

	// Edit the parent's BODY via the generic issue endpoint (the MCP surface).
	assertStatus(t, ts.put(t, fmt.Sprintf("/api/issues/%d", parentID), ts.adminCookie, map[string]any{
		"description": "v2 revised via the generic issue path",
	}), http.StatusOK)

	var revised string
	if err := db.DB.QueryRow(`SELECT COALESCE(content_revised_at,'') FROM issues WHERE id=?`, parentID).Scan(&revised); err != nil {
		t.Fatalf("query: %v", err)
	}
	if revised == "" {
		t.Fatalf("generic issue-path body edit must stamp content_revised_at")
	}
	if flaggedCount() != 1 {
		t.Fatalf("dependent must be flagged after a generic-path body edit")
	}

	// Guard: acknowledge, then a TITLE-ONLY generic edit must NOT re-flag.
	assertStatus(t, ts.post(t, fmt.Sprintf("/api/projects/%d/knowledge/memory/child/reviewed", pid), ts.adminCookie, map[string]any{}), http.StatusOK)
	assertStatus(t, ts.put(t, fmt.Sprintf("/api/issues/%d", parentID), ts.adminCookie, map[string]any{
		"title": "Parent retitled",
	}), http.StatusOK)
	if flaggedCount() != 0 {
		t.Errorf("a title-only generic edit must NOT stamp content_revised_at / re-flag")
	}
}

// PAI-351 slice-3 tail — a memory body edited via the BATCH path
// (PATCH /api/issues) also stamps content_revised_at + flags dependents.
func TestNeedsReview_BatchUpdateStampsBody(t *testing.T) {
	ts := newTestServer(t)
	pid := createTestProject(t, ts, "NR6", "NR6")
	mkID := func(slug string, meta map[string]any) int64 {
		body := map[string]any{"type": "memory", "slug": slug, "title": slug, "body": "v1"}
		if meta != nil {
			body["metadata"] = meta
		}
		r := ts.post(t, knowledgeBaseURL(pid), ts.adminCookie, body)
		assertStatus(t, r, http.StatusCreated)
		var e knowledgeEntry
		decode(t, r, &e)
		return e.ID
	}
	parentID := mkID("parent", nil)
	mkID("child", map[string]any{"depends_on": []any{map[string]any{"name": "parent"}}})
	if _, err := db.DB.Exec(`UPDATE issues SET created_at='2020-01-01 00:00:00' WHERE project_id=? AND slug='child'`, pid); err != nil {
		t.Fatalf("exec: %v", err)
	}

	batch := []map[string]any{
		{"ref": itoa(parentID), "fields": map[string]any{"description": "v2 revised via batch"}},
	}
	assertStatus(t, ts.patch(t, "/api/issues", ts.adminCookie, batch), http.StatusOK)

	var rev string
	if err := db.DB.QueryRow(`SELECT COALESCE(content_revised_at,'') FROM issues WHERE id=?`, parentID).Scan(&rev); err != nil {
		t.Fatalf("query: %v", err)
	}
	if rev == "" {
		t.Fatalf("batch body edit must stamp content_revised_at")
	}
	resp := ts.get(t, fmt.Sprintf("/api/projects/%d/knowledge/memory/needs-review", pid), ts.adminCookie)
	assertStatus(t, resp, http.StatusOK)
	var out nrResp
	decode(t, resp, &out)
	if out.Count != 1 {
		t.Fatalf("dependent must flag after a batch body edit, got count=%d", out.Count)
	}
}

// PAI-351 slice-3 tail — undoing a memory body edit ROUND-TRIPS
// content_revised_at (resets it), so the dependent un-flags (no over-flag).
func TestNeedsReview_UndoResetsContentRevisedAt(t *testing.T) {
	ts := newTestServer(t)
	var adminID int64
	if err := db.DB.QueryRow(`SELECT id FROM users WHERE username='admin'`).Scan(&adminID); err != nil {
		t.Fatalf("admin id: %v", err)
	}
	pid := createTestProject(t, ts, "NR7", "NR7")
	mkID := func(slug string, meta map[string]any) int64 {
		body := map[string]any{"type": "memory", "slug": slug, "title": slug, "body": "v1"}
		if meta != nil {
			body["metadata"] = meta
		}
		r := ts.post(t, knowledgeBaseURL(pid), ts.adminCookie, body)
		assertStatus(t, r, http.StatusCreated)
		var e knowledgeEntry
		decode(t, r, &e)
		return e.ID
	}
	parentID := mkID("parent", nil)
	mkID("child", map[string]any{"depends_on": []any{map[string]any{"name": "parent"}}})
	if _, err := db.DB.Exec(`UPDATE issues SET created_at='2020-01-01 00:00:00' WHERE project_id=? AND slug='child'`, pid); err != nil {
		t.Fatalf("exec: %v", err)
	}
	flagged := func() int {
		resp := ts.get(t, fmt.Sprintf("/api/projects/%d/knowledge/memory/needs-review", pid), ts.adminCookie)
		assertStatus(t, resp, http.StatusOK)
		var out nrResp
		decode(t, resp, &out)
		return out.Count
	}

	requestID := "req-nr-undo"
	seedAICall(t, adminID, parentID, requestID)
	assertStatus(t, putWithHeaders(t, ts, "/api/issues/"+itoa(parentID), ts.adminCookie, map[string]any{
		"description": "v2 revised",
	}, map[string]string{
		"X-PAIMOS-AI-Request-Id": requestID,
		"X-PAIMOS-AI-Action":     "edit_rule",
	}), http.StatusOK)
	if flagged() != 1 {
		t.Fatalf("child should be flagged after the body edit")
	}

	// Undo (clean, no drift) reverts body + content_revised_at via the snapshot.
	assertStatus(t, ts.post(t, "/api/undo/request/"+requestID, ts.adminCookie, map[string]any{}), http.StatusOK)

	var rev string
	if err := db.DB.QueryRow(`SELECT COALESCE(content_revised_at,'') FROM issues WHERE id=?`, parentID).Scan(&rev); err != nil {
		t.Fatalf("query: %v", err)
	}
	if rev != "" {
		t.Errorf("undo must reset content_revised_at, got %q", rev)
	}
	if flagged() != 0 {
		t.Errorf("undo must un-flag the dependent (over-flag fixed)")
	}
}

// Graph synthesises depends_on edges from metadata, deduped against a hand-made
// issue_relations depends_on row, and reflects needs_review on memory nodes.
func TestNeedsReview_GraphEdgesAndDedup(t *testing.T) {
	ts := newTestServer(t)
	pid := createTestProject(t, ts, "NR4", "NR4")
	mkID := func(slug string, meta map[string]any) int64 {
		body := map[string]any{"type": "memory", "slug": slug, "title": slug, "body": "x"}
		if meta != nil {
			body["metadata"] = meta
		}
		r := ts.post(t, knowledgeBaseURL(pid), ts.adminCookie, body)
		assertStatus(t, r, http.StatusCreated)
		var e knowledgeEntry
		decode(t, r, &e)
		return e.ID
	}
	parent := mkID("parent", nil)
	child := mkID("child", map[string]any{"depends_on": []any{map[string]any{"name": "parent"}}})

	graphURL := fmt.Sprintf("/api/projects/%d/knowledge/graph", pid)
	type gEdge struct {
		Source int64  `json:"source"`
		Target int64  `json:"target"`
		Type   string `json:"type"`
	}
	type gNode struct {
		ID          int64 `json:"id"`
		NeedsReview bool  `json:"needs_review"`
	}
	loadGraph := func() (nodes []gNode, edges []gEdge) {
		resp := ts.get(t, graphURL, ts.adminCookie)
		assertStatus(t, resp, http.StatusOK)
		var g struct {
			Nodes []gNode `json:"nodes"`
			Edges []gEdge `json:"edges"`
		}
		decode(t, resp, &g)
		return g.Nodes, g.Edges
	}

	countDepEdges := func(edges []gEdge) int {
		n := 0
		for _, e := range edges {
			if e.Type == "depends_on" && e.Source == child && e.Target == parent {
				n++
			}
		}
		return n
	}

	// Metadata-derived edge present (child -> parent).
	_, edges := loadGraph()
	if countDepEdges(edges) != 1 {
		t.Fatalf("expected exactly one metadata depends_on edge child(%d)->parent(%d), got %#v", child, parent, edges)
	}
	// Add a hand-made issue_relations depends_on row for the same pair → still 1 (deduped).
	if _, err := db.DB.Exec(`INSERT INTO issue_relations(source_id,target_id,type) VALUES(?,?,'depends_on')`, child, parent); err != nil {
		t.Fatalf("insert relation: %v", err)
	}
	_, edges = loadGraph()
	if got := countDepEdges(edges); got != 1 {
		t.Fatalf("depends_on edge must dedupe against issue_relations row, got %d edges", got)
	}

	// needs_review on the child node reflects a parent revision.
	if _, err := db.DB.Exec(`UPDATE issues SET created_at='2020-01-01 00:00:00' WHERE id=?`, child); err != nil {
		t.Fatalf("exec: %v", err)
	}
	if _, err := db.DB.Exec(`UPDATE issues SET content_revised_at='2026-01-01 00:00:00' WHERE id=?`, parent); err != nil {
		t.Fatalf("exec: %v", err)
	}
	nodes, _ := loadGraph()
	var childNode *gNode
	for i := range nodes {
		if nodes[i].ID == child {
			childNode = &nodes[i]
		}
	}
	if childNode == nil || !childNode.NeedsReview {
		t.Fatalf("child graph node must report needs_review=true after a parent revision: %#v", childNode)
	}
}
