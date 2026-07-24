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

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/inspr-at/paimos/backend/db"
)

// PAI-347 — coverage for the reference-count tracking endpoint and the
// stale-memory archive proposal endpoint.
//
// Surface under test:
//   - POST /api/projects/:id/knowledge/memory/references — bumps the counter
//     and stamps last_referenced_at on every (project_id, type='memory')
//     row whose id is in the body.
//   - The auto-suggest path also bumps counts (covered indirectly via
//     a follow-up read of the affected rows).
//   - GET  /api/projects/:id/knowledge/memory/stale — returns proposals when
//     all three conditions hold (no recent ref + confidence ≤ medium +
//     no in-flight originating ticket).

func readReferenceCount(t *testing.T, memID int64) int64 {
	t.Helper()
	var n int64
	if err := db.DB.QueryRow(
		`SELECT reference_count FROM issues WHERE id=?`, memID,
	).Scan(&n); err != nil {
		t.Fatalf("read reference_count for %d: %v", memID, err)
	}
	return n
}

func readLastReferencedAt(t *testing.T, memID int64) string {
	t.Helper()
	var s *string
	if err := db.DB.QueryRow(
		`SELECT last_referenced_at FROM issues WHERE id=?`, memID,
	).Scan(&s); err != nil {
		t.Fatalf("read last_referenced_at for %d: %v", memID, err)
	}
	if s == nil {
		return ""
	}
	return *s
}

// TestBumpMemoryReferencesPersists confirms the endpoint increments
// the counter and stamps the timestamp for every id in the body, and
// that re-bumping the same id doubles the count.
func TestBumpMemoryReferencesPersists(t *testing.T) {
	ts := newTestServer(t)
	projID := seedBatchProject(t, "PAI", "PAI")

	mem1 := seedMemory(t, projID, "rule_one", "Rule one", "body", `{}`)
	mem2 := seedMemory(t, projID, "rule_two", "Rule two", "body", `{}`)

	// Pre-condition — counts start at 0.
	if got := readReferenceCount(t, mem1); got != 0 {
		t.Fatalf("mem1 initial=%d, want 0", got)
	}
	if got := readLastReferencedAt(t, mem1); got != "" {
		t.Fatalf("mem1 initial last_ref=%q, want empty", got)
	}

	resp := ts.post(t,
		"/api/projects/"+itoa(projID)+"/knowledge/memory/references",
		ts.adminCookie,
		map[string]any{"ids": []int64{mem1, mem2}, "source": "bundle"},
	)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST: status=%d body=%s", resp.StatusCode, b)
	}
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if upd, _ := body["updated"].(float64); int64(upd) != 2 {
		t.Errorf("updated=%v, want 2", body["updated"])
	}

	if got := readReferenceCount(t, mem1); got != 1 {
		t.Errorf("mem1 after first bump=%d, want 1", got)
	}
	if got := readReferenceCount(t, mem2); got != 1 {
		t.Errorf("mem2 after first bump=%d, want 1", got)
	}
	if readLastReferencedAt(t, mem1) == "" {
		t.Errorf("mem1 last_referenced_at not set")
	}

	// Second bump on mem1 only — counter doubles, mem2 untouched.
	resp = ts.post(t,
		"/api/projects/"+itoa(projID)+"/knowledge/memory/references",
		ts.adminCookie,
		map[string]any{"ids": []int64{mem1}},
	)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("second bump: %d", resp.StatusCode)
	}
	if got := readReferenceCount(t, mem1); got != 2 {
		t.Errorf("mem1 after second bump=%d, want 2", got)
	}
	if got := readReferenceCount(t, mem2); got != 1 {
		t.Errorf("mem2 after second bump=%d, want 1 (unchanged)", got)
	}
}

// TestBumpMemoryReferencesIgnoresCrossProject ensures an id from a
// different project doesn't get bumped — the WHERE clause includes
// project_id and the count for the foreign id stays at 0.
func TestBumpMemoryReferencesIgnoresCrossProject(t *testing.T) {
	ts := newTestServer(t)
	projA := seedBatchProject(t, "PAI", "PAI")
	projB := seedBatchProject(t, "ACME", "ACME")

	memA := seedMemory(t, projA, "in_a", "In A", "x", `{}`)
	memB := seedMemory(t, projB, "in_b", "In B", "x", `{}`)

	// Send memB's id into projA's endpoint — must be ignored.
	resp := ts.post(t,
		"/api/projects/"+itoa(projA)+"/knowledge/memory/references",
		ts.adminCookie,
		map[string]any{"ids": []int64{memA, memB}},
	)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	if got := readReferenceCount(t, memA); got != 1 {
		t.Errorf("memA=%d, want 1", got)
	}
	if got := readReferenceCount(t, memB); got != 0 {
		t.Errorf("memB=%d, want 0 (cross-project)", got)
	}
}

// TestBumpMemoryReferencesEmptyBody verifies an empty list short-circuits
// without touching the DB and returns updated=0.
func TestBumpMemoryReferencesEmptyBody(t *testing.T) {
	ts := newTestServer(t)
	projID := seedBatchProject(t, "PAI", "PAI")
	resp := ts.post(t,
		"/api/projects/"+itoa(projID)+"/knowledge/memory/references",
		ts.adminCookie,
		map[string]any{"ids": []int64{}},
	)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if upd, _ := body["updated"].(float64); int64(upd) != 0 {
		t.Errorf("updated=%v, want 0", body["updated"])
	}
}

// TestSuggestEndpointBumpsCounter exercises the indirect bump path:
// every memory the auto-suggest endpoint surfaces should have its
// counter incremented.
func TestSuggestEndpointBumpsCounter(t *testing.T) {
	ts := newTestServer(t)
	projID := seedBatchProject(t, "PAI", "PAI")

	ticketID := seedTicketRow(t, projID, 1, "Ticket A", nil, "")
	// Tag both ticket + memory so rule 1 fires. Mirror the existing
	// applicable_memories_test seed pattern — a previous test run may
	// have already inserted the 'bug' tag (seed runs once per binary).
	var bugTagID int64
	if err := db.DB.QueryRow(`SELECT id FROM tags WHERE name='bug'`).Scan(&bugTagID); err != nil || bugTagID == 0 {
		res, err := db.DB.Exec(`INSERT INTO tags(name,color,description) VALUES('bug','red','')`)
		if err != nil {
			t.Fatalf("insert bug tag: %v", err)
		}
		bugTagID, _ = res.LastInsertId()
	}
	_, _ = db.DB.Exec(`INSERT OR IGNORE INTO issue_tags(issue_id,tag_id) VALUES(?,?)`, ticketID, bugTagID)

	memID := seedMemory(t, projID, "tagged", "Tagged memory", "x", `{}`)
	_, _ = db.DB.Exec(`INSERT OR IGNORE INTO issue_tags(issue_id,tag_id) VALUES(?,?)`, memID, bugTagID)

	if got := readReferenceCount(t, memID); got != 0 {
		t.Fatalf("pre-suggest count=%d, want 0", got)
	}
	resp := ts.get(t, "/api/issues/"+itoa(ticketID)+"/applicable-memories?suggest=1", ts.adminCookie)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("suggest: %d", resp.StatusCode)
	}
	if got := readReferenceCount(t, memID); got != 1 {
		t.Errorf("post-suggest count=%d, want 1", got)
	}
	// Re-call should bump again — every fresh suggest counts.
	_ = ts.get(t, "/api/issues/"+itoa(ticketID)+"/applicable-memories?suggest=1", ts.adminCookie)
	if got := readReferenceCount(t, memID); got != 2 {
		t.Errorf("post-second-suggest count=%d, want 2", got)
	}
}

// TestStaleMemoryProposesOnlyEligible exercises every condition gate
// of the /knowledge/memory/stale endpoint:
//   - high-confidence rows are excluded.
//   - rows whose originating ticket is in 'in-progress' / 'qa' are
//     excluded even when they otherwise look stale.
//   - rows with confidence ≤ medium and no recent reference and no
//     in-flight ticket are returned.
//
// The test backdates created_at + last_referenced_at so we can drive
// the days-since-reference SQL expression deterministically.
func TestStaleMemoryProposesOnlyEligible(t *testing.T) {
	ts := newTestServer(t)
	projID := seedBatchProject(t, "PAI", "PAI")

	// Backdated > 90 days, low confidence, no ticket — should propose.
	memStale := seedMemory(t, projID, "stale_low", "Stale low", "x",
		`{"confidence":"low"}`)
	if _, err := db.DB.Exec(
		`UPDATE issues SET created_at=datetime('now','-200 days'),
		                  last_referenced_at=datetime('now','-180 days')
		 WHERE id=?`, memStale,
	); err != nil {
		t.Fatalf("backdate stale: %v", err)
	}

	// Backdated > 90 days but high confidence — must NOT propose.
	memHigh := seedMemory(t, projID, "stale_high", "Stale high", "x",
		`{"confidence":"high"}`)
	if _, err := db.DB.Exec(
		`UPDATE issues SET created_at=datetime('now','-200 days'),
		                  last_referenced_at=datetime('now','-180 days')
		 WHERE id=?`, memHigh,
	); err != nil {
		t.Fatalf("backdate high: %v", err)
	}

	// Backdated, medium-confidence (default), but linked from an
	// in-progress ticket via applies_to_memory relation — must NOT
	// propose despite age + medium confidence.
	memActive := seedMemory(t, projID, "stale_active_link", "Stale active",
		"x", `{}`)
	if _, err := db.DB.Exec(
		`UPDATE issues SET created_at=datetime('now','-200 days'),
		                  last_referenced_at=datetime('now','-180 days')
		 WHERE id=?`, memActive,
	); err != nil {
		t.Fatalf("backdate active: %v", err)
	}
	// Seed a ticket in 'in-progress' status linked to memActive.
	res, _ := db.DB.Exec(`
		INSERT INTO issues(project_id, issue_number, type, title, status, priority)
		VALUES(?, 5, 'ticket', 'In-progress ticket', 'in-progress', 'medium')
	`, projID)
	activeTicketID, _ := res.LastInsertId()
	if _, err := db.DB.Exec(
		`INSERT INTO issue_relations(source_id, target_id, type) VALUES(?, ?, 'applies_to_memory')`,
		activeTicketID, memActive,
	); err != nil {
		t.Fatalf("link active ticket: %v", err)
	}

	// Recent (5 days), medium confidence, no ticket — must NOT propose
	// (fails the age gate).
	memRecent := seedMemory(t, projID, "recent_med", "Recent medium", "x", `{}`)
	if _, err := db.DB.Exec(
		`UPDATE issues SET created_at=datetime('now','-5 days'),
		                  last_referenced_at=datetime('now','-2 days')
		 WHERE id=?`, memRecent,
	); err != nil {
		t.Fatalf("backdate recent: %v", err)
	}

	// Memory with NULL last_referenced_at and recent created_at —
	// backwards-compat: treated as freshly referenced, must NOT propose.
	memBackcompat := seedMemory(t, projID, "fresh_backcompat", "Fresh backcompat",
		"x", `{}`)
	if _, err := db.DB.Exec(
		`UPDATE issues SET created_at=datetime('now','-3 days')
		 WHERE id=?`, memBackcompat,
	); err != nil {
		t.Fatalf("backdate backcompat: %v", err)
	}

	resp := ts.get(t, "/api/projects/"+itoa(projID)+"/knowledge/memory/stale", ts.adminCookie)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET stale: status=%d body=%s", resp.StatusCode, b)
	}
	var out []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	// Only memStale should be in the result.
	if len(out) != 1 {
		t.Fatalf("expected 1 proposal, got %d (%v)", len(out), out)
	}
	if out[0]["slug"] != "stale_low" {
		t.Errorf("got slug=%v, want stale_low", out[0]["slug"])
	}
	if out[0]["confidence"] != "low" {
		t.Errorf("got confidence=%v, want low", out[0]["confidence"])
	}
}

// TestStaleMemoryHonorsDaysOverride verifies that ?days=N narrows or
// widens the proposal set as expected.
func TestStaleMemoryHonorsDaysOverride(t *testing.T) {
	ts := newTestServer(t)
	projID := seedBatchProject(t, "PAI", "PAI")

	memID := seedMemory(t, projID, "any", "Any", "x", `{}`)
	if _, err := db.DB.Exec(
		`UPDATE issues SET created_at=datetime('now','-30 days'),
		                  last_referenced_at=datetime('now','-30 days')
		 WHERE id=?`, memID,
	); err != nil {
		t.Fatalf("backdate: %v", err)
	}

	// days=90 (default) → no proposal (only 30 days old).
	resp := ts.get(t, "/api/projects/"+itoa(projID)+"/knowledge/memory/stale", ts.adminCookie)
	var out []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if len(out) != 0 {
		t.Errorf("days=default → expected 0 proposals, got %d", len(out))
	}

	// days=10 → proposal fires (30 > 10).
	resp = ts.get(t, "/api/projects/"+itoa(projID)+"/knowledge/memory/stale?days=10", ts.adminCookie)
	out = nil
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if len(out) != 1 {
		t.Errorf("days=10 → expected 1 proposal, got %d", len(out))
	}
}

// TestStaleMemoryRespectsOriginatingTicketsArray verifies the
// originating_tickets[] free-text array is honoured: when one of the
// listed keys resolves to a local in-progress ticket, the memory is
// NOT proposed for archive.
func TestStaleMemoryRespectsOriginatingTicketsArray(t *testing.T) {
	ts := newTestServer(t)
	projID := seedBatchProject(t, "PAI", "PAI")

	// Ticket PAI-7 in qa.
	if _, err := db.DB.Exec(`
		INSERT INTO issues(project_id, issue_number, type, title, status, priority)
		VALUES(?, 7, 'ticket', 'QA ticket', 'qa', 'medium')
	`, projID); err != nil {
		t.Fatalf("seed qa ticket: %v", err)
	}

	// Memory with originating_tickets = ["PAI-7"]
	memID := seedMemory(t, projID, "linked_via_array", "Linked", "x",
		`{"originating_tickets":["PAI-7"],"confidence":"medium"}`)
	if _, err := db.DB.Exec(
		`UPDATE issues SET created_at=datetime('now','-200 days'),
		                  last_referenced_at=datetime('now','-180 days')
		 WHERE id=?`, memID,
	); err != nil {
		t.Fatalf("backdate: %v", err)
	}

	resp := ts.get(t, "/api/projects/"+itoa(projID)+"/knowledge/memory/stale", ts.adminCookie)
	var out []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if len(out) != 0 {
		t.Errorf("expected 0 proposals (qa ticket suppresses), got %d", len(out))
	}
}

// TestBundleEndpointBumpsViaCLIPath ensures the reference-count
// endpoint plays nicely when called with the same (project, memory)
// pair the CLI bundle resolver would assemble. Strictly a smoke test
// of the integration shape — the CLI itself is exercised by
// cmd_session_bundle_test.go.
func TestBundleEndpointBumpsViaCLIPath(t *testing.T) {
	ts := newTestServer(t)
	projID := seedBatchProject(t, "PAI", "PAI")

	mem1 := seedMemory(t, projID, "bundle_one", "Bundle one", "x", `{}`)
	mem2 := seedMemory(t, projID, "bundle_two", "Bundle two", "x", `{}`)
	mem3 := seedMemory(t, projID, "bundle_three", "Bundle three", "x", `{}`)

	resp := ts.post(t,
		"/api/projects/"+itoa(projID)+"/knowledge/memory/references",
		ts.adminCookie,
		map[string]any{
			"ids":    []int64{mem1, mem2, mem3},
			"source": "bundle",
		},
	)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST: %d %s", resp.StatusCode, b)
	}
	for _, id := range []int64{mem1, mem2, mem3} {
		if got := readReferenceCount(t, id); got != 1 {
			t.Errorf("mem id=%d count=%d, want 1", id, got)
		}
		if got := readLastReferencedAt(t, id); !strings.Contains(got, "-") {
			// Cheap sanity: the timestamp must look like an ISO
			// date string.
			t.Errorf("mem id=%d last_ref=%q does not look ISO", id, got)
		}
	}
}
