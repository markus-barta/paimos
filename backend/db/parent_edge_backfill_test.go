package db

import "testing"

// TestParentEdgeBackfill validates migration 118's one-time backfill: every
// issues.parent_id AND every legacy groups relation becomes a 'parent' edge
// (source=parent, target=child); the overlap collapses to a single row; and
// self-referential parents are skipped so the new table's PK/FK never reject a
// degenerate row. (Dangling parent_id can't occur — it's an FK with
// ON DELETE SET NULL — so there's nothing to seed for that case.)
//
// The groups-only case is the original PAI-584 bug — a ticket linked to its
// epic via the relation API alone (no parent_id), invisible to every
// parent_id-based read until this backfill captures it as a parent edge.
//
// M118 runs its (empty) backfill during Open, so this test re-creates the
// legacy state afterwards and re-runs the two canonical backfill statements
// to lock their semantics on real data.
func TestParentEdgeBackfill(t *testing.T) {
	db := openTestDB(t)

	pid := lastID(t, mustExec(t, db, "INSERT INTO projects(name,key) VALUES('Proj','PRJ')"))

	ins := func(num int, typ string, parent any) int64 {
		t.Helper()
		return lastID(t, mustExec(t,
			db,
			"INSERT INTO issues(project_id,issue_number,title,type,status,priority,parent_id) VALUES(?,?,?,?,'backlog','medium',?)",
			pid, num, typ, typ, parent))
	}

	epic := ins(1, "epic", nil)
	epic2 := ins(2, "epic", nil)
	t1 := ins(3, "ticket", epic) // (a) dual-stored: parent_id set
	t2 := ins(4, "ticket", nil)  // (b) groups-only orphan: no parent_id
	task := ins(5, "task", t1)   // (a) task→ticket level
	t4 := ins(6, "ticket", nil)  // self-parent → must skip
	t5 := ins(7, "ticket", epic) // (a) parent_id=epic AND a divergent groups→epic2
	t6 := ins(8, "ticket", nil)  // (b) two divergent groups sources, no parent_id
	cu := ins(9, "cost_unit", nil)
	t7 := ins(10, "ticket", nil) // grouped under a cost_unit via groups — NOT WBS
	mustExec(t, db, "UPDATE issues SET parent_id=id WHERE id=?", t4)

	// Legacy groups relations (polymorphic container membership):
	//   epic→t1  (agrees with parent_id — collapses)
	//   epic→t2  (the orphan — becomes the parent)
	//   epic2→t5 (DISAGREES with t5.parent_id=epic — parent_id must win)
	//   epic→t6, epic2→t6 (two epic sources, no parent_id — collapse to MIN)
	//   cu→t7    (cost_unit container — must NOT fold into the parent edge)
	for _, gr := range [][2]int64{{epic, t1}, {epic, t2}, {epic2, t5}, {epic, t6}, {epic2, t6}, {cu, t7}} {
		mustExec(t, db,
			"INSERT OR IGNORE INTO issue_relations(source_id,target_id,type) VALUES(?,?,'groups')",
			gr[0], gr[1])
	}

	// The parent-sync triggers (active post-migration) already mirrored the
	// parent_id-seeded rows above into edges. Clear them so this test
	// exercises the BACKFILL SQL in isolation — i.e. the pre-existing-rows
	// state the migration actually faces (rows present, no edges yet). The
	// groups relations stay (they are backfill (b)'s source).
	mustExec(t, db, "DELETE FROM issue_relations WHERE type='parent'")

	// ── Canonical M118 backfill — mirrors the two INSERT OR IGNORE steps ──
	mustExec(t, db, `
		INSERT OR IGNORE INTO issue_relations(source_id, target_id, type)
		SELECT i.parent_id, i.id, 'parent'
		FROM issues i
		WHERE i.parent_id IS NOT NULL
		  AND i.parent_id <> i.id
		  AND EXISTS (SELECT 1 FROM issues p WHERE p.id = i.parent_id)`)
	mustExec(t, db, `
		INSERT OR IGNORE INTO issue_relations(source_id, target_id, type)
		SELECT MIN(g.source_id), g.target_id, 'parent'
		FROM issue_relations g
		JOIN issues src ON src.id = g.source_id AND src.type = 'epic'
		WHERE g.type='groups'
		  AND NOT EXISTS (
		      SELECT 1 FROM issue_relations existing
		      WHERE existing.target_id = g.target_id
		        AND existing.type = 'parent')
		GROUP BY g.target_id`)

	parentOf := func(child int64) []int64 {
		t.Helper()
		rows, err := db.Query(
			"SELECT source_id FROM issue_relations WHERE target_id=? AND type='parent' ORDER BY source_id", child)
		if err != nil {
			t.Fatalf("query parent of %d: %v", child, err)
		}
		defer rows.Close()
		var out []int64
		for rows.Next() {
			var s int64
			if err := rows.Scan(&s); err != nil {
				t.Fatalf("scan: %v", err)
			}
			out = append(out, s)
		}
		return out
	}
	assertOne := func(child, want int64, label string) {
		t.Helper()
		if got := parentOf(child); len(got) != 1 || got[0] != want {
			t.Errorf("%s: parent edges for %d = %v, want exactly [%d]", label, child, got, want)
		}
	}
	assertNone := func(child int64, label string) {
		t.Helper()
		if got := parentOf(child); len(got) != 0 {
			t.Errorf("%s: parent edges for %d = %v, want none", label, child, got)
		}
	}

	assertOne(t1, epic, "parent_id+groups overlap collapses to one")
	assertOne(t2, epic, "groups-only orphan gets a parent edge (PAI-584 bug)")
	assertOne(task, t1, "task→ticket level backfilled")
	assertNone(t4, "self-referential parent skipped")
	assertOne(t5, epic, "parent_id wins over divergent groups source")
	assertOne(t6, epic, "two divergent groups sources collapse to MIN")
	assertNone(t7, "cost_unit groups membership is NOT folded into parent (P7-P9 axis)")
}
