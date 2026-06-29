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

	// A non-requester, non-admin member cannot read or write the run.
	resp = ts.patch(t, "/api/runs/"+itoa(runID), ts.memberCookie, map[string]any{"status": "running"})
	assertStatus(t, resp, http.StatusForbidden)
	resp = ts.get(t, "/api/runs/"+itoa(runID), ts.memberCookie)
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
