package handlers_test

import (
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/markus-barta/paimos/backend/db"
)

func Test_ProjectContextEndpoints(t *testing.T) {
	ts := newTestServer(t)

	projectResp := ts.post(t, "/api/projects", ts.adminCookie, map[string]any{
		"name": "Context Project",
		"key":  "CTX",
	})
	assertStatus(t, projectResp, http.StatusCreated)
	var project struct {
		ID int64 `json:"id"`
	}
	decode(t, projectResp, &project)

	repoResp := ts.post(t, "/api/projects/"+strconv.FormatInt(project.ID, 10)+"/repos", ts.adminCookie, map[string]any{
		"url":            "https://github.com/example/context-project",
		"default_branch": "main",
		"label":          "app",
		"sort_order":     0,
	})
	assertStatus(t, repoResp, http.StatusCreated)
	var repo struct {
		ID int64 `json:"id"`
	}
	decode(t, repoResp, &repo)

	issueResp := ts.post(t, "/api/projects/"+strconv.FormatInt(project.ID, 10)+"/issues", ts.adminCookie, map[string]any{
		"title": "Anchored issue",
		"type":  "ticket",
	})
	assertStatus(t, issueResp, http.StatusCreated)
	var issue struct {
		ID       int64  `json:"id"`
		IssueKey string `json:"issue_key"`
	}
	decode(t, issueResp, &issue)

	relatedResp := ts.post(t, "/api/projects/"+strconv.FormatInt(project.ID, 10)+"/issues", ts.adminCookie, map[string]any{
		"title": "Neighbor issue",
		"type":  "ticket",
	})
	assertStatus(t, relatedResp, http.StatusCreated)
	var related struct {
		ID int64 `json:"id"`
	}
	decode(t, relatedResp, &related)

	linkResp := ts.post(t, "/api/issues/"+strconv.FormatInt(issue.ID, 10)+"/relations", ts.adminCookie, map[string]any{
		"target_id": related.ID,
		"type":      "related",
	})
	assertStatus(t, linkResp, http.StatusCreated)

	// PAI-358: PUT /manifest deleted with the project_manifests table.
	// NFR/ADR retrieval that this test used to assert on now flows via
	// the knowledge-plane (PAI-338) issue path, not the manifest blob.

	anchorResp := ts.post(t, "/api/projects/"+strconv.FormatInt(project.ID, 10)+"/anchors", ts.adminCookie, map[string]any{
		"repo_id":        repo.ID,
		"schema_version": "1",
		"generated_at":   "2026-04-25T20:00:00Z",
		"repo_revision":  "abc123",
		"anchors": map[string]any{
			issue.IssueKey: []map[string]any{{
				"file":       "backend/handlers/context.go",
				"line":       42,
				"label":      "entry point",
				"confidence": "declared",
				"symbol": map[string]any{
					"name":       "RetrieveProjectContext",
					"kind":       "function",
					"start_line": 40,
					"end_line":   60,
					"language":   "go",
				},
			}},
		},
	})
	assertStatus(t, anchorResp, http.StatusOK)

	getAnchors := ts.get(t, "/api/issues/"+issue.IssueKey+"/anchors", ts.adminCookie)
	assertStatus(t, getAnchors, http.StatusOK)
	var anchors []map[string]any
	decode(t, getAnchors, &anchors)
	if len(anchors) != 1 {
		t.Fatalf("anchors: got %d want 1", len(anchors))
	}
	if anchors[0]["deep_link"] == nil || anchors[0]["deep_link"] == "" {
		t.Fatalf("deep_link missing: %#v", anchors[0])
	}
	projectAnchorsResp := ts.get(t, "/api/projects/"+strconv.FormatInt(project.ID, 10)+"/anchors", ts.adminCookie)
	assertStatus(t, projectAnchorsResp, http.StatusOK)
	var projectAnchors []map[string]any
	decode(t, projectAnchorsResp, &projectAnchors)
	if len(projectAnchors) != 1 {
		t.Fatalf("project anchors: got %d want 1", len(projectAnchors))
	}
	if projectAnchors[0]["issue_key"] != issue.IssueKey {
		t.Fatalf("project anchor issue_key=%v, want %s", projectAnchors[0]["issue_key"], issue.IssueKey)
	}
	if projectAnchors[0]["repo_label"] != "app" {
		t.Fatalf("project anchor repo_label=%v, want app", projectAnchors[0]["repo_label"])
	}

	graphResp := ts.get(t, "/api/projects/"+strconv.FormatInt(project.ID, 10)+"/graph?root=issue:"+strconv.FormatInt(issue.ID, 10)+"&depth=2", ts.adminCookie)
	assertStatus(t, graphResp, http.StatusOK)
	var graph struct {
		Nodes []map[string]any `json:"nodes"`
		Edges []map[string]any `json:"edges"`
	}
	decode(t, graphResp, &graph)
	if len(graph.Edges) < 3 {
		t.Fatalf("graph edges: got %d want at least 3", len(graph.Edges))
	}
	foundSymbolNode := false
	for _, node := range graph.Nodes {
		if node["entity_type"] == "symbol" {
			foundSymbolNode = true
			break
		}
	}
	if !foundSymbolNode {
		t.Fatalf("graph missing symbol node: %#v", graph.Nodes)
	}

	retrieveResp := ts.post(t, "/api/projects/"+strconv.FormatInt(project.ID, 10)+"/retrieve", ts.adminCookie, map[string]any{
		"q": "Anchored",
		"k": 10,
	})
	assertStatus(t, retrieveResp, http.StatusOK)
	var retrieve struct {
		Hits     []map[string]any `json:"hits"`
		Strategy map[string]any   `json:"strategy"`
		Meta     map[string]any   `json:"meta"`
	}
	decode(t, retrieveResp, &retrieve)
	if len(retrieve.Hits) == 0 {
		t.Fatalf("retrieve hits empty")
	}
	if retrieve.Strategy["fusion"] != "rrf" {
		t.Fatalf("retrieve strategy missing rrf: %#v", retrieve.Strategy)
	}
	if retrieve.Meta["fusion"] != "rrf" {
		t.Fatalf("retrieve meta missing rrf: %#v", retrieve.Meta)
	}
	if retrieve.Meta["embedding_indexing"] != "async" {
		t.Fatalf("retrieve meta missing async embedding indexing: %#v", retrieve.Meta)
	}
	foundExpanded := false
	for _, hit := range retrieve.Hits {
		if hit["expanded_from"] != nil {
			foundExpanded = true
			break
		}
	}
	if !foundExpanded {
		t.Fatalf("retrieve missing expanded graph hit: %#v", retrieve.Hits)
	}
	waitForProjectEmbeddings(t, project.ID, 4)

	vectorResp := ts.post(t, "/api/projects/"+strconv.FormatInt(project.ID, 10)+"/retrieve", ts.adminCookie, map[string]any{
		"q": "RetrieveProjectContext",
		"k": 10,
	})
	assertStatus(t, vectorResp, http.StatusOK)
	var vectorRetrieve struct {
		Meta map[string]any `json:"meta"`
	}
	decode(t, vectorResp, &vectorRetrieve)
	stages, ok := vectorRetrieve.Meta["stages"].(map[string]any)
	if !ok {
		t.Fatalf("retrieve meta stages missing: %#v", vectorRetrieve.Meta)
	}
	if vectorCount, ok := stages["vector"].(float64); !ok || vectorCount <= 0 {
		t.Fatalf("retrieve vector stage count = %#v, want > 0 after async index", stages["vector"])
	}

	anchorSearchResp := ts.post(t, "/api/projects/"+strconv.FormatInt(project.ID, 10)+"/retrieve", ts.adminCookie, map[string]any{
		"q": "context.go",
		"k": 10,
	})
	assertStatus(t, anchorSearchResp, http.StatusOK)
	var anchorSearch struct {
		Hits []map[string]any `json:"hits"`
	}
	decode(t, anchorSearchResp, &anchorSearch)
	foundAnchor := false
	for _, hit := range anchorSearch.Hits {
		if hit["entity_type"] == "anchor" {
			foundAnchor = true
			break
		}
	}
	if !foundAnchor {
		t.Fatalf("retrieve missing anchor hit: %#v", anchorSearch.Hits)
	}

	// PAI-358: NFR/ADR manifest retrieval assertions removed with the
	// manifest table. These payloads are now first-class knowledge
	// entries (memory/runbook/guideline) and exercised by knowledge_test.go.

	symbolSearchResp := ts.post(t, "/api/projects/"+strconv.FormatInt(project.ID, 10)+"/retrieve", ts.adminCookie, map[string]any{
		"q": "RetrieveProjectContext function",
		"k": 10,
	})
	assertStatus(t, symbolSearchResp, http.StatusOK)
	var symbolSearch struct {
		Hits []map[string]any `json:"hits"`
	}
	decode(t, symbolSearchResp, &symbolSearch)
	foundSymbolHit := false
	for _, hit := range symbolSearch.Hits {
		if hit["entity_type"] == "symbol" {
			foundSymbolHit = true
			break
		}
	}
	if !foundSymbolHit {
		t.Fatalf("retrieve missing symbol hit: %#v", symbolSearch.Hits)
	}

	blastResp := ts.get(t, "/api/projects/"+strconv.FormatInt(project.ID, 10)+"/graph/blast-radius?issue="+issue.IssueKey+"&depth=2", ts.adminCookie)
	assertStatus(t, blastResp, http.StatusOK)
	var blast struct {
		Reached map[string][]map[string]any `json:"reached"`
	}
	decode(t, blastResp, &blast)
	if len(blast.Reached["issue"]) == 0 {
		t.Fatalf("blast radius issue set empty: %#v", blast.Reached)
	}
	if len(blast.Reached["anchor"]) == 0 {
		t.Fatalf("blast radius anchor set empty: %#v", blast.Reached)
	}
	if len(blast.Reached["symbol"]) == 0 {
		t.Fatalf("blast radius symbol set empty: %#v", blast.Reached)
	}
}

func waitForProjectEmbeddings(t *testing.T, projectID int64, wantAtLeast int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var count int
	for time.Now().Before(deadline) {
		if err := db.DB.QueryRow(`
			SELECT COUNT(*)
			FROM entity_embeddings
			WHERE project_id = ? AND model = 'local-hash-v1'
		`, projectID).Scan(&count); err != nil {
			t.Fatalf("count embeddings: %v", err)
		}
		if count >= wantAtLeast {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for async embeddings for project %d: got %d, want at least %d", projectID, count, wantAtLeast)
}
