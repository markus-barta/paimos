package handlers_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/markus-barta/paimos/backend/db"
)

func seedAICall(t *testing.T, userID, issueID int64) {
	t.Helper()
	_, err := db.DB.Exec(`
		INSERT INTO ai_calls(
			request_id, user_id, action_key, sub_action, surface, issue_id,
			provider, model, prompt_tokens, completion_tokens, total_tokens,
			cost_micro_usd, outcome, error_class, latency_ms
		) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
	`,
		"req-test-1", userID, "estimate_effort", "", "issue", issueID,
		"openrouter", "anthropic/claude-sonnet-4.5", 100, 20, 120,
		5000, "ok", "", 850,
	)
	if err != nil {
		t.Fatalf("seed ai_call: %v", err)
	}
}

func TestAIListCallsAndSelfScope(t *testing.T) {
	ts := newTestServer(t)
	var memberID int64
	if err := db.DB.QueryRow(`SELECT id FROM users WHERE username='member'`).Scan(&memberID); err != nil {
		t.Fatalf("lookup member id: %v", err)
	}
	projectID := responseID(t, ts.post(t, "/api/projects", ts.adminCookie, map[string]any{
		"name": "AI Trail Project",
		"key":  "AITR",
	}))
	issueID := responseID(t, ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.adminCookie, map[string]any{
		"title": "AI trail issue",
		"type":  "task",
	}))
	seedAICall(t, memberID, issueID)

	resp := ts.get(t, "/api/ai/calls", ts.adminCookie)
	assertStatus(t, resp, http.StatusOK)
	var adminBody struct {
		Rows []struct {
			ActionKey string `json:"action_key"`
			Username  string `json:"username"`
			IssueID   int64  `json:"issue_id"`
		} `json:"rows"`
		TotalCount int `json:"total_count"`
	}
	decode(t, resp, &adminBody)
	if adminBody.TotalCount < 1 {
		t.Fatal("expected at least one admin-visible ai_call")
	}
	if adminBody.Rows[0].ActionKey != "estimate_effort" || adminBody.Rows[0].IssueID != issueID {
		t.Fatalf("unexpected row: %#v", adminBody.Rows[0])
	}

	selfResp := ts.get(t, "/api/ai/calls/me", ts.memberCookie)
	assertStatus(t, selfResp, http.StatusOK)
	var selfBody struct {
		Rows []struct {
			ActionKey string `json:"action_key"`
		} `json:"rows"`
	}
	decode(t, selfResp, &selfBody)
	if len(selfBody.Rows) != 1 || selfBody.Rows[0].ActionKey != "estimate_effort" {
		t.Fatalf("unexpected self rows: %#v", selfBody.Rows)
	}
}

func TestAIListIssueCalls(t *testing.T) {
	ts := newTestServer(t)
	var memberID int64
	if err := db.DB.QueryRow(`SELECT id FROM users WHERE username='member'`).Scan(&memberID); err != nil {
		t.Fatalf("lookup member id: %v", err)
	}
	projectID := responseID(t, ts.post(t, "/api/projects", ts.adminCookie, map[string]any{
		"name": "AI Trail Project",
		"key":  "AITR2",
	}))
	issueID := responseID(t, ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.adminCookie, map[string]any{
		"title": "AI trail issue",
		"type":  "task",
	}))
	seedAICall(t, memberID, issueID)

	resp := ts.get(t, "/api/issues/"+itoa(issueID)+"/ai-calls", ts.memberCookie)
	assertStatus(t, resp, http.StatusOK)
	var body struct {
		Rows []struct {
			IssueID int64 `json:"issue_id"`
		} `json:"rows"`
	}
	decode(t, resp, &body)
	if len(body.Rows) != 1 || body.Rows[0].IssueID != issueID {
		t.Fatalf("unexpected issue rows: %#v", body.Rows)
	}
}

func TestAIListIssueActivityAndSelfExport(t *testing.T) {
	ts := newTestServer(t)
	var memberID int64
	if err := db.DB.QueryRow(`SELECT id FROM users WHERE username='member'`).Scan(&memberID); err != nil {
		t.Fatalf("lookup member id: %v", err)
	}
	projectID := responseID(t, ts.post(t, "/api/projects", ts.adminCookie, map[string]any{
		"name": "AI Trail Project",
		"key":  "AITR3",
	}))
	issueID := responseID(t, ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.adminCookie, map[string]any{
		"title": "AI trail issue",
		"type":  "task",
	}))
	seedAICall(t, memberID, issueID)

	activityResp := ts.get(t, "/api/issues/"+itoa(issueID)+"/ai-activity", ts.memberCookie)
	assertStatus(t, activityResp, http.StatusOK)
	var activityBody struct {
		Rows []struct {
			RequestID string `json:"request_id"`
			ActionKey string `json:"action_key"`
		} `json:"rows"`
		Count int `json:"count"`
	}
	decode(t, activityResp, &activityBody)
	if activityBody.Count != 1 || activityBody.Rows[0].ActionKey != "estimate_effort" || activityBody.Rows[0].RequestID == "" {
		t.Fatalf("unexpected issue activity body: %#v", activityBody)
	}

	exportResp := ts.get(t, "/api/ai/calls/me/export.csv", ts.memberCookie)
	assertStatus(t, exportResp, http.StatusOK)
	if got := exportResp.Header.Get("Content-Type"); len(got) < 8 || got[:8] != "text/csv" {
		t.Fatalf("expected csv content-type, got %q", got)
	}
}
