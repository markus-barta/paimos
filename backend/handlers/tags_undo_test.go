package handlers_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/markus-barta/paimos/backend/db"
)

func seedTagUndoIssue(t *testing.T, title string) int64 {
	t.Helper()
	projectID := seedBatchProject(t, "Tag Undo", "TU")
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

func tagMutationLogID(t *testing.T, issueID int64, mutationType string) int64 {
	t.Helper()
	var id int64
	var undoable, onStack int
	if err := db.DB.QueryRow(`
		SELECT id, undoable, on_user_stack
		FROM mutation_log
		WHERE subject_type='issue_tag' AND subject_id=? AND mutation_type=?
		ORDER BY id DESC
		LIMIT 1
	`, issueID, mutationType).Scan(&id, &undoable, &onStack); err != nil {
		t.Fatalf("tag mutation_log %s/%d: %v", mutationType, issueID, err)
	}
	if undoable != 1 || onStack != 1 {
		t.Fatalf("mutation_log id=%d undoable/on_user_stack = %d/%d, want 1/1", id, undoable, onStack)
	}
	return id
}

func issueHasTag(t *testing.T, issueID, tagID int64) bool {
	t.Helper()
	var exists int
	err := db.DB.QueryRow(`SELECT 1 FROM issue_tags WHERE issue_id=? AND tag_id=?`, issueID, tagID).Scan(&exists)
	return err == nil
}

func TestIssueTagAddUndoRedoAndActivityDetails(t *testing.T) {
	ts := newTestServer(t)
	issueID := seedTagUndoIssue(t, "tag add")
	tagID := firstTagID(t)

	resp := ts.requestWithHeaders(t, http.MethodPost,
		fmt.Sprintf("/api/issues/%d/tags", issueID),
		ts.adminCookie,
		map[string]any{"tag_id": tagID},
		map[string]string{
			"X-Paimos-Agent-Name": "tag-agent",
			"X-Paimos-Session-Id": "session-tag-add",
		})
	assertStatus(t, resp, http.StatusNoContent)
	logID := tagMutationLogID(t, issueID, "issue.tag.add")
	if !issueHasTag(t, issueID, tagID) {
		t.Fatalf("issue %d missing tag %d after add", issueID, tagID)
	}

	activityResp := ts.get(t, fmt.Sprintf("/api/issues/%d/activity", issueID), ts.adminCookie)
	assertStatus(t, activityResp, http.StatusOK)
	var activity struct {
		UndoRows []struct {
			ID           int64  `json:"id"`
			Summary      string `json:"summary"`
			ChangeDetail string `json:"change_detail"`
			ActorLabel   string `json:"actor_label"`
			OriginLabel  string `json:"origin_label"`
		} `json:"undo_rows"`
	}
	decode(t, activityResp, &activity)
	if len(activity.UndoRows) == 0 || activity.UndoRows[0].ID != logID {
		t.Fatalf("activity undo rows = %#v, want tag mutation %d first", activity.UndoRows, logID)
	}
	row := activity.UndoRows[0]
	if row.Summary != "Added tag" {
		t.Fatalf("summary=%q, want Added tag", row.Summary)
	}
	for _, want := range []string{"#bug", "absent -> present"} {
		if !strings.Contains(row.ChangeDetail, want) {
			t.Fatalf("change_detail=%q, missing %q", row.ChangeDetail, want)
		}
	}
	if !strings.Contains(row.ActorLabel, "tag-agent via admin") {
		t.Fatalf("actor_label=%q, want tag-agent via admin", row.ActorLabel)
	}
	if row.OriginLabel != "session session-tag-add" {
		t.Fatalf("origin_label=%q", row.OriginLabel)
	}

	undoResp := ts.post(t, fmt.Sprintf("/api/undo/%d", logID), ts.adminCookie, map[string]any{})
	assertStatus(t, undoResp, http.StatusOK)
	if issueHasTag(t, issueID, tagID) {
		t.Fatalf("issue %d still has tag %d after undo", issueID, tagID)
	}

	redoResp := ts.post(t, fmt.Sprintf("/api/redo/%d", logID), ts.adminCookie, map[string]any{})
	assertStatus(t, redoResp, http.StatusOK)
	if !issueHasTag(t, issueID, tagID) {
		t.Fatalf("issue %d missing tag %d after redo", issueID, tagID)
	}
}

func TestIssueTagRemoveUndoRedo(t *testing.T) {
	ts := newTestServer(t)
	issueID := seedTagUndoIssue(t, "tag remove")
	tagID := firstTagID(t)
	if _, err := db.DB.Exec(`INSERT INTO issue_tags(issue_id, tag_id) VALUES(?,?)`, issueID, tagID); err != nil {
		t.Fatalf("seed issue tag: %v", err)
	}

	resp := ts.requestWithHeaders(t, http.MethodDelete,
		fmt.Sprintf("/api/issues/%d/tags/%d", issueID, tagID),
		ts.adminCookie,
		nil,
		map[string]string{"X-Paimos-Agent-Name": "tag-cleanup"})
	assertStatus(t, resp, http.StatusNoContent)
	logID := tagMutationLogID(t, issueID, "issue.tag.remove")
	if issueHasTag(t, issueID, tagID) {
		t.Fatalf("issue %d still has tag %d after remove", issueID, tagID)
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
		t.Fatalf("activity undo rows = %#v, want remove mutation %d first", activity.UndoRows, logID)
	}
	if activity.UndoRows[0].Summary != "Removed tag" {
		t.Fatalf("summary=%q, want Removed tag", activity.UndoRows[0].Summary)
	}
	for _, want := range []string{"#bug", "present -> absent"} {
		if !strings.Contains(activity.UndoRows[0].ChangeDetail, want) {
			t.Fatalf("change_detail=%q, missing %q", activity.UndoRows[0].ChangeDetail, want)
		}
	}
	if !strings.Contains(activity.UndoRows[0].ActorLabel, "tag-cleanup via admin") {
		t.Fatalf("actor_label=%q, want tag-cleanup via admin", activity.UndoRows[0].ActorLabel)
	}

	undoResp := ts.post(t, fmt.Sprintf("/api/undo/%d", logID), ts.adminCookie, map[string]any{})
	assertStatus(t, undoResp, http.StatusOK)
	if !issueHasTag(t, issueID, tagID) {
		t.Fatalf("issue %d missing tag %d after undo", issueID, tagID)
	}

	redoResp := ts.post(t, fmt.Sprintf("/api/redo/%d", logID), ts.adminCookie, map[string]any{})
	assertStatus(t, redoResp, http.StatusOK)
	if issueHasTag(t, issueID, tagID) {
		t.Fatalf("issue %d still has tag %d after redo", issueID, tagID)
	}
}

func TestIssueTagUndoRestoreReportsDeletedTagConflict(t *testing.T) {
	ts := newTestServer(t)
	issueID := seedTagUndoIssue(t, "tag deleted conflict")
	tagID := firstTagID(t)
	if _, err := db.DB.Exec(`INSERT INTO issue_tags(issue_id, tag_id) VALUES(?,?)`, issueID, tagID); err != nil {
		t.Fatalf("seed issue tag: %v", err)
	}

	removeResp := ts.del(t, fmt.Sprintf("/api/issues/%d/tags/%d", issueID, tagID), ts.adminCookie)
	assertStatus(t, removeResp, http.StatusNoContent)
	logID := tagMutationLogID(t, issueID, "issue.tag.remove")

	deleteTagResp := ts.del(t, fmt.Sprintf("/api/tags/%d", tagID), ts.adminCookie)
	assertStatus(t, deleteTagResp, http.StatusNoContent)

	undoResp := ts.post(t, fmt.Sprintf("/api/undo/%d", logID), ts.adminCookie, map[string]any{})
	assertStatus(t, undoResp, http.StatusConflict)
	var conflict struct {
		Status    string `json:"status"`
		LogID     int64  `json:"log_id"`
		Conflicts []struct {
			Pattern string `json:"pattern"`
			Field   string `json:"field"`
			Options []struct {
				ID string `json:"id"`
			} `json:"options"`
		} `json:"conflicts"`
	}
	decode(t, undoResp, &conflict)
	if conflict.Status != "conflict" || conflict.LogID != logID || len(conflict.Conflicts) != 1 {
		t.Fatalf("unexpected conflict payload: %#v", conflict)
	}
	c := conflict.Conflicts[0]
	if c.Pattern != "field-set-deleted" || c.Field != "tag" {
		t.Fatalf("conflict = %#v, want field-set-deleted tag", c)
	}
	if len(c.Options) == 0 || c.Options[0].ID != "skip_tag" {
		t.Fatalf("conflict options = %#v, want skip_tag first", c.Options)
	}
}
