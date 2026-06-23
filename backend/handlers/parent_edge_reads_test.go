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

// TestParentEdgeReads covers PAI-584 P2: backend hierarchy reads now source
// from the `parent` edge, not issues.parent_id. The decisive cases are seeded
// directly so edge-state and column-state diverge:
//   - an epic→ticket link that exists ONLY as a parent edge (no parent_id) —
//     the original PAI-584 orphan — must now appear in children / filter /
//     members / aggregation.
//   - a ticket with parent_id set but NO parent edge must NOT appear (proves
//     the read moved off the column).
//   - a cost_unit container's `groups` membership must still aggregate and
//     list (it is NOT a parent edge — orthogonal axis).
func TestParentEdgeReads(t *testing.T) {
	ts := newTestServer(t)
	pid := seedBatchProject(t, "PAI", "PAI")

	mk := func(num int, typ, title string, parent any, estHours any) int64 {
		t.Helper()
		res, err := db.DB.Exec(
			`INSERT INTO issues(project_id,issue_number,type,title,status,priority,estimate_hours,parent_id)
			 VALUES(?,?,?,?,'backlog','medium',?,?)`,
			pid, num, typ, title, estHours, parent)
		if err != nil {
			t.Fatalf("seed issue %d: %v", num, err)
		}
		id, _ := res.LastInsertId()
		return id
	}
	rel := func(src, tgt int64, typ string) {
		t.Helper()
		if _, err := db.DB.Exec(
			`INSERT INTO issue_relations(source_id,target_id,type) VALUES(?,?,?)`, src, tgt, typ); err != nil {
			t.Fatalf("seed relation %s %d→%d: %v", typ, src, tgt, err)
		}
	}

	epic := mk(1, "epic", "Epic", nil, nil)
	// Orphan: parent edge only, parent_id NULL (the PAI-584 bug scenario).
	orphan := mk(2, "ticket", "Edge-only child", nil, 3.0)
	rel(epic, orphan, "parent")
	// Stale column: parent_id set but NO parent edge → must be invisible now.
	// The parent-sync trigger auto-creates the edge on insert, so delete it
	// to force the divergent state this assertion needs (proving reads follow
	// the edge, not the column).
	staleEpic := mk(3, "epic", "Stale epic", nil, nil)
	stale := mk(4, "ticket", "Column-only child", staleEpic, 7.0)
	if _, err := db.DB.Exec("DELETE FROM issue_relations WHERE target_id=? AND type='parent'", stale); err != nil {
		t.Fatalf("clear stale edge: %v", err)
	}
	// cost_unit container with a `groups` member (orthogonal axis, not WBS).
	cu := mk(5, "cost_unit", "Cost unit", nil, nil)
	cuMember := mk(6, "ticket", "Cost unit member", nil, 5.0)
	rel(cu, cuMember, "groups")

	idsFrom := func(t *testing.T, path string) map[int64]bool {
		t.Helper()
		resp := ts.get(t, path, ts.adminCookie)
		assertStatus(t, resp, http.StatusOK)
		var arr []struct {
			ID int64 `json:"id"`
		}
		decode(t, resp, &arr)
		out := map[int64]bool{}
		for _, e := range arr {
			out[e.ID] = true
		}
		return out
	}

	t.Run("GetIssueChildren reads the parent edge", func(t *testing.T) {
		got := idsFrom(t, fmt.Sprintf("/api/issues/%d/children", epic))
		if !got[orphan] {
			t.Errorf("epic children missing edge-only orphan %d: %v", orphan, got)
		}
		staleGot := idsFrom(t, fmt.Sprintf("/api/issues/%d/children", staleEpic))
		if staleGot[stale] {
			t.Errorf("stale epic children wrongly include parent_id-only child %d (read still on column?)", stale)
		}
	})

	t.Run("members?type=groups unions parent + groups", func(t *testing.T) {
		epicMembers := idsFrom(t, fmt.Sprintf("/api/issues/%d/members?type=groups", epic))
		if !epicMembers[orphan] {
			t.Errorf("epic members missing edge-only orphan %d: %v", orphan, epicMembers)
		}
		cuMembers := idsFrom(t, fmt.Sprintf("/api/issues/%d/members?type=groups", cu))
		if !cuMembers[cuMember] {
			t.Errorf("cost_unit members missing groups member %d: %v", cuMember, cuMembers)
		}
	})

	t.Run("epic filter reads the parent edge", func(t *testing.T) {
		resp := ts.get(t, fmt.Sprintf("/api/issues?parent_id=%d", epic), ts.adminCookie)
		assertStatus(t, resp, http.StatusOK)
		var env struct {
			Issues []struct {
				ID int64 `json:"id"`
			} `json:"issues"`
		}
		decode(t, resp, &env)
		found := false
		for _, e := range env.Issues {
			if e.ID == orphan {
				found = true
			}
		}
		if !found {
			t.Errorf("parent_id=%d filter missing edge-only orphan %d: %+v", epic, orphan, env.Issues)
		}
	})

	t.Run("issue payload parent_id is edge-sourced (P3)", func(t *testing.T) {
		// Orphan: parent_id column is NULL but a parent edge exists →
		// payload must report the edge's parent (so the FE tree/badge
		// place it correctly).
		var orphanIssue struct {
			ParentID *int64 `json:"parent_id"`
		}
		decode(t, ts.get(t, fmt.Sprintf("/api/issues/%d", orphan), ts.adminCookie), &orphanIssue)
		if orphanIssue.ParentID == nil || *orphanIssue.ParentID != epic {
			t.Errorf("orphan payload parent_id=%v, want %d (edge-sourced)", orphanIssue.ParentID, epic)
		}
		// Stale: parent_id column set but NO edge → payload must report
		// NULL, proving the field follows the edge, not the column.
		var staleIssue struct {
			ParentID *int64 `json:"parent_id"`
		}
		decode(t, ts.get(t, fmt.Sprintf("/api/issues/%d", stale), ts.adminCookie), &staleIssue)
		if staleIssue.ParentID != nil {
			t.Errorf("stale payload parent_id=%v, want null (payload follows edge, not column)", staleIssue.ParentID)
		}
	})

	t.Run("aggregation sums parent + groups members", func(t *testing.T) {
		var aggEpic struct {
			MemberCount   int      `json:"member_count"`
			EstimateHours *float64 `json:"estimate_hours"`
		}
		decode(t, ts.get(t, fmt.Sprintf("/api/issues/%d/aggregation", epic), ts.adminCookie), &aggEpic)
		if aggEpic.MemberCount != 1 {
			t.Errorf("epic aggregation member_count=%d, want 1 (the edge-only orphan)", aggEpic.MemberCount)
		}
		if aggEpic.EstimateHours == nil || *aggEpic.EstimateHours != 3.0 {
			t.Errorf("epic aggregation estimate_hours=%v, want 3.0", aggEpic.EstimateHours)
		}
		var aggCU struct {
			MemberCount int `json:"member_count"`
		}
		decode(t, ts.get(t, fmt.Sprintf("/api/issues/%d/aggregation", cu), ts.adminCookie), &aggCU)
		if aggCU.MemberCount != 1 {
			t.Errorf("cost_unit aggregation member_count=%d, want 1 (groups member still counts)", aggCU.MemberCount)
		}
	})
}
