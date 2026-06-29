// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package handlers_test

import (
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/markus-barta/paimos/backend/db"
	"github.com/markus-barta/paimos/backend/sse"
)

// TestProjectRunnersAndImplementPublish covers PAI-607: the runner registry
// (online + implement-capable only) and the implement_requested SSE publish
// fired by POST /implement. The handler and the test share sse.GlobalBroker().
func TestProjectRunnersAndImplementPublish(t *testing.T) {
	ts := newTestServer(t)
	projID := seedBatchProject(t, "PAI", "PAI")
	res, err := db.DB.Exec(
		`INSERT INTO issues(project_id, issue_number, type, title, status) VALUES(?,?,?,?,?)`,
		projID, 1, "ticket", "Build me", "backlog")
	if err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	issueID, _ := res.LastInsertId()

	var adminID int64
	if err := db.DB.QueryRow(`SELECT id FROM users WHERE username='admin'`).Scan(&adminID); err != nil {
		t.Fatalf("admin id: %v", err)
	}

	insertWatch := func(device string, canImplement int) {
		_, err := db.DB.Exec(
			`INSERT INTO auto_watch_subscriptions(user_id, device_id, project_id, enabled, can_implement, created_at, updated_at)
			 VALUES(?,?,?,1,?,datetime('now'),datetime('now'))`,
			adminID, device, projID, canImplement)
		if err != nil {
			t.Fatalf("seed watch %s: %v", device, err)
		}
	}

	// A live, implement-capable runner... (capability is per-connection now)
	runner := sse.GlobalBroker().Subscribe(adminID, "runner-1", projID, true)
	defer sse.GlobalBroker().Close(runner)
	insertWatch("runner-1", 1)
	// ...and a live browser tab that is NOT a runner.
	browser := sse.GlobalBroker().Subscribe(adminID, "browser-1", projID, false)
	defer sse.GlobalBroker().Close(browser)
	insertWatch("browser-1", 0)

	// Registry returns only the implement-capable, currently-online device.
	resp := ts.get(t, "/api/projects/"+itoa(projID)+"/runners", ts.adminCookie)
	assertStatus(t, resp, http.StatusOK)
	var reg struct {
		Runners []map[string]any `json:"runners"`
	}
	decode(t, resp, &reg)
	if len(reg.Runners) != 1 || reg.Runners[0]["device_id"] != "runner-1" {
		t.Fatalf("runners=%+v, want only runner-1", reg.Runners)
	}

	// POST /implement creates the run AND publishes implement_requested.
	resp = ts.post(t, "/api/issues/"+itoa(issueID)+"/implement", ts.adminCookie,
		map[string]any{"device_id": "runner-1"})
	assertStatus(t, resp, http.StatusCreated)
	var run map[string]any
	decode(t, resp, &run)
	runID := int64(run["id"].(float64))

	select {
	case ev := <-runner.Events():
		if ev.Type != "implement_requested" {
			t.Errorf("event type=%q, want implement_requested", ev.Type)
		}
		if ev.Rev != strconv.FormatInt(runID, 10) {
			t.Errorf("event rev=%q, want run id %d", ev.Rev, runID)
		}
		if ev.Name != "PAI-1" {
			t.Errorf("event name=%q, want the issue key PAI-1", ev.Name)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runner did not receive implement_requested")
	}
}
