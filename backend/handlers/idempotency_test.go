package handlers_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

func TestIdempotencyMiddleware_ReplaysSameIssueCreate(t *testing.T) {
	ts := newTestServer(t)
	projectID := responseID(t, ts.post(t, "/api/projects", ts.adminCookie, map[string]any{
		"name": "Idempotency Test",
		"key":  "IDEM",
	}))
	body := map[string]any{"title": "Retry-safe create", "type": "task"}
	key := "01HXIDEMPOTENCYREPLAY0000000001"

	first := postWithIdempotency(t, ts, fmt.Sprintf("/api/projects/%d/issues", projectID), body, key)
	assertStatus(t, first, http.StatusCreated)
	var firstIssue map[string]any
	decode(t, first, &firstIssue)

	second := postWithIdempotency(t, ts, fmt.Sprintf("/api/projects/%d/issues", projectID), body, key)
	assertStatus(t, second, http.StatusCreated)
	if got := second.Header.Get("X-PAIMOS-Idempotency-Replay"); got != "true" {
		t.Fatalf("replay header = %q, want true", got)
	}
	var secondIssue map[string]any
	decode(t, second, &secondIssue)
	if firstIssue["id"] != secondIssue["id"] {
		t.Fatalf("replayed id = %v, want original id %v", secondIssue["id"], firstIssue["id"])
	}
}

func TestIdempotencyMiddleware_ConflictsOnDifferentBody(t *testing.T) {
	ts := newTestServer(t)
	projectID := responseID(t, ts.post(t, "/api/projects", ts.adminCookie, map[string]any{
		"name": "Idempotency Conflict",
		"key":  "IDEC",
	}))
	path := fmt.Sprintf("/api/projects/%d/issues", projectID)
	key := "01HXIDEMPOTENCYCONFLICT00000001"

	first := postWithIdempotency(t, ts, path, map[string]any{"title": "Original", "type": "task"}, key)
	assertStatus(t, first, http.StatusCreated)
	_ = first.Body.Close()

	second := postWithIdempotency(t, ts, path, map[string]any{"title": "Changed", "type": "task"}, key)
	assertStatus(t, second, http.StatusConflict)
	var problem struct {
		Code string `json:"code"`
	}
	decode(t, second, &problem)
	if problem.Code != "idempotency_key_conflict" {
		t.Fatalf("code = %q, want idempotency_key_conflict", problem.Code)
	}
}

func TestIdempotencyMiddleware_ConflictsOnDifferentRoute(t *testing.T) {
	ts := newTestServer(t)
	projectID := responseID(t, ts.post(t, "/api/projects", ts.adminCookie, map[string]any{
		"name": "Idempotency Route Conflict",
		"key":  "IDER",
	}))
	key := "01HXIDEMPOTENCYROUTECONFLICT001"
	first := postWithIdempotency(t, ts, fmt.Sprintf("/api/projects/%d/issues", projectID), map[string]any{
		"title": "Original",
		"type":  "task",
	}, key)
	assertStatus(t, first, http.StatusCreated)
	var issue struct {
		ID int64 `json:"id"`
	}
	decode(t, first, &issue)

	second := postWithIdempotency(t, ts, fmt.Sprintf("/api/issues/%d/comments", issue.ID), map[string]any{
		"body": "same key, different route",
	}, key)
	assertStatus(t, second, http.StatusConflict)
	var problem struct {
		Code string `json:"code"`
	}
	decode(t, second, &problem)
	if problem.Code != "idempotency_key_conflict" {
		t.Fatalf("code = %q, want idempotency_key_conflict", problem.Code)
	}
}

func postWithIdempotency(t *testing.T, ts *testServer, path string, body any, key string) *http.Response {
	t.Helper()
	raw, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, ts.srv.URL+path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", ts.adminCookie)
	req.Header.Set("Idempotency-Key", key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	return resp
}
