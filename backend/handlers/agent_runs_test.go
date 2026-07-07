// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package handlers_test

import (
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/markus-barta/paimos/backend/auth"
	"github.com/markus-barta/paimos/backend/db"
	"github.com/markus-barta/paimos/backend/sse"
)

// TestAgentRunsLifecycle exercises PAI-606: create a queued run via the
// "Implement this" endpoint, list it on the issue, then transition it through
// running → deployed with the structured report, asserting the clock stamps,
// status validation, and requester/admin-only write access.
func TestAgentRunsLifecycle(t *testing.T) {
	ts := newTestServer(t)
	projID := seedBatchProject(t, "PAI", "PAI")
	res, err := db.DB.Exec(
		`INSERT INTO issues(project_id, issue_number, type, title, status) VALUES(?,?,?,?,?)`,
		projID, 1, "ticket", "Implement me", "backlog")
	if err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	issueID, _ := res.LastInsertId()

	// POST /implement → a queued run carrying the device + deploy target.
	resp := ts.post(t, "/api/issues/"+itoa(issueID)+"/implement", ts.adminCookie,
		map[string]any{"device_id": "laptop-1", "deploy_target": "ppm"})
	assertStatus(t, resp, http.StatusCreated)
	var run map[string]any
	decode(t, resp, &run)
	if run["status"] != "queued" {
		t.Fatalf("status=%v, want queued", run["status"])
	}
	if run["device_id"] != "laptop-1" {
		t.Errorf("device_id=%v, want laptop-1", run["device_id"])
	}
	if run["started_at"] != nil || run["finished_at"] != nil {
		t.Errorf("clocks should be nil on a queued run: started=%v finished=%v", run["started_at"], run["finished_at"])
	}
	runID := int64(run["id"].(float64))

	// GET /issues/{id}/runs → the run shows up (newest first).
	resp = ts.get(t, "/api/issues/"+itoa(issueID)+"/runs", ts.adminCookie)
	assertStatus(t, resp, http.StatusOK)
	var list struct {
		Runs []map[string]any `json:"runs"`
	}
	decode(t, resp, &list)
	if len(list.Runs) != 1 || int64(list.Runs[0]["id"].(float64)) != runID {
		t.Fatalf("issue runs = %+v, want the one run %d", list.Runs, runID)
	}

	// PATCH → running stamps started_at.
	resp = ts.patch(t, "/api/runs/"+itoa(runID), ts.adminCookie, map[string]any{"status": "running"})
	assertStatus(t, resp, http.StatusOK)
	decode(t, resp, &run)
	if run["status"] != "running" {
		t.Fatalf("status=%v, want running", run["status"])
	}
	if run["started_at"] == nil {
		t.Errorf("started_at should be stamped on the move to running")
	}

	// PATCH → deployed records the report and stamps finished_at.
	resp = ts.patch(t, "/api/runs/"+itoa(runID), ts.adminCookie, map[string]any{
		"status":        "deployed",
		"version":       "4.6.0",
		"deploy_target": "ppm",
		"tests_summary": `{"passed":42,"failed":0}`,
	})
	assertStatus(t, resp, http.StatusOK)
	decode(t, resp, &run)
	if run["status"] != "deployed" {
		t.Fatalf("status=%v, want deployed", run["status"])
	}
	if run["version"] != "4.6.0" {
		t.Errorf("version=%v, want 4.6.0", run["version"])
	}
	if run["tests_summary"] != `{"passed":42,"failed":0}` {
		t.Errorf("tests_summary=%v", run["tests_summary"])
	}
	if run["finished_at"] == nil {
		t.Errorf("finished_at should be stamped on a terminal status")
	}

	// An unknown status is rejected.
	resp = ts.patch(t, "/api/runs/"+itoa(runID), ts.adminCookie, map[string]any{"status": "bogus"})
	assertStatus(t, resp, http.StatusBadRequest)

	// Project editors can read a run even when they did not request it.
	resp = ts.get(t, "/api/runs/"+itoa(runID), ts.memberCookie)
	assertStatus(t, resp, http.StatusOK)

	// A user with no access to the project (external) cannot read or write it.
	resp = ts.patch(t, "/api/runs/"+itoa(runID), ts.externalCookie, map[string]any{"status": "running"})
	assertStatus(t, resp, http.StatusForbidden)
	resp = ts.get(t, "/api/runs/"+itoa(runID), ts.externalCookie)
	assertStatus(t, resp, http.StatusForbidden)

	// The requester (admin here) can fetch the single run.
	resp = ts.get(t, "/api/runs/"+itoa(runID), ts.adminCookie)
	assertStatus(t, resp, http.StatusOK)
}

// TestAgentRunReportComment covers PAI-609: a terminal transition auto-posts a
// human-readable summary comment on the issue, exactly once.
func TestAgentRunReportComment(t *testing.T) {
	ts := newTestServer(t)
	projID := seedBatchProject(t, "PAI", "PAI")
	res, err := db.DB.Exec(
		`INSERT INTO issues(project_id, issue_number, type, title, status) VALUES(?,?,?,?,?)`,
		projID, 1, "ticket", "Report me", "backlog")
	if err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	issueID, _ := res.LastInsertId()

	resp := ts.post(t, "/api/issues/"+itoa(issueID)+"/implement", ts.adminCookie,
		map[string]any{"device_id": "laptop-1"})
	assertStatus(t, resp, http.StatusCreated)
	var run map[string]any
	decode(t, resp, &run)
	runID := int64(run["id"].(float64))

	// running → no comment yet.
	ts.patch(t, "/api/runs/"+itoa(runID), ts.adminCookie, map[string]any{"status": "running"})
	if n := commentCount(t, issueID); n != 0 {
		t.Fatalf("comments after running = %d, want 0", n)
	}

	// deployed → one report comment.
	ts.patch(t, "/api/runs/"+itoa(runID), ts.adminCookie, map[string]any{
		"status": "deployed", "version": "4.6.0", "deploy_target": "ppm",
	})
	body, n := firstComment(t, issueID)
	if n != 1 {
		t.Fatalf("comments after deployed = %d, want 1", n)
	}
	for _, want := range []string{"Implemented", "v4.6.0", "ppm", "laptop-1"} {
		if !strings.Contains(body, want) {
			t.Errorf("report comment %q missing %q", body, want)
		}
	}

	// A redundant deployed PATCH must not post a second comment.
	ts.patch(t, "/api/runs/"+itoa(runID), ts.adminCookie, map[string]any{"status": "deployed"})
	if n := commentCount(t, issueID); n != 1 {
		t.Errorf("comments after redundant deployed = %d, want 1", n)
	}
}

// TestImplementIsIdempotent covers PAI-605 M7: repeated "Implement this" clicks
// while a run is active return the SAME run, not a pile of duplicates.
func TestImplementIsIdempotent(t *testing.T) {
	ts := newTestServer(t)
	projID := seedBatchProject(t, "PAI", "PAI")
	res, err := db.DB.Exec(
		`INSERT INTO issues(project_id, issue_number, type, title, status) VALUES(?,?,?,?,?)`,
		projID, 1, "ticket", "Once please", "backlog")
	if err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	issueID, _ := res.LastInsertId()

	resp := ts.post(t, "/api/issues/"+itoa(issueID)+"/implement", ts.adminCookie, map[string]any{})
	assertStatus(t, resp, http.StatusCreated)
	var r1 map[string]any
	decode(t, resp, &r1)

	// Second click returns the existing run (200, not 201) with the same id.
	resp = ts.post(t, "/api/issues/"+itoa(issueID)+"/implement", ts.adminCookie, map[string]any{})
	assertStatus(t, resp, http.StatusOK)
	var r2 map[string]any
	decode(t, resp, &r2)
	if r1["id"] != r2["id"] {
		t.Fatalf("expected the same run id, got %v then %v", r1["id"], r2["id"])
	}

	resp = ts.get(t, "/api/issues/"+itoa(issueID)+"/runs", ts.adminCookie)
	var list struct {
		Runs []map[string]any `json:"runs"`
	}
	decode(t, resp, &list)
	if len(list.Runs) != 1 {
		t.Errorf("runs = %d, want 1 (idempotent)", len(list.Runs))
	}
}

// seedRunForIssue creates an issue and a queued run on it, returning both ids.
func seedRunForIssue(t *testing.T, ts *testServer, projID int64, num int) (int64, int64) {
	t.Helper()
	res, err := db.DB.Exec(
		`INSERT INTO issues(project_id, issue_number, type, title, status) VALUES(?,?,?,?,?)`,
		projID, num, "ticket", "Run probe", "backlog")
	if err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	issueID, _ := res.LastInsertId()
	resp := ts.post(t, "/api/issues/"+itoa(issueID)+"/implement", ts.adminCookie, map[string]any{})
	assertStatus(t, resp, http.StatusCreated)
	var run map[string]any
	decode(t, resp, &run)
	return issueID, int64(run["id"].(float64))
}

func seedImplementWatch(t *testing.T, userID, projectID int64, deviceID string) {
	t.Helper()
	_, err := db.DB.Exec(
		`INSERT INTO auto_watch_subscriptions(user_id, device_id, project_id, enabled, can_implement, created_at, updated_at)
		 VALUES(?,?,?,?,1,datetime('now'),datetime('now'))
		 ON CONFLICT(user_id, device_id, project_id) DO UPDATE SET
		   enabled=1,
		   can_implement=1,
		   updated_at=datetime('now')`,
		userID, deviceID, projectID, 1)
	if err != nil {
		t.Fatalf("seed implement watch: %v", err)
	}
}

func seedLiveImplementRunner(t *testing.T, userID, projectID int64, deviceID string) {
	t.Helper()
	seedImplementWatch(t, userID, projectID, deviceID)
	sub := sse.GlobalBroker().Subscribe(userID, deviceID, projectID, true)
	t.Cleanup(func() { sse.GlobalBroker().Close(sub) })
}

func seedLiveImplementRunnerAction(t *testing.T, userID, projectID int64, deviceID, actionKey, label string) {
	t.Helper()
	seedImplementWatch(t, userID, projectID, deviceID)
	sub := sse.GlobalBroker().Subscribe(userID, deviceID, projectID, true, []sse.ActionCapability{{
		ActionKey:    actionKey,
		ProviderKind: "local_cli",
		ProviderID:   strings.TrimSuffix(actionKey, ".implement"),
		Label:        label,
		RunModes:     []string{"edit"},
		CanTest:      true,
	}})
	t.Cleanup(func() { sse.GlobalBroker().Close(sub) })
}

func seedMemberUser(t *testing.T, ts *testServer, username, password string) (int64, string) {
	t.Helper()
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	res, err := db.DB.Exec(
		`INSERT INTO users(username, password, role, status) VALUES(?,?,?,?)`,
		username, hash, "member", "active")
	if err != nil {
		t.Fatalf("seed member user: %v", err)
	}
	id, _ := res.LastInsertId()
	auth.SeedAccessForUser(id, "member")
	return id, ts.login(t, username, password)
}

func TestAgentRunProjectEditorClaimRequiresLiveRunner(t *testing.T) {
	ts := newTestServer(t)
	projID := seedBatchProject(t, "PAI", "PAI")
	_, runID := seedRunForIssue(t, ts, projID, 1)
	memberID := userID(t, "member")

	seedImplementWatch(t, memberID, projID, "member-dev")
	resp := ts.patch(t, "/api/runs/"+itoa(runID), ts.memberCookie, map[string]any{
		"status":    "running",
		"if_status": "queued",
		"device_id": "member-dev",
	})
	assertStatus(t, resp, http.StatusForbidden)

	seedLiveImplementRunner(t, memberID, projID, "member-dev")
	resp = ts.patch(t, "/api/runs/"+itoa(runID), ts.memberCookie, map[string]any{
		"status":    "running",
		"if_status": "queued",
		"device_id": "member-dev",
	})
	assertStatus(t, resp, http.StatusOK)
	var run map[string]any
	decode(t, resp, &run)
	if run["claimed_by"] == nil || int64(run["claimed_by"].(float64)) != memberID {
		t.Fatalf("claimed_by=%v, want member id %d", run["claimed_by"], memberID)
	}
}

func TestAgentRunClaimedRunOnlyClaimerCanReport(t *testing.T) {
	ts := newTestServer(t)
	projID := seedBatchProject(t, "PAI", "PAI")
	_, runID := seedRunForIssue(t, ts, projID, 1)
	memberID := userID(t, "member")
	_, peerCookie := seedMemberUser(t, ts, "peer", "peerpass123")

	seedLiveImplementRunner(t, memberID, projID, "member-dev")
	resp := ts.patch(t, "/api/runs/"+itoa(runID), ts.memberCookie, map[string]any{
		"status":    "running",
		"if_status": "queued",
		"device_id": "member-dev",
	})
	assertStatus(t, resp, http.StatusOK)

	resp = ts.patch(t, "/api/runs/"+itoa(runID), peerCookie, map[string]any{
		"status": "failed",
		"error":  "wrong runner",
	})
	assertStatus(t, resp, http.StatusForbidden)

	resp = ts.patch(t, "/api/runs/"+itoa(runID), ts.memberCookie, map[string]any{
		"status": "failed",
		"error":  "legitimate failure",
	})
	assertStatus(t, resp, http.StatusOK)
	var run map[string]any
	decode(t, resp, &run)
	if run["status"] != "failed" || run["error"] != "legitimate failure" {
		t.Fatalf("run after claimer report = %+v, want failed with claimer error", run)
	}
	if run["claimed_by"] == nil || int64(run["claimed_by"].(float64)) != memberID {
		t.Fatalf("claimed_by=%v, want member id %d", run["claimed_by"], memberID)
	}
}

func TestImplementRecordsRequestedProviderAction(t *testing.T) {
	ts := newTestServer(t)
	projID := seedBatchProject(t, "PAI", "PAI")
	res, err := db.DB.Exec(
		`INSERT INTO issues(project_id, issue_number, type, title, status) VALUES(?,?,?,?,?)`,
		projID, 1, "ticket", "Run with Codex", "backlog")
	if err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	issueID, _ := res.LastInsertId()
	adminID := userID(t, "admin")
	seedLiveImplementRunnerAction(t, adminID, projID, "dev-codex", "codex_cli.implement", "Codex CLI")

	resp := ts.post(t, "/api/issues/"+itoa(issueID)+"/implement", ts.adminCookie, map[string]any{
		"device_id":  "dev-codex",
		"action_key": "codex_cli.implement",
	})
	assertStatus(t, resp, http.StatusCreated)
	var run map[string]any
	decode(t, resp, &run)
	if run["action_key"] != "codex_cli.implement" ||
		run["provider_kind"] != "local_cli" ||
		run["provider_id"] != "codex_cli" ||
		run["provider_label"] != "Codex CLI" ||
		run["run_mode"] != "edit" {
		t.Fatalf("provider fields = %+v, want Codex local CLI action", run)
	}
}

func TestImplementRejectsUnavailableExplicitActionDevice(t *testing.T) {
	ts := newTestServer(t)
	projID := seedBatchProject(t, "PAI", "PAI")
	res, err := db.DB.Exec(
		`INSERT INTO issues(project_id, issue_number, type, title, status) VALUES(?,?,?,?,?)`,
		projID, 1, "ticket", "Wrong runner", "backlog")
	if err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	issueID, _ := res.LastInsertId()
	adminID := userID(t, "admin")
	seedLiveImplementRunnerAction(t, adminID, projID, "dev-claude", "claude_cli.implement", "Claude Code")

	resp := ts.post(t, "/api/issues/"+itoa(issueID)+"/implement", ts.adminCookie, map[string]any{
		"device_id":  "dev-claude",
		"action_key": "codex_cli.implement",
	})
	assertStatus(t, resp, http.StatusConflict)
}

func TestAgentRunClaimRequiresMatchingAction(t *testing.T) {
	ts := newTestServer(t)
	projID := seedBatchProject(t, "PAI", "PAI")
	res, err := db.DB.Exec(
		`INSERT INTO issues(project_id, issue_number, type, title, status) VALUES(?,?,?,?,?)`,
		projID, 1, "ticket", "Claim with action", "backlog")
	if err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	issueID, _ := res.LastInsertId()
	resp := ts.post(t, "/api/issues/"+itoa(issueID)+"/implement", ts.adminCookie, map[string]any{
		"action_key": "codex_cli.implement",
	})
	assertStatus(t, resp, http.StatusCreated)
	var run map[string]any
	decode(t, resp, &run)
	runID := int64(run["id"].(float64))
	memberID := userID(t, "member")

	seedLiveImplementRunnerAction(t, memberID, projID, "member-dev", "claude_cli.implement", "Claude Code")
	resp = ts.patch(t, "/api/runs/"+itoa(runID), ts.memberCookie, map[string]any{
		"status":     "running",
		"if_status":  "queued",
		"device_id":  "member-dev",
		"action_key": "codex_cli.implement",
	})
	assertStatus(t, resp, http.StatusForbidden)

	seedLiveImplementRunnerAction(t, memberID, projID, "member-dev", "codex_cli.implement", "Codex CLI")
	resp = ts.patch(t, "/api/runs/"+itoa(runID), ts.memberCookie, map[string]any{
		"status":     "running",
		"if_status":  "queued",
		"device_id":  "member-dev",
		"action_key": "codex_cli.implement",
	})
	assertStatus(t, resp, http.StatusOK)
}

// TestImplementConcurrentCreatesOneRun proves the idempotency is atomic (the
// partial unique index, migration 127), not a racy SELECT-then-INSERT (audit F1).
func TestImplementConcurrentCreatesOneRun(t *testing.T) {
	ts := newTestServer(t)
	projID := seedBatchProject(t, "PAI", "PAI")
	res, err := db.DB.Exec(
		`INSERT INTO issues(project_id, issue_number, type, title, status) VALUES(?,?,?,?,?)`,
		projID, 1, "ticket", "Race", "backlog")
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	issueID, _ := res.LastInsertId()

	const N = 8
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, _ := http.NewRequest(http.MethodPost, ts.srv.URL+"/api/issues/"+itoa(issueID)+"/implement", strings.NewReader("{}"))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Cookie", ts.adminCookie)
			if resp, e := http.DefaultClient.Do(req); e == nil {
				resp.Body.Close()
			}
		}()
	}
	wg.Wait()

	var count int
	if err := db.DB.QueryRow(
		`SELECT COUNT(*) FROM agent_runs WHERE issue_id=? AND status IN ('queued','running')`,
		issueID).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("active runs = %d, want exactly 1 (idempotent under concurrency)", count)
	}
}

// TestAgentRunClaimConcurrent proves the queued→running claim is atomic under
// real concurrency: exactly one claimant wins (audit F2).
func TestAgentRunClaimConcurrent(t *testing.T) {
	ts := newTestServer(t)
	projID := seedBatchProject(t, "PAI", "PAI")
	_, runID := seedRunForIssue(t, ts, projID, 1)

	const N = 8
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins := 0
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, _ := http.NewRequest(http.MethodPatch, ts.srv.URL+"/api/runs/"+itoa(runID),
				strings.NewReader(`{"status":"running","if_status":"queued"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Cookie", ts.adminCookie)
			resp, e := http.DefaultClient.Do(req)
			if e != nil {
				return
			}
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("claim wins = %d, want exactly 1 (atomic claim)", wins)
	}
}

// TestAgentRunIllegalTransition rejects a status jump that skips the lifecycle.
func TestAgentRunIllegalTransition(t *testing.T) {
	ts := newTestServer(t)
	projID := seedBatchProject(t, "PAI", "PAI")
	_, runID := seedRunForIssue(t, ts, projID, 1)
	// queued → deployed is illegal (must pass through running).
	resp := ts.patch(t, "/api/runs/"+itoa(runID), ts.adminCookie, map[string]any{"status": "deployed", "version": "9.9.9"})
	assertStatus(t, resp, http.StatusConflict)
	// queued → running is legal.
	resp = ts.patch(t, "/api/runs/"+itoa(runID), ts.adminCookie, map[string]any{"status": "running"})
	assertStatus(t, resp, http.StatusOK)
}

// TestAgentRunTestsPassedStampsFinishedAtButCanDeploy pins the result-state
// semantics used by report-back-only runners: tests_passed is a completed
// result with a timestamp, but it remains non-terminal so a later deploy report
// can still move tests_passed -> deployed.
func TestAgentRunTestsPassedStampsFinishedAtButCanDeploy(t *testing.T) {
	ts := newTestServer(t)
	projID := seedBatchProject(t, "PAI", "PAI")
	_, runID := seedRunForIssue(t, ts, projID, 1)
	assertStatus(t, ts.patch(t, "/api/runs/"+itoa(runID), ts.adminCookie, map[string]any{"status": "running"}), http.StatusOK)

	resp := ts.patch(t, "/api/runs/"+itoa(runID), ts.adminCookie, map[string]any{"status": "tests_passed"})
	assertStatus(t, resp, http.StatusOK)
	var run map[string]any
	decode(t, resp, &run)
	if run["finished_at"] == nil {
		t.Fatalf("tests_passed should stamp finished_at")
	}

	resp = ts.patch(t, "/api/runs/"+itoa(runID), ts.adminCookie, map[string]any{
		"status":        "deployed",
		"version":       "4.6.4",
		"deploy_target": "local-dev",
	})
	assertStatus(t, resp, http.StatusOK)
	decode(t, resp, &run)
	if run["status"] != "deployed" || run["version"] != "4.6.4" {
		t.Fatalf("run after deploy = %+v, want deployed v4.6.4", run)
	}
	if run["finished_at"] == nil {
		t.Fatalf("deployed should keep/stamp finished_at")
	}
}

func TestIssueResponsesIncludeLatestAIWorkStatus(t *testing.T) {
	ts := newTestServer(t)
	projID := seedBatchProject(t, "PAI", "PAI")
	issueID, oldRunID := seedRunForIssue(t, ts, projID, 1)
	assertStatus(t, ts.patch(t, "/api/runs/"+itoa(oldRunID), ts.adminCookie, map[string]any{
		"status": "running",
	}), http.StatusOK)
	assertStatus(t, ts.patch(t, "/api/runs/"+itoa(oldRunID), ts.adminCookie, map[string]any{
		"status": "failed",
		"error":  "superseded",
	}), http.StatusOK)

	resp := ts.post(t, "/api/issues/"+itoa(issueID)+"/implement", ts.adminCookie,
		map[string]any{"device_id": "dev-latest", "deploy_target": "local-dev"})
	assertStatus(t, resp, http.StatusCreated)
	var created map[string]any
	decode(t, resp, &created)
	runID := int64(created["id"].(float64))
	req, _ := http.NewRequest(http.MethodPatch, ts.srv.URL+"/api/runs/"+itoa(runID),
		strings.NewReader(`{"status":"running","if_status":"queued","device_id":"dev-latest","tests_summary":"npm test passed"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", ts.adminCookie)
	req.Header.Set("X-Paimos-Agent-Name", "claude-pai625")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("claim run with attribution: %v", err)
	}
	assertStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	assertStatus(t, ts.patch(t, "/api/runs/"+itoa(runID), ts.adminCookie, map[string]any{
		"status":        "deployed",
		"version":       "4.6.4",
		"deploy_target": "local-dev",
	}), http.StatusOK)

	var single struct {
		AIWorkStatus *struct {
			ID           int64   `json:"id"`
			Status       string  `json:"status"`
			AgentName    string  `json:"agent_name"`
			DeviceID     string  `json:"device_id"`
			Version      string  `json:"version"`
			DeployTarget string  `json:"deploy_target"`
			TestsSummary *string `json:"tests_summary"`
			FinishedAt   *string `json:"finished_at"`
		} `json:"ai_work_status"`
	}
	resp = ts.get(t, "/api/issues/"+itoa(issueID), ts.adminCookie)
	assertStatus(t, resp, http.StatusOK)
	decode(t, resp, &single)
	if single.AIWorkStatus == nil {
		t.Fatalf("single issue missing ai_work_status")
	}
	if single.AIWorkStatus.ID != runID || single.AIWorkStatus.Status != "deployed" {
		t.Fatalf("single ai_work_status = %+v, want latest deployed run %d", single.AIWorkStatus, runID)
	}
	if single.AIWorkStatus.AgentName != "claude-pai625" || single.AIWorkStatus.DeviceID != "dev-latest" {
		t.Fatalf("single ai_work_status attribution = %+v", single.AIWorkStatus)
	}
	if single.AIWorkStatus.Version != "4.6.4" || single.AIWorkStatus.DeployTarget != "local-dev" {
		t.Fatalf("single ai_work_status report = %+v", single.AIWorkStatus)
	}
	if single.AIWorkStatus.TestsSummary == nil || *single.AIWorkStatus.TestsSummary != "npm test passed" {
		t.Fatalf("single tests summary = %v", single.AIWorkStatus.TestsSummary)
	}
	if single.AIWorkStatus.FinishedAt == nil {
		t.Fatalf("single ai_work_status should include finished_at")
	}

	var list struct {
		Issues []struct {
			ID           int64 `json:"id"`
			AIWorkStatus *struct {
				ID     int64  `json:"id"`
				Status string `json:"status"`
			} `json:"ai_work_status"`
		} `json:"issues"`
	}
	resp = ts.get(t, "/api/projects/"+itoa(projID)+"/issues?envelope=1&fields=list", ts.adminCookie)
	assertStatus(t, resp, http.StatusOK)
	decode(t, resp, &list)
	for _, issue := range list.Issues {
		if issue.ID != issueID {
			continue
		}
		if issue.AIWorkStatus == nil || issue.AIWorkStatus.ID != runID || issue.AIWorkStatus.Status != "deployed" {
			t.Fatalf("list ai_work_status = %+v, want latest deployed run %d", issue.AIWorkStatus, runID)
		}
		return
	}
	t.Fatalf("issue %d not returned in project issue list: %+v", issueID, list.Issues)
}

// TestAgentRunTerminalImmutable rejects any edit (even non-status) once terminal.
func TestAgentRunTerminalImmutable(t *testing.T) {
	ts := newTestServer(t)
	projID := seedBatchProject(t, "PAI", "PAI")
	_, runID := seedRunForIssue(t, ts, projID, 1)
	assertStatus(t, ts.patch(t, "/api/runs/"+itoa(runID), ts.adminCookie, map[string]any{"status": "running"}), http.StatusOK)
	assertStatus(t, ts.patch(t, "/api/runs/"+itoa(runID), ts.adminCookie, map[string]any{"status": "deployed"}), http.StatusOK)
	// A non-status edit on a terminal run must be refused.
	resp := ts.patch(t, "/api/runs/"+itoa(runID), ts.adminCookie, map[string]any{"error": "tampered"})
	assertStatus(t, resp, http.StatusConflict)
}

// TestAgentRunNonStatusIfStatusGuard proves non-status edits participate in
// the same compare-and-set guard as lifecycle transitions. This is the contract
// that keeps a stale non-status reporter from mutating a run after another
// request moved it to a different status.
func TestAgentRunNonStatusIfStatusGuard(t *testing.T) {
	ts := newTestServer(t)
	projID := seedBatchProject(t, "PAI", "PAI")
	_, runID := seedRunForIssue(t, ts, projID, 1)
	assertStatus(t, ts.patch(t, "/api/runs/"+itoa(runID), ts.adminCookie, map[string]any{"status": "running"}), http.StatusOK)

	resp := ts.patch(t, "/api/runs/"+itoa(runID), ts.adminCookie, map[string]any{
		"if_status": "queued",
		"error":     "stale reporter should not land",
	})
	assertStatus(t, resp, http.StatusConflict)

	var errText string
	if err := db.DB.QueryRow(`SELECT error FROM agent_runs WHERE id=?`, runID).Scan(&errText); err != nil {
		t.Fatalf("reload run error: %v", err)
	}
	if errText != "" {
		t.Fatalf("stale non-status update changed error to %q", errText)
	}

	resp = ts.patch(t, "/api/runs/"+itoa(runID), ts.adminCookie, map[string]any{
		"if_status": "bogus",
		"error":     "bad guard",
	})
	assertStatus(t, resp, http.StatusBadRequest)
}

func TestAgentRunClaimStampsActualDeviceID(t *testing.T) {
	ts := newTestServer(t)
	projID := seedBatchProject(t, "PAI", "PAI")
	_, runID := seedRunForIssue(t, ts, projID, 1)

	resp := ts.patch(t, "/api/runs/"+itoa(runID), ts.adminCookie, map[string]any{
		"status":    "running",
		"if_status": "queued",
		"device_id": "dev-1",
	})
	assertStatus(t, resp, http.StatusOK)
	var run map[string]any
	decode(t, resp, &run)
	if run["device_id"] != "dev-1" {
		t.Fatalf("device_id=%v, want dev-1", run["device_id"])
	}

	resp = ts.patch(t, "/api/runs/"+itoa(runID), ts.adminCookie, map[string]any{
		"device_id": "dev-2",
		"error":     "late retarget",
	})
	assertStatus(t, resp, http.StatusConflict)
}

func TestAgentRunClaimCannotRetargetSpecificDevice(t *testing.T) {
	ts := newTestServer(t)
	projID := seedBatchProject(t, "PAI", "PAI")
	_, runID := seedRunForIssue(t, ts, projID, 1)
	if _, err := db.DB.Exec(`UPDATE agent_runs SET device_id='dev-1' WHERE id=?`, runID); err != nil {
		t.Fatalf("target run: %v", err)
	}

	resp := ts.patch(t, "/api/runs/"+itoa(runID), ts.adminCookie, map[string]any{
		"status":    "running",
		"if_status": "queued",
		"device_id": "dev-2",
	})
	assertStatus(t, resp, http.StatusConflict)
}

// TestAgentRunLogAttachmentValidation rejects an attachment not on the run's issue.
func TestAgentRunLogAttachmentValidation(t *testing.T) {
	ts := newTestServer(t)
	projID := seedBatchProject(t, "PAI", "PAI")
	_, runID := seedRunForIssue(t, ts, projID, 1)
	resp := ts.patch(t, "/api/runs/"+itoa(runID), ts.adminCookie, map[string]any{"log_attachment_id": 999999})
	assertStatus(t, resp, http.StatusBadRequest)
}

// TestImplementReapsStaleRunning recovers a pipeline wedged by a crashed runner.
func TestImplementReapsStaleRunning(t *testing.T) {
	ts := newTestServer(t)
	projID := seedBatchProject(t, "PAI", "PAI")
	issueID, oldRunID := seedRunForIssue(t, ts, projID, 1)
	// Simulate a crashed runner: an old 'running' run that never finished.
	if _, err := db.DB.Exec(
		`UPDATE agent_runs SET status='running', started_at=datetime('now','-3 hours') WHERE id=?`, oldRunID); err != nil {
		t.Fatalf("wedge run: %v", err)
	}
	// A fresh implement reaps the stale run and queues a new one.
	resp := ts.post(t, "/api/issues/"+itoa(issueID)+"/implement", ts.adminCookie, map[string]any{})
	assertStatus(t, resp, http.StatusCreated)
	var staleStatus string
	if err := db.DB.QueryRow(`SELECT status FROM agent_runs WHERE id=?`, oldRunID).Scan(&staleStatus); err != nil {
		t.Fatalf("reload stale: %v", err)
	}
	if staleStatus != "failed" {
		t.Fatalf("stale run status = %q, want failed (reaped)", staleStatus)
	}
}

// TestImplementReapsRunningWithNullStartedAt covers the audit edge where a
// legacy/corrupt running row has no started_at. created_at is the fallback
// staleness clock, otherwise the active-run unique index wedges the issue.
func TestImplementReapsRunningWithNullStartedAt(t *testing.T) {
	ts := newTestServer(t)
	projID := seedBatchProject(t, "PAI", "PAI")
	issueID, oldRunID := seedRunForIssue(t, ts, projID, 1)
	if _, err := db.DB.Exec(
		`UPDATE agent_runs SET status='running', started_at=NULL, created_at=datetime('now','-3 hours') WHERE id=?`, oldRunID); err != nil {
		t.Fatalf("wedge null-started run: %v", err)
	}

	resp := ts.post(t, "/api/issues/"+itoa(issueID)+"/implement", ts.adminCookie, map[string]any{})
	assertStatus(t, resp, http.StatusCreated)
	var staleStatus string
	if err := db.DB.QueryRow(`SELECT status FROM agent_runs WHERE id=?`, oldRunID).Scan(&staleStatus); err != nil {
		t.Fatalf("reload stale: %v", err)
	}
	if staleStatus != "failed" {
		t.Fatalf("null-started stale run status = %q, want failed (reaped)", staleStatus)
	}
}

// TestAgentRunClaimGuard covers the atomic claim (if_status), terminal-status
// enforcement, and the catch-up listing (PAI-605 H3 / L1 / M1).
func TestAgentRunClaimGuard(t *testing.T) {
	ts := newTestServer(t)
	projID := seedBatchProject(t, "PAI", "PAI")
	res, err := db.DB.Exec(
		`INSERT INTO issues(project_id, issue_number, type, title, status) VALUES(?,?,?,?,?)`,
		projID, 1, "ticket", "Claim me", "backlog")
	if err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	issueID, _ := res.LastInsertId()
	resp := ts.post(t, "/api/issues/"+itoa(issueID)+"/implement", ts.adminCookie, map[string]any{})
	assertStatus(t, resp, http.StatusCreated)
	var run map[string]any
	decode(t, resp, &run)
	runID := int64(run["id"].(float64))

	var list struct {
		Runs []map[string]any `json:"runs"`
	}
	// Catch-up endpoint lists the queued run.
	resp = ts.get(t, "/api/projects/"+itoa(projID)+"/runs?status=queued", ts.adminCookie)
	assertStatus(t, resp, http.StatusOK)
	decode(t, resp, &list)
	if len(list.Runs) != 1 {
		t.Fatalf("queued runs = %d, want 1", len(list.Runs))
	}

	// First claim (if_status=queued) wins; a second loses → 409.
	resp = ts.patch(t, "/api/runs/"+itoa(runID), ts.adminCookie, map[string]any{"status": "running", "if_status": "queued"})
	assertStatus(t, resp, http.StatusOK)
	resp = ts.patch(t, "/api/runs/"+itoa(runID), ts.adminCookie, map[string]any{"status": "running", "if_status": "queued"})
	assertStatus(t, resp, http.StatusConflict)

	// Move to a terminal status; a transition out of it is rejected (L1).
	resp = ts.patch(t, "/api/runs/"+itoa(runID), ts.adminCookie, map[string]any{"status": "deployed"})
	assertStatus(t, resp, http.StatusOK)
	resp = ts.patch(t, "/api/runs/"+itoa(runID), ts.adminCookie, map[string]any{"status": "running"})
	assertStatus(t, resp, http.StatusConflict)

	// The queued list is now empty.
	resp = ts.get(t, "/api/projects/"+itoa(projID)+"/runs?status=queued", ts.adminCookie)
	assertStatus(t, resp, http.StatusOK)
	decode(t, resp, &list)
	if len(list.Runs) != 0 {
		t.Errorf("queued runs after claim = %d, want 0", len(list.Runs))
	}
}

func commentCount(t *testing.T, issueID int64) int {
	t.Helper()
	var n int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM comments WHERE issue_id=?`, issueID).Scan(&n); err != nil {
		t.Fatalf("count comments: %v", err)
	}
	return n
}

func firstComment(t *testing.T, issueID int64) (string, int) {
	t.Helper()
	var body string
	_ = db.DB.QueryRow(`SELECT body FROM comments WHERE issue_id=? ORDER BY id LIMIT 1`, issueID).Scan(&body)
	return body, commentCount(t, issueID)
}
