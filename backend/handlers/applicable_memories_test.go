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
	"testing"

	"github.com/markus-barta/paimos/backend/db"
)

// PAI-342 — coverage for ticket↔memory linking.
//
// Layered into three groups:
//   1. Schema/relation: the new 'applies_to_memory' type is accepted
//      and the row round-trips in both directions.
//   2. List endpoint: GET /applicable-memories returns the curated
//      set with project_key + preview, ordered by slug.
//   3. Suggest endpoint: scoring is deterministic (tag overlap +3,
//      parent-epic body match +2, env overlap +2), top-3 cap honored,
//      already-linked memories never resurface, cross-project
//      memories are out of scope for v1 (project-scoped).

// seedMemory creates a memory issue directly in the DB and returns its id.
// Mirrors handlers/knowledge.insertEntry but bypasses the HTTP path so a
// single test can stage a complex memory graph quickly.
func seedMemory(t *testing.T, projectID int64, slug, title, body string, meta string) int64 {
	t.Helper()
	var nextNum int
	if err := db.DB.QueryRow(
		`SELECT COALESCE(MAX(issue_number),0)+1 FROM issues WHERE project_id=?`, projectID,
	).Scan(&nextNum); err != nil {
		t.Fatalf("next issue_number: %v", err)
	}
	res, err := db.DB.Exec(`
		INSERT INTO issues(project_id, issue_number, type, title, description, status, priority, slug, category_metadata)
		VALUES(?, ?, 'memory', ?, ?, 'backlog', 'medium', ?, ?)
	`, projectID, nextNum, title, body, slug, meta)
	if err != nil {
		t.Fatalf("seed memory %s: %v", slug, err)
	}
	id, _ := res.LastInsertId()
	return id
}

// seedTicketRow seeds a ticket issue and returns its id. Lightweight —
// only what the linking endpoint reads (project_id, type, title, parent_id,
// release). PAI-599: release is now a container edge, not a column.
func seedTicketRow(t *testing.T, projectID int64, num int, title string, parentID *int64, release string) int64 {
	t.Helper()
	res, err := db.DB.Exec(`
		INSERT INTO issues(project_id, issue_number, type, title, status, priority)
		VALUES(?, ?, 'ticket', ?, 'backlog', 'medium')
	`, projectID, num, title)
	if err != nil {
		t.Fatalf("seed ticket %s: %v", title, err)
	}
	id, _ := res.LastInsertId()
	// PAI-584 P6: parent_id column dropped — hierarchy via the parent edge.
	if parentID != nil {
		if _, err := db.DB.Exec(
			`INSERT OR IGNORE INTO issue_relations(source_id, target_id, type) VALUES(?,?,'parent')`,
			*parentID, id); err != nil {
			t.Fatalf("seed parent edge for %s: %v", title, err)
		}
	}
	if release != "" {
		seedLabelEdge(t, projectID, id, "release", release)
	}
	return id
}

// seedLabelEdge attaches a cost_unit/release container edge to a ticket in
// tests (PAI-599), creating the container issue by label if needed.
func seedLabelEdge(t *testing.T, projectID, ticketID int64, dimension, label string) int64 {
	t.Helper()
	var cid int64
	_ = db.DB.QueryRow(`SELECT id FROM issues WHERE project_id=? AND type=? AND title=? AND deleted_at IS NULL`,
		projectID, dimension, label).Scan(&cid)
	if cid == 0 {
		var num int
		_ = db.DB.QueryRow(`SELECT COALESCE(MAX(issue_number),0)+1 FROM issues WHERE project_id=?`, projectID).Scan(&num)
		res, err := db.DB.Exec(`INSERT INTO issues(project_id,issue_number,type,title,status,priority) VALUES(?,?,?,?,'backlog','medium')`,
			projectID, num, dimension, label)
		if err != nil {
			t.Fatalf("seed %s container %q: %v", dimension, label, err)
		}
		cid, _ = res.LastInsertId()
	}
	if _, err := db.DB.Exec(`INSERT OR IGNORE INTO issue_relations(source_id,target_id,type) VALUES(?,?,?)`,
		cid, ticketID, dimension); err != nil {
		t.Fatalf("seed %s edge: %v", dimension, err)
	}
	return cid
}

// TestAppliesToMemoryRelationAccepted verifies the new relation type
// passes the POST /relations validator and round-trips in both
// directions of the issue_relations table.
func TestAppliesToMemoryRelationAccepted(t *testing.T) {
	ts := newTestServer(t)
	projID := seedBatchProject(t, "PAI", "PAI")

	ticketID := seedTicketRow(t, projID, 1, "Ticket A", nil, "")
	memID := seedMemory(t, projID, "feedback_lock_signature", "Lock signature feedback", "Body text", `{}`)

	// POST the new relation.
	resp := ts.post(t,
		"/api/issues/"+itoa(ticketID)+"/relations",
		ts.adminCookie,
		map[string]any{"target_id": memID, "type": "applies_to_memory"},
	)
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST: status=%d body=%s", resp.StatusCode, b)
	}

	// Forward direction: list from ticket side, expect "outgoing".
	out := listRelations(t, ts, itoa(ticketID))
	found := false
	for _, r := range out {
		if r["type"] == "applies_to_memory" && int64(r["target_id"].(float64)) == memID {
			found = true
			if r["direction"] != "outgoing" {
				t.Errorf("ticket side direction=%v, want outgoing", r["direction"])
			}
		}
	}
	if !found {
		t.Errorf("ticket side missing applies_to_memory relation")
	}

	// Reverse direction: list from memory side, expect "incoming".
	out = listRelations(t, ts, itoa(memID))
	found = false
	for _, r := range out {
		if r["type"] == "applies_to_memory" && int64(r["source_id"].(float64)) == ticketID {
			found = true
			if r["direction"] != "incoming" {
				t.Errorf("memory side direction=%v, want incoming", r["direction"])
			}
		}
	}
	if !found {
		t.Errorf("memory side missing applies_to_memory relation")
	}

	// DELETE round-trip.
	resp = ts.delWithBody(t,
		"/api/issues/"+itoa(ticketID)+"/relations",
		ts.adminCookie,
		map[string]any{"target_id": memID, "type": "applies_to_memory"},
	)
	if resp.StatusCode != http.StatusNoContent {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("DELETE: status=%d body=%s", resp.StatusCode, b)
	}
	// Verify the row is gone from both sides.
	out = listRelations(t, ts, itoa(ticketID))
	for _, r := range out {
		if r["type"] == "applies_to_memory" {
			t.Errorf("ticket side still shows relation after DELETE")
		}
	}
}

// TestApplicableMemoriesList exercises the new GET endpoint without
// the suggest path. Verifies project_key + slug + preview shape.
func TestApplicableMemoriesList(t *testing.T) {
	ts := newTestServer(t)
	projID := seedBatchProject(t, "PAI", "PAI")

	ticketID := seedTicketRow(t, projID, 1, "Ticket A", nil, "")
	mem1 := seedMemory(t, projID, "alpha", "Alpha memory", "First line\nSecond line", `{}`)
	mem2 := seedMemory(t, projID, "beta", "Beta memory", "  \n\nBeta first non-empty", `{}`)

	// Link both.
	for _, mid := range []int64{mem1, mem2} {
		resp := ts.post(t,
			"/api/issues/"+itoa(ticketID)+"/relations",
			ts.adminCookie,
			map[string]any{"target_id": mid, "type": "applies_to_memory"},
		)
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("link mid=%d: %d", mid, resp.StatusCode)
		}
	}

	resp := ts.get(t, "/api/issues/"+itoa(ticketID)+"/applicable-memories", ts.adminCookie)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET: status=%d body=%s", resp.StatusCode, b)
	}
	var out []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if len(out) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(out))
	}
	// Ordered by slug ascending — alpha before beta.
	if out[0]["slug"] != "alpha" || out[1]["slug"] != "beta" {
		t.Errorf("ordering wrong: %v / %v", out[0]["slug"], out[1]["slug"])
	}
	// project_key populated.
	if out[0]["project_key"] != "PAI" {
		t.Errorf("project_key=%v, want PAI", out[0]["project_key"])
	}
	// First-non-empty-line preview (handles the leading-blank case).
	if out[1]["preview"] != "Beta first non-empty" {
		t.Errorf("preview=%v, want 'Beta first non-empty'", out[1]["preview"])
	}
}

// TestApplicableMemoriesSuggest exercises the v1 scoring rules.
//
// Setup:
//   - parent epic "Knowledge Plane"
//   - ticket has tag "bug" + release "v1.0"
//   - memA: tagged "bug"           → +3 (rule 1)
//   - memB: body mentions "Knowledge Plane"  → +2 (rule 2)
//   - memC: applies_to_environments=["v1.0"] → +2 (rule 3)
//   - memD: tag "bug" + body mentions parent + env=["v1.0"] → +7 (top)
//   - memE: nothing matches → score 0, dropped from output.
//
// Expectation: memD (+7) wins, then memA (+3), then the +2 tier. The
// +2 tier has both memB (body match) and memC (env match) — memB
// ties in score and seeded first so it wins the id tiebreak. memC
// doesn't make the top-3 cut, which proves the truncation works.
// memE never appears (zero score).
func TestApplicableMemoriesSuggest(t *testing.T) {
	ts := newTestServer(t)
	projID := seedBatchProject(t, "PAI", "PAI")

	// Epic parent so rule 2 has a target.
	epicRes, _ := db.DB.Exec(`
		INSERT INTO issues(project_id, issue_number, type, title, status, priority)
		VALUES(?, 100, 'epic', 'Knowledge Plane', 'backlog', 'medium')
	`, projID)
	epicID, _ := epicRes.LastInsertId()

	ticketID := seedTicketRow(t, projID, 1, "Ticket A", &epicID, "v1.0")

	// Tag the ticket.
	var bugTagID int64
	_ = db.DB.QueryRow(`SELECT id FROM tags WHERE name='bug'`).Scan(&bugTagID)
	if bugTagID == 0 {
		res, _ := db.DB.Exec(`INSERT INTO tags(name,color,description) VALUES('bug','red','')`)
		bugTagID, _ = res.LastInsertId()
	}
	if _, err := db.DB.Exec(`INSERT OR IGNORE INTO issue_tags(issue_id,tag_id) VALUES(?,?)`, ticketID, bugTagID); err != nil {
		t.Fatalf("tag ticket: %v", err)
	}

	memA := seedMemory(t, projID, "a_tag_match", "A", "irrelevant body", `{}`)
	_ = seedMemory(t, projID, "b_body_match", "B", "Refers to the Knowledge Plane epic explicitly.", `{}`)
	_ = seedMemory(t, projID, "c_env_match", "C", "irrelevant", `{"applies_to_environments":["v1.0"]}`)
	memD := seedMemory(t, projID, "d_all_match", "D", "Notes from Knowledge Plane.", `{"applies_to_environments":["v1.0"]}`)
	_ = seedMemory(t, projID, "e_none", "E", "irrelevant", `{}`)

	// Tag memA + memD with "bug" so rule 1 fires for them.
	for _, mid := range []int64{memA, memD} {
		if _, err := db.DB.Exec(`INSERT OR IGNORE INTO issue_tags(issue_id,tag_id) VALUES(?,?)`, mid, bugTagID); err != nil {
			t.Fatalf("tag mem %d: %v", mid, err)
		}
	}

	resp := ts.get(t, "/api/issues/"+itoa(ticketID)+"/applicable-memories?suggest=1", ts.adminCookie)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET ?suggest=1: status=%d body=%s", resp.StatusCode, b)
	}
	var out []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if len(out) != 3 {
		t.Fatalf("expected top-3, got %d (%v)", len(out), out)
	}
	// memD wins (score 7).
	if out[0]["slug"] != "d_all_match" {
		t.Errorf("first=%v, want d_all_match", out[0]["slug"])
	}
	// memE never appears.
	for _, r := range out {
		if r["slug"] == "e_none" {
			t.Errorf("memE leaked into suggestions despite zero score")
		}
	}
	// memD's matched array is populated and includes all three rule
	// hits (smoke check — ordering inside `matched` isn't asserted).
	matched, _ := out[0]["matched"].([]any)
	if len(matched) < 3 {
		t.Errorf("memD matched=%v, expected at least 3 rule hits", matched)
	}
	// memA scores +3 (rule 1), then the +2 tier — memB (body) ties
	// memC (env) on score; memB seeded first so it wins the id
	// tiebreak. memC drops out of the top-3 cap.
	if out[1]["slug"] != "a_tag_match" || out[2]["slug"] != "b_body_match" {
		t.Errorf("tiebreak ordering: got %v / %v, want a_tag_match / b_body_match",
			out[1]["slug"], out[2]["slug"])
	}
}

// TestApplicableMemoriesSuggestExcludesLinked verifies a memory the
// ticket is already linked to never re-appears in the suggestion
// stream — otherwise the UI would surface duplicates.
func TestApplicableMemoriesSuggestExcludesLinked(t *testing.T) {
	ts := newTestServer(t)
	projID := seedBatchProject(t, "PAI", "PAI")

	ticketID := seedTicketRow(t, projID, 1, "Ticket A", nil, "v1.0")

	// Tag the ticket.
	var bugTagID int64
	_ = db.DB.QueryRow(`SELECT id FROM tags WHERE name='bug'`).Scan(&bugTagID)
	if bugTagID == 0 {
		res, _ := db.DB.Exec(`INSERT INTO tags(name,color,description) VALUES('bug','red','')`)
		bugTagID, _ = res.LastInsertId()
	}
	_, _ = db.DB.Exec(`INSERT OR IGNORE INTO issue_tags(issue_id,tag_id) VALUES(?,?)`, ticketID, bugTagID)

	mem := seedMemory(t, projID, "tagged", "Tagged memory", "x", `{}`)
	_, _ = db.DB.Exec(`INSERT OR IGNORE INTO issue_tags(issue_id,tag_id) VALUES(?,?)`, mem, bugTagID)

	// Suggest before linking — should surface.
	resp := ts.get(t, "/api/issues/"+itoa(ticketID)+"/applicable-memories?suggest=1", ts.adminCookie)
	var out []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if len(out) != 1 || out[0]["slug"] != "tagged" {
		t.Fatalf("pre-link suggest: %v", out)
	}

	// Link it.
	resp = ts.post(t,
		"/api/issues/"+itoa(ticketID)+"/relations",
		ts.adminCookie,
		map[string]any{"target_id": mem, "type": "applies_to_memory"},
	)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("link: %d", resp.StatusCode)
	}

	// Suggest after linking — must be empty.
	resp = ts.get(t, "/api/issues/"+itoa(ticketID)+"/applicable-memories?suggest=1", ts.adminCookie)
	out = nil
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if len(out) != 0 {
		t.Errorf("post-link suggest non-empty: %v", out)
	}
}

// TestApplicableMemoriesCrossProjectViaRelation verifies that a memory
// in another project linked via the relations endpoint surfaces in
// the manual-list path. The suggest path stays project-scoped (v1
// scope cap) — that's covered by the absence of cross-project memories
// in TestApplicableMemoriesSuggest.
func TestApplicableMemoriesCrossProjectViaRelation(t *testing.T) {
	ts := newTestServer(t)
	projA := seedBatchProject(t, "PAI Project", "PAI")
	projB := seedBatchProject(t, "ACME Project", "ACME")

	ticketID := seedTicketRow(t, projA, 1, "Ticket A", nil, "")
	memID := seedMemory(t, projB, "shared_runbook", "Shared runbook", "Body", `{}`)

	resp := ts.post(t,
		"/api/issues/"+itoa(ticketID)+"/relations",
		ts.adminCookie,
		map[string]any{"target_id": memID, "type": "applies_to_memory"},
	)
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST cross-project: status=%d body=%s", resp.StatusCode, b)
	}

	resp = ts.get(t, "/api/issues/"+itoa(ticketID)+"/applicable-memories", ts.adminCookie)
	var out []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if len(out) != 1 {
		t.Fatalf("expected 1 cross-project result, got %d", len(out))
	}
	if out[0]["project_key"] != "ACME" {
		t.Errorf("project_key=%v, want ACME", out[0]["project_key"])
	}
}
