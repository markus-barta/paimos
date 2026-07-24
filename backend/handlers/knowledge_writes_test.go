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

// PAI-353 — knowledge convenience-endpoint writes route through the
// canonical issue handler core, so they inherit the cross-cuts
// PAI-338's direct-SQL paths skipped:
//
//   • issue_history snapshot rows (PAI-324 attribution columns).
//   • mutation_log rows (PAI-209 undo / redo).
//
// The acceptance check in the ticket is concrete: PUT a memory entry
// with X-Paimos-Agent-Name + X-Paimos-Session-Id, then verify both a
// history row carrying the attribution and a mutation_log entry got
// written. CREATE + DELETE follow the same template so the consistency
// promise holds across the verb triplet.

import (
	"net/http"
	"testing"

	"github.com/inspr-at/paimos/backend/db"
)

// fetchKnowledgeIssueID resolves the underlying issues.id for a
// (project_id, type, slug) tuple — the convenience endpoint payload
// doesn't expose deleted_at, but tests need to query history /
// mutation_log by id.
func fetchKnowledgeIssueID(t *testing.T, projectID int64, typ, slug string) int64 {
	t.Helper()
	var id int64
	if err := db.DB.QueryRow(
		`SELECT id FROM issues WHERE project_id=? AND type=? AND slug=?`,
		projectID, typ, slug,
	).Scan(&id); err != nil {
		t.Fatalf("fetchKnowledgeIssueID: project=%d type=%s slug=%s: %v", projectID, typ, slug, err)
	}
	return id
}

// countMutationLogRows returns the number of mutation_log rows whose
// subject_id matches issueID — the cross-cut signal for "undo
// machinery saw this write".
func countMutationLogRows(t *testing.T, issueID int64) int {
	t.Helper()
	var n int
	if err := db.DB.QueryRow(
		`SELECT COUNT(*) FROM mutation_log WHERE subject_type='issue' AND subject_id=?`,
		issueID,
	).Scan(&n); err != nil {
		t.Fatalf("countMutationLogRows: %v", err)
	}
	return n
}

// TestKnowledgeWrites_UpdateInheritsHistoryAndMutationLog is the
// load-bearing PAI-353 acceptance test. PUT /api/projects/:id/memory/:slug
// with both attribution headers must:
//   1. Return 200 with the updated payload.
//   2. Mint an issue_history row carrying agent_name + session_id.
//   3. Mint a mutation_log row (mutation_type='issue.update').
func TestKnowledgeWrites_UpdateInheritsHistoryAndMutationLog(t *testing.T) {
	ts := newTestServer(t)
	projectID := createTestProject(t, ts, "PAI-353 Update", "P53U")

	// Seed a memory entry — no headers, so the create snapshot stays
	// NULL (matches the existing PAI-324 backwards-compat contract).
	createResp := ts.post(t, knowledgeURL(projectID, "memory"), ts.adminCookie,
		map[string]any{
			"slug":     "feedback_thread",
			"title":    "Initial title",
			"body":     "Initial body.",
			"metadata": map[string]any{"category": "feedback"},
		})
	assertStatus(t, createResp, http.StatusCreated)

	issueID := fetchKnowledgeIssueID(t, projectID, "memory", "feedback_thread")

	// Sanity: the create itself wrote a history + mutation_log row
	// (PAI-353 also covers the create cross-cut).
	beforeUpdateMutations := countMutationLogRows(t, issueID)
	if beforeUpdateMutations < 1 {
		t.Fatalf("expected ≥1 mutation_log row after create, got %d", beforeUpdateMutations)
	}

	// Update WITH attribution headers — the canonical PAI-353 path.
	const wantAgent = "ops"
	const wantSession = "1f6046a7-aaaa-bbbb-cccc-1234567890ab"
	updResp := ts.requestWithHeaders(t, http.MethodPut,
		knowledgeEntryURL(projectID, "memory", "feedback_thread"),
		ts.adminCookie,
		map[string]any{
			"slug":     "feedback_thread",
			"title":    "Updated title",
			"body":     "Updated body.",
			"metadata": map[string]any{"category": "feedback"},
		},
		map[string]string{
			"X-Paimos-Agent-Name": wantAgent,
			"X-Paimos-Session-Id": wantSession,
		},
	)
	assertStatus(t, updResp, http.StatusOK)

	// (1) issue_history surfaces attribution on the latest row.
	entries := fetchHistory(t, ts, issueID)
	if len(entries) < 2 {
		t.Fatalf("expected ≥2 history rows (create + update), got %d", len(entries))
	}
	last := entries[len(entries)-1]
	if last.AgentName == nil || *last.AgentName != wantAgent {
		t.Errorf("last.AgentName = %v, want %q", last.AgentName, wantAgent)
	}
	if last.SessionID == nil || *last.SessionID != wantSession {
		t.Errorf("last.SessionID = %v, want %q", last.SessionID, wantSession)
	}

	// (2) mutation_log got a new row from the update.
	afterUpdateMutations := countMutationLogRows(t, issueID)
	if afterUpdateMutations <= beforeUpdateMutations {
		t.Errorf("mutation_log row count: before=%d, after=%d — expected new row", beforeUpdateMutations, afterUpdateMutations)
	}

	// (3) The update mutation row carries the right type + session.
	var mutationType, sessionID string
	if err := db.DB.QueryRow(
		`SELECT mutation_type, COALESCE(session_id, '') FROM mutation_log
		 WHERE subject_type='issue' AND subject_id=?
		 ORDER BY id DESC LIMIT 1`,
		issueID,
	).Scan(&mutationType, &sessionID); err != nil {
		t.Fatalf("read latest mutation_log row: %v", err)
	}
	if mutationType != "issue.update" {
		t.Errorf("latest mutation_type = %q, want %q", mutationType, "issue.update")
	}
	if sessionID != wantSession {
		t.Errorf("latest session_id = %q, want %q", sessionID, wantSession)
	}
}

// TestKnowledgeWrites_CreateInheritsHistoryAndMutationLog asserts the
// create cross-cut: POST /api/projects/:id/memory with attribution
// headers writes an issue_history row carrying both, plus a
// mutation_log row of type "issue.create".
func TestKnowledgeWrites_CreateInheritsHistoryAndMutationLog(t *testing.T) {
	ts := newTestServer(t)
	projectID := createTestProject(t, ts, "PAI-353 Create", "P53C")

	const wantAgent = "dev"
	const wantSession = "9e1c2cbe-3333-4444-5555-deadbeef0001"
	createResp := ts.requestWithHeaders(t, http.MethodPost,
		knowledgeURL(projectID, "memory"),
		ts.adminCookie,
		map[string]any{
			"slug":  "deploy_facts",
			"title": "Deploy facts",
			"body":  "Notes on the deploy flow.",
		},
		map[string]string{
			"X-Paimos-Agent-Name": wantAgent,
			"X-Paimos-Session-Id": wantSession,
		},
	)
	assertStatus(t, createResp, http.StatusCreated)

	issueID := fetchKnowledgeIssueID(t, projectID, "memory", "deploy_facts")

	// (1) issue_history row carries attribution on the create snapshot.
	entries := fetchHistory(t, ts, issueID)
	if len(entries) == 0 {
		t.Fatalf("expected ≥1 history row after create, got 0")
	}
	createRow := entries[0]
	if createRow.AgentName == nil || *createRow.AgentName != wantAgent {
		t.Errorf("createRow.AgentName = %v, want %q", createRow.AgentName, wantAgent)
	}
	if createRow.SessionID == nil || *createRow.SessionID != wantSession {
		t.Errorf("createRow.SessionID = %v, want %q", createRow.SessionID, wantSession)
	}

	// (2) mutation_log carries an issue.create row.
	var mutationType, sessionID string
	if err := db.DB.QueryRow(
		`SELECT mutation_type, COALESCE(session_id, '') FROM mutation_log
		 WHERE subject_type='issue' AND subject_id=?
		 ORDER BY id ASC LIMIT 1`,
		issueID,
	).Scan(&mutationType, &sessionID); err != nil {
		t.Fatalf("read first mutation_log row: %v", err)
	}
	if mutationType != "issue.create" {
		t.Errorf("first mutation_type = %q, want %q", mutationType, "issue.create")
	}
	if sessionID != wantSession {
		t.Errorf("first session_id = %q, want %q", sessionID, wantSession)
	}
}

// TestKnowledgeWrites_DeleteInheritsHistoryAndMutationLog asserts the
// soft-delete cross-cut: DELETE /api/projects/:id/memory/:slug with
// attribution headers writes a snapshot row + a mutation_log entry of
// type "issue.delete".
func TestKnowledgeWrites_DeleteInheritsHistoryAndMutationLog(t *testing.T) {
	ts := newTestServer(t)
	projectID := createTestProject(t, ts, "PAI-353 Delete", "P53D")

	createResp := ts.post(t, knowledgeURL(projectID, "memory"), ts.adminCookie,
		map[string]any{"slug": "doomed", "title": "Doomed entry"})
	assertStatus(t, createResp, http.StatusCreated)
	issueID := fetchKnowledgeIssueID(t, projectID, "memory", "doomed")

	beforeMutations := countMutationLogRows(t, issueID)

	const wantAgent = "ops"
	const wantSession = "abcd1234-aaaa-bbbb-cccc-deadbeefdead"
	delResp := ts.requestWithHeaders(t, http.MethodDelete,
		knowledgeEntryURL(projectID, "memory", "doomed"),
		ts.adminCookie,
		nil,
		map[string]string{
			"X-Paimos-Agent-Name": wantAgent,
			"X-Paimos-Session-Id": wantSession,
		},
	)
	assertStatus(t, delResp, http.StatusNoContent)

	// (1) history row gets attribution (the post-delete snapshot).
	entries := fetchHistory(t, ts, issueID)
	if len(entries) == 0 {
		t.Fatalf("expected ≥1 history row, got 0")
	}
	last := entries[len(entries)-1]
	if last.AgentName == nil || *last.AgentName != wantAgent {
		t.Errorf("last.AgentName = %v, want %q", last.AgentName, wantAgent)
	}
	if last.SessionID == nil || *last.SessionID != wantSession {
		t.Errorf("last.SessionID = %v, want %q", last.SessionID, wantSession)
	}

	// (2) mutation_log gained a new row.
	afterMutations := countMutationLogRows(t, issueID)
	if afterMutations <= beforeMutations {
		t.Errorf("mutation_log: before=%d, after=%d — expected new row from delete", beforeMutations, afterMutations)
	}

	// (3) Latest mutation_log row is the delete.
	var mutationType string
	if err := db.DB.QueryRow(
		`SELECT mutation_type FROM mutation_log
		 WHERE subject_type='issue' AND subject_id=?
		 ORDER BY id DESC LIMIT 1`,
		issueID,
	).Scan(&mutationType); err != nil {
		t.Fatalf("read latest mutation_log row: %v", err)
	}
	if mutationType != "issue.delete" {
		t.Errorf("latest mutation_type = %q, want %q", mutationType, "issue.delete")
	}
}

// TestKnowledgeWrites_UpdateWithoutHeadersStaysNull guards the
// backwards-compat contract: writes without headers still succeed and
// persist NULL on the attribution columns. Web-UI clicks must not
// require any new ceremony.
func TestKnowledgeWrites_UpdateWithoutHeadersStaysNull(t *testing.T) {
	ts := newTestServer(t)
	projectID := createTestProject(t, ts, "PAI-353 NoHdr", "P53N")

	createResp := ts.post(t, knowledgeURL(projectID, "memory"), ts.adminCookie,
		map[string]any{"slug": "no_headers", "title": "Initial"})
	assertStatus(t, createResp, http.StatusCreated)
	issueID := fetchKnowledgeIssueID(t, projectID, "memory", "no_headers")

	updResp := ts.put(t, knowledgeEntryURL(projectID, "memory", "no_headers"),
		ts.adminCookie,
		map[string]any{"slug": "no_headers", "title": "Updated"},
	)
	assertStatus(t, updResp, http.StatusOK)

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

