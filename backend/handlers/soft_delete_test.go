// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// Soft-delete (Trash) tests for issues. Covers the end-to-end lifecycle —
// delete → restore → purge — plus descendant cascade, relation survival,
// and the rule that every user-facing list endpoint must filter trashed
// issues out.

package handlers_test

import (
	"fmt"
	"net/http"
	"testing"
)

// softDeleteSetup creates a project + one ticket + one child task.
// Returns (ts, projectID, ticketID, taskID).
func softDeleteSetup(t *testing.T) (*testServer, int64, int64, int64) {
	ts := newTestServer(t)

	pResp := ts.post(t, "/api/projects", ts.adminCookie, map[string]string{
		"name": "Soft Delete Test", "key": "SDT",
	})
	assertStatus(t, pResp, http.StatusCreated)
	projectID := responseID(t, pResp)

	tkResp := ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.adminCookie, map[string]any{
		"title": "Parent Ticket", "type": "ticket", "status": "backlog",
	})
	assertStatus(t, tkResp, http.StatusCreated)
	ticketID := responseID(t, tkResp)

	taskResp := ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.adminCookie, map[string]any{
		"title": "Child Task", "type": "task", "status": "backlog", "parent_id": ticketID,
	})
	assertStatus(t, taskResp, http.StatusCreated)
	taskID := responseID(t, taskResp)

	return ts, projectID, ticketID, taskID
}

func Test_SoftDelete_GoesToTrash(t *testing.T) {
	ts, projectID, ticketID, _ := softDeleteSetup(t)

	// DELETE flips deleted_at and returns 204.
	resp := ts.del(t, fmt.Sprintf("/api/issues/%d", ticketID), ts.adminCookie)
	assertStatus(t, resp, http.StatusNoContent)

	// GET by id now returns 404 — trashed issues are invisible to normal reads.
	resp = ts.get(t, fmt.Sprintf("/api/issues/%d", ticketID), ts.adminCookie)
	assertStatus(t, resp, http.StatusNotFound)

	// Project list no longer includes it.
	resp = ts.get(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.adminCookie)
	assertStatus(t, resp, http.StatusOK)
	var list []struct{ ID int64 `json:"id"` }
	decode(t, resp, &list)
	for _, i := range list {
		if i.ID == ticketID {
			t.Errorf("trashed issue %d leaked into project issue list", ticketID)
		}
	}

	// Trash list contains it (admin only).
	resp = ts.get(t, "/api/issues/trash", ts.adminCookie)
	assertStatus(t, resp, http.StatusOK)
	var trash []struct {
		ID        int64  `json:"id"`
		DeletedAt string `json:"deleted_at"`
	}
	decode(t, resp, &trash)
	found := false
	for _, i := range trash {
		if i.ID == ticketID {
			found = true
			if i.DeletedAt == "" {
				t.Error("trash row missing deleted_at timestamp")
			}
		}
	}
	if !found {
		t.Errorf("trashed issue %d missing from /issues/trash", ticketID)
	}
}

func Test_SoftDelete_CascadeToDescendants(t *testing.T) {
	ts, _, ticketID, taskID := softDeleteSetup(t)

	// Delete the ticket — its child task should cascade into the Trash too.
	resp := ts.del(t, fmt.Sprintf("/api/issues/%d", ticketID), ts.adminCookie)
	assertStatus(t, resp, http.StatusNoContent)

	// Both should be in trash.
	resp = ts.get(t, "/api/issues/trash", ts.adminCookie)
	assertStatus(t, resp, http.StatusOK)
	var trash []struct{ ID int64 `json:"id"` }
	decode(t, resp, &trash)
	seen := map[int64]bool{}
	for _, i := range trash {
		seen[i.ID] = true
	}
	if !seen[ticketID] {
		t.Errorf("ticket %d not in trash after cascade", ticketID)
	}
	if !seen[taskID] {
		t.Errorf("child task %d not cascaded into trash when ticket %d was deleted",
			taskID, ticketID)
	}
}

func Test_SoftDelete_Restore(t *testing.T) {
	ts, projectID, ticketID, taskID := softDeleteSetup(t)

	// Delete ticket (also cascades task).
	resp := ts.del(t, fmt.Sprintf("/api/issues/%d", ticketID), ts.adminCookie)
	assertStatus(t, resp, http.StatusNoContent)

	// Restore only the ticket — task should stay in trash (restore is explicit).
	resp = ts.post(t, fmt.Sprintf("/api/issues/%d/restore", ticketID), ts.adminCookie, nil)
	assertStatus(t, resp, http.StatusOK)

	// Ticket is live again.
	resp = ts.get(t, fmt.Sprintf("/api/issues/%d", ticketID), ts.adminCookie)
	assertStatus(t, resp, http.StatusOK)

	// It's back in the project listing.
	resp = ts.get(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.adminCookie)
	assertStatus(t, resp, http.StatusOK)
	var list []struct{ ID int64 `json:"id"` }
	decode(t, resp, &list)
	foundTicket := false
	foundTask := false
	for _, i := range list {
		if i.ID == ticketID {
			foundTicket = true
		}
		if i.ID == taskID {
			foundTask = true
		}
	}
	if !foundTicket {
		t.Error("restored ticket missing from project list")
	}
	if foundTask {
		t.Error("task should stay in trash — restore is not supposed to cascade down")
	}
}

func Test_SoftDelete_Purge(t *testing.T) {
	ts, _, ticketID, _ := softDeleteSetup(t)

	// Can't purge a live issue.
	resp := ts.del(t, fmt.Sprintf("/api/issues/%d/purge", ticketID), ts.adminCookie)
	assertStatus(t, resp, http.StatusNotFound)

	// Move to trash first.
	resp = ts.del(t, fmt.Sprintf("/api/issues/%d", ticketID), ts.adminCookie)
	assertStatus(t, resp, http.StatusNoContent)

	// Now purge succeeds.
	resp = ts.del(t, fmt.Sprintf("/api/issues/%d/purge", ticketID), ts.adminCookie)
	assertStatus(t, resp, http.StatusNoContent)

	// And it's gone from trash entirely.
	resp = ts.get(t, "/api/issues/trash", ts.adminCookie)
	assertStatus(t, resp, http.StatusOK)
	var trash []struct{ ID int64 `json:"id"` }
	decode(t, resp, &trash)
	for _, i := range trash {
		if i.ID == ticketID {
			t.Errorf("purged issue %d still in trash", ticketID)
		}
	}

	// Second purge is a no-op 404.
	resp = ts.del(t, fmt.Sprintf("/api/issues/%d/purge", ticketID), ts.adminCookie)
	assertStatus(t, resp, http.StatusNotFound)
}

func Test_SoftDelete_RestoreReattachesRelations(t *testing.T) {
	ts, projectID, ticketID, _ := softDeleteSetup(t)

	// Attach a tag and post a comment so we have related rows to verify.
	tagID := firstTagID(t)
	resp := ts.post(t, fmt.Sprintf("/api/issues/%d/tags", ticketID), ts.adminCookie, map[string]int64{
		"tag_id": tagID,
	})
	assertStatus(t, resp, http.StatusNoContent)

	resp = ts.post(t, fmt.Sprintf("/api/issues/%d/comments", ticketID), ts.adminCookie, map[string]string{
		"body": "pre-trash note",
	})
	assertStatus(t, resp, http.StatusCreated)

	// Delete + restore.
	resp = ts.del(t, fmt.Sprintf("/api/issues/%d", ticketID), ts.adminCookie)
	assertStatus(t, resp, http.StatusNoContent)
	resp = ts.post(t, fmt.Sprintf("/api/issues/%d/restore", ticketID), ts.adminCookie, nil)
	assertStatus(t, resp, http.StatusOK)

	// Relations survive.
	resp = ts.get(t, fmt.Sprintf("/api/issues/%d", ticketID), ts.adminCookie)
	assertStatus(t, resp, http.StatusOK)
	var issue struct {
		Tags []struct{ ID int64 `json:"id"` } `json:"tags"`
	}
	decode(t, resp, &issue)
	if len(issue.Tags) == 0 {
		t.Error("tag didn't survive soft-delete + restore round trip")
	}

	resp = ts.get(t, fmt.Sprintf("/api/issues/%d/comments", ticketID), ts.adminCookie)
	assertStatus(t, resp, http.StatusOK)
	var comments []struct{ ID int64 `json:"id"` }
	decode(t, resp, &comments)
	if len(comments) == 0 {
		t.Error("comment didn't survive soft-delete + restore round trip")
	}

	_ = projectID
}

func Test_SoftDelete_UpdateBlockedForTrashed(t *testing.T) {
	ts, _, ticketID, _ := softDeleteSetup(t)

	resp := ts.del(t, fmt.Sprintf("/api/issues/%d", ticketID), ts.adminCookie)
	assertStatus(t, resp, http.StatusNoContent)

	// PUT on a trashed issue is a 404 — consistent with GET.
	resp = ts.put(t, fmt.Sprintf("/api/issues/%d", ticketID), ts.adminCookie, map[string]string{
		"title": "should not stick",
	})
	assertStatus(t, resp, http.StatusNotFound)
}

func Test_SoftDelete_TrashAdminOnly(t *testing.T) {
	ts := newTestServer(t)

	// Members can't peek into the trash.
	resp := ts.get(t, "/api/issues/trash", ts.memberCookie)
	assertStatus(t, resp, http.StatusForbidden)
}

func Test_SoftDelete_CrossProjectListExcludesTrashed(t *testing.T) {
	ts, _, ticketID, _ := softDeleteSetup(t)

	// Envelope is { issues: [...], total, offset, limit }.
	type envT struct {
		Issues []struct{ ID int64 `json:"id"` } `json:"issues"`
		Total  int                              `json:"total"`
	}

	// Baseline: ticket shows up.
	resp := ts.get(t, "/api/issues", ts.adminCookie)
	assertStatus(t, resp, http.StatusOK)
	var env envT
	decode(t, resp, &env)
	found := false
	for _, i := range env.Issues {
		if i.ID == ticketID {
			found = true
		}
	}
	if !found {
		t.Fatalf("baseline: ticket %d missing from /api/issues before delete (total=%d)",
			ticketID, env.Total)
	}

	// Trash it — then it should be gone.
	resp = ts.del(t, fmt.Sprintf("/api/issues/%d", ticketID), ts.adminCookie)
	assertStatus(t, resp, http.StatusNoContent)

	resp = ts.get(t, "/api/issues", ts.adminCookie)
	assertStatus(t, resp, http.StatusOK)
	decode(t, resp, &env)
	for _, i := range env.Issues {
		if i.ID == ticketID {
			t.Errorf("trashed issue %d leaked into cross-project list", ticketID)
		}
	}
}

func Test_SoftDelete_SearchExcludesTrashed(t *testing.T) {
	ts, _, ticketID, _ := softDeleteSetup(t)

	// Trash it.
	resp := ts.del(t, fmt.Sprintf("/api/issues/%d", ticketID), ts.adminCookie)
	assertStatus(t, resp, http.StatusNoContent)

	// Search by issue key: should not find the trashed ticket.
	resp = ts.get(t, "/api/search?q=SDT-1", ts.adminCookie)
	assertStatus(t, resp, http.StatusOK)
	var res struct {
		Issues []struct{ ID int64 `json:"id"` } `json:"issues"`
	}
	decode(t, resp, &res)
	for _, i := range res.Issues {
		if i.ID == ticketID {
			t.Errorf("search returned trashed issue %d", ticketID)
		}
	}
}
