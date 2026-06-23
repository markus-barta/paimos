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
	"testing"

	"github.com/markus-barta/paimos/backend/db"
	"github.com/markus-barta/paimos/backend/handlers"
)

// TestEnsureCostUnitReleaseEdges covers the PAI-599 boot backfill: a container
// issue is created for each label that lacks one, edges link tickets to their
// container (existing or new), and the run is idempotent.
func TestEnsureCostUnitReleaseEdges(t *testing.T) {
	ts := newTestServer(t)
	_ = ts
	projID := seedBatchProject(t, "PAI", "PAI")

	mk := func(num int, typ, title, costUnit, release string) int64 {
		t.Helper()
		res, err := db.DB.Exec(
			`INSERT INTO issues(project_id,issue_number,type,title,status,priority,cost_unit,release)
			 VALUES(?,?,?,?,'backlog','medium',?,?)`,
			projID, num, typ, title, costUnit, release)
		if err != nil {
			t.Fatalf("seed issue %d: %v", num, err)
		}
		id, _ := res.LastInsertId()
		return id
	}
	containerID := func(typ, title string) int64 {
		t.Helper()
		var id int64
		_ = db.DB.QueryRow(`SELECT id FROM issues WHERE project_id=? AND type=? AND title=? AND deleted_at IS NULL`,
			projID, typ, title).Scan(&id)
		return id
	}
	edgeExists := func(src, tgt int64, typ string) bool {
		t.Helper()
		var n int
		_ = db.DB.QueryRow(`SELECT COUNT(*) FROM issue_relations WHERE source_id=? AND target_id=? AND type=?`,
			src, tgt, typ).Scan(&n)
		return n == 1
	}

	// Pre-existing cost_unit container "ENG-A".
	engA := mk(1, "cost_unit", "ENG-A", "", "")
	t1 := mk(2, "ticket", "T1", "ENG-A", "") // matches existing container
	t2 := mk(3, "ticket", "T2", "ENG-B", "") // no container → must be created
	t3 := mk(4, "ticket", "T3", "", "v1.0")  // release, no container → created
	t4 := mk(5, "ticket", "T4", "ENG-B", "v1.0")

	handlers.EnsureCostUnitReleaseEdges()

	// Containers created for the orphan labels.
	engB := containerID("cost_unit", "ENG-B")
	v10 := containerID("release", "v1.0")
	if engB == 0 {
		t.Fatal("cost_unit container 'ENG-B' was not created")
	}
	if v10 == 0 {
		t.Fatal("release container 'v1.0' was not created")
	}

	// Edges link each ticket to its container.
	if !edgeExists(engA, t1, "cost_unit") {
		t.Errorf("missing cost_unit edge ENG-A→T1 (existing container)")
	}
	if !edgeExists(engB, t2, "cost_unit") {
		t.Errorf("missing cost_unit edge ENG-B→T2 (new container)")
	}
	if !edgeExists(v10, t3, "release") {
		t.Errorf("missing release edge v1.0→T3")
	}
	if !edgeExists(engB, t4, "cost_unit") || !edgeExists(v10, t4, "release") {
		t.Errorf("T4 should have both cost_unit and release edges")
	}

	// Idempotent: a second run creates no duplicate containers.
	handlers.EnsureCostUnitReleaseEdges()
	var engBcount int
	_ = db.DB.QueryRow(`SELECT COUNT(*) FROM issues WHERE project_id=? AND type='cost_unit' AND title='ENG-B'`, projID).Scan(&engBcount)
	if engBcount != 1 {
		t.Errorf("idempotency broken: %d 'ENG-B' containers after second run, want 1", engBcount)
	}
}
