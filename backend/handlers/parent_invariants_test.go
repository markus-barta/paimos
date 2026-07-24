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
	"net/http"
	"testing"

	"github.com/inspr-at/paimos/backend/db"
)

// TestParentInvariants covers PAI-584 P5: the one-parent-per-child DB index,
// cycle guards (relation API + reparent), and the hierarchy type guard.
func TestParentInvariants(t *testing.T) {
	ts := newTestServer(t)
	projID := seedBatchProject(t, "PAI", "PAI")

	ins := func(num int, typ string) int64 {
		t.Helper()
		res, err := db.DB.Exec(
			`INSERT INTO issues(project_id, issue_number, type, title, status, priority)
			 VALUES(?,?,?,?,'backlog','medium')`, projID, num, typ, typ)
		if err != nil {
			t.Fatalf("seed %s: %v", typ, err)
		}
		id, _ := res.LastInsertId()
		return id
	}
	addParent := func(parent, child int64) *http.Response {
		return ts.post(t, "/api/issues/"+itoa(parent)+"/relations", ts.adminCookie,
			map[string]any{"target_id": child, "type": "parent"})
	}

	epic := ins(1, "epic")
	epic2 := ins(2, "epic")
	tA := ins(3, "ticket")
	tB := ins(4, "ticket")

	t.Run("partial unique index rejects a second parent (raw insert)", func(t *testing.T) {
		if _, err := db.DB.Exec(
			`INSERT INTO issue_relations(source_id, target_id, type) VALUES(?,?,'parent')`, epic, tA); err != nil {
			t.Fatalf("first parent edge: %v", err)
		}
		_, err := db.DB.Exec(
			`INSERT INTO issue_relations(source_id, target_id, type) VALUES(?,?,'parent')`, epic2, tA)
		if err == nil {
			t.Fatalf("second parent edge for the same child was accepted — unique index missing")
		}
		// clean up so later subtests start fresh
		if _, err := db.DB.Exec(`DELETE FROM issue_relations WHERE target_id=? AND type='parent'`, tA); err != nil {
			t.Fatalf("cleanup: %v", err)
		}
	})

	t.Run("relation API rejects a parent cycle", func(t *testing.T) {
		// tA parent of tB (edge tA→tB).
		resp := addParent(tA, tB)
		assertStatus(t, resp, http.StatusCreated)
		resp.Body.Close()
		// tB parent of tA would close the cycle tA→tB→tA.
		resp = addParent(tB, tA)
		assertStatus(t, resp, http.StatusUnprocessableEntity)
		resp.Body.Close()
		if got := parentEdgeSources(t, tA); len(got) != 0 {
			t.Fatalf("cycle edge was created: tA parents = %v", got)
		}
	})

	t.Run("relation API rejects an invalid hierarchy (epic as child)", func(t *testing.T) {
		// An epic cannot have a parent — making it a child must be rejected.
		resp := addParent(tB, epic)
		assertStatus(t, resp, http.StatusUnprocessableEntity)
		resp.Body.Close()
	})

	t.Run("reparent (parent_id) rejects a cycle", func(t *testing.T) {
		c1 := ins(5, "ticket")
		c2 := ins(6, "ticket")
		// c1 parent of c2 via parent_id (trigger makes the edge).
		resp := ts.put(t, "/api/issues/"+itoa(c2), ts.adminCookie, map[string]any{"parent_id": c1})
		assertStatus(t, resp, http.StatusOK)
		resp.Body.Close()
		// Reparent c1 under c2 → cycle c1→c2→c1.
		resp = ts.put(t, "/api/issues/"+itoa(c1), ts.adminCookie, map[string]any{"parent_id": c2})
		assertStatus(t, resp, http.StatusUnprocessableEntity)
		resp.Body.Close()
	})
}
