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

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/markus-barta/paimos/backend/db"
)

// parentEdgeSources returns the source_ids of every 'parent' edge that points
// at childID. Under the PAI-584 P1 invariant there is at most one.
func parentEdgeSources(t *testing.T, childID int64) []int64 {
	t.Helper()
	rows, err := db.DB.Query(
		`SELECT source_id FROM issue_relations WHERE target_id=? AND type='parent' ORDER BY source_id`,
		childID)
	if err != nil {
		t.Fatalf("query parent edges: %v", err)
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var s int64
		if err := rows.Scan(&s); err != nil {
			t.Fatalf("scan parent edge: %v", err)
		}
		out = append(out, s)
	}
	return out
}

// assertSingleParentEdge asserts exactly one parent edge want→childID exists.
func assertSingleParentEdge(t *testing.T, childID, want int64) {
	t.Helper()
	got := parentEdgeSources(t, childID)
	if len(got) != 1 || got[0] != want {
		t.Fatalf("parent edges for child %d = %v, want exactly [%d]", childID, got, want)
	}
}

// TestParentEdgeDualWrite covers PAI-584 P1: the DB parent-sync triggers keep
// the issue_relations 'parent' edge (source=parent, target=child) in lockstep
// with issues.parent_id across every write path, and a cleared parent removes
// the edge. Each subtest drives the real HTTP surface so the trigger is
// exercised end-to-end through the actual handlers, not in isolation.
func TestParentEdgeDualWrite(t *testing.T) {
	ts := newTestServer(t)
	projID := seedBatchProject(t, "PAI", "PAI")
	// CreateIssue is keyed by numeric project id ({id}); batch is keyed by {key}.
	createURL := fmt.Sprintf("/api/projects/%d/issues", projID)

	// Two epics to parent under / reparent between.
	epic1 := responseID(t, ts.post(t, createURL, ts.adminCookie, map[string]any{
		"title": "Epic 1", "type": "epic", "status": "backlog",
	}))
	epic2 := responseID(t, ts.post(t, createURL, ts.adminCookie, map[string]any{
		"title": "Epic 2", "type": "epic", "status": "backlog",
	}))
	if epic1 == 0 || epic2 == 0 {
		t.Fatalf("failed to create epics: epic1=%d epic2=%d", epic1, epic2)
	}

	t.Run("CreateIssue with parent writes the edge", func(t *testing.T) {
		ticket := responseID(t, ts.post(t, createURL, ts.adminCookie, map[string]any{
			"title": "Child ticket", "type": "ticket", "status": "backlog", "parent_id": epic1,
		}))
		if ticket == 0 {
			t.Fatal("create failed")
		}
		assertSingleParentEdge(t, ticket, epic1)
	})

	t.Run("UpdateIssue reparent moves the edge", func(t *testing.T) {
		ticket := responseID(t, ts.post(t, createURL, ts.adminCookie, map[string]any{
			"title": "Reparent me", "type": "ticket", "status": "backlog", "parent_id": epic1,
		}))
		assertSingleParentEdge(t, ticket, epic1)

		resp := ts.put(t, "/api/issues/"+itoa(ticket), ts.adminCookie, map[string]any{"parent_id": epic2})
		assertStatus(t, resp, http.StatusOK)
		resp.Body.Close()
		assertSingleParentEdge(t, ticket, epic2) // old edge gone, new edge present
	})

	t.Run("UpdateIssue clear parent removes the edge", func(t *testing.T) {
		ticket := responseID(t, ts.post(t, createURL, ts.adminCookie, map[string]any{
			"title": "Orphan me", "type": "ticket", "status": "backlog", "parent_id": epic1,
		}))
		assertSingleParentEdge(t, ticket, epic1)

		resp := ts.put(t, "/api/issues/"+itoa(ticket), ts.adminCookie, map[string]any{"parent_id": nil})
		assertStatus(t, resp, http.StatusOK)
		resp.Body.Close()
		if got := parentEdgeSources(t, ticket); len(got) != 0 {
			t.Fatalf("parent edges after clear = %v, want none", got)
		}
	})

	t.Run("UpdateIssue without parent_id leaves the edge intact", func(t *testing.T) {
		ticket := responseID(t, ts.post(t, createURL, ts.adminCookie, map[string]any{
			"title": "Keep parent", "type": "ticket", "status": "backlog", "parent_id": epic1,
		}))
		assertSingleParentEdge(t, ticket, epic1)

		resp := ts.put(t, "/api/issues/"+itoa(ticket), ts.adminCookie, map[string]any{"title": "Renamed"})
		assertStatus(t, resp, http.StatusOK)
		resp.Body.Close()
		assertSingleParentEdge(t, ticket, epic1) // untouched — parent_id not in request
	})

	t.Run("CloneIssue inherits the parent edge", func(t *testing.T) {
		ticket := responseID(t, ts.post(t, createURL, ts.adminCookie, map[string]any{
			"title": "Clone source", "type": "ticket", "status": "backlog", "parent_id": epic2,
		}))
		assertSingleParentEdge(t, ticket, epic2)

		clone := responseID(t, ts.post(t, "/api/issues/"+itoa(ticket)+"/clone", ts.adminCookie, map[string]any{}))
		if clone == 0 {
			t.Fatal("clone failed")
		}
		assertSingleParentEdge(t, clone, epic2)
	})

	t.Run("CreateIssuesBatch writes edges (parent_id and parent_ref)", func(t *testing.T) {
		body := []map[string]any{
			{"title": "Batch epic", "type": "epic"},
			{"title": "Via parent_ref", "type": "ticket", "parent_ref": "#0"},
			{"title": "Via parent_id", "type": "ticket", "parent_id": epic1},
		}
		resp := ts.post(t, "/api/projects/PAI/issues/batch", ts.adminCookie, body)
		assertStatus(t, resp, http.StatusCreated)
		var out struct {
			Issues []map[string]any `json:"issues"`
		}
		decode(t, resp, &out)
		if len(out.Issues) != 3 {
			t.Fatalf("len(issues)=%d, want 3", len(out.Issues))
		}
		batchEpic := int64(out.Issues[0]["id"].(float64))
		viaRef := int64(out.Issues[1]["id"].(float64))
		viaID := int64(out.Issues[2]["id"].(float64))
		assertSingleParentEdge(t, viaRef, batchEpic)
		assertSingleParentEdge(t, viaID, epic1)
	})

	t.Run("UpdateIssuesBatch reparents the edge", func(t *testing.T) {
		ticket := responseID(t, ts.post(t, createURL, ts.adminCookie, map[string]any{
			"title": "Batch reparent", "type": "ticket", "status": "backlog", "parent_id": epic1,
		}))
		assertSingleParentEdge(t, ticket, epic1)

		body := []map[string]any{
			{"ref": itoa(ticket), "fields": map[string]any{"parent_id": epic2}},
		}
		resp := ts.patch(t, "/api/issues", ts.adminCookie, body)
		assertStatus(t, resp, http.StatusOK)
		resp.Body.Close()
		assertSingleParentEdge(t, ticket, epic2)
	})
}
