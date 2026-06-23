package db

import "testing"

// TestParentEdgeBackfill validates migration 118's groups→parent backfill — the
// part that fixes the original PAI-584 bug: an epic→ticket link created via the
// relation API alone (type=groups, no parent_id) was invisible to every read,
// until the backfill captured it as a `parent` edge. Only EPIC-sourced groups
// fold into `parent` (cost_unit/release are orthogonal axes, P7–P9), and
// multiple divergent epic sources collapse to one (MIN).
//
// (The other half of M118 — backfilling from issues.parent_id — is no longer
// exercisable here: P6/M120 dropped the parent_id column. That transform ran
// historically on the live data before the column was removed.)
func TestParentEdgeBackfill(t *testing.T) {
	db := openTestDB(t)

	pid := lastID(t, mustExec(t, db, "INSERT INTO projects(name,key) VALUES('Proj','PRJ')"))

	ins := func(num int, typ string) int64 {
		t.Helper()
		return lastID(t, mustExec(t, db,
			"INSERT INTO issues(project_id,issue_number,title,type,status,priority) VALUES(?,?,?,?,'backlog','medium')",
			pid, num, typ, typ))
	}

	epic := ins(1, "epic")
	epic2 := ins(2, "epic")
	t2 := ins(4, "ticket") // groups-only orphan (epic→t2)
	t6 := ins(8, "ticket") // two divergent epic groups sources → MIN collapse
	cu := ins(9, "cost_unit")
	t7 := ins(10, "ticket") // cost_unit groups member — must NOT fold into parent

	// Legacy groups relations (polymorphic container membership):
	//   epic→t2           (the orphan — becomes the parent)
	//   epic→t6, epic2→t6 (two epic sources, no parent — collapse to MIN)
	//   cu→t7             (cost_unit container — must NOT fold into parent)
	for _, gr := range [][2]int64{{epic, t2}, {epic, t6}, {epic2, t6}, {cu, t7}} {
		mustExec(t, db,
			"INSERT OR IGNORE INTO issue_relations(source_id,target_id,type) VALUES(?,?,'groups')",
			gr[0], gr[1])
	}

	// ── Canonical M118 backfill (b): EPIC-sourced groups → parent edge ──
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

	assertOne(t2, epic, "groups-only orphan gets a parent edge (PAI-584 bug)")
	assertOne(t6, epic, "two divergent epic groups sources collapse to MIN")
	assertNone(t7, "cost_unit groups membership is NOT folded into parent (P7-P9 axis)")
}
