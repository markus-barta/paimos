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

	retrieveResp := ts.post(t, "/api/projects/"+strconv.FormatInt(project.ID, 10)+"/retrieve", ts.adminCookie, map[string]any{
		"q": "Anchored",
		"k": 10,
	})
	assertStatus(t, retrieveResp, http.StatusOK)
	var retrieve struct {
		Hits []map[string]any `json:"hits"`
	}
	decode(t, retrieveResp, &retrieve)
	if len(retrieve.Hits) == 0 {
		t.Fatalf("retrieve hits empty")
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
}
