// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public
// License along with this program. If not, see <https://www.gnu.org/licenses/>.

package handlers_test

// PAI-324 — agent + session attribution on issue_history snapshot rows.
//
// Coverage:
//   • issue create + update with X-Paimos-Agent-Name + X-Paimos-Session-Id
//     headers persists both onto the relevant snapshot row.
//   • Same write paths without the headers succeed and persist NULL —
//     no 400, no 500, backwards-compatible with pre-PAI-324 callers.
//   • GET /api/issues/{id}/history surfaces both fields per row.
//   • comment-add and tag-add succeed regardless of header presence
//     (they don't currently mint issue_history rows; the test guards
//     the no-error-on-headers contract that the ticket calls out).

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// requestWithHeaders sends a JSON request with extra headers — the
// stock testServer.post / put / patch helpers don't accept arbitrary
// headers, so PAI-324 carries its own.
func (ts *testServer) requestWithHeaders(t *testing.T, method, path, cookie string, body any, headers map[string]string) *http.Response {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	}
	req, _ := http.NewRequestWithContext(context.Background(), method, ts.srv.URL+path, rdr)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp
}

// historyEntry mirrors handlers.HistoryEntry for tests — a private
// alias keeps the test honest about the JSON contract.
type historyEntry struct {
	ID            int64   `json:"id"`
	IssueID       int64   `json:"issue_id"`
	ChangedBy     *int64  `json:"changed_by"`
	ChangedByName string  `json:"changed_by_name"`
	Snapshot      any     `json:"snapshot"`
	ChangedAt     string  `json:"changed_at"`
	AgentName     *string `json:"agent_name"`
	SessionID     *string `json:"session_id"`
}

// fetchHistory returns the (decoded) history rows for an issue, in
// chronological order — the same order the handler emits.
func fetchHistory(t *testing.T, ts *testServer, issueID int64) []historyEntry {
	t.Helper()
	resp := ts.get(t, fmt.Sprintf("/api/issues/%d/history", issueID), ts.adminCookie)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("history GET: status=%d body=%s", resp.StatusCode, body)
	}
	var entries []historyEntry
	decode(t, resp, &entries)
	return entries
}

// seedAttrProject creates a project for the PAI-324 tests.
func seedAttrProject(t *testing.T, ts *testServer, name, key string) int64 {
	t.Helper()
	resp := ts.post(t, "/api/projects", ts.adminCookie, map[string]string{
		"name": name, "key": key,
	})
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("create project %s/%s: status=%d body=%s", name, key, resp.StatusCode, body)
	}
	return responseID(t, resp)
}

// TestHistoryAttribution_IssueUpdate_WithHeaders is the load-bearing
// path: PUT /api/issues/{id} with both headers persists them onto the
// new snapshot row, and the GET surfaces them.
func TestHistoryAttribution_IssueUpdate_WithHeaders(t *testing.T) {
	ts := newTestServer(t)
	projectID := seedAttrProject(t, ts, "Attribution Project", "ATR")

	// Create an issue first (no headers — the create snapshot stays NULL).
	createResp := ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.adminCookie,
		map[string]any{"title": "T1", "type": "ticket", "status": "backlog", "priority": "medium"})
	assertStatus(t, createResp, http.StatusCreated)
	issueID := responseID(t, createResp)

	// Update with both headers.
	updateResp := ts.requestWithHeaders(t, http.MethodPut, fmt.Sprintf("/api/issues/%d", issueID), ts.adminCookie,
		map[string]any{"status": "in-progress"},
		map[string]string{
			"X-Paimos-Agent-Name": "ops",
			"X-Paimos-Session-Id": "1f6046a7-aaaa-bbbb-cccc-1234567890ab",
		})
	assertStatus(t, updateResp, http.StatusOK)

	entries := fetchHistory(t, ts, issueID)
	if len(entries) < 2 {
		t.Fatalf("expected at least 2 history rows (create + update), got %d", len(entries))
	}
	last := entries[len(entries)-1]

	if last.AgentName == nil || *last.AgentName != "ops" {
		t.Errorf("last.AgentName = %v, want \"ops\"", last.AgentName)
	}
	if last.SessionID == nil || *last.SessionID != "1f6046a7-aaaa-bbbb-cccc-1234567890ab" {
		t.Errorf("last.SessionID = %v, want canonical UUID", last.SessionID)
	}

	// First (create) row must be NULL — backwards-compat: the create
	// happened without headers.
	first := entries[0]
	if first.AgentName != nil {
		t.Errorf("first.AgentName = %v, want nil (created without header)", *first.AgentName)
	}
	if first.SessionID != nil {
		t.Errorf("first.SessionID = %v, want nil (created without header)", *first.SessionID)
	}
}

// TestHistoryAttribution_IssueUpdate_WithoutHeaders is the
// backwards-compat path: PUT without headers succeeds and stores NULL.
// The acceptance criteria lean hard on this — agent-blind callers
// (web UI clicks today) must keep working without ceremony.
func TestHistoryAttribution_IssueUpdate_WithoutHeaders(t *testing.T) {
	ts := newTestServer(t)
	projectID := seedAttrProject(t, ts, "Attribution Project 2", "AT2")

	createResp := ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.adminCookie,
		map[string]any{"title": "T2", "type": "ticket", "status": "backlog", "priority": "medium"})
	assertStatus(t, createResp, http.StatusCreated)
	issueID := responseID(t, createResp)

	updateResp := ts.put(t, fmt.Sprintf("/api/issues/%d", issueID), ts.adminCookie,
		map[string]any{"status": "in-progress"})
	assertStatus(t, updateResp, http.StatusOK)

	entries := fetchHistory(t, ts, issueID)
	for i, e := range entries {
		if e.AgentName != nil {
			t.Errorf("entries[%d].AgentName = %v, want nil", i, *e.AgentName)
		}
		if e.SessionID != nil {
			t.Errorf("entries[%d].SessionID = %v, want nil", i, *e.SessionID)
		}
	}
}

// TestHistoryAttribution_IssueCreate_WithHeaders covers the create
// path — POST /api/projects/{id}/issues with both headers persists
// them onto the create snapshot.
func TestHistoryAttribution_IssueCreate_WithHeaders(t *testing.T) {
	ts := newTestServer(t)
	projectID := seedAttrProject(t, ts, "Attribution Create", "ATC")

	createResp := ts.requestWithHeaders(t, http.MethodPost, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.adminCookie,
		map[string]any{"title": "TC", "type": "ticket", "status": "backlog", "priority": "medium"},
		map[string]string{
			"X-Paimos-Agent-Name": "dev",
			"X-Paimos-Session-Id": "9e1c2cbe-3333-4444-5555-deadbeef0001",
		})
	assertStatus(t, createResp, http.StatusCreated)
	issueID := responseID(t, createResp)

	entries := fetchHistory(t, ts, issueID)
	if len(entries) == 0 {
		t.Fatalf("no history rows after create")
	}
	createRow := entries[0]
	if createRow.AgentName == nil || *createRow.AgentName != "dev" {
		t.Errorf("createRow.AgentName = %v, want \"dev\"", createRow.AgentName)
	}
	if createRow.SessionID == nil || *createRow.SessionID != "9e1c2cbe-3333-4444-5555-deadbeef0001" {
		t.Errorf("createRow.SessionID = %v, want canonical UUID", createRow.SessionID)
	}
}

// TestHistoryAttribution_LongHeader_Truncation guards the 64-char cap
// — defensive against accidental log-spam payloads. SQLite ALTER TABLE
// can't add CHECK retroactively so the truncation lives in the
// handler.
func TestHistoryAttribution_LongHeader_Truncation(t *testing.T) {
	ts := newTestServer(t)
	projectID := seedAttrProject(t, ts, "Attribution Long", "ATL")

	createResp := ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.adminCookie,
		map[string]any{"title": "TL", "type": "ticket", "status": "backlog", "priority": "medium"})
	assertStatus(t, createResp, http.StatusCreated)
	issueID := responseID(t, createResp)

	longAgent := strings.Repeat("a", 200)
	longSession := strings.Repeat("s", 200)

	updateResp := ts.requestWithHeaders(t, http.MethodPut, fmt.Sprintf("/api/issues/%d", issueID), ts.adminCookie,
		map[string]any{"status": "in-progress"},
		map[string]string{
			"X-Paimos-Agent-Name": longAgent,
			"X-Paimos-Session-Id": longSession,
		})
	assertStatus(t, updateResp, http.StatusOK)

	entries := fetchHistory(t, ts, issueID)
	last := entries[len(entries)-1]
	if last.AgentName == nil || len(*last.AgentName) != 64 {
		t.Errorf("AgentName not capped at 64: got %v", last.AgentName)
	}
	if last.SessionID == nil || len(*last.SessionID) != 64 {
		t.Errorf("SessionID not capped at 64: got %v", last.SessionID)
	}
}

// TestHistoryAttribution_EmptyHeaders_StoreNull guards the
// empty-string-vs-NULL contract: an empty/whitespace header value
// behaves the same as the header being absent — column persists as
// SQL NULL, not "".
func TestHistoryAttribution_EmptyHeaders_StoreNull(t *testing.T) {
	ts := newTestServer(t)
	projectID := seedAttrProject(t, ts, "Attribution Empty", "ATE")

	createResp := ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.adminCookie,
		map[string]any{"title": "TE", "type": "ticket", "status": "backlog", "priority": "medium"})
	assertStatus(t, createResp, http.StatusCreated)
	issueID := responseID(t, createResp)

	updateResp := ts.requestWithHeaders(t, http.MethodPut, fmt.Sprintf("/api/issues/%d", issueID), ts.adminCookie,
		map[string]any{"status": "in-progress"},
		map[string]string{
			"X-Paimos-Agent-Name": "   ",
			"X-Paimos-Session-Id": "",
		})
	assertStatus(t, updateResp, http.StatusOK)

	entries := fetchHistory(t, ts, issueID)
	last := entries[len(entries)-1]
	if last.AgentName != nil {
		t.Errorf("AgentName = %v, want nil for whitespace-only header", *last.AgentName)
	}
	if last.SessionID != nil {
		t.Errorf("SessionID = %v, want nil for empty header", *last.SessionID)
	}
}

// TestHistoryAttribution_CommentAdd_HeadersTolerated guards the
// no-error contract on POST /api/issues/{id}/comments: headers may
// be present or absent — the request must not 400 / 500. Comments
// don't mint issue_history rows today, so we only assert the request
// succeeds; the snapshot question is moot.
func TestHistoryAttribution_CommentAdd_HeadersTolerated(t *testing.T) {
	ts := newTestServer(t)
	projectID := seedAttrProject(t, ts, "Attribution Comment", "ATM")

	createResp := ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.adminCookie,
		map[string]any{"title": "TM", "type": "ticket", "status": "backlog", "priority": "medium"})
	assertStatus(t, createResp, http.StatusCreated)
	issueID := responseID(t, createResp)

	t.Run("with headers", func(t *testing.T) {
		resp := ts.requestWithHeaders(t, http.MethodPost, fmt.Sprintf("/api/issues/%d/comments", issueID), ts.adminCookie,
			map[string]any{"body": "from agent ops"},
			map[string]string{
				"X-Paimos-Agent-Name": "ops",
				"X-Paimos-Session-Id": "5b3a2c1e-aaaa-bbbb-cccc-deadbeef0002",
			})
		assertStatus(t, resp, http.StatusCreated)
	})

	t.Run("without headers", func(t *testing.T) {
		resp := ts.post(t, fmt.Sprintf("/api/issues/%d/comments", issueID), ts.adminCookie,
			map[string]any{"body": "from web ui"})
		assertStatus(t, resp, http.StatusCreated)
	})
}

// TestHistoryAttribution_TagAdd_HeadersTolerated mirrors the comment
// case for POST /api/issues/{id}/tags — headers tolerated regardless
// of presence, no snapshot row produced.
func TestHistoryAttribution_TagAdd_HeadersTolerated(t *testing.T) {
	ts := newTestServer(t)
	projectID := seedAttrProject(t, ts, "Attribution Tag", "ATG")

	createResp := ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.adminCookie,
		map[string]any{"title": "TG", "type": "ticket", "status": "backlog", "priority": "medium"})
	assertStatus(t, createResp, http.StatusCreated)
	issueID := responseID(t, createResp)

	tagID := firstTagID(t)

	t.Run("with headers", func(t *testing.T) {
		resp := ts.requestWithHeaders(t, http.MethodPost, fmt.Sprintf("/api/issues/%d/tags", issueID), ts.adminCookie,
			map[string]any{"tag_id": tagID},
			map[string]string{
				"X-Paimos-Agent-Name": "tooling",
				"X-Paimos-Session-Id": "6c4b3d2f-aaaa-bbbb-cccc-deadbeef0003",
			})
		assertStatus(t, resp, http.StatusNoContent)
	})

	t.Run("without headers", func(t *testing.T) {
		// Re-add via DELETE+POST since the first POST was idempotent
		// (INSERT OR IGNORE). Tag-add with a fresh tag covers the
		// without-headers happy path.
		// Using a second project + issue + tag avoids cross-test coupling.
		project2 := seedAttrProject(t, ts, "Attribution Tag 2", "ATX")
		c2 := ts.post(t, fmt.Sprintf("/api/projects/%d/issues", project2), ts.adminCookie,
			map[string]any{"title": "TX", "type": "ticket", "status": "backlog", "priority": "medium"})
		assertStatus(t, c2, http.StatusCreated)
		issue2 := responseID(t, c2)
		resp := ts.post(t, fmt.Sprintf("/api/issues/%d/tags", issue2), ts.adminCookie,
			map[string]any{"tag_id": tagID})
		assertStatus(t, resp, http.StatusNoContent)
	})
}
