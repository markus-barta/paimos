package handlers_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/inspr-at/paimos/backend/db"
)

func seedBatchTagUndoIssues(t *testing.T) ([]int64, int64) {
	t.Helper()
	projectID := seedBatchProject(t, "Batch Tag Undo", "BTU")
	ids := make([]int64, 3)
	for i := range ids {
		res, err := db.DB.Exec(
			`INSERT INTO issues(project_id, issue_number, type, title, status) VALUES(?, ?, ?, ?, ?)`,
			projectID, i+1, "ticket", fmt.Sprintf("BTU-%d", i+1), "backlog",
		)
		if err != nil {
			t.Fatalf("seed issue %d: %v", i+1, err)
		}
		ids[i], _ = res.LastInsertId()
	}
	return ids, firstTagID(t)
}

func batchTag(t *testing.T, ts *testServer, ids []int64, tagID int64, op string) (string, string, int) {
	t.Helper()
	resp := ts.post(t, "/api/issues/batch/tags", ts.adminCookie, map[string]any{
		"issue_ids": ids,
		"tag_id":    tagID,
		"op":        op,
	})
	assertStatus(t, resp, http.StatusOK)
	var out struct {
		BatchID  string `json:"batch_id"`
		Affected int    `json:"affected"`
	}
	decode(t, resp, &out)
	if out.BatchID == "" {
		t.Fatal("batch_id empty")
	}
	requestID := resp.Header.Get("X-PAIMOS-Request-Id")
	if requestID == "" {
		t.Fatal("response missing X-PAIMOS-Request-Id")
	}
	return requestID, out.BatchID, out.Affected
}

func countBatchTagUndoRows(t *testing.T, batchID string) (undoable int, onStack int) {
	t.Helper()
	if err := db.DB.QueryRow(`
		SELECT COALESCE(SUM(undoable), 0), COALESCE(SUM(on_user_stack), 0)
		FROM mutation_log
		WHERE batch_id = ?
	`, batchID).Scan(&undoable, &onStack); err != nil {
		t.Fatalf("count mutation rows for batch %s: %v", batchID, err)
	}
	return undoable, onStack
}

func assertIssueTagStates(t *testing.T, ids []int64, tagID int64, want []bool) {
	t.Helper()
	if len(ids) != len(want) {
		t.Fatalf("len(ids)=%d len(want)=%d", len(ids), len(want))
	}
	for i, id := range ids {
		if got := issueHasTag(t, id, tagID); got != want[i] {
			t.Fatalf("issue %d tag state = %v, want %v", id, got, want[i])
		}
	}
}

func TestBatchTagIssues_UndoRedoByRequestID(t *testing.T) {
	ts := newTestServer(t)
	ids, tagID := seedBatchTagUndoIssues(t)

	if _, err := db.DB.Exec(`INSERT INTO issue_tags(issue_id, tag_id) VALUES(?, ?)`, ids[0], tagID); err != nil {
		t.Fatalf("seed pre-existing tag: %v", err)
	}

	requestID, batchID, affected := batchTag(t, ts, ids, tagID, "add")
	if affected != 2 {
		t.Fatalf("bulk add affected=%d, want 2", affected)
	}
	assertIssueTagStates(t, ids, tagID, []bool{true, true, true})
	undoable, onStack := countBatchTagUndoRows(t, batchID)
	if undoable != 2 || onStack != 1 {
		t.Fatalf("bulk add undoable/on_stack = %d/%d, want 2/1", undoable, onStack)
	}

	resp := ts.post(t, "/api/undo/request/"+requestID, ts.adminCookie, map[string]any{})
	assertStatus(t, resp, http.StatusOK)
	var undoBody struct {
		BatchSize int `json:"batch_size"`
	}
	decode(t, resp, &undoBody)
	if undoBody.BatchSize != 2 {
		t.Fatalf("undo batch_size=%d, want 2", undoBody.BatchSize)
	}
	assertIssueTagStates(t, ids, tagID, []bool{true, false, false})

	resp = ts.post(t, "/api/redo/request/"+requestID, ts.adminCookie, map[string]any{})
	assertStatus(t, resp, http.StatusOK)
	assertIssueTagStates(t, ids, tagID, []bool{true, true, true})

	noopRequestID, noopBatchID, affected := batchTag(t, ts, ids, tagID, "add")
	if affected != 0 {
		t.Fatalf("no-op bulk add affected=%d, want 0", affected)
	}
	undoable, onStack = countBatchTagUndoRows(t, noopBatchID)
	if undoable != 0 || onStack != 0 {
		t.Fatalf("no-op bulk add undoable/on_stack = %d/%d, want 0/0", undoable, onStack)
	}
	resp = ts.post(t, "/api/undo/request/"+noopRequestID, ts.adminCookie, map[string]any{})
	assertStatus(t, resp, http.StatusNotFound)
	assertIssueTagStates(t, ids, tagID, []bool{true, true, true})

	removeRequestID, removeBatchID, affected := batchTag(t, ts, ids, tagID, "remove")
	if affected != 3 {
		t.Fatalf("bulk remove affected=%d, want 3", affected)
	}
	assertIssueTagStates(t, ids, tagID, []bool{false, false, false})
	undoable, onStack = countBatchTagUndoRows(t, removeBatchID)
	if undoable != 3 || onStack != 1 {
		t.Fatalf("bulk remove undoable/on_stack = %d/%d, want 3/1", undoable, onStack)
	}

	resp = ts.post(t, "/api/undo/request/"+removeRequestID, ts.adminCookie, map[string]any{})
	assertStatus(t, resp, http.StatusOK)
	assertIssueTagStates(t, ids, tagID, []bool{true, true, true})

	resp = ts.post(t, "/api/redo/request/"+removeRequestID, ts.adminCookie, map[string]any{})
	assertStatus(t, resp, http.StatusOK)
	assertIssueTagStates(t, ids, tagID, []bool{false, false, false})
}
