package handlers_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/inspr-at/paimos/backend/db"
)

func seedCommentUndoIssue(t *testing.T, title string) int64 {
	t.Helper()
	projectID := seedBatchProject(t, "Comment Undo", "CU")
	res, err := db.DB.Exec(
		`INSERT INTO issues(project_id, issue_number, type, title, status) VALUES(?, ?, ?, ?, ?)`,
		projectID, 1, "ticket", title, "backlog",
	)
	if err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

func commentMutationLogID(t *testing.T, commentID int64, mutationType string) int64 {
	t.Helper()
	var id int64
	var undoable, onStack int
	if err := db.DB.QueryRow(`
		SELECT id, undoable, on_user_stack
		FROM mutation_log
		WHERE subject_type='comment' AND subject_id=? AND mutation_type=?
		ORDER BY id DESC
		LIMIT 1
	`, commentID, mutationType).Scan(&id, &undoable, &onStack); err != nil {
		t.Fatalf("comment mutation_log %s/%d: %v", mutationType, commentID, err)
	}
	if undoable != 1 || onStack != 1 {
		t.Fatalf("mutation_log id=%d undoable/on_user_stack = %d/%d, want 1/1", id, undoable, onStack)
	}
	return id
}

func commentExistsWithBody(t *testing.T, commentID int64) (bool, string) {
	t.Helper()
	var body string
	err := db.DB.QueryRow(`SELECT body FROM comments WHERE id=?`, commentID).Scan(&body)
	if err != nil {
		return false, ""
	}
	return true, body
}

func TestCommentCreateUndoRedoAndActivityDetails(t *testing.T) {
	ts := newTestServer(t)
	issueID := seedCommentUndoIssue(t, "comment create")

	resp := ts.requestWithHeaders(t, http.MethodPost,
		fmt.Sprintf("/api/issues/%d/comments", issueID),
		ts.adminCookie,
		map[string]any{"body": "first undoable comment"},
		map[string]string{
			"X-Paimos-Agent-Name": "comment-agent",
			"X-Paimos-Session-Id": "session-comment-create",
		})
	assertStatus(t, resp, http.StatusCreated)
	commentID := responseID(t, resp)
	logID := commentMutationLogID(t, commentID, "issue.comment.create")

	activityResp := ts.get(t, fmt.Sprintf("/api/issues/%d/activity", issueID), ts.adminCookie)
	assertStatus(t, activityResp, http.StatusOK)
	var activity struct {
		UndoRows []struct {
			ID           int64  `json:"id"`
			SubjectLabel string `json:"subject_label"`
			Summary      string `json:"summary"`
			ChangeDetail string `json:"change_detail"`
			ActorLabel   string `json:"actor_label"`
			OriginLabel  string `json:"origin_label"`
		} `json:"undo_rows"`
	}
	decode(t, activityResp, &activity)
	if len(activity.UndoRows) == 0 || activity.UndoRows[0].ID != logID {
		t.Fatalf("activity undo rows = %#v, want comment mutation %d first", activity.UndoRows, logID)
	}
	row := activity.UndoRows[0]
	if row.Summary != "Added comment" {
		t.Fatalf("summary=%q, want Added comment", row.Summary)
	}
	for _, want := range []string{"absent ->", "first undoable comment"} {
		if !strings.Contains(row.ChangeDetail, want) {
			t.Fatalf("change_detail=%q, missing %q", row.ChangeDetail, want)
		}
	}
	if !strings.Contains(row.ActorLabel, "comment-agent via admin") {
		t.Fatalf("actor_label=%q, want agent and user", row.ActorLabel)
	}
	if row.OriginLabel != "session session-comment-create" {
		t.Fatalf("origin_label=%q", row.OriginLabel)
	}

	undoResp := ts.post(t, fmt.Sprintf("/api/undo/%d", logID), ts.adminCookie, map[string]any{})
	assertStatus(t, undoResp, http.StatusOK)
	if exists, _ := commentExistsWithBody(t, commentID); exists {
		t.Fatalf("comment %d still exists after undo", commentID)
	}

	redoResp := ts.post(t, fmt.Sprintf("/api/redo/%d", logID), ts.adminCookie, map[string]any{})
	assertStatus(t, redoResp, http.StatusOK)
	if exists, body := commentExistsWithBody(t, commentID); !exists || body != "first undoable comment" {
		t.Fatalf("comment after redo exists/body = %v/%q", exists, body)
	}
}

func TestCommentDeleteUndoRedo(t *testing.T) {
	ts := newTestServer(t)
	issueID := seedCommentUndoIssue(t, "comment delete")

	createResp := ts.post(t, fmt.Sprintf("/api/issues/%d/comments", issueID), ts.adminCookie, map[string]any{
		"body": "delete and restore me",
	})
	assertStatus(t, createResp, http.StatusCreated)
	commentID := responseID(t, createResp)

	deleteResp := ts.requestWithHeaders(t, http.MethodDelete,
		fmt.Sprintf("/api/comments/%d", commentID),
		ts.adminCookie,
		nil,
		map[string]string{
			"X-Paimos-Agent-Name": "comment-cleanup",
		})
	assertStatus(t, deleteResp, http.StatusNoContent)
	logID := commentMutationLogID(t, commentID, "issue.comment.delete")
	if exists, _ := commentExistsWithBody(t, commentID); exists {
		t.Fatalf("comment %d still exists after delete", commentID)
	}

	activityResp := ts.get(t, fmt.Sprintf("/api/issues/%d/activity", issueID), ts.adminCookie)
	assertStatus(t, activityResp, http.StatusOK)
	var activity struct {
		UndoRows []struct {
			ID           int64  `json:"id"`
			Summary      string `json:"summary"`
			ChangeDetail string `json:"change_detail"`
			ActorLabel   string `json:"actor_label"`
		} `json:"undo_rows"`
	}
	decode(t, activityResp, &activity)
	if len(activity.UndoRows) == 0 || activity.UndoRows[0].ID != logID {
		t.Fatalf("activity undo rows = %#v, want delete mutation %d first", activity.UndoRows, logID)
	}
	if activity.UndoRows[0].Summary != "Removed comment" {
		t.Fatalf("summary=%q, want Removed comment", activity.UndoRows[0].Summary)
	}
	for _, want := range []string{"delete and restore me", "-> absent"} {
		if !strings.Contains(activity.UndoRows[0].ChangeDetail, want) {
			t.Fatalf("change_detail=%q, missing %q", activity.UndoRows[0].ChangeDetail, want)
		}
	}
	if !strings.Contains(activity.UndoRows[0].ActorLabel, "comment-cleanup via admin") {
		t.Fatalf("actor_label=%q, want cleanup agent and user", activity.UndoRows[0].ActorLabel)
	}

	undoResp := ts.post(t, fmt.Sprintf("/api/undo/%d", logID), ts.adminCookie, map[string]any{})
	assertStatus(t, undoResp, http.StatusOK)
	if exists, body := commentExistsWithBody(t, commentID); !exists || body != "delete and restore me" {
		t.Fatalf("comment after delete undo exists/body = %v/%q", exists, body)
	}

	redoResp := ts.post(t, fmt.Sprintf("/api/redo/%d", logID), ts.adminCookie, map[string]any{})
	assertStatus(t, redoResp, http.StatusOK)
	if exists, _ := commentExistsWithBody(t, commentID); exists {
		t.Fatalf("comment %d still exists after delete redo", commentID)
	}
}

func TestCommentUndoRestoreReportsParentDeletedConflict(t *testing.T) {
	ts := newTestServer(t)
	issueID := seedCommentUndoIssue(t, "comment parent conflict")

	createResp := ts.post(t, fmt.Sprintf("/api/issues/%d/comments", issueID), ts.adminCookie, map[string]any{
		"body": "restore needs parent",
	})
	assertStatus(t, createResp, http.StatusCreated)
	commentID := responseID(t, createResp)

	deleteCommentResp := ts.del(t, fmt.Sprintf("/api/comments/%d", commentID), ts.adminCookie)
	assertStatus(t, deleteCommentResp, http.StatusNoContent)
	logID := commentMutationLogID(t, commentID, "issue.comment.delete")

	deleteIssueResp := ts.del(t, fmt.Sprintf("/api/issues/%d", issueID), ts.adminCookie)
	assertStatus(t, deleteIssueResp, http.StatusNoContent)

	undoResp := ts.post(t, fmt.Sprintf("/api/undo/%d", logID), ts.adminCookie, map[string]any{})
	assertStatus(t, undoResp, http.StatusConflict)
	var conflict struct {
		Status            string `json:"status"`
		LogID             int64  `json:"log_id"`
		CascadingBlockers []struct {
			Pattern  string `json:"pattern"`
			TargetID int64  `json:"target_id"`
			Options  []struct {
				ID string `json:"id"`
			} `json:"options"`
		} `json:"cascading_blockers"`
	}
	decode(t, undoResp, &conflict)
	if conflict.Status != "conflict" || conflict.LogID != logID || len(conflict.CascadingBlockers) != 1 {
		t.Fatalf("unexpected conflict payload: %#v", conflict)
	}
	blocker := conflict.CascadingBlockers[0]
	if blocker.Pattern != "parent-deleted" || blocker.TargetID != issueID {
		t.Fatalf("blocker = %#v, want parent-deleted for issue %d", blocker, issueID)
	}
	if len(blocker.Options) == 0 || blocker.Options[0].ID != "skip_comment" {
		t.Fatalf("blocker options = %#v, want skip_comment first", blocker.Options)
	}
}
