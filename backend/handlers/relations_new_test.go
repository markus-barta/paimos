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

// TestNewRelationTypes covers PAI-89: follows_from / blocks / related
// accepted by the POST handler AND surfaced with direction markers
// when listed from either side.
func TestNewRelationTypes(t *testing.T) {
	ts := newTestServer(t)
	projID := seedBatchProject(t, "PAI", "PAI")

	// Seed two issues: PAI-1 (the "new" ticket) and PAI-2 (the predecessor).
	r1, _ := db.DB.Exec(
		`INSERT INTO issues(project_id, issue_number, type, title, status) VALUES(?,?,?,?,?)`,
		projID, 1, "ticket", "Spin-off", "backlog")
	id1, _ := r1.LastInsertId()
	r2, _ := db.DB.Exec(
		`INSERT INTO issues(project_id, issue_number, type, title, status) VALUES(?,?,?,?,?)`,
		projID, 2, "ticket", "Predecessor", "backlog")
	id2, _ := r2.LastInsertId()

	for _, relType := range []string{"follows_from", "blocks", "related"} {
		t.Run(relType, func(t *testing.T) {
			// Clear any relations from the prior subtest.
			db.DB.Exec(`DELETE FROM issue_relations WHERE source_id IN (?,?) OR target_id IN (?,?)`, id1, id2, id1, id2)

			// id1 (source) --relType--> id2 (target). Test router uses
			// numeric ids since it doesn't wire the RequireIssueAccess
			// middleware that handles key→id resolution in prod.
			resp := ts.post(t,
				"/api/issues/"+itoa(id1)+"/relations",
				ts.adminCookie,
				map[string]any{"target_id": id2, "type": relType},
			)
			if resp.StatusCode != http.StatusCreated {
				b, _ := io.ReadAll(resp.Body)
				t.Fatalf("POST: status=%d, body=%s", resp.StatusCode, b)
			}

			// List from the source side — direction should be "outgoing".
			out := listRelations(t, ts, itoa(id1))
			found := false
			for _, r := range out {
				if r["type"] == relType && int64(r["source_id"].(float64)) == id1 {
					found = true
					if r["direction"] != "outgoing" {
						t.Errorf("source side: direction=%v, want outgoing", r["direction"])
					}
				}
			}
			if !found {
				t.Errorf("source side missing %s relation to id2", relType)
			}

			// List from the target side — direction should be "incoming".
			out = listRelations(t, ts, itoa(id2))
			found = false
			for _, r := range out {
				if r["type"] == relType && int64(r["source_id"].(float64)) == id1 {
					found = true
					if r["direction"] != "incoming" {
						t.Errorf("target side: direction=%v, want incoming", r["direction"])
					}
				}
			}
			if !found {
				t.Errorf("target side missing %s relation from id1", relType)
			}
		})
	}
}

// TestInvalidRelationType ensures the allowlist check works.
func TestInvalidRelationType(t *testing.T) {
	ts := newTestServer(t)
	projID := seedBatchProject(t, "PAI", "PAI")
	r1, _ := db.DB.Exec(`INSERT INTO issues(project_id, issue_number, type, title, status) VALUES(?,?,?,?,?)`, projID, 1, "ticket", "A", "backlog")
	id1, _ := r1.LastInsertId()
	r2, _ := db.DB.Exec(`INSERT INTO issues(project_id, issue_number, type, title, status) VALUES(?,?,?,?,?)`, projID, 2, "ticket", "B", "backlog")
	id2, _ := r2.LastInsertId()
	_ = id1

	resp := ts.post(t, "/api/issues/"+itoa(id1)+"/relations", ts.adminCookie, map[string]any{
		"target_id": id2, "type": "obsoletes",
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("bogus type: status=%d, want 400", resp.StatusCode)
	}
}

func listRelations(t *testing.T, ts *testServer, ref string) []map[string]any {
	t.Helper()
	resp := ts.get(t, "/api/issues/"+ref+"/relations", ts.adminCookie)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET /issues/%s/relations: status=%d, body=%s", ref, resp.StatusCode, b)
	}
	var out []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return out
}
