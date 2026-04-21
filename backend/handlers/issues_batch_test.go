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
	"strconv"
	"strings"
	"testing"

	"github.com/markus-barta/paimos/backend/db"
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
