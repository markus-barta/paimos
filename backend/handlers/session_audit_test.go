// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package handlers_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"testing"

	"github.com/markus-barta/paimos/backend/db"
	"github.com/markus-barta/paimos/backend/handlers"
)

// countActivity reports how many session_activity rows exist for the
// given session_id (empty matches NULL).
func countActivity(t *testing.T, sessionID string) int {
	t.Helper()
	var n int
	var err error
	if sessionID == "" {
		err = db.DB.QueryRow(`SELECT COUNT(*) FROM session_activity WHERE session_id IS NULL`).Scan(&n)
	} else {
		err = db.DB.QueryRow(`SELECT COUNT(*) FROM session_activity WHERE session_id=?`, sessionID).Scan(&n)
	}
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

// TestSessionAudit_OffWhenDisabled — PAI-116 flipped the default to ON.
// The opt-OUT path is the one that needs verifying now: setting the env
// var to "false" or "0" must keep the audit table empty.
func TestSessionAudit_OffWhenDisabled(t *testing.T) {
	t.Setenv("PAIMOS_AUDIT_SESSIONS", "false")
	ts := newTestServer(t)
	resp := ts.post(t, "/api/tags", ts.adminCookie, map[string]string{"name": "x"})
	_ = resp

	var n int
	_ = db.DB.QueryRow(`SELECT COUNT(*) FROM session_activity`).Scan(&n)
	if n != 0 {
		t.Errorf("audit rows written despite PAIMOS_AUDIT_SESSIONS=false: %d", n)
	}
}

// TestSessionAudit_OnByDefault — PAI-116. NIS2 readiness target wants
// a complete audit trail by default; operators can opt out, but they
// shouldn't have to opt in.
func TestSessionAudit_OnByDefault(t *testing.T) {
	ts := newTestServer(t)
	resp := ts.post(t, "/api/tags", ts.adminCookie, map[string]string{"name": "default-on"})
	_ = resp

	var n int
	_ = db.DB.QueryRow(`SELECT COUNT(*) FROM session_activity`).Scan(&n)
	if n == 0 {
		t.Errorf("expected audit rows by default; got %d", n)
	}
}

func TestSessionAudit_OnEnabled(t *testing.T) {
	t.Setenv("PAIMOS_AUDIT_SESSIONS", "true")
	ts := newTestServer(t)

	// POST (mutation) with session header → should audit.
	session := "01HXYZ-testsession-001"
	body := []byte(`{"name":"audited-tag"}`)
	req, _ := http.NewRequest("POST", ts.srv.URL+"/api/tags", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", ts.adminCookie)
	req.Header.Set(handlers.SessionHeader, session)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /tags: %v", err)
	}
	resp.Body.Close()

	if got := countActivity(t, session); got != 1 {
		t.Errorf("expected 1 audit row for session %q, got %d", session, got)
	}

	// GET (not a mutation) → must NOT audit.
	ts.get(t, "/api/tags", ts.adminCookie)
	if got := countActivity(t, session); got != 1 {
		t.Errorf("GET bumped audit count to %d — audits must only track mutations", got)
	}
}

func TestSessionAudit_NoHeader_WritesNullSession(t *testing.T) {
	t.Setenv("PAIMOS_AUDIT_SESSIONS", "true")
	ts := newTestServer(t)

	// POST without the session header → should still audit, session_id=NULL.
	resp := ts.post(t, "/api/tags", ts.adminCookie, map[string]string{"name": "headerless"})
	resp.Body.Close()

	if got := countActivity(t, ""); got < 1 {
		t.Errorf("expected at least one NULL-session audit row, got %d", got)
	}
}

func TestSessionActivityEndpoint_KeysetPagination(t *testing.T) {
	t.Setenv("PAIMOS_AUDIT_SESSIONS", "true")
	ts := newTestServer(t)

	session := "paginate-me"
	// Produce 5 mutations tagged with the same session.
	for i := 0; i < 5; i++ {
		req, _ := http.NewRequest("POST", ts.srv.URL+"/api/tags", bytes.NewReader([]byte(`{"name":"pg`+strconv.Itoa(i)+`"}`)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Cookie", ts.adminCookie)
		req.Header.Set(handlers.SessionHeader, session)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("POST /tags: %v", err)
		}
		resp.Body.Close()
	}

	// Page 1 (limit=3).
	resp := ts.get(t, "/api/sessions/"+session+"/activity?limit=3", ts.adminCookie)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d, body=%s", resp.StatusCode, b)
	}
	var page struct {
		Activity   []map[string]any `json:"activity"`
		NextCursor any              `json:"next_cursor"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&page)
	if len(page.Activity) != 3 {
		t.Errorf("page 1: len=%d, want 3", len(page.Activity))
	}
	if page.NextCursor == nil {
		t.Error("page 1: next_cursor missing — should point past item #3")
	}
}
