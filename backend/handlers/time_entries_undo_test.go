package handlers_test

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/markus-barta/paimos/backend/db"
)

func seedTimeEntryUndoIssue(t *testing.T, title string) int64 {
	t.Helper()
	projectID := seedBatchProject(t, "Time Entry Undo", "TEU")
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

func timeEntryMutationLogID(t *testing.T, entryID int64, mutationType string) int64 {
	t.Helper()
	var id int64
	var undoable, onStack int
	if err := db.DB.QueryRow(`
		SELECT id, undoable, on_user_stack
		FROM mutation_log
		WHERE subject_type='time_entry' AND subject_id=? AND mutation_type=?
		ORDER BY id DESC
		LIMIT 1
	`, entryID, mutationType).Scan(&id, &undoable, &onStack); err != nil {
		t.Fatalf("time_entry mutation_log %s/%d: %v", mutationType, entryID, err)
	}
	if undoable != 1 || onStack != 1 {
		t.Fatalf("mutation_log id=%d undoable/on_user_stack = %d/%d, want 1/1", id, undoable, onStack)
	}
	return id
}

func timeEntryState(t *testing.T, entryID int64) (bool, string, float64, bool) {
	t.Helper()
	var comment string
	var override sql.NullFloat64
	err := db.DB.QueryRow(`SELECT comment, override FROM time_entries WHERE id=?`, entryID).Scan(&comment, &override)
	if errors.Is(err, sql.ErrNoRows) {
		return false, "", 0, false
	}
	if err != nil {
		t.Fatalf("time entry %d: %v", entryID, err)
	}
	return true, comment, override.Float64, override.Valid
}

func TestTimeEntryCreateUndoRedoAndActivityDetails(t *testing.T) {
	ts := newTestServer(t)
	issueID := seedTimeEntryUndoIssue(t, "time entry create")

	resp := ts.requestWithHeaders(t, http.MethodPost,
		fmt.Sprintf("/api/issues/%d/time-entries", issueID),
		ts.adminCookie,
		map[string]any{
			"started_at": "2026-05-12T09:00:00Z",
			"stopped_at": "2026-05-12T10:15:00Z",
			"override":   1.25,
			"comment":    "analysis work",
		},
		map[string]string{
			"X-Paimos-Agent-Name": "timer-agent",
			"X-Paimos-Session-Id": "session-time-create",
		})
	assertStatus(t, resp, http.StatusCreated)
	entryID := responseID(t, resp)
	logID := timeEntryMutationLogID(t, entryID, "issue.time_entry.create")

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
		t.Fatalf("activity undo rows = %#v, want time-entry mutation %d first", activity.UndoRows, logID)
	}
	row := activity.UndoRows[0]
	if row.SubjectLabel == "" || !strings.Contains(row.SubjectLabel, "Time entry on") {
		t.Fatalf("subject_label=%q, want time-entry issue label", row.SubjectLabel)
	}
	if row.Summary != "Added time entry" {
		t.Fatalf("summary=%q, want Added time entry", row.Summary)
	}
	for _, want := range []string{"absent ->", "1.25h", "analysis work"} {
		if !strings.Contains(row.ChangeDetail, want) {
			t.Fatalf("change_detail=%q, missing %q", row.ChangeDetail, want)
		}
	}
	if !strings.Contains(row.ActorLabel, "timer-agent via admin") {
		t.Fatalf("actor_label=%q, want timer-agent via admin", row.ActorLabel)
	}
	if row.OriginLabel != "session session-time-create" {
		t.Fatalf("origin_label=%q", row.OriginLabel)
	}

	undoResp := ts.post(t, fmt.Sprintf("/api/undo/%d", logID), ts.adminCookie, map[string]any{})
	assertStatus(t, undoResp, http.StatusOK)
	if exists, _, _, _ := timeEntryState(t, entryID); exists {
		t.Fatalf("time entry %d still exists after create undo", entryID)
	}

	redoResp := ts.post(t, fmt.Sprintf("/api/redo/%d", logID), ts.adminCookie, map[string]any{})
	assertStatus(t, redoResp, http.StatusOK)
	if exists, comment, override, hasOverride := timeEntryState(t, entryID); !exists || comment != "analysis work" || !hasOverride || override != 1.25 {
		t.Fatalf("time entry after redo exists/comment/override = %v/%q/%v/%v", exists, comment, override, hasOverride)
	}
}

func TestTimeEntryUpdateUndoRedo(t *testing.T) {
	ts := newTestServer(t)
	issueID := seedTimeEntryUndoIssue(t, "time entry update")

	createResp := ts.post(t, fmt.Sprintf("/api/issues/%d/time-entries", issueID), ts.adminCookie, map[string]any{
		"started_at": "2026-05-12T09:00:00Z",
		"stopped_at": "2026-05-12T10:00:00Z",
		"override":   1.0,
		"comment":    "initial work",
	})
	assertStatus(t, createResp, http.StatusCreated)
	entryID := responseID(t, createResp)

	updateResp := ts.requestWithHeaders(t, http.MethodPut,
		fmt.Sprintf("/api/time-entries/%d", entryID),
		ts.adminCookie,
		map[string]any{"override": 2.5, "comment": "updated work"},
		map[string]string{"X-Paimos-Agent-Name": "timer-editor"})
	assertStatus(t, updateResp, http.StatusOK)
	logID := timeEntryMutationLogID(t, entryID, "issue.time_entry.update")

	activityResp := ts.get(t, fmt.Sprintf("/api/issues/%d/activity", issueID), ts.adminCookie)
	assertStatus(t, activityResp, http.StatusOK)
	var activity struct {
		UndoRows []struct {
			ID           int64  `json:"id"`
			Summary      string `json:"summary"`
			ChangeDetail string `json:"change_detail"`
		} `json:"undo_rows"`
	}
	decode(t, activityResp, &activity)
	if len(activity.UndoRows) == 0 || activity.UndoRows[0].ID != logID {
		t.Fatalf("activity undo rows = %#v, want update mutation %d first", activity.UndoRows, logID)
	}
	if activity.UndoRows[0].Summary != "Updated time entry" {
		t.Fatalf("summary=%q, want Updated time entry", activity.UndoRows[0].Summary)
	}
	for _, want := range []string{"1h", "2.5h", "updated work", "override"} {
		if !strings.Contains(activity.UndoRows[0].ChangeDetail, want) {
			t.Fatalf("change_detail=%q, missing %q", activity.UndoRows[0].ChangeDetail, want)
		}
	}

	undoResp := ts.post(t, fmt.Sprintf("/api/undo/%d", logID), ts.adminCookie, map[string]any{})
	assertStatus(t, undoResp, http.StatusOK)
	if exists, comment, override, hasOverride := timeEntryState(t, entryID); !exists || comment != "initial work" || !hasOverride || override != 1.0 {
		t.Fatalf("time entry after update undo exists/comment/override = %v/%q/%v/%v", exists, comment, override, hasOverride)
	}

	redoResp := ts.post(t, fmt.Sprintf("/api/redo/%d", logID), ts.adminCookie, map[string]any{})
	assertStatus(t, redoResp, http.StatusOK)
	if exists, comment, override, hasOverride := timeEntryState(t, entryID); !exists || comment != "updated work" || !hasOverride || override != 2.5 {
		t.Fatalf("time entry after update redo exists/comment/override = %v/%q/%v/%v", exists, comment, override, hasOverride)
	}
}

func TestTimeEntryDeleteUndoRedo(t *testing.T) {
	ts := newTestServer(t)
	issueID := seedTimeEntryUndoIssue(t, "time entry delete")

	createResp := ts.post(t, fmt.Sprintf("/api/issues/%d/time-entries", issueID), ts.adminCookie, map[string]any{
		"started_at": "2026-05-12T11:00:00Z",
		"stopped_at": "2026-05-12T12:00:00Z",
		"comment":    "remove and restore",
	})
	assertStatus(t, createResp, http.StatusCreated)
	entryID := responseID(t, createResp)

	deleteResp := ts.del(t, fmt.Sprintf("/api/time-entries/%d", entryID), ts.adminCookie)
	assertStatus(t, deleteResp, http.StatusNoContent)
	logID := timeEntryMutationLogID(t, entryID, "issue.time_entry.delete")
	if exists, _, _, _ := timeEntryState(t, entryID); exists {
		t.Fatalf("time entry %d still exists after delete", entryID)
	}

	undoResp := ts.post(t, fmt.Sprintf("/api/undo/%d", logID), ts.adminCookie, map[string]any{})
	assertStatus(t, undoResp, http.StatusOK)
	if exists, comment, _, _ := timeEntryState(t, entryID); !exists || comment != "remove and restore" {
		t.Fatalf("time entry after delete undo exists/comment = %v/%q", exists, comment)
	}

	redoResp := ts.post(t, fmt.Sprintf("/api/redo/%d", logID), ts.adminCookie, map[string]any{})
	assertStatus(t, redoResp, http.StatusOK)
	if exists, _, _, _ := timeEntryState(t, entryID); exists {
		t.Fatalf("time entry %d still exists after delete redo", entryID)
	}
}

func TestTimeEntryUndoRefusesInvoicedIssue(t *testing.T) {
	ts := newTestServer(t)
	issueID := seedTimeEntryUndoIssue(t, "time entry invoiced")

	createResp := ts.post(t, fmt.Sprintf("/api/issues/%d/time-entries", issueID), ts.adminCookie, map[string]any{
		"started_at": "2026-05-12T13:00:00Z",
		"stopped_at": "2026-05-12T14:00:00Z",
		"comment":    "invoice locked",
	})
	assertStatus(t, createResp, http.StatusCreated)
	entryID := responseID(t, createResp)
	logID := timeEntryMutationLogID(t, entryID, "issue.time_entry.create")

	if _, err := db.DB.Exec(`UPDATE issues SET status='invoiced', invoiced_at='2026-05-12 14:30:00' WHERE id=?`, issueID); err != nil {
		t.Fatalf("mark issue invoiced: %v", err)
	}
	undoResp := ts.post(t, fmt.Sprintf("/api/undo/%d", logID), ts.adminCookie, map[string]any{})
	assertStatus(t, undoResp, http.StatusLocked)
	if exists, comment, _, _ := timeEntryState(t, entryID); !exists || comment != "invoice locked" {
		t.Fatalf("time entry after locked undo exists/comment = %v/%q", exists, comment)
	}
}

func TestTimeEntryUndoRestoreReportsParentDeletedConflict(t *testing.T) {
	ts := newTestServer(t)
	issueID := seedTimeEntryUndoIssue(t, "time entry parent conflict")

	createResp := ts.post(t, fmt.Sprintf("/api/issues/%d/time-entries", issueID), ts.adminCookie, map[string]any{
		"started_at": "2026-05-12T15:00:00Z",
		"stopped_at": "2026-05-12T16:00:00Z",
		"comment":    "restore needs parent",
	})
	assertStatus(t, createResp, http.StatusCreated)
	entryID := responseID(t, createResp)

	deleteEntryResp := ts.del(t, fmt.Sprintf("/api/time-entries/%d", entryID), ts.adminCookie)
	assertStatus(t, deleteEntryResp, http.StatusNoContent)
	logID := timeEntryMutationLogID(t, entryID, "issue.time_entry.delete")

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
	if len(blocker.Options) == 0 || blocker.Options[0].ID != "skip_time_entry" {
		t.Fatalf("blocker options = %#v, want skip_time_entry first", blocker.Options)
	}
}
