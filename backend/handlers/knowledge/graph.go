package knowledge

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
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
	// PAI-351 slice 2 — true for a memory entry whose depends_on parent was
	// revised after its last review (computed on read). false for non-memory.
	NeedsReview bool `json:"needs_review"`
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
	computeNeedsReview(entries)
	nodes := make(map[int64]graphNode, len(entries))
	knownIDs := make([]int64, 0, len(entries))
	for _, e := range entries {
		nodes[e.ID] = graphNode{ID: e.ID, Type: e.Type, Slug: e.Slug, Title: e.Title, ReferenceCount: e.ReferenceCount, NeedsReview: e.NeedsReview}
		knownIDs = append(knownIDs, e.ID)
	}

	edges := []graphEdge{}
	// Dedup key (source, target, type) so a metadata-derived depends_on edge
	// never doubles a hand-made issue_relations depends_on row.
	seen := map[[3]any]struct{}{}
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
			seen[[3]any{src, tgt, et}] = struct{}{}
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

	// PAI-351 — also surface declared memory depends_on (category_metadata) as
	// edges, deduped against any hand-made issue_relations depends_on row.
	// Source = the dependent, Target = the parent (matching the issue_relations
	// depends_on direction + dependents.go reverse semantics). Both endpoints
	// are memory entries (already nodes); no SQL, built from loaded entries.
	memSlugToID := make(map[string]int64)
	for _, e := range entries {
		if e.Type == "memory" {
			memSlugToID[e.Slug] = e.ID
		}
	}
	for _, e := range entries {
		if e.Type != "memory" {
			continue
		}
		deps, ok := e.Metadata["depends_on"].([]any)
		if !ok {
			continue
		}
		for _, item := range deps {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			name, _ := m["name"].(string)
			if name == "" || name == e.Slug {
				continue
			}
			tgt, ok := memSlugToID[name]
			if !ok || tgt == e.ID {
				continue
			}
			key := [3]any{e.ID, tgt, "depends_on"}
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			edges = append(edges, graphEdge{Source: e.ID, Target: tgt, Type: "depends_on"})
		}
	}

	// PAI-350 — surface the project's AGENTS as nodes, with an edge to each
	// memory their non_negotiable_rules reference (the governance view: which
	// agent is bound by which rule). Agents live in their own table with their
	// own id sequence, so namespace them as NEGATIVE ids to never collide with
	// the positive issue/memory node ids.
	ar, err := db.DB.Query(`SELECT id, name, COALESCE(non_negotiable_rules,'[]') FROM project_agents WHERE project_id = ?`, projectID)
	if err != nil {
		writeError(w, r, "query failed", http.StatusInternalServerError)
		return
	}
	for ar.Next() {
		var aid int64
		var name, rulesJSON string
		if err := ar.Scan(&aid, &name, &rulesJSON); err != nil {
			ar.Close()
			writeError(w, r, "query failed", http.StatusInternalServerError)
			return
		}
		nodeID := -aid
		nodes[nodeID] = graphNode{ID: nodeID, Type: "agent", Slug: name, Title: name}
		var rules []struct {
			MemoryRef string `json:"memory_ref"`
		}
		_ = json.Unmarshal([]byte(rulesJSON), &rules)
		for _, rule := range rules {
			if rule.MemoryRef == "" {
				continue
			}
			tgt, ok := memSlugToID[rule.MemoryRef]
			if !ok {
				continue
			}
			key := [3]any{nodeID, tgt, "governed_by"}
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			edges = append(edges, graphEdge{Source: nodeID, Target: tgt, Type: "governed_by"})
		}
	}
	ar.Close()

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

	// PAI-350 — keep big graphs renderable: optionally focus on one node's
	// N-hop neighborhood (?focus=<id>&hops=<n>, default 2), then cap the node
	// count (?limit, default 500) by keeping the most-connected nodes. `total`
	// + `truncated` let the UI say "showing X of Y".
	q := r.URL.Query()
	total := len(nodes)
	limit := 500
	if v, err := strconv.Atoi(q.Get("limit")); err == nil && v > 0 {
		limit = v
		if limit > 5000 {
			limit = 5000
		}
	}

	// Adjacency (undirected) + degree from the live edges.
	adj := make(map[int64][]int64, len(nodes))
	degree := make(map[int64]int, len(nodes))
	for _, e := range out {
		adj[e.Source] = append(adj[e.Source], e.Target)
		adj[e.Target] = append(adj[e.Target], e.Source)
		degree[e.Source]++
		degree[e.Target]++
	}

	keep := map[int64]struct{}{}
	if focus, err := strconv.ParseInt(q.Get("focus"), 10, 64); err == nil {
		if _, ok := nodes[focus]; ok {
			hops := 2
			if v, err := strconv.Atoi(q.Get("hops")); err == nil && v > 0 {
				hops = v
				if hops > 5 {
					hops = 5
				}
			}
			keep[focus] = struct{}{}
			frontier := []int64{focus}
			for h := 0; h < hops && len(frontier) > 0; h++ {
				var next []int64
				for _, n := range frontier {
					for _, m := range adj[n] {
						if _, seen := keep[m]; !seen {
							keep[m] = struct{}{}
							next = append(next, m)
						}
					}
				}
				frontier = next
			}
		}
	}
	if len(keep) == 0 { // no (valid) focus → every node is a candidate
		for id := range nodes {
			keep[id] = struct{}{}
		}
	}

	// Cap to the most-connected nodes when over the limit.
	if len(keep) > limit {
		ids := make([]int64, 0, len(keep))
		for id := range keep {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool {
			if degree[ids[i]] != degree[ids[j]] {
				return degree[ids[i]] > degree[ids[j]]
			}
			return ids[i] < ids[j] // stable tie-break
		})
		keep = make(map[int64]struct{}, limit)
		for _, id := range ids[:limit] {
			keep[id] = struct{}{}
		}
	}

	nodeList := make([]graphNode, 0, len(keep))
	for id := range keep {
		nodeList = append(nodeList, nodes[id])
	}
	keptEdges := make([]graphEdge, 0, len(out))
	for _, e := range out {
		if _, okS := keep[e.Source]; !okS {
			continue
		}
		if _, okT := keep[e.Target]; !okT {
			continue
		}
		keptEdges = append(keptEdges, e)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"nodes":     nodeList,
		"edges":     keptEdges,
		"total":     total,
		"truncated": len(nodeList) < total,
	})
}

// placeholders returns "?,?,…" with n placeholders for a SQL IN clause.
func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat("?,", n-1) + "?"
}
