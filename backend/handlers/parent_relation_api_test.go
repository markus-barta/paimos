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

// TestParentRelationAPI covers PAI-584 P4: the public relation API exposes the
// `parent` edge, enforces one parent per child, and auto-translates legacy
// epic-sourced `groups` relations into `parent` (so old agent calls produce a
// fully-visible link) while leaving cost_unit/release `groups` untouched.
func TestParentRelationAPI(t *testing.T) {
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
	edgeType := func(parent, child int64) string {
		t.Helper()
		var typ string
		_ = db.DB.QueryRow(
			`SELECT type FROM issue_relations WHERE source_id=? AND target_id=?`, parent, child).Scan(&typ)
		return typ
	}

	epic := ins(1, "epic")
	epic2 := ins(2, "epic")
	ticketA := ins(3, "ticket")
	ticketB := ins(4, "ticket")
	cu := ins(5, "cost_unit")
	cuTicket := ins(6, "ticket")

	t.Run("type=parent creates a parent edge", func(t *testing.T) {
		resp := ts.post(t, "/api/issues/"+itoa(epic)+"/relations", ts.adminCookie,
			map[string]any{"target_id": ticketA, "type": "parent"})
		assertStatus(t, resp, http.StatusCreated)
		resp.Body.Close()
		if got := edgeType(epic, ticketA); got != "parent" {
			t.Fatalf("edge epic→ticketA type=%q, want parent", got)
		}
	})

	t.Run("epic-sourced groups is auto-translated to parent", func(t *testing.T) {
		resp := ts.post(t, "/api/issues/"+itoa(epic2)+"/relations", ts.adminCookie,
			map[string]any{"target_id": ticketB, "type": "groups"})
		assertStatus(t, resp, http.StatusCreated)
		resp.Body.Close()
		if got := edgeType(epic2, ticketB); got != "parent" {
			t.Fatalf("epic-sourced groups stored as %q, want parent (auto-translate)", got)
		}
	})

	t.Run("second parent is rejected; same parent is idempotent", func(t *testing.T) {
		// ticketA already parented to epic (subtest 1). A different parent → 409.
		resp := ts.post(t, "/api/issues/"+itoa(epic2)+"/relations", ts.adminCookie,
			map[string]any{"target_id": ticketA, "type": "parent"})
		assertStatus(t, resp, http.StatusConflict)
		resp.Body.Close()
		if got := edgeType(epic2, ticketA); got != "" {
			t.Fatalf("second parent edge epic2→ticketA was created (%q); want none", got)
		}
		if got := edgeType(epic, ticketA); got != "parent" {
			t.Fatalf("original parent edge lost after rejected reparent: %q", got)
		}
		// Re-adding the SAME parent is a no-op success.
		resp = ts.post(t, "/api/issues/"+itoa(epic)+"/relations", ts.adminCookie,
			map[string]any{"target_id": ticketA, "type": "parent"})
		assertStatus(t, resp, http.StatusCreated)
		resp.Body.Close()
	})

	t.Run("cost_unit-sourced groups stays groups (not translated)", func(t *testing.T) {
		resp := ts.post(t, "/api/issues/"+itoa(cu)+"/relations", ts.adminCookie,
			map[string]any{"target_id": cuTicket, "type": "groups"})
		assertStatus(t, resp, http.StatusCreated)
		resp.Body.Close()
		if got := edgeType(cu, cuTicket); got != "groups" {
			t.Fatalf("cost_unit-sourced groups stored as %q, want groups", got)
		}
	})
}
