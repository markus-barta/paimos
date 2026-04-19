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
)

// TestSprintRelationCRUD covers the full lifecycle of sprint assignment:
// create sprint, create issue, assign to sprint, verify sprint_ids on issue,
// verify members on sprint, remove assignment, verify removal.
func TestSprintRelationCRUD(t *testing.T) {
	ts := newTestServer(t)

	// 1. Create a project
	projResp := ts.post(t, "/api/projects", ts.adminCookie, map[string]string{
		"name": "Sprint Test Project",
	})
	projID := responseID(t, projResp)
	if projID == 0 {
		t.Fatal("failed to create project")
	}

	// 2. Create a ticket in the project
	issueID := responseID(t, ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projID), ts.adminCookie, map[string]interface{}{
		"title":  "Test Ticket",
		"type":   "ticket",
		"status": "backlog",
	}))
	if issueID == 0 {
		t.Fatal("failed to create ticket")
	}

	// 3. Create an orphan sprint
	sprintID := responseID(t, ts.post(t, "/api/issues", ts.adminCookie, map[string]interface{}{
		"title":        "Test Sprint",
		"type":         "sprint",
		"sprint_state": "active",
		"start_date":   "2026-03-16",
		"end_date":     "2026-03-29",
	}))
	if sprintID == 0 {
		t.Fatal("failed to create sprint")
	}

	// 4. Assign issue to sprint: POST /api/issues/:issueId/relations
	relResp := ts.post(t, fmt.Sprintf("/api/issues/%d/relations", issueID), ts.adminCookie, map[string]interface{}{
		"target_id": sprintID,
		"type":      "sprint",
	})
	assertStatus(t, relResp, http.StatusCreated)
	relResp.Body.Close()

	// 5. Verify: GET issue → sprint_ids should contain the sprint
	t.Run("issue sprint_ids populated after assign", func(t *testing.T) {
		resp := ts.get(t, fmt.Sprintf("/api/issues/%d", issueID), ts.adminCookie)
		assertStatus(t, resp, http.StatusOK)
		var issue map[string]interface{}
		decode(t, resp, &issue)
		ids, ok := issue["sprint_ids"].([]interface{})
		if !ok || len(ids) == 0 {
			t.Fatalf("sprint_ids empty or wrong type: %v", issue["sprint_ids"])
		}
		found := false
		for _, v := range ids {
			if int64(v.(float64)) == sprintID {
				found = true
			}
		}
		if !found {
			t.Errorf("sprint_ids %v does not contain sprint %d", ids, sprintID)
		}
	})

	// 6. Verify: GET /issues/:sprintId/members?type=sprint returns the issue
	t.Run("sprint members contains the assigned issue", func(t *testing.T) {
		resp := ts.get(t, fmt.Sprintf("/api/issues/%d/members?type=sprint", sprintID), ts.adminCookie)
		assertStatus(t, resp, http.StatusOK)
		var members []map[string]interface{}
		decode(t, resp, &members)
		if len(members) == 0 {
			t.Fatal("sprint members is empty")
		}
		found := false
		for _, m := range members {
			if int64(m["id"].(float64)) == issueID {
				found = true
			}
		}
		if !found {
			t.Errorf("sprint members %v does not contain issue %d", members, issueID)
		}
	})

	// 7. Verify: GET /issues/:issueId/relations returns the sprint relation
	// After M32: source=sprint, target=issue. Relation list is bidirectional so
	// we just check a relation of type=sprint exists.
	t.Run("issue relations lists the sprint", func(t *testing.T) {
		resp := ts.get(t, fmt.Sprintf("/api/issues/%d/relations", issueID), ts.adminCookie)
		assertStatus(t, resp, http.StatusOK)
		var rels []map[string]interface{}
		decode(t, resp, &rels)
		if len(rels) == 0 {
			t.Fatal("issue relations is empty")
		}
		found := false
		for _, r := range rels {
			if r["type"] == "sprint" {
				// source=sprint, target=issue (M32 direction convention)
				sid := int64(r["source_id"].(float64))
				tid := int64(r["target_id"].(float64))
				if sid == sprintID && tid == issueID {
					found = true
				}
			}
		}
		if !found {
			t.Errorf("relations %v does not contain sprint→issue relation", rels)
		}
	})

	// 8. Remove the sprint assignment
	delResp := ts.delWithBody(t, fmt.Sprintf("/api/issues/%d/relations", issueID), ts.adminCookie, map[string]interface{}{
		"target_id": sprintID,
		"type":      "sprint",
	})
	if delResp.StatusCode != http.StatusNoContent && delResp.StatusCode != http.StatusOK {
		t.Fatalf("delete relation: got %d, want 204", delResp.StatusCode)
	}
	delResp.Body.Close()

	// 9. Verify: issue sprint_ids is now empty
	t.Run("sprint_ids empty after removal", func(t *testing.T) {
		resp := ts.get(t, fmt.Sprintf("/api/issues/%d", issueID), ts.adminCookie)
		assertStatus(t, resp, http.StatusOK)
		var issue map[string]interface{}
		decode(t, resp, &issue)
		ids, _ := issue["sprint_ids"].([]interface{})
		if len(ids) != 0 {
			t.Errorf("sprint_ids should be empty after removal, got %v", ids)
		}
	})

	// 10. Verify: sprint members is now empty
	t.Run("sprint members empty after removal", func(t *testing.T) {
		resp := ts.get(t, fmt.Sprintf("/api/issues/%d/members?type=sprint", sprintID), ts.adminCookie)
		assertStatus(t, resp, http.StatusOK)
		var members []map[string]interface{}
		decode(t, resp, &members)
		if len(members) != 0 {
			t.Errorf("sprint members should be empty after removal, got %d", len(members))
		}
	})
}

// TestSprintMultiAssign verifies assigning multiple sprints to one issue.
func TestSprintMultiAssign(t *testing.T) {
	ts := newTestServer(t)

	// Setup: project, issue, 2 sprints
	projID := responseID(t, ts.post(t, "/api/projects", ts.adminCookie, map[string]string{"name": "Multi Sprint"}))
	issueID := responseID(t, ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projID), ts.adminCookie, map[string]interface{}{
		"title": "Multi Sprint Issue", "type": "ticket", "status": "backlog",
	}))
	sprint1ID := responseID(t, ts.post(t, "/api/issues", ts.adminCookie, map[string]interface{}{
		"title": "Sprint A", "type": "sprint",
	}))
	sprint2ID := responseID(t, ts.post(t, "/api/issues", ts.adminCookie, map[string]interface{}{
		"title": "Sprint B", "type": "sprint",
	}))

	// Assign both
	assertStatus(t, ts.post(t, fmt.Sprintf("/api/issues/%d/relations", issueID), ts.adminCookie, map[string]interface{}{
		"target_id": sprint1ID, "type": "sprint",
	}), http.StatusCreated)
	assertStatus(t, ts.post(t, fmt.Sprintf("/api/issues/%d/relations", issueID), ts.adminCookie, map[string]interface{}{
		"target_id": sprint2ID, "type": "sprint",
	}), http.StatusCreated)

	// Verify issue has both sprint_ids
	resp := ts.get(t, fmt.Sprintf("/api/issues/%d", issueID), ts.adminCookie)
	assertStatus(t, resp, http.StatusOK)
	var issue map[string]interface{}
	decode(t, resp, &issue)
	ids, ok := issue["sprint_ids"].([]interface{})
	if !ok || len(ids) != 2 {
		t.Fatalf("expected 2 sprint_ids, got %v", issue["sprint_ids"])
	}

	// Verify each sprint lists the issue as member
	for _, sid := range []int64{sprint1ID, sprint2ID} {
		resp := ts.get(t, fmt.Sprintf("/api/issues/%d/members?type=sprint", sid), ts.adminCookie)
		assertStatus(t, resp, http.StatusOK)
		var members []map[string]interface{}
		decode(t, resp, &members)
		if len(members) != 1 {
			t.Errorf("sprint %d: expected 1 member, got %d", sid, len(members))
		}
	}
}

// TestStatusNormalisation verifies migration 32 canonical status values.
// After M32 the only valid statuses are: backlog, in-progress, done, cancelled.
func TestStatusNormalisation(t *testing.T) {
	ts := newTestServer(t)

	projID := responseID(t, ts.post(t, "/api/projects", ts.adminCookie, map[string]string{"name": "Status Test"}))
	if projID == 0 {
		t.Fatal("create project failed")
	}

	cases := []struct {
		input    string
		expected string
	}{
		{"backlog", "backlog"},
		{"in-progress", "in-progress"},
		{"done", "done"},
		{"cancelled", "cancelled"},
	}

	for _, c := range cases {
		t.Run("status_"+c.input, func(t *testing.T) {
			resp := ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projID), ts.adminCookie, map[string]interface{}{
				"title":  "Status " + c.input,
				"type":   "ticket",
				"status": c.input,
			})
			issueID := responseID(t, resp)
			if issueID == 0 {
				t.Fatalf("create issue with status=%q failed", c.input)
			}
			getResp := ts.get(t, fmt.Sprintf("/api/issues/%d", issueID), ts.adminCookie)
			assertStatus(t, getResp, http.StatusOK)
			var issue map[string]interface{}
			decode(t, getResp, &issue)
			got := issue["status"].(string)
			if got != c.expected {
				t.Errorf("status: got %q, want %q", got, c.expected)
			}
		})
	}

	// Verify CHECK constraint rejects invalid status
	t.Run("invalid_status_rejected", func(t *testing.T) {
		resp := ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projID), ts.adminCookie, map[string]interface{}{
			"title":  "Bad Status",
			"type":   "ticket",
			"status": "open", // legacy, no longer valid after M32
		})
		// DB CHECK constraint will cause insert to fail → 500
		if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated {
			t.Error("expected error for invalid status 'open', got success")
		}
		resp.Body.Close()
	})
}

// TestSprintIdempotentAssign verifies duplicate assignment doesn't error.
func TestSprintIdempotentAssign(t *testing.T) {
	ts := newTestServer(t)

	projID := responseID(t, ts.post(t, "/api/projects", ts.adminCookie, map[string]string{"name": "Idempotent"}))
	issueID := responseID(t, ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projID), ts.adminCookie, map[string]interface{}{
		"title": "Idem Issue", "type": "ticket", "status": "backlog",
	}))
	sprintID := responseID(t, ts.post(t, "/api/issues", ts.adminCookie, map[string]interface{}{
		"title": "Idem Sprint", "type": "sprint",
	}))

	// First assign
	assertStatus(t, ts.post(t, fmt.Sprintf("/api/issues/%d/relations", issueID), ts.adminCookie, map[string]interface{}{
		"target_id": sprintID, "type": "sprint",
	}), http.StatusCreated)

	// Second assign — should be idempotent (INSERT OR IGNORE)
	resp := ts.post(t, fmt.Sprintf("/api/issues/%d/relations", issueID), ts.adminCookie, map[string]interface{}{
		"target_id": sprintID, "type": "sprint",
	})
	assertStatus(t, resp, http.StatusCreated)

	// Still only 1 sprint_id
	getResp := ts.get(t, fmt.Sprintf("/api/issues/%d", issueID), ts.adminCookie)
	var issue map[string]interface{}
	decode(t, getResp, &issue)
	ids, _ := issue["sprint_ids"].([]interface{})
	if len(ids) != 1 {
		t.Errorf("expected 1 sprint_id after duplicate assign, got %v", ids)
	}
}
