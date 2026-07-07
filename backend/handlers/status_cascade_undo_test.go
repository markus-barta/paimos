package handlers_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/markus-barta/paimos/backend/db"
)

func seedStatusCascadeUndoPair(t *testing.T) (int64, int64) {
	t.Helper()
	projectID := seedBatchProject(t, "Status Cascade Undo", "SCU")
	insertIssue := func(num int, typ, title string) int64 {
		t.Helper()
		res, err := db.DB.Exec(
			`INSERT INTO issues(project_id, issue_number, type, title, status) VALUES(?, ?, ?, ?, ?)`,
			projectID, num, typ, title, "backlog",
		)
		if err != nil {
			t.Fatalf("seed %s: %v", title, err)
		}
		id, _ := res.LastInsertId()
		return id
	}
	ticketID := insertIssue(1, "ticket", "Parent ticket")
	taskID := insertIssue(2, "task", "Child task")
	if _, err := db.DB.Exec(
		`INSERT INTO issue_relations(source_id, target_id, type) VALUES(?, ?, 'parent')`,
		ticketID, taskID,
	); err != nil {
		t.Fatalf("seed parent edge: %v", err)
	}
	return ticketID, taskID
}

func issueStatus(t *testing.T, issueID int64) string {
	t.Helper()
	var status string
	if err := db.DB.QueryRow(`SELECT status FROM issues WHERE id = ?`, issueID).Scan(&status); err != nil {
		t.Fatalf("status issue %d: %v", issueID, err)
	}
	return status
}

func TestIssueStatusCascade_UndoRedoByRequestID(t *testing.T) {
	ts := newTestServer(t)
	ticketID, taskID := seedStatusCascadeUndoPair(t)

	resp := ts.put(t, fmt.Sprintf("/api/issues/%d", ticketID), ts.adminCookie, map[string]any{
		"status": "done",
	})
	assertStatus(t, resp, http.StatusOK)
	requestID := resp.Header.Get("X-PAIMOS-Request-Id")
	if requestID == "" {
		t.Fatal("response missing X-PAIMOS-Request-Id")
	}
	if got := issueStatus(t, ticketID); got != "done" {
		t.Fatalf("ticket status=%q, want done", got)
	}
	if got := issueStatus(t, taskID); got != "done" {
		t.Fatalf("task status=%q, want cascaded done", got)
	}

	var parentLogID int64
	var parentBatch string
	var parentOnStack int
	if err := db.DB.QueryRow(`
		SELECT id, COALESCE(batch_id, ''), on_user_stack
		FROM mutation_log
		WHERE request_id = ? AND subject_type = 'issue' AND subject_id = ?
		ORDER BY id DESC
		LIMIT 1
	`, requestID, ticketID).Scan(&parentLogID, &parentBatch, &parentOnStack); err != nil {
		t.Fatalf("parent mutation row: %v", err)
	}
	if parentBatch == "" {
		t.Fatal("parent cascade mutation batch_id empty")
	}
	if parentOnStack != 1 {
		t.Fatalf("parent on_user_stack=%d, want 1", parentOnStack)
	}

	var childParentLogID int64
	var childBatch string
	var childOnStack int
	if err := db.DB.QueryRow(`
		SELECT parent_log_id, COALESCE(batch_id, ''), on_user_stack
		FROM mutation_log
		WHERE request_id = ? AND subject_type = 'issue' AND subject_id = ?
		ORDER BY id DESC
		LIMIT 1
	`, requestID, taskID).Scan(&childParentLogID, &childBatch, &childOnStack); err != nil {
		t.Fatalf("child mutation row: %v", err)
	}
	if childParentLogID != parentLogID {
		t.Fatalf("child parent_log_id=%d, want parent log %d", childParentLogID, parentLogID)
	}
	if childBatch != parentBatch {
		t.Fatalf("child batch_id=%q, want parent batch %q", childBatch, parentBatch)
	}
	if childOnStack != 0 {
		t.Fatalf("child on_user_stack=%d, want 0", childOnStack)
	}

	undoResp := ts.post(t, "/api/undo/request/"+requestID, ts.adminCookie, map[string]any{})
	assertStatus(t, undoResp, http.StatusOK)
	var undoBody struct {
		BatchSize int `json:"batch_size"`
	}
	decode(t, undoResp, &undoBody)
	if undoBody.BatchSize != 2 {
		t.Fatalf("undo batch_size=%d, want 2", undoBody.BatchSize)
	}
	if got := issueStatus(t, ticketID); got != "backlog" {
		t.Fatalf("ticket after undo=%q, want backlog", got)
	}
	if got := issueStatus(t, taskID); got != "backlog" {
		t.Fatalf("task after undo=%q, want backlog", got)
	}

	redoResp := ts.post(t, "/api/redo/request/"+requestID, ts.adminCookie, map[string]any{})
	assertStatus(t, redoResp, http.StatusOK)
	if got := issueStatus(t, ticketID); got != "done" {
		t.Fatalf("ticket after redo=%q, want done", got)
	}
	if got := issueStatus(t, taskID); got != "done" {
		t.Fatalf("task after redo=%q, want done", got)
	}
}
