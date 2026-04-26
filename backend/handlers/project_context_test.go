package handlers_test

import (
	"net/http"
	"strconv"
	"testing"
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

	manifestResp := ts.put(t, "/api/projects/"+strconv.FormatInt(project.ID, 10)+"/manifest", ts.adminCookie, map[string]any{
		"data": map[string]any{
			"stack":    map[string]any{"languages": []string{"Go", "TypeScript"}},
			"commands": map[string]any{"build": "make build", "test": "make test"},
			"nfrs": []map[string]any{{
				"title":       "Latency budget",
				"description": "Interactive paths should stay under 200ms p95.",
			}},
			"adrs": []map[string]any{{
				"title":   "Context indexing",
				"status":  "accepted",
				"summary": "Use a dedicated lexical index for anchors and manifest context.",
			}},
		},
	})
	assertStatus(t, manifestResp, http.StatusOK)

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

	manifestSearchResp := ts.post(t, "/api/projects/"+strconv.FormatInt(project.ID, 10)+"/retrieve", ts.adminCookie, map[string]any{
		"q": "latency budget",
		"k": 10,
	})
	assertStatus(t, manifestSearchResp, http.StatusOK)
	var manifestSearch struct {
		Hits []map[string]any `json:"hits"`
	}
	decode(t, manifestSearchResp, &manifestSearch)
	foundManifestSection := false
	for _, hit := range manifestSearch.Hits {
		if hit["entity_type"] == "nfr" {
			foundManifestSection = true
			break
		}
	}
	if !foundManifestSection {
		t.Fatalf("retrieve missing nfr hit: %#v", manifestSearch.Hits)
	}

	adrSearchResp := ts.post(t, "/api/projects/"+strconv.FormatInt(project.ID, 10)+"/retrieve", ts.adminCookie, map[string]any{
		"q": "accepted lexical index",
		"k": 10,
	})
	assertStatus(t, adrSearchResp, http.StatusOK)
	var adrSearch struct {
		Hits []map[string]any `json:"hits"`
	}
	decode(t, adrSearchResp, &adrSearch)
	foundADR := false
	for _, hit := range adrSearch.Hits {
		if hit["entity_type"] == "adr" {
			foundADR = true
			break
		}
	}
	if !foundADR {
		t.Fatalf("retrieve missing adr hit: %#v", adrSearch.Hits)
	}
	foundVectorSource := false
	for _, hit := range adrSearch.Hits {
		if hit["entity_type"] == "adr" {
			if rawSources, ok := hit["sources"].([]any); ok {
				for _, src := range rawSources {
					if s, ok := src.(string); ok && s == "vector" {
						foundVectorSource = true
					}
				}
			}
		}
	}
	if !foundVectorSource {
		t.Fatalf("retrieve missing vector source on adr hit: %#v", adrSearch.Hits)
	}

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
