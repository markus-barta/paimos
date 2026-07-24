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
	"strings"
	"testing"

	"github.com/inspr-at/paimos/backend/db"
)

type moveResp struct {
	IssueID   int64    `json:"issue_id"`
	OldKey    string   `json:"old_key"`
	NewKey    string   `json:"new_key"`
	ProjectID int64    `json:"project_id"`
	Detached  []string `json:"detached"`
	Notes     []string `json:"notes"`
}

func issueProjectAndNumber(t *testing.T, id int64) (int64, int) {
	t.Helper()
	var pid int64
	var num int
	if err := db.DB.QueryRow(`SELECT project_id, issue_number FROM issues WHERE id=?`, id).Scan(&pid, &num); err != nil {
		t.Fatalf("read issue %d: %v", id, err)
	}
	return pid, num
}

// TestMoveIssue_HappyPath drives the whole PAI-690 move through the HTTP
// surface: an issue with a comment and a parent is re-homed PAI -> OPS, keeps
// its comment (same id), is re-keyed, detaches the now-cross-project parent,
// and its former key still resolves via the alias fallback.
func TestMoveIssue_HappyPath(t *testing.T) {
	ts := newTestServer(t)
	paiID := seedBatchProject(t, "PAI Project", "PAI")
	opsID := seedBatchProject(t, "OPS Project", "OPS")
	paiURL := fmt.Sprintf("/api/projects/%d/issues", paiID)

	epic := responseID(t, ts.post(t, paiURL, ts.adminCookie, map[string]any{
		"title": "Epic", "type": "epic", "status": "backlog",
	}))
	ticket := responseID(t, ts.post(t, paiURL, ts.adminCookie, map[string]any{
		"title": "Move me", "type": "ticket", "status": "backlog", "parent_id": epic,
	}))
	if epic == 0 || ticket == 0 {
		t.Fatalf("seed failed: epic=%d ticket=%d", epic, ticket)
	}
	// A comment so we can prove issue_id-keyed children survive the move.
	assertStatus(t, ts.post(t, fmt.Sprintf("/api/issues/%d/comments", ticket), ts.adminCookie,
		map[string]string{"body": "keep me"}), http.StatusCreated)

	_, oldNum := issueProjectAndNumber(t, ticket)
	oldKey := fmt.Sprintf("PAI-%d", oldNum)

	resp := ts.post(t, fmt.Sprintf("/api/issues/%d/move", ticket), ts.adminCookie,
		map[string]any{"project_id": opsID})
	assertStatus(t, resp, http.StatusOK)
	var got moveResp
	decode(t, resp, &got)

	if got.OldKey != oldKey {
		t.Errorf("old_key = %q, want %q", got.OldKey, oldKey)
	}
	if !strings.HasPrefix(got.NewKey, "OPS-") {
		t.Errorf("new_key = %q, want OPS- prefix", got.NewKey)
	}
	if got.ProjectID != opsID {
		t.Errorf("project_id = %d, want %d", got.ProjectID, opsID)
	}

	// The row moved projects and was re-numbered.
	if pid, _ := issueProjectAndNumber(t, ticket); pid != opsID {
		t.Errorf("issue project_id = %d, want %d", pid, opsID)
	}

	// The cross-project parent was detached and reported.
	if len(got.Detached) == 0 {
		t.Fatalf("expected a detached parent, got none")
	}
	foundParent := false
	for _, d := range got.Detached {
		if strings.HasPrefix(d, "parent ") {
			foundParent = true
		}
	}
	if !foundParent {
		t.Errorf("detached = %v, want a 'parent ...' entry", got.Detached)
	}
	var parentEdges int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM issue_relations WHERE target_id=? AND type='parent'`, ticket).Scan(&parentEdges); err != nil {
		t.Fatal(err)
	}
	if parentEdges != 0 {
		t.Errorf("parent edges after move = %d, want 0", parentEdges)
	}

	// The comment survived (same issue id).
	cresp := ts.get(t, fmt.Sprintf("/api/issues/%d/comments", ticket), ts.adminCookie)
	assertStatus(t, cresp, http.StatusOK)
	var comments []map[string]any
	decode(t, cresp, &comments)
	if len(comments) != 1 {
		t.Fatalf("comments after move = %d, want 1", len(comments))
	}

	// The former key still resolves (alias fallback) to the moved issue...
	aliasResp := ts.get(t, "/api/issues/"+oldKey, ts.adminCookie)
	assertStatus(t, aliasResp, http.StatusOK)
	var viaAlias map[string]any
	decode(t, aliasResp, &viaAlias)
	if int64(viaAlias["id"].(float64)) != ticket {
		t.Errorf("alias %s resolved to id %v, want %d", oldKey, viaAlias["id"], ticket)
	}
	if viaAlias["issue_key"] != got.NewKey {
		t.Errorf("issue via alias reports key %v, want %q", viaAlias["issue_key"], got.NewKey)
	}

	// ...and so does the new key.
	newResp := ts.get(t, "/api/issues/"+got.NewKey, ts.adminCookie)
	assertStatus(t, newResp, http.StatusOK)
}

// TestMoveIssue_SameProject rejects a no-op move with 400 rather than churning
// the issue number.
func TestMoveIssue_SameProject(t *testing.T) {
	ts := newTestServer(t)
	paiID := seedBatchProject(t, "PAI Project", "PAI")
	ticket := responseID(t, ts.post(t, fmt.Sprintf("/api/projects/%d/issues", paiID), ts.adminCookie,
		map[string]any{"title": "Stay", "type": "ticket"}))
	resp := ts.post(t, fmt.Sprintf("/api/issues/%d/move", ticket), ts.adminCookie,
		map[string]any{"project_id": paiID})
	assertStatus(t, resp, http.StatusBadRequest)
}

// TestMoveIssue_TargetForbidden verifies the destination-project write check:
// a member explicitly denied on the target project cannot move an issue there,
// even though they can edit the source.
func TestMoveIssue_TargetForbidden(t *testing.T) {
	ts := newTestServer(t)
	paiID := seedBatchProject(t, "PAI Project", "PAI")
	opsID := seedBatchProject(t, "OPS Project", "OPS")
	ticket := responseID(t, ts.post(t, fmt.Sprintf("/api/projects/%d/issues", paiID), ts.adminCookie,
		map[string]any{"title": "Guarded", "type": "ticket"}))

	// Deny the member on the target project only.
	if _, err := db.DB.Exec(
		`INSERT OR REPLACE INTO project_members(user_id, project_id, access_level) VALUES(?,?,'none')`,
		userID(t, "member"), opsID); err != nil {
		t.Fatal(err)
	}
	resp := ts.post(t, fmt.Sprintf("/api/issues/%d/move", ticket), ts.memberCookie,
		map[string]any{"project_id": opsID})
	assertStatus(t, resp, http.StatusForbidden)
}

// TestMoveIssue_PUTRejectsProjectChange guards the update path: a differing
// project_id on PUT is rejected with a pointer to the move endpoint, never
// silently dropped.
func TestMoveIssue_PUTRejectsProjectChange(t *testing.T) {
	ts := newTestServer(t)
	paiID := seedBatchProject(t, "PAI Project", "PAI")
	opsID := seedBatchProject(t, "OPS Project", "OPS")
	ticket := responseID(t, ts.post(t, fmt.Sprintf("/api/projects/%d/issues", paiID), ts.adminCookie,
		map[string]any{"title": "Via PUT", "type": "ticket"}))
	resp := ts.put(t, fmt.Sprintf("/api/issues/%d", ticket), ts.adminCookie,
		map[string]any{"project_id": opsID})
	assertStatus(t, resp, http.StatusBadRequest)
}

// TestMoveIssuesBulk moves several issues in one call and reports each.
func TestMoveIssuesBulk(t *testing.T) {
	ts := newTestServer(t)
	paiID := seedBatchProject(t, "PAI Project", "PAI")
	opsID := seedBatchProject(t, "OPS Project", "OPS")
	paiURL := fmt.Sprintf("/api/projects/%d/issues", paiID)
	a := responseID(t, ts.post(t, paiURL, ts.adminCookie, map[string]any{"title": "A", "type": "ticket"}))
	b := responseID(t, ts.post(t, paiURL, ts.adminCookie, map[string]any{"title": "B", "type": "ticket"}))

	resp := ts.post(t, "/api/issues/move", ts.adminCookie, map[string]any{
		"issue_ids": []int64{a, b}, "project_id": opsID,
	})
	assertStatus(t, resp, http.StatusOK)
	var bulk struct {
		Moved   int `json:"moved"`
		Failed  int `json:"failed"`
		Results []struct {
			OK     bool      `json:"ok"`
			Result *moveResp `json:"result"`
		} `json:"results"`
	}
	decode(t, resp, &bulk)
	if bulk.Moved != 2 || bulk.Failed != 0 {
		t.Fatalf("bulk moved=%d failed=%d, want 2/0", bulk.Moved, bulk.Failed)
	}
	for _, r := range bulk.Results {
		if !r.OK || r.Result == nil || !strings.HasPrefix(r.Result.NewKey, "OPS-") {
			t.Errorf("bulk result not OK/re-keyed: %+v", r)
		}
	}
	if pid, _ := issueProjectAndNumber(t, a); pid != opsID {
		t.Errorf("issue A project = %d, want %d", pid, opsID)
	}
}
