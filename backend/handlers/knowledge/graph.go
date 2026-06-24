package knowledge

import (
	"net/http"
	"strings"

	"github.com/markus-barta/paimos/backend/db"
)

// GraphHandler is GET /api/projects/{id}/knowledge/graph (PAI-350).
//
// It returns the knowledge graph for a project — derived purely from
// existing data, no schema changes:
//   - nodes: the project's knowledge entries (memory / runbook /
//     external_system / related_project / guideline), plus any other issue
//     (ticket / task / epic …) that is linked to one of them.
//   - edges: issue_relations of the knowledge-meaningful types between those
//     nodes — `applies_to_memory` (ticket → memory) and the generic
//     cross-references (depends_on, impacts, follows_from, blocks, related).
//
// Structural relations (parent / groups / sprint / cost_unit / release) are
// intentionally excluded — they describe work hierarchy, not knowledge.
type graphNode struct {
	ID             int64  `json:"id"`
	Type           string `json:"type"`
	Slug           string `json:"slug"`
	Title          string `json:"title"`
	ReferenceCount int64  `json:"reference_count"`
}

type graphEdge struct {
	Source int64  `json:"source"`
	Target int64  `json:"target"`
	Type   string `json:"type"`
}

// knowledgeEdgeTypes are the issue_relations types that represent a knowledge
// cross-reference (as opposed to a structural work relation).
var knowledgeEdgeTypes = []string{
	"applies_to_memory", "depends_on", "impacts", "follows_from", "blocks", "related",
}

func GraphHandler(w http.ResponseWriter, r *http.Request) {
	projectID, ok := projectIDFromRequest(r)
	if !ok {
		writeError(w, r, "invalid project id", http.StatusBadRequest)
		return
	}

	// Seed nodes with every knowledge entry in the project.
	entries, err := loadAllTypes(projectID)
	if err != nil {
		writeError(w, r, "query failed", http.StatusInternalServerError)
		return
	}
	nodes := make(map[int64]graphNode, len(entries))
	knownIDs := make([]int64, 0, len(entries))
	for _, e := range entries {
		nodes[e.ID] = graphNode{ID: e.ID, Type: e.Type, Slug: e.Slug, Title: e.Title, ReferenceCount: e.ReferenceCount}
		knownIDs = append(knownIDs, e.ID)
	}

	edges := []graphEdge{}
	if len(knownIDs) > 0 {
		// Edges touching at least one knowledge entry, restricted to the
		// knowledge-meaningful relation types.
		args := make([]any, 0, len(knowledgeEdgeTypes)+2*len(knownIDs))
		for _, t := range knowledgeEdgeTypes {
			args = append(args, t)
		}
		for _, id := range knownIDs {
			args = append(args, id)
		}
		for _, id := range knownIDs {
			args = append(args, id)
		}
		inEdgeTypes := placeholders(len(knowledgeEdgeTypes))
		inIDs := placeholders(len(knownIDs))
		q := "SELECT source_id, target_id, type FROM issue_relations WHERE type IN (" + inEdgeTypes + ") AND (source_id IN (" + inIDs + ") OR target_id IN (" + inIDs + "))" // #nosec G202 -- only ?-placeholders are concatenated into the IN clauses; every value is bound as a parameterized arg via args... below.
		rows, err := db.DB.Query(q, args...)
		if err != nil {
			writeError(w, r, "query failed", http.StatusInternalServerError)
			return
		}
		extra := map[int64]struct{}{}
		for rows.Next() {
			var src, tgt int64
			var et string
			if err := rows.Scan(&src, &tgt, &et); err != nil {
				rows.Close()
				writeError(w, r, "query failed", http.StatusInternalServerError)
				return
			}
			edges = append(edges, graphEdge{Source: src, Target: tgt, Type: et})
			if _, ok := nodes[src]; !ok {
				extra[src] = struct{}{}
			}
			if _, ok := nodes[tgt]; !ok {
				extra[tgt] = struct{}{}
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			writeError(w, r, "query failed", http.StatusInternalServerError)
			return
		}
		rows.Close()

		// Hydrate the non-knowledge endpoints (tickets/tasks/…) as nodes.
		if len(extra) > 0 {
			ids := make([]any, 0, len(extra))
			for id := range extra {
				ids = append(ids, id)
			}
			nr, err := db.DB.Query("SELECT id, type, COALESCE(slug,''), title FROM issues WHERE deleted_at IS NULL AND id IN ("+placeholders(len(ids))+")", ids...) // #nosec G202 -- only ?-placeholders are concatenated; ids are bound as parameterized args.
			if err != nil {
				writeError(w, r, "query failed", http.StatusInternalServerError)
				return
			}
			for nr.Next() {
				var n graphNode
				if err := nr.Scan(&n.ID, &n.Type, &n.Slug, &n.Title); err != nil {
					nr.Close()
					writeError(w, r, "query failed", http.StatusInternalServerError)
					return
				}
				nodes[n.ID] = n
			}
			nr.Close()
		}
	}

	// Drop edges whose endpoints didn't resolve to a live node (e.g. a
	// soft-deleted ticket), so the graph never dangles.
	out := make([]graphEdge, 0, len(edges))
	for _, e := range edges {
		if _, okS := nodes[e.Source]; !okS {
			continue
		}
		if _, okT := nodes[e.Target]; !okT {
			continue
		}
		out = append(out, e)
	}

	nodeList := make([]graphNode, 0, len(nodes))
	for _, n := range nodes {
		nodeList = append(nodeList, n)
	}
	writeJSON(w, http.StatusOK, map[string]any{"nodes": nodeList, "edges": out})
}

// placeholders returns "?,?,…" with n placeholders for a SQL IN clause.
func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat("?,", n-1) + "?"
}
