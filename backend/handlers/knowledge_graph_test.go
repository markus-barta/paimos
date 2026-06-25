package handlers_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/markus-barta/paimos/backend/db"
)

// PAI-350: GET /api/projects/{id}/knowledge/graph returns knowledge entries +
// the issues linked to them, with the knowledge-meaningful relation edges only
// (applies_to_memory + generic cross-refs) — structural relations excluded.
func TestKnowledgeGraph_NodesAndEdges(t *testing.T) {
	ts := newTestServer(t)
	projectID := createTestProject(t, ts, "Graph", "GRPH")

	mk := func(seg string, payload map[string]any) int64 {
		body := map[string]any{"type": seg}
		for k, v := range payload {
			body[k] = v
		}
		r := ts.post(t, knowledgeBaseURL(projectID), ts.adminCookie, body)
		assertStatus(t, r, http.StatusCreated)
		var e knowledgeEntry
		decode(t, r, &e)
		return e.ID
	}

	memA := mk("memory", map[string]any{"slug": "mem_a", "title": "Memory A", "body": "x"})
	memB := mk("memory", map[string]any{"slug": "mem_b", "title": "Memory B", "body": "y"})
	sysX := mk("external-system", map[string]any{"slug": "sys_x", "title": "Sentry", "body": "z", "metadata": map[string]any{"url": "https://sentry.example.com"}})

	// A regular ticket that applies to memory A.
	tr := ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.adminCookie, map[string]any{"title": "Ticket 1", "type": "ticket"})
	assertStatus(t, tr, http.StatusCreated)
	ticket := responseID(t, tr)

	rel := func(src, tgt int64, typ string) {
		if _, err := db.DB.Exec(`INSERT INTO issue_relations(source_id,target_id,type) VALUES(?,?,?)`, src, tgt, typ); err != nil {
			t.Fatalf("insert relation %s: %v", typ, err)
		}
	}
	rel(ticket, memA, "applies_to_memory") // ticket → memory (knowledge edge)
	rel(memB, memA, "depends_on")          // memory → memory (knowledge edge)
	rel(memB, memA, "parent")              // structural — must NOT appear in the graph

	resp := ts.get(t, fmt.Sprintf("/api/projects/%d/knowledge/graph", projectID), ts.adminCookie)
	assertStatus(t, resp, http.StatusOK)

	type gNode struct {
		ID    int64  `json:"id"`
		Type  string `json:"type"`
		Slug  string `json:"slug"`
		Title string `json:"title"`
	}
	type gEdge struct {
		Source int64  `json:"source"`
		Target int64  `json:"target"`
		Type   string `json:"type"`
	}
	var graph struct {
		Nodes []gNode `json:"nodes"`
		Edges []gEdge `json:"edges"`
	}
	decode(t, resp, &graph)

	byID := map[int64]gNode{}
	for _, n := range graph.Nodes {
		byID[n.ID] = n
	}
	// 3 knowledge entries + the linked ticket = 4 nodes.
	if len(graph.Nodes) != 4 {
		t.Fatalf("nodes = %d, want 4 (memA, memB, sysX, ticket): %#v", len(graph.Nodes), graph.Nodes)
	}
	for id, wantType := range map[int64]string{memA: "memory", memB: "memory", sysX: "external_system", ticket: "ticket"} {
		if byID[id].Type != wantType {
			t.Errorf("node %d type = %q, want %q", id, byID[id].Type, wantType)
		}
	}

	// Exactly the two knowledge edges; the 'parent' structural edge is excluded.
	if len(graph.Edges) != 2 {
		t.Fatalf("edges = %d, want 2 (applies_to_memory + depends_on, NOT parent): %#v", len(graph.Edges), graph.Edges)
	}
	got := map[string]gEdge{}
	for _, e := range graph.Edges {
		got[e.Type] = e
		if e.Type == "parent" {
			t.Errorf("structural 'parent' edge leaked into the knowledge graph")
		}
	}
	if e := got["applies_to_memory"]; e.Source != ticket || e.Target != memA {
		t.Errorf("applies_to_memory edge = %d→%d, want %d→%d", e.Source, e.Target, ticket, memA)
	}
	if e := got["depends_on"]; e.Source != memB || e.Target != memA {
		t.Errorf("depends_on edge = %d→%d, want %d→%d", e.Source, e.Target, memB, memA)
	}
}

// Empty project → empty graph (never null).
func TestKnowledgeGraph_EmptyProject(t *testing.T) {
	ts := newTestServer(t)
	projectID := createTestProject(t, ts, "Empty", "EMPT")
	resp := ts.get(t, fmt.Sprintf("/api/projects/%d/knowledge/graph", projectID), ts.adminCookie)
	assertStatus(t, resp, http.StatusOK)
	var graph struct {
		Nodes []any `json:"nodes"`
		Edges []any `json:"edges"`
	}
	decode(t, resp, &graph)
	if graph.Nodes == nil || graph.Edges == nil {
		t.Fatalf("nodes/edges must be empty arrays, not null: %#v", graph)
	}
	if len(graph.Nodes) != 0 || len(graph.Edges) != 0 {
		t.Fatalf("empty project graph = %d nodes, %d edges; want 0/0", len(graph.Nodes), len(graph.Edges))
	}
}

// PAI-350 — agent nodes + governance edges: a project agent whose
// non_negotiable_rules reference a memory appears as a node (negative id so it
// never collides with issue ids, type "agent") with a "governed_by" edge to
// that memory. A rule with no memory_ref contributes no edge.
func TestKnowledgeGraph_AgentNodesAndGovernanceEdges(t *testing.T) {
	ts := newTestServer(t)
	pid := createTestProject(t, ts, "AgentGraph", "AGRF")

	mr := ts.post(t, knowledgeBaseURL(pid), ts.adminCookie, map[string]any{
		"type": "memory", "slug": "safe_deploy", "title": "Safe deploy rule", "body": "always backup first",
	})
	assertStatus(t, mr, http.StatusCreated)
	var mem knowledgeEntry
	decode(t, mr, &mem)

	ag := ts.post(t, fmt.Sprintf("/api/projects/%d/agents", pid), ts.adminCookie, map[string]any{
		"name":               "ops",
		"description":        "Infra agent.",
		"slash_command_name": "ops",
		"non_negotiable_rules": []map[string]any{
			{"title": "Backups", "body": "back up first", "memory_ref": "safe_deploy"},
			{"title": "No ref", "body": "general", "memory_ref": ""},
		},
	})
	assertStatus(t, ag, http.StatusCreated)

	resp := ts.get(t, fmt.Sprintf("/api/projects/%d/knowledge/graph", pid), ts.adminCookie)
	assertStatus(t, resp, http.StatusOK)
	type gNode struct {
		ID   int64  `json:"id"`
		Type string `json:"type"`
		Slug string `json:"slug"`
	}
	type gEdge struct {
		Source int64  `json:"source"`
		Target int64  `json:"target"`
		Type   string `json:"type"`
	}
	var g struct {
		Nodes []gNode `json:"nodes"`
		Edges []gEdge `json:"edges"`
	}
	decode(t, resp, &g)

	var agentNode *gNode
	for i := range g.Nodes {
		if g.Nodes[i].Type == "agent" {
			agentNode = &g.Nodes[i]
		}
	}
	if agentNode == nil {
		t.Fatalf("expected an agent node, got %#v", g.Nodes)
	}
	if agentNode.ID >= 0 {
		t.Errorf("agent node id must be negative (namespaced from issue ids), got %d", agentNode.ID)
	}
	if agentNode.Slug != "ops" {
		t.Errorf("agent node slug = %q, want ops", agentNode.Slug)
	}

	governed := 0
	for _, e := range g.Edges {
		if e.Type == "governed_by" {
			governed++
			if e.Source != agentNode.ID || e.Target != mem.ID {
				t.Errorf("governed_by edge = %d→%d, want %d→%d", e.Source, e.Target, agentNode.ID, mem.ID)
			}
		}
	}
	if governed != 1 {
		t.Fatalf("expected exactly 1 governed_by edge (the empty memory_ref rule adds none), got %d", governed)
	}
}
