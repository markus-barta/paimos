package handlers_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/markus-barta/paimos/backend/db"
)

func TestUndoConflictResolveAndRedo(t *testing.T) {
	ts := newTestServer(t)
	var adminID int64
	if err := db.DB.QueryRow(`SELECT id FROM users WHERE username='admin'`).Scan(&adminID); err != nil {
		t.Fatalf("lookup admin id: %v", err)
	}
	projectID := responseID(t, ts.post(t, "/api/projects", ts.adminCookie, map[string]any{
		"name": "Undo Project",
		"key":  "UNDO1",
	}))
	issueID := responseID(t, ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.adminCookie, map[string]any{
		"title": "Undo Issue",
		"type":  "task",
	}))
	requestID := "req-undo-flow"
	seedAICall(t, adminID, issueID, requestID)

	updateResp := putWithHeaders(t, ts, "/api/issues/"+itoa(issueID), ts.adminCookie, map[string]any{
		"estimate_hours": 5,
		"estimate_lp":    2,
	}, map[string]string{
		"X-PAIMOS-AI-Request-Id": requestID,
		"X-PAIMOS-AI-Action":     "estimate_effort",
	})
	assertStatus(t, updateResp, http.StatusOK)

	// Drift the field so undo must classify rather than overwrite silently.
	driftResp := ts.put(t, "/api/issues/"+itoa(issueID), ts.adminCookie, map[string]any{
		"estimate_hours": 8,
		"estimate_lp":    3,
	})
	assertStatus(t, driftResp, http.StatusOK)

	conflictResp := ts.post(t, "/api/undo/request/"+requestID, ts.adminCookie, map[string]any{})
	assertStatus(t, conflictResp, http.StatusConflict)
	var conflictBody struct {
		Status    string `json:"status"`
		LogID     int64  `json:"log_id"`
		Mode      string `json:"mode"`
		Conflicts []struct {
			Field string `json:"field"`
		} `json:"conflicts"`
	}
	decode(t, conflictResp, &conflictBody)
	if conflictBody.Status != "conflict" || conflictBody.Mode != "undo" || len(conflictBody.Conflicts) == 0 || conflictBody.Conflicts[0].Field != "estimate_hours" {
		t.Fatalf("unexpected conflict payload: %#v", conflictBody)
	}

	resolveResp := ts.post(t, fmt.Sprintf("/api/undo/%d/resolve", conflictBody.LogID), ts.adminCookie, map[string]any{
		"field_choices": map[string]string{
			"estimate_hours": "overwrite",
			"estimate_lp":    "overwrite",
		},
	})
	assertStatus(t, resolveResp, http.StatusOK)

	var estimate sqlNullFloat
	if err := db.DB.QueryRow(`SELECT estimate_hours FROM issues WHERE id=?`, issueID).Scan(&estimate); err != nil {
		t.Fatalf("lookup resolved estimate_hours: %v", err)
	}
	if estimate.Valid {
		t.Fatalf("expected estimate_hours to be reverted to NULL after resolve, got %v", estimate.Float64)
	}

	redoResp := ts.post(t, fmt.Sprintf("/api/redo/%d", conflictBody.LogID), ts.adminCookie, map[string]any{})
	assertStatus(t, redoResp, http.StatusOK)
	if err := db.DB.QueryRow(`SELECT estimate_hours FROM issues WHERE id=?`, issueID).Scan(&estimate); err != nil {
		t.Fatalf("lookup redone estimate_hours: %v", err)
	}
	if !estimate.Valid || estimate.Float64 != 5 {
		t.Fatalf("expected estimate_hours to be redone to 5, got %#v", estimate)
	}
}

func TestSystemSettingsAndUndoActivityEndpoints(t *testing.T) {
	ts := newTestServer(t)
	resp := ts.get(t, "/api/system/settings", ts.adminCookie)
	assertStatus(t, resp, http.StatusOK)
	var body struct {
		UndoStackDepth int `json:"undo_stack_depth"`
	}
	decode(t, resp, &body)
	if body.UndoStackDepth != 3 {
		t.Fatalf("expected default undo_stack_depth=3, got %d", body.UndoStackDepth)
	}

	putResp := ts.put(t, "/api/system/settings", ts.adminCookie, map[string]any{"undo_stack_depth": 5})
	assertStatus(t, putResp, http.StatusOK)
	decode(t, putResp, &body)
	if body.UndoStackDepth != 5 {
		t.Fatalf("expected saved undo_stack_depth=5, got %d", body.UndoStackDepth)
	}

	policyResp := ts.get(t, "/api/gdpr/retention", ts.adminCookie)
	assertStatus(t, policyResp, http.StatusOK)
	var retention struct {
		MutationLogDays int `json:"mutation_log_days"`
	}
	decode(t, policyResp, &retention)
	if retention.MutationLogDays <= 0 {
		t.Fatalf("expected mutation_log_days in retention policy, got %#v", retention)
	}
}
