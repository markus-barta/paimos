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

	"github.com/markus-barta/paimos/backend/db"
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

	// PAI-605 M5: a project editor (the member) CAN manage the run even though
	// they're neither admin nor the requester — this is what lets a developer's
	// runner report back when someone else clicked "Implement this". (The run is
	// terminal here, so a write is a 409, not a 403 — the point is the member got
	// past the access gate; reads succeed.)
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
