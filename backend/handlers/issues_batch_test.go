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
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/markus-barta/paimos/backend/db"
	"github.com/markus-barta/paimos/backend/handlers"
)

// seedBatchProject seeds a project and returns its numeric id + key.
// Uses direct SQL to avoid depending on project-create handler semantics.
func seedBatchProject(t *testing.T, name, key string) int64 {
	t.Helper()
	res, err := db.DB.Exec(`INSERT INTO projects(name, key) VALUES(?, ?)`, name, key)
	if err != nil {
		t.Fatalf("seed project %s/%s: %v", name, key, err)
	}
	id, _ := res.LastInsertId()
	return id
}

// issueCountIn returns the count of live (non-trashed) issues in a project —
// used to verify "nothing committed" on a rolled-back batch.
func issueCountIn(t *testing.T, projectID int64) int {
	t.Helper()
	var n int
	if err := db.DB.QueryRow(
		"SELECT COUNT(*) FROM issues WHERE project_id=? AND deleted_at IS NULL", projectID,
	).Scan(&n); err != nil {
		t.Fatalf("count issues: %v", err)
	}
	return n
}

func TestBatchCreate_Success_ParentRef(t *testing.T) {
	ts := newTestServer(t)
	seedBatchProject(t, "PAI Project", "PAI")

	// Epic + 2 children via parent_ref — the canonical agent use-case.
	body := []map[string]any{
		{"title": "Epic A", "type": "epic"},
		{"title": "Sub 1", "type": "ticket", "parent_ref": "#0"},
		{"title": "Sub 2", "type": "ticket", "parent_ref": "#0"},
	}
	resp := ts.post(t, "/api/projects/PAI/issues/batch", ts.adminCookie, body)
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d (want 201), body=%s", resp.StatusCode, b)
	}
	var out struct {
		Issues []map[string]any `json:"issues"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if len(out.Issues) != 3 {
		t.Fatalf("len(issues)=%d, want 3", len(out.Issues))
	}
	epicID := int64(out.Issues[0]["id"].(float64))
	if p := out.Issues[1]["parent_id"]; p == nil || int64(p.(float64)) != epicID {
		t.Errorf("sub1 parent_id = %v, want epic id %d", p, epicID)
	}
	if p := out.Issues[2]["parent_id"]; p == nil || int64(p.(float64)) != epicID {
		t.Errorf("sub2 parent_id = %v, want epic id %d", p, epicID)
	}
	// Keys should be assigned sequentially.
	if k := out.Issues[0]["issue_key"]; k != "PAI-1" {
		t.Errorf("epic key = %v, want PAI-1", k)
	}
	if k := out.Issues[2]["issue_key"]; k != "PAI-3" {
		t.Errorf("sub2 key = %v, want PAI-3", k)
	}
}

// This is the critical Op AC: if ANY row fails validation, NOTHING commits.
// Previously-valid rows must not leak into the DB.
func TestBatchCreate_Atomicity_NoPartialCommits(t *testing.T) {
	ts := newTestServer(t)
	projID := seedBatchProject(t, "PAI", "PAI")

	before := issueCountIn(t, projID)

	body := []map[string]any{
		{"title": "Valid 1", "type": "ticket"},
		{"title": "Valid 2", "type": "ticket"},
		{"title": "", "type": "ticket"}, // ← fails: title required
		{"title": "Valid 3", "type": "ticket"},
	}
	resp := ts.post(t, "/api/projects/PAI/issues/batch", ts.adminCookie, body)
	if resp.StatusCode != http.StatusBadRequest {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d (want 400), body=%s", resp.StatusCode, b)
	}
	var err struct {
		Errors     []map[string]any `json:"errors"`
		RolledBack bool             `json:"rolled_back"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&err)
	if !err.RolledBack {
		t.Error("response missing rolled_back=true marker")
	}
	if len(err.Errors) == 0 {
		t.Error("response missing per-row errors")
	}
	after := issueCountIn(t, projID)
	if after != before {
		t.Errorf("issue count changed from %d to %d — partial commit leaked through",
			before, after)
	}
}

func TestBatchCreate_InvalidParentRef(t *testing.T) {
	ts := newTestServer(t)
	seedBatchProject(t, "PAI", "PAI")

	body := []map[string]any{
		{"title": "Child", "type": "ticket", "parent_ref": "#5"}, // forward ref — invalid
		{"title": "Parent", "type": "epic"},
	}
	resp := ts.post(t, "/api/projects/PAI/issues/batch", ts.adminCookie, body)
	if resp.StatusCode != http.StatusBadRequest {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d (want 400), body=%s", resp.StatusCode, b)
	}
}

func TestBatchCreate_OverLimit_413(t *testing.T) {
	ts := newTestServer(t)
	seedBatchProject(t, "PAI", "PAI")

	body := make([]map[string]any, 101)
	for i := range body {
		body[i] = map[string]any{"title": "X", "type": "ticket"}
	}
	resp := ts.post(t, "/api/projects/PAI/issues/batch", ts.adminCookie, body)
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("status=%d, want 413", resp.StatusCode)
	}
	b, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(b), "exceeds limit") {
		t.Errorf("missing limit message in body: %s", b)
	}
}

func TestBatchCreate_UnknownProjectKey(t *testing.T) {
	ts := newTestServer(t)
	body := []map[string]any{{"title": "X", "type": "ticket"}}
	resp := ts.post(t, "/api/projects/ZZZ/issues/batch", ts.adminCookie, body)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status=%d, want 404", resp.StatusCode)
	}
}

func TestBatchCreate_NumericProjectID(t *testing.T) {
	ts := newTestServer(t)
	pid := seedBatchProject(t, "PAI", "PAI")

	body := []map[string]any{{"title": "Only", "type": "ticket"}}
	// Use the numeric id instead of the key.
	resp := ts.post(t, "/api/projects/"+itoa(pid)+"/issues/batch", ts.adminCookie, body)
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d, body=%s", resp.StatusCode, b)
	}
}

func TestBatchCreate_NonAdmin_403(t *testing.T) {
	ts := newTestServer(t)
	seedBatchProject(t, "PAI", "PAI")
	body := []map[string]any{{"title": "X", "type": "ticket"}}
	resp := ts.post(t, "/api/projects/PAI/issues/batch", ts.memberCookie, body)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("member POST: status=%d, want 403", resp.StatusCode)
	}
}

func TestBatchUpdate_Success_MixedKeysAndIds(t *testing.T) {
	ts := newTestServer(t)
	projID := seedBatchProject(t, "PAI", "PAI")

	// Seed two issues directly.
	r1, _ := db.DB.Exec(
		`INSERT INTO issues(project_id, issue_number, type, title, status) VALUES(?,?,?,?,?)`,
		projID, 1, "ticket", "A", "backlog")
	id1, _ := r1.LastInsertId()
	r2, _ := db.DB.Exec(
		`INSERT INTO issues(project_id, issue_number, type, title, status) VALUES(?,?,?,?,?)`,
		projID, 2, "ticket", "B", "backlog")
	id2, _ := r2.LastInsertId()

	body := []map[string]any{
		{"ref": "PAI-1", "fields": map[string]any{"status": "in-progress"}},
		{"ref": itoa(id2), "fields": map[string]any{"status": "done"}},
	}
	resp := ts.patch(t, "/api/issues", ts.adminCookie, body)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d, body=%s", resp.StatusCode, b)
	}
	// Verify state.
	got := func(id int64) string {
		var s string
		_ = db.DB.QueryRow("SELECT status FROM issues WHERE id=?", id).Scan(&s)
		return s
	}
	if got(id1) != "in-progress" {
		t.Errorf("PAI-1 status = %q, want in-progress", got(id1))
	}
	if got(id2) != "done" {
		t.Errorf("id2 status = %q, want done", got(id2))
	}
}

func TestBatchUpdate_Atomicity_NotFound_RollsBack(t *testing.T) {
	ts := newTestServer(t)
	projID := seedBatchProject(t, "PAI", "PAI")
	r1, _ := db.DB.Exec(
		`INSERT INTO issues(project_id, issue_number, type, title, status) VALUES(?,?,?,?,?)`,
		projID, 1, "ticket", "A", "backlog")
	id1, _ := r1.LastInsertId()

	body := []map[string]any{
		{"ref": "PAI-1", "fields": map[string]any{"status": "in-progress"}},
		{"ref": "PAI-999", "fields": map[string]any{"status": "done"}}, // ← doesn't exist
	}
	resp := ts.patch(t, "/api/issues", ts.adminCookie, body)
	if resp.StatusCode != http.StatusBadRequest {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d (want 400), body=%s", resp.StatusCode, b)
	}
	// PAI-1's status must still be "backlog" — the valid row did not commit.
	var s string
	_ = db.DB.QueryRow("SELECT status FROM issues WHERE id=?", id1).Scan(&s)
	if s != "backlog" {
		t.Errorf("PAI-1 status leaked through rollback: got %q, want backlog", s)
	}
}

// TestBatchUpdate_AllScalarFields walks every field the BulkChangeModal
// can set (status, priority, assignee_id, parent_id, cost_unit, release)
// in a single PATCH /api/issues call and verifies all rows updated.
//
// Regression for the PAI bulk-modal "internal error" caused by parallel
// per-row PUTs racing the SQLite single-writer — the modal now uses this
// single atomic batch, so each shape needs explicit coverage.
func TestBatchUpdate_AllScalarFields(t *testing.T) {
	ts := newTestServer(t)
	projID := seedBatchProject(t, "PAI", "PAI")

	// Seed an epic for the parent test, plus 6 tickets — one per field.
	epicRes, _ := db.DB.Exec(
		`INSERT INTO issues(project_id,issue_number,type,title,status) VALUES(?,?,?,?,?)`,
		projID, 1, "epic", "Epic", "backlog")
	epicID, _ := epicRes.LastInsertId()
	ids := make([]int64, 6)
	for i := 0; i < 6; i++ {
		r, _ := db.DB.Exec(
			`INSERT INTO issues(project_id,issue_number,type,title,status,priority) VALUES(?,?,?,?,?,?)`,
			projID, i+2, "ticket", "T", "backlog", "medium")
		ids[i], _ = r.LastInsertId()
	}

	// Resolve admin user id for the assignee_id case.
	var adminID int64
	if err := db.DB.QueryRow(`SELECT id FROM users WHERE username='admin'`).Scan(&adminID); err != nil {
		t.Fatalf("resolve admin id: %v", err)
	}

	body := []map[string]any{
		{"ref": itoa(ids[0]), "fields": map[string]any{"status": "in-progress"}},
		{"ref": itoa(ids[1]), "fields": map[string]any{"priority": "high"}},
		{"ref": itoa(ids[2]), "fields": map[string]any{"assignee_id": adminID}},
		{"ref": itoa(ids[3]), "fields": map[string]any{"parent_id": epicID}},
		{"ref": itoa(ids[4]), "fields": map[string]any{"cost_unit": "ENG-A"}},
		{"ref": itoa(ids[5]), "fields": map[string]any{"release": "v1.2"}},
	}
	resp := ts.patch(t, "/api/issues", ts.adminCookie, body)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d, body=%s", resp.StatusCode, b)
	}

	type expect struct {
		id   int64
		col  string
		want any
	}
	// PAI-584 P6: parent_id is the `parent` edge now (column dropped) — checked
	// separately below, not as a column in this scalar loop. Cases carry their
	// own id so dropping the parent_id column case doesn't misalign indexing.
	cases := []expect{
		{ids[0], "status", "in-progress"},
		{ids[1], "priority", "high"},
		{ids[2], "assignee_id", adminID},
	}
	for _, c := range cases {
		var got any
		if err := db.DB.QueryRow(
			"SELECT "+c.col+" FROM issues WHERE id=?", c.id,
		).Scan(&got); err != nil {
			t.Errorf("read %s for id=%d: %v", c.col, c.id, err)
			continue
		}
		// SQLite returns int64 for INTEGER columns; coerce ints for compare.
		if g, ok := got.(int64); ok {
			if w, ok := c.want.(int64); ok && g != w {
				t.Errorf("id=%d %s = %d, want %d", c.id, c.col, g, w)
			}
			continue
		}
		gs := strings.TrimSpace(toStr(got))
		ws := toStr(c.want)
		if gs != ws {
			t.Errorf("id=%d %s = %q, want %q", c.id, c.col, gs, ws)
		}
	}
	// parent_id case (ids[3]) → verify the parent edge to the epic.
	if got := parentEdgeSources(t, ids[3]); len(got) != 1 || got[0] != epicID {
		t.Errorf("batch parent_id update: parent edge for %d = %v, want [%d]", ids[3], got, epicID)
	}
	// PAI-599: cost_unit (ids[4]) / release (ids[5]) are container edges now.
	assertLabelEdge := func(ticketID int64, dimension, want string) {
		t.Helper()
		var label string
		err := db.DB.QueryRow(
			`SELECT c.title FROM issue_relations r JOIN issues c ON c.id=r.source_id WHERE r.target_id=? AND r.type=?`,
			ticketID, dimension).Scan(&label)
		if err != nil || label != want {
			t.Errorf("batch %s update: id=%d edge label=%q (err=%v), want %q", dimension, ticketID, label, err, want)
		}
	}
	assertLabelEdge(ids[4], "cost_unit", "ENG-A")
	assertLabelEdge(ids[5], "release", "v1.2")
}

// TestBatchUpdate_AssigneeFK_RollsBack — an unknown assignee_id must
// reject the whole batch (FK violation in SQLite). The other valid row
// must NOT commit. Mirrors the live failure mode the modal guards against.
func TestBatchUpdate_AssigneeFK_RollsBack(t *testing.T) {
	ts := newTestServer(t)
	projID := seedBatchProject(t, "PAI", "PAI")
	r1, _ := db.DB.Exec(
		`INSERT INTO issues(project_id,issue_number,type,title,status,priority) VALUES(?,?,?,?,?,?)`,
		projID, 1, "ticket", "Keep", "backlog", "medium")
	id1, _ := r1.LastInsertId()
	r2, _ := db.DB.Exec(
		`INSERT INTO issues(project_id,issue_number,type,title,status,priority) VALUES(?,?,?,?,?,?)`,
		projID, 2, "ticket", "Target", "backlog", "medium")
	id2, _ := r2.LastInsertId()

	body := []map[string]any{
		{"ref": itoa(id1), "fields": map[string]any{"priority": "high"}},
		{"ref": itoa(id2), "fields": map[string]any{"assignee_id": 999999}}, // unknown user
	}
	resp := ts.patch(t, "/api/issues", ts.adminCookie, body)
	if resp.StatusCode != http.StatusBadRequest {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d (want 400), body=%s", resp.StatusCode, b)
	}

	// id1 must NOT have priority=high — the batch rolled back.
	var prio string
	_ = db.DB.QueryRow(`SELECT priority FROM issues WHERE id=?`, id1).Scan(&prio)
	if prio != "medium" {
		t.Errorf("id1 priority=%q (want medium) — partial commit leaked through rollback", prio)
	}
}

// TestBatchUpdate_RecordsBatchedMutationLog — the bulk endpoint must
// write one mutation_log row per issue, all sharing a single batch_id,
// so the undo stack treats the bulk operation as one user action.
func TestBatchUpdate_RecordsBatchedMutationLog(t *testing.T) {
	ts := newTestServer(t)
	projID := seedBatchProject(t, "PAI", "PAI")
	ids := make([]int64, 3)
	for i := 0; i < 3; i++ {
		r, _ := db.DB.Exec(
			`INSERT INTO issues(project_id,issue_number,type,title,status) VALUES(?,?,?,?,?)`,
			projID, i+1, "ticket", "T", "backlog")
		ids[i], _ = r.LastInsertId()
	}

	body := []map[string]any{
		{"ref": itoa(ids[0]), "fields": map[string]any{"status": "in-progress"}},
		{"ref": itoa(ids[1]), "fields": map[string]any{"status": "in-progress"}},
		{"ref": itoa(ids[2]), "fields": map[string]any{"status": "in-progress"}},
	}
	resp := ts.patch(t, "/api/issues", ts.adminCookie, body)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d, body=%s", resp.StatusCode, b)
	}

	rows, err := db.DB.Query(`
		SELECT subject_id, COALESCE(batch_id,'')
		FROM mutation_log
		WHERE subject_type='issue' AND subject_id IN (?,?,?)
		ORDER BY id ASC`,
		ids[0], ids[1], ids[2])
	if err != nil {
		t.Fatalf("query mutation_log: %v", err)
	}
	defer rows.Close()
	var seen int
	var firstBatch string
	for rows.Next() {
		var sid int64
		var batchID string
		if err := rows.Scan(&sid, &batchID); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if batchID == "" {
			t.Errorf("subject_id=%d: batch_id is empty — bulk ops must group via batch_id", sid)
			continue
		}
		if firstBatch == "" {
			firstBatch = batchID
		} else if batchID != firstBatch {
			t.Errorf("subject_id=%d: batch_id=%q diverges from first row %q — should share one id", sid, batchID, firstBatch)
		}
		seen++
	}
	if seen != 3 {
		t.Errorf("expected 3 mutation_log rows, got %d", seen)
	}
}

// TestBatchUpdate_ErrorEnvelope_IncludesSummary — generic UI error
// handlers read `data.error` from the body. The batch failure shape is
// {errors:[…], rolled_back:true}; without a top-level `error` summary
// the modal would render "request failed" instead of the real cause.
func TestBatchUpdate_ErrorEnvelope_IncludesSummary(t *testing.T) {
	ts := newTestServer(t)
	seedBatchProject(t, "PAI", "PAI")

	body := []map[string]any{
		{"ref": "PAI-999", "fields": map[string]any{"status": "done"}},
	}
	resp := ts.patch(t, "/api/issues", ts.adminCookie, body)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", resp.StatusCode)
	}
	var env map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if s, _ := env["error"].(string); s == "" {
		t.Errorf("missing top-level error summary; body=%v", env)
	}
	if env["rolled_back"] != true {
		t.Errorf("missing rolled_back=true marker; body=%v", env)
	}
}

// TestBatchUpdate_ExplicitNullClearsAssigneeAndParent — explicit JSON null
// on assignee_id / parent_id must CLEAR the column. With the older
// `CASE WHEN ? IS NOT NULL` SQL, *int64 collapsed both "key absent" and
// "key set to null" to nil, so the modal's Unassigned / Orphan options
// were silently ignored. Presence-based parsing makes them work.
func TestBatchUpdate_ExplicitNullClearsAssigneeAndParent(t *testing.T) {
	ts := newTestServer(t)
	projID := seedBatchProject(t, "PAI", "PAI")

	// Resolve admin user id for the assigned-then-unassigned flow.
	var adminID int64
	if err := db.DB.QueryRow(`SELECT id FROM users WHERE username='admin'`).Scan(&adminID); err != nil {
		t.Fatalf("resolve admin id: %v", err)
	}
	// Seed an epic + a ticket parented to it AND assigned to admin.
	epicRes, _ := db.DB.Exec(
		`INSERT INTO issues(project_id,issue_number,type,title,status) VALUES(?,?,?,?,?)`,
		projID, 1, "epic", "Epic", "backlog")
	epicID, _ := epicRes.LastInsertId()
	// PAI-584 P6: parent_id column dropped — seed the parent edge.
	r, _ := db.DB.Exec(
		`INSERT INTO issues(project_id,issue_number,type,title,status,assignee_id) VALUES(?,?,?,?,?,?)`,
		projID, 2, "ticket", "T", "backlog", adminID)
	id1, _ := r.LastInsertId()
	if _, err := db.DB.Exec(`INSERT OR IGNORE INTO issue_relations(source_id,target_id,type) VALUES(?,?,'parent')`, epicID, id1); err != nil {
		t.Fatalf("seed parent edge: %v", err)
	}

	body := []map[string]any{
		{"ref": itoa(id1), "fields": map[string]any{
			"assignee_id": nil,
			"parent_id":   nil,
		}},
	}
	resp := ts.patch(t, "/api/issues", ts.adminCookie, body)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d, body=%s", resp.StatusCode, b)
	}

	// Verify assignee column cleared and the parent edge removed.
	var assignee any
	_ = db.DB.QueryRow(`SELECT assignee_id FROM issues WHERE id=?`, id1).Scan(&assignee)
	if assignee != nil {
		t.Errorf("assignee_id = %v (want nil after explicit-null clear)", assignee)
	}
	if got := parentEdgeSources(t, id1); len(got) != 0 {
		t.Errorf("parent edge = %v (want none after explicit-null clear)", got)
	}
}

// TestBatchUpdate_AbsentKeysDoNotClear — sending a fields object that
// omits assignee_id/parent_id must NOT touch those columns. Regression
// guard so the presence-based fix doesn't accidentally treat "absent"
// like "null".
func TestBatchUpdate_AbsentKeysDoNotClear(t *testing.T) {
	ts := newTestServer(t)
	projID := seedBatchProject(t, "PAI", "PAI")
	var adminID int64
	_ = db.DB.QueryRow(`SELECT id FROM users WHERE username='admin'`).Scan(&adminID)
	epicRes, _ := db.DB.Exec(
		`INSERT INTO issues(project_id,issue_number,type,title,status) VALUES(?,?,?,?,?)`,
		projID, 1, "epic", "Epic", "backlog")
	epicID, _ := epicRes.LastInsertId()
	// PAI-584 P6: parent_id column dropped — seed the parent edge.
	r, _ := db.DB.Exec(
		`INSERT INTO issues(project_id,issue_number,type,title,status,assignee_id) VALUES(?,?,?,?,?,?)`,
		projID, 2, "ticket", "T", "backlog", adminID)
	id1, _ := r.LastInsertId()
	if _, err := db.DB.Exec(`INSERT OR IGNORE INTO issue_relations(source_id,target_id,type) VALUES(?,?,'parent')`, epicID, id1); err != nil {
		t.Fatalf("seed parent edge: %v", err)
	}

	body := []map[string]any{
		{"ref": itoa(id1), "fields": map[string]any{"status": "in-progress"}},
	}
	resp := ts.patch(t, "/api/issues", ts.adminCookie, body)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d, body=%s", resp.StatusCode, b)
	}
	var assignee sql.NullInt64
	_ = db.DB.QueryRow(`SELECT assignee_id FROM issues WHERE id=?`, id1).Scan(&assignee)
	if !assignee.Valid || assignee.Int64 != adminID {
		t.Errorf("assignee_id=%v (want %d) — absent key wrongly cleared the column", assignee, adminID)
	}
	if got := parentEdgeSources(t, id1); len(got) != 1 || got[0] != epicID {
		t.Errorf("parent edge = %v (want [%d]) — absent key wrongly cleared it", got, epicID)
	}
}

// TestSinglePut_ExplicitNullClearsAllNullableColumns — PAI-315.
// Every truly-nullable issue column (per the schema) must be clearable
// via PUT /api/issues/{id} with `<col>: null`. Pre-fix, these silently
// no-op'd because *float64/*int64/*string collapse "absent" and "null"
// to nil, and the SQL was COALESCE(?, col).
func TestSinglePut_ExplicitNullClearsAllNullableColumns(t *testing.T) {
	ts := newTestServer(t)
	projID := seedBatchProject(t, "PAI", "PAI")
	r, _ := db.DB.Exec(
		`INSERT INTO issues(project_id,issue_number,type,title,status,
		                    total_budget,rate_hourly,rate_lp,
		                    estimate_hours,estimate_lp,ar_hours,ar_lp,
		                    time_override,color)
		 VALUES(?,?,?,?,?, ?,?,?, ?,?,?,?, ?,?)`,
		projID, 1, "ticket", "T", "backlog",
		1000.0, 110.0, 90.0,
		8.0, 5.0, 4.0, 3.0,
		2.5, "#abcdef")
	id1, _ := r.LastInsertId()

	resp := ts.put(t, "/api/issues/"+itoa(id1), ts.adminCookie, map[string]any{
		"total_budget":   nil,
		"rate_hourly":    nil,
		"rate_lp":        nil,
		"estimate_hours": nil,
		"estimate_lp":    nil,
		"ar_hours":       nil,
		"ar_lp":          nil,
		"time_override":  nil,
		"color":          nil,
	})
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d, body=%s", resp.StatusCode, b)
	}
	cols := []string{
		"total_budget", "rate_hourly", "rate_lp",
		"estimate_hours", "estimate_lp", "ar_hours", "ar_lp",
		"time_override", "color",
	}
	for _, col := range cols {
		var v any
		_ = db.DB.QueryRow("SELECT "+col+" FROM issues WHERE id=?", id1).Scan(&v)
		if v != nil {
			t.Errorf("%s = %v (want nil after explicit-null clear)", col, v)
		}
	}
}

// TestSinglePut_NullableAbsentKeysDoNotClear — regression guard
// matching the assignee/parent test above, extended to the new
// presence-aware columns. Sending a body that omits the keys must
// leave the values intact.
func TestSinglePut_NullableAbsentKeysDoNotClear(t *testing.T) {
	ts := newTestServer(t)
	projID := seedBatchProject(t, "PAI", "PAI")
	r, _ := db.DB.Exec(
		`INSERT INTO issues(project_id,issue_number,type,title,status,
		                    total_budget,estimate_hours,color)
		 VALUES(?,?,?,?,?, ?,?,?)`,
		projID, 1, "ticket", "T", "backlog",
		2500.0, 7.5, "#112233")
	id1, _ := r.LastInsertId()

	// Touch only `notes` — every other column must stay put.
	resp := ts.put(t, "/api/issues/"+itoa(id1), ts.adminCookie, map[string]any{
		"notes": "edited",
	})
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d, body=%s", resp.StatusCode, b)
	}
	var totalBudget sql.NullFloat64
	var estimateHours sql.NullFloat64
	var color sql.NullString
	_ = db.DB.QueryRow(
		`SELECT total_budget, estimate_hours, color FROM issues WHERE id=?`, id1,
	).Scan(&totalBudget, &estimateHours, &color)
	if !totalBudget.Valid || totalBudget.Float64 != 2500.0 {
		t.Errorf("total_budget=%v (want 2500) — absent key wrongly cleared", totalBudget)
	}
	if !estimateHours.Valid || estimateHours.Float64 != 7.5 {
		t.Errorf("estimate_hours=%v (want 7.5) — absent key wrongly cleared", estimateHours)
	}
	if !color.Valid || color.String != "#112233" {
		t.Errorf("color=%v (want #112233) — absent key wrongly cleared", color)
	}
}

// TestBatchUpdate_ExplicitNullClearsEstimates — PAI-315 batch surface
// covers estimate_hours and estimate_lp; the remaining nullable
// numeric columns aren't in the batch body shape today.
func TestBatchUpdate_ExplicitNullClearsEstimates(t *testing.T) {
	ts := newTestServer(t)
	projID := seedBatchProject(t, "PAI", "PAI")
	r, _ := db.DB.Exec(
		`INSERT INTO issues(project_id,issue_number,type,title,status,estimate_hours,estimate_lp)
		 VALUES(?,?,?,?,?,?,?)`,
		projID, 1, "ticket", "T", "backlog", 12.0, 8.0)
	id1, _ := r.LastInsertId()

	body := []map[string]any{
		{"ref": itoa(id1), "fields": map[string]any{
			"estimate_hours": nil,
			"estimate_lp":    nil,
		}},
	}
	resp := ts.patch(t, "/api/issues", ts.adminCookie, body)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d, body=%s", resp.StatusCode, b)
	}
	var eh, el any
	_ = db.DB.QueryRow(`SELECT estimate_hours, estimate_lp FROM issues WHERE id=?`, id1).Scan(&eh, &el)
	if eh != nil {
		t.Errorf("estimate_hours = %v (want nil)", eh)
	}
	if el != nil {
		t.Errorf("estimate_lp = %v (want nil)", el)
	}
}

// TestSinglePut_ExplicitNullClearsAssigneeAndParent — same fix applied
// to PUT /api/issues/{id} (the issue drawer's update path), so a single
// edit can clear assignee/parent the same way bulk now can.
func TestSinglePut_ExplicitNullClearsAssigneeAndParent(t *testing.T) {
	ts := newTestServer(t)
	projID := seedBatchProject(t, "PAI", "PAI")
	var adminID int64
	_ = db.DB.QueryRow(`SELECT id FROM users WHERE username='admin'`).Scan(&adminID)
	epicRes, _ := db.DB.Exec(
		`INSERT INTO issues(project_id,issue_number,type,title,status) VALUES(?,?,?,?,?)`,
		projID, 1, "epic", "Epic", "backlog")
	epicID, _ := epicRes.LastInsertId()
	// PAI-584 P6: parent_id column dropped — seed the parent edge.
	r, _ := db.DB.Exec(
		`INSERT INTO issues(project_id,issue_number,type,title,status,assignee_id) VALUES(?,?,?,?,?,?)`,
		projID, 2, "ticket", "T", "backlog", adminID)
	id1, _ := r.LastInsertId()
	if _, err := db.DB.Exec(`INSERT OR IGNORE INTO issue_relations(source_id,target_id,type) VALUES(?,?,'parent')`, epicID, id1); err != nil {
		t.Fatalf("seed parent edge: %v", err)
	}

	resp := ts.put(t, "/api/issues/"+itoa(id1), ts.adminCookie, map[string]any{
		"assignee_id": nil,
		"parent_id":   nil,
	})
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d, body=%s", resp.StatusCode, b)
	}
	var assignee any
	_ = db.DB.QueryRow(`SELECT assignee_id FROM issues WHERE id=?`, id1).Scan(&assignee)
	if assignee != nil {
		t.Errorf("assignee_id = %v (want nil)", assignee)
	}
	if got := parentEdgeSources(t, id1); len(got) != 0 {
		t.Errorf("parent edge = %v (want none after clear)", got)
	}
}

// TestBatchUpdate_UndoByRequestID_RevertsAllRows — PAI-316. The bulk
// PATCH writes one mutation_log row per subject sharing one request_id;
// a single POST /api/undo/request/{requestID} must revert ALL of them.
// Pre-fix the loader was LIMIT 1, so only the most-recent row reverted
// and the rest stayed in the post-bulk state. Round-trips with redo.
func TestBatchUpdate_UndoByRequestID_RevertsAllRows(t *testing.T) {
	ts := newTestServer(t)
	projID := seedBatchProject(t, "PAI", "PAI")
	ids := make([]int64, 3)
	for i := 0; i < 3; i++ {
		r, _ := db.DB.Exec(
			`INSERT INTO issues(project_id,issue_number,type,title,status,priority) VALUES(?,?,?,?,?,?)`,
			projID, i+1, "ticket", fmt.Sprintf("T%d", i+1), "backlog", "medium")
		ids[i], _ = r.LastInsertId()
	}

	// Bulk-update all 3 issues' status backlog → in-progress.
	body := []map[string]any{
		{"ref": itoa(ids[0]), "fields": map[string]any{"status": "in-progress"}},
		{"ref": itoa(ids[1]), "fields": map[string]any{"status": "in-progress"}},
		{"ref": itoa(ids[2]), "fields": map[string]any{"status": "in-progress"}},
	}
	resp := ts.patch(t, "/api/issues", ts.adminCookie, body)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("bulk PATCH status=%d, body=%s", resp.StatusCode, b)
	}
	requestID := resp.Header.Get("X-PAIMOS-Request-Id")
	if requestID == "" {
		t.Fatal("response missing X-PAIMOS-Request-Id header")
	}

	// Sanity: all 3 are in-progress before undo.
	statusOf := func(id int64) string {
		var s string
		_ = db.DB.QueryRow(`SELECT status FROM issues WHERE id=?`, id).Scan(&s)
		return s
	}
	for _, id := range ids {
		if statusOf(id) != "in-progress" {
			t.Fatalf("pre-undo: id=%d status=%q, want in-progress", id, statusOf(id))
		}
	}

	// One undo call must revert ALL 3.
	undoResp := ts.post(t, "/api/undo/request/"+requestID, ts.adminCookie, map[string]any{})
	if undoResp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(undoResp.Body)
		t.Fatalf("undo status=%d, body=%s", undoResp.StatusCode, b)
	}
	var undoBody map[string]any
	_ = json.NewDecoder(undoResp.Body).Decode(&undoBody)
	if got, _ := undoBody["batch_size"].(float64); int(got) != 3 {
		t.Errorf("undo response batch_size=%v, want 3", undoBody["batch_size"])
	}
	for _, id := range ids {
		if statusOf(id) != "backlog" {
			t.Errorf("post-undo: id=%d status=%q, want backlog (single undo failed to revert all batch rows)", id, statusOf(id))
		}
	}

	// Redo round-trip — a single redo must re-apply the bulk to all 3.
	redoResp := ts.post(t, "/api/redo/request/"+requestID, ts.adminCookie, map[string]any{})
	if redoResp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(redoResp.Body)
		t.Fatalf("redo status=%d, body=%s", redoResp.StatusCode, b)
	}
	for _, id := range ids {
		if statusOf(id) != "in-progress" {
			t.Errorf("post-redo: id=%d status=%q, want in-progress", id, statusOf(id))
		}
	}
}

// TestListIssues_IdsOnly — PAI-318. The IssueList "Select all N matching"
// chip needs the server-filtered id set without paying for full issue
// payloads. /api/issues?ids_only=1 returns just {ids, total, truncated, cap}.
func TestListIssues_IdsOnly(t *testing.T) {
	ts := newTestServer(t)
	projID := seedBatchProject(t, "PAI", "PAI")
	want := make([]int64, 0, 5)
	for i := 1; i <= 5; i++ {
		r, _ := db.DB.Exec(
			`INSERT INTO issues(project_id,issue_number,type,title,status) VALUES(?,?,?,?,?)`,
			projID, i, "ticket", fmt.Sprintf("T%d", i), "backlog")
		id, _ := r.LastInsertId()
		want = append(want, id)
	}
	resp := ts.get(t, "/api/issues?ids_only=1&project_ids="+itoa(projID), ts.adminCookie)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d, body=%s", resp.StatusCode, b)
	}
	var body struct {
		IDs       []int64 `json:"ids"`
		Total     int     `json:"total"`
		Truncated bool    `json:"truncated"`
		Cap       int     `json:"cap"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body.Total != len(want) {
		t.Errorf("total=%d, want %d", body.Total, len(want))
	}
	if body.Cap != handlers.MaxBatchSize*50 {
		t.Errorf("cap=%d, want %d", body.Cap, handlers.MaxBatchSize*50)
	}
	if body.Truncated {
		t.Errorf("truncated=true unexpectedly (only %d issues seeded)", len(want))
	}
	if len(body.IDs) != len(want) {
		t.Errorf("len(ids)=%d, want %d", len(body.IDs), len(want))
	}
	gotSet := map[int64]bool{}
	for _, id := range body.IDs {
		gotSet[id] = true
	}
	for _, id := range want {
		if !gotSet[id] {
			t.Errorf("missing id %d in ids_only response", id)
		}
	}
}

// TestListIssues_IdsOnly_RespectsFilters — same filter pipeline as the
// hydrated path, so bulk selection sees exactly what the user sees.
func TestListIssues_IdsOnly_RespectsFilters(t *testing.T) {
	ts := newTestServer(t)
	projID := seedBatchProject(t, "PAI", "PAI")
	// 3 backlog + 2 in-progress tickets.
	mkIssue := func(num int, status string) {
		_, _ = db.DB.Exec(
			`INSERT INTO issues(project_id,issue_number,type,title,status) VALUES(?,?,?,?,?)`,
			projID, num, "ticket", "T", status)
	}
	mkIssue(1, "backlog")
	mkIssue(2, "backlog")
	mkIssue(3, "backlog")
	mkIssue(4, "in-progress")
	mkIssue(5, "in-progress")

	resp := ts.get(t, "/api/issues?ids_only=1&status=backlog", ts.adminCookie)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	var body struct {
		IDs   []int64 `json:"ids"`
		Total int     `json:"total"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body.Total != 3 {
		t.Errorf("filtered total=%d, want 3 backlog", body.Total)
	}
}

// TestBatchUpdate_NonAdmin_403 — bulk modal is gated to admins in the UI;
// the endpoint must enforce the same on the wire.
func TestBatchUpdate_NonAdmin_403(t *testing.T) {
	ts := newTestServer(t)
	projID := seedBatchProject(t, "PAI", "PAI")
	r1, _ := db.DB.Exec(
		`INSERT INTO issues(project_id,issue_number,type,title,status) VALUES(?,?,?,?,?)`,
		projID, 1, "ticket", "T", "backlog")
	id1, _ := r1.LastInsertId()

	body := []map[string]any{{"ref": itoa(id1), "fields": map[string]any{"status": "in-progress"}}}
	resp := ts.patch(t, "/api/issues", ts.memberCookie, body)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("member PATCH: status=%d, want 403", resp.StatusCode)
	}
}

// toStr coerces SQLite scan-result types (string, []byte, int64, etc.)
// into a string for equality comparison in field-shape tests.
func toStr(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []byte:
		return string(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", x)
	}
}

func TestLookupByKeys_OrderPreservedAndMissingMarked(t *testing.T) {
	ts := newTestServer(t)
	projID := seedBatchProject(t, "PAI", "PAI")
	for i := 1; i <= 3; i++ {
		db.DB.Exec(
			`INSERT INTO issues(project_id, issue_number, type, title, status) VALUES(?,?,?,?,?)`,
			projID, i, "ticket", "T", "backlog")
	}

	resp := ts.get(t, "/api/issues?keys=PAI-3,PAI-999,PAI-1,PAI-2", ts.adminCookie)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d, body=%s", resp.StatusCode, b)
	}
	var out struct {
		Issues []map[string]any `json:"issues"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if len(out.Issues) != 4 {
		t.Fatalf("len=%d, want 4", len(out.Issues))
	}
	// Order preserved.
	want := []string{"PAI-3", "PAI-999", "PAI-1", "PAI-2"}
	for i, w := range want {
		item := out.Issues[i]
		if errStr, ok := item["error"].(string); ok {
			// Error entry → ref must match.
			if item["ref"] != w {
				t.Errorf("item %d: ref=%v, want %s (err=%q)", i, item["ref"], w, errStr)
			}
			continue
		}
		if item["issue_key"] != w {
			t.Errorf("item %d: issue_key=%v, want %s", i, item["issue_key"], w)
		}
	}
	// The PAI-999 slot specifically must carry an error.
	if _, ok := out.Issues[1]["error"]; !ok {
		t.Error("PAI-999 slot missing error marker")
	}
}

// itoa is a shorter spelling of strconv.FormatInt base 10, used
// liberally to compose URL paths and JSON refs from int64 ids.
func itoa(n int64) string { return strconv.FormatInt(n, 10) }
