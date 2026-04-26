package handlers_test

import (
	"bytes"
	"context"
	"fmt"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/markus-barta/paimos/backend/db"
)

func seedAICall(t *testing.T, userID, issueID int64, requestID string) {
	t.Helper()
	_, err := db.DB.Exec(`
		INSERT INTO ai_calls(
			request_id, user_id, action_key, sub_action, surface, issue_id,
			provider, model, prompt_tokens, completion_tokens, total_tokens,
			cost_micro_usd, outcome, error_class, latency_ms
		) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
	`,
		requestID, userID, "estimate_effort", "", "issue", issueID,
		"openrouter", "anthropic/claude-sonnet-4.5", 100, 20, 120,
		5000, "ok", "", 850,
	)
	if err != nil {
		t.Fatalf("seed ai_call: %v", err)
	}
}

func putWithHeaders(t *testing.T, ts *testServer, path, cookie string, body any, headers map[string]string) *http.Response {
	t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPut, ts.srv.URL+path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT %s: %v", path, err)
	}
	return resp
}

func postWithHeaders(t *testing.T, ts *testServer, path, cookie string, body any, headers map[string]string) *http.Response {
	t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, ts.srv.URL+path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	return resp
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
	seedAICall(t, memberID, issueID, "req-test-1")

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
	seedAICall(t, memberID, issueID, "req-test-1")

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
	var adminID int64
	if err := db.DB.QueryRow(`SELECT id FROM users WHERE username='admin'`).Scan(&adminID); err != nil {
		t.Fatalf("lookup admin id: %v", err)
	}
	projectID := responseID(t, ts.post(t, "/api/projects", ts.adminCookie, map[string]any{
		"name": "AI Trail Project",
		"key":  "AITR3",
	}))
	issueID := responseID(t, ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.adminCookie, map[string]any{
		"title": "AI trail issue",
		"type":  "task",
	}))
	seedAICall(t, memberID, issueID, "req-member-export")
	requestID := "req-test-activity"
	seedAICall(t, adminID, issueID, requestID)
	updateResp := putWithHeaders(t, ts, "/api/issues/"+itoa(issueID), ts.adminCookie, map[string]any{
		"estimate_hours": 5,
		"estimate_lp":    2,
	}, map[string]string{
		"X-PAIMOS-AI-Request-Id": requestID,
		"X-PAIMOS-AI-Action":     "estimate_effort",
	})
	assertStatus(t, updateResp, http.StatusOK)

	activityResp := ts.get(t, "/api/issues/"+itoa(issueID)+"/ai-activity", ts.adminCookie)
	assertStatus(t, activityResp, http.StatusOK)
	var activityBody struct {
		Rows []struct {
			LogID       int64  `json:"log_id"`
			RequestID   string `json:"request_id"`
			ActionKey   string `json:"action_key"`
			Outcome     string `json:"outcome"`
			OnUserStack bool `json:"on_user_stack"`
		} `json:"rows"`
		Count int `json:"count"`
	}
	decode(t, activityResp, &activityBody)
	if activityBody.Count != 1 || activityBody.Rows[0].ActionKey != "estimate_effort" || activityBody.Rows[0].RequestID == "" || !activityBody.Rows[0].OnUserStack || activityBody.Rows[0].Outcome != "ok" {
		t.Fatalf("unexpected issue activity body: %#v", activityBody)
	}
	undoResp := postWithHeaders(t, ts, "/api/undo/request/"+activityBody.Rows[0].RequestID, ts.adminCookie, map[string]any{}, nil)
	assertStatus(t, undoResp, http.StatusOK)
	var reverted sqlNullFloat
	if err := db.DB.QueryRow(`SELECT estimate_hours FROM issues WHERE id=?`, issueID).Scan(&reverted); err != nil {
		t.Fatalf("lookup reverted estimate_hours: %v", err)
	}
	if reverted.Valid {
		t.Fatalf("expected estimate_hours to revert to NULL, got %v", reverted.Float64)
	}
	activityResp = ts.get(t, "/api/issues/"+itoa(issueID)+"/ai-activity", ts.adminCookie)
	assertStatus(t, activityResp, http.StatusOK)
	decode(t, activityResp, &activityBody)
	if activityBody.Count != 1 || activityBody.Rows[0].Outcome != "undone" || activityBody.Rows[0].OnUserStack {
		t.Fatalf("expected undone activity row after request undo, got %#v", activityBody)
	}

	exportResp := ts.get(t, "/api/ai/calls/me/export.csv", ts.memberCookie)
	assertStatus(t, exportResp, http.StatusOK)
	if got := exportResp.Header.Get("Content-Type"); len(got) < 8 || got[:8] != "text/csv" {
		t.Fatalf("expected csv content-type, got %q", got)
	}
}

type sqlNullFloat struct {
	Float64 float64
	Valid   bool
}

func (n *sqlNullFloat) Scan(src any) error {
	if src == nil {
		n.Float64 = 0
		n.Valid = false
		return nil
	}
	switch v := src.(type) {
	case float64:
		n.Float64 = v
	case int64:
		n.Float64 = float64(v)
	case []byte:
		f, err := strconv.ParseFloat(string(v), 64)
		if err != nil {
			return err
		}
		n.Float64 = f
	case string:
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return err
		}
		n.Float64 = f
	default:
		return fmt.Errorf("unsupported scan type %T", src)
	}
	n.Valid = true
	return nil
}
